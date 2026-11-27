import logging
import os
import re
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from typing import Callable, Optional, Sequence

from slack_bolt import App
from slack_bolt.adapter.socket_mode import SocketModeHandler
from slack_bolt.authorization import AuthorizeResult
from slack_bolt.middleware import Middleware
from slack_sdk import WebClient
from slack_sdk.socket_mode.websocket_client import SocketModeClient

logging.basicConfig(
    level=os.environ.get("LOG_LEVEL", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
logger = logging.getLogger("echo-bot")

MENTION_RE = re.compile(r"<@[A-Z0-9]+(?:\|[^>]*)?>\s*")
DEFAULT_TOKEN_RETRY_DELAYS: tuple[float, ...] = (0.05, 0.1, 0.2, 0.4)


def load_token(
    file_path: Optional[str],
    fallback: str,
    label: str,
    *,
    retry_delays: Sequence[float] = DEFAULT_TOKEN_RETRY_DELAYS,
) -> str:
    """トークンを読み込む。file_path が設定されていれば毎回ファイルから読み、fallback より優先する。

    起動直後やファイル更新の瞬間で一時的に不在・空・読取失敗となった場合は短く待って読み直す。
    """
    path = (file_path or "").strip()
    if path:
        last_err: Optional[Exception] = None
        for attempt in range(len(retry_delays) + 1):
            try:
                token = Path(path).read_text(encoding="utf-8").strip()
                if token:
                    return token
                last_err = ValueError(f"{label} token file {path!r} is empty")
            except OSError as exc:
                last_err = exc
            if attempt < len(retry_delays):
                time.sleep(retry_delays[attempt])
        if last_err is not None:
            raise last_err
        raise ValueError(f"{label} token file {path!r} is empty")
    token = (fallback or "").strip()
    if not token:
        raise ValueError(f"{label} token is not configured")
    return token


def make_token_loader(
    file_path: Optional[str],
    fallback: str,
    label: str,
    *,
    retry_delays: Sequence[float] = DEFAULT_TOKEN_RETRY_DELAYS,
) -> Callable[[], str]:
    """呼び出しのたびに最新トークンを返すローダー関数を生成する。"""
    return lambda: load_token(file_path, fallback, label, retry_delays=retry_delays)


class TokenFileWebClient(WebClient):
    """API 呼び出しごとに bot トークンローダーから最新トークンを読み直す WebClient。"""

    def __init__(
        self,
        *,
        bot_token_loader: Callable[[], str],
        base_url: str,
        **kwargs,
    ):
        self._bot_token_loader = bot_token_loader
        super().__init__(token=None, base_url=base_url, **kwargs)

    def api_call(self, api_method: str, **kwargs):
        params = kwargs.get("params")
        json_body = kwargs.get("json")
        has_explicit_token = (
            isinstance(params, dict) and "token" in params and params["token"] is not None
        ) or (
            isinstance(json_body, dict) and "token" in json_body and json_body["token"] is not None
        )
        if not has_explicit_token:
            self.token = self._bot_token_loader()
        return super().api_call(api_method, **kwargs)


class TokenFileSocketModeClient(SocketModeClient):
    """接続および再接続のたびに app_token を読み直して apps.connections.open を呼ぶ SocketModeClient。"""

    def __init__(
        self,
        *args,
        app_token_loader: Callable[[], str],
        **kwargs,
    ):
        self._app_token_loader = app_token_loader
        super().__init__(*args, **kwargs)

    def issue_new_wss_url(self) -> str:
        self.app_token = self._app_token_loader()
        return super().issue_new_wss_url()


class TokenFileSocketModeHandler(SocketModeHandler):
    """app_token_loader を使って接続・再接続時にトークンを更新する SocketModeHandler。"""

    def __init__(
        self,
        app: App,
        app_token_loader: Callable[[], str],
        logger: Optional[logging.Logger] = None,
        web_client: Optional[WebClient] = None,
        proxy: Optional[str] = None,
        proxy_headers: Optional[dict] = None,
        auto_reconnect_enabled: bool = True,
        trace_enabled: bool = False,
        all_message_trace_enabled: bool = False,
        ping_pong_trace_enabled: bool = False,
        ping_interval: float = 10,
        receive_buffer_size: int = 1024,
        concurrency: int = 10,
    ):
        self.app = app
        self._app_token_loader = app_token_loader
        self.app_token = app_token_loader()
        self.client = TokenFileSocketModeClient(
            app_token=self.app_token,
            app_token_loader=app_token_loader,
            logger=logger if logger is not None else app.logger,
            web_client=web_client if web_client is not None else app.client,
            auto_reconnect_enabled=auto_reconnect_enabled,
            trace_enabled=trace_enabled,
            ping_interval=ping_interval,
            concurrency=concurrency,
        )
        self.client.socket_mode_request_listeners.append(self.handle)  # type: ignore[arg-type]


def format_echo_text(raw_text: str, bot_user_id: Optional[str] = None) -> str:
    """メンション表記を取り除いたオウム返し用テキストを整形する。"""
    text = (raw_text or "").strip()
    if bot_user_id:
        pattern = re.compile(rf"<@{re.escape(bot_user_id)}(?:\|[^>]*)?>\s*")
        text = pattern.sub("", text).strip()
    else:
        text = MENTION_RE.sub("", text).strip()
    if not text:
        return "(空のメッセージ)"
    return text


def _contains_user_mention(raw_text: str, user_id: str) -> bool:
    return bool(re.search(rf"<@{re.escape(user_id)}(?:\|[^>]*)?>", raw_text))


class _ReloadingClientMiddleware(Middleware):
    """Bolt がリクエスト毎に生成する素の WebClient を TokenFileWebClient に差し替える。"""

    def __init__(self, bot_token_loader: Callable[[], str]):
        self._bot_token_loader = bot_token_loader

    def process(self, *, req, resp, next):
        base = req.context.client
        req.context["client"] = TokenFileWebClient(
            bot_token_loader=self._bot_token_loader,
            base_url=base.base_url,
            timeout=base.timeout,
            ssl=base.ssl,
            proxy=base.proxy,
            headers=base.headers,
            team_id=req.context.team_id,
            logger=base.logger,
            retry_handlers=base.retry_handlers.copy() if base.retry_handlers is not None else None,
        )
        req.context.pop("say", None)
        return next()


def create_app(
    bot_token: Optional[str] = None,
    api_base_url: str = "",
    client: Optional[WebClient] = None,
    bot_user_id: Optional[str] = None,
    bot_id: Optional[str] = None,
    process_before_response: bool = False,
    bot_token_loader: Optional[Callable[[], str]] = None,
) -> App:
    """Slack Bolt アプリとオウム返しハンドラを構築する。"""
    loader = bot_token_loader or (lambda: (bot_token or "").strip())
    web_client = client or TokenFileWebClient(bot_token_loader=loader, base_url=api_base_url)

    state = {
        "bot_user_id": bot_user_id,
        "bot_id": bot_id,
        "team_id": None,
    }

    def resolve_bot_identity(token: str) -> tuple[Optional[str], Optional[str], Optional[str]]:
        if state["bot_user_id"] and state["bot_id"]:
            return state["bot_user_id"], state["bot_id"], state["team_id"]
        try:
            res = web_client.auth_test(token=token)
            state["bot_user_id"] = res.get("user_id") or state["bot_user_id"]
            state["bot_id"] = res.get("bot_id") or state["bot_id"]
            state["team_id"] = res.get("team_id") or state["team_id"]
        except Exception:
            pass
        return state["bot_user_id"], state["bot_id"], state["team_id"]

    def authorize(context, enterprise_id, team_id, user_id):
        token = loader()
        uid, bid, resolved_team_id = resolve_bot_identity(token)
        return AuthorizeResult(
            enterprise_id=enterprise_id,
            team_id=team_id or resolved_team_id,
            bot_user_id=uid,
            bot_id=bid,
            bot_token=token,
        )

    app = App(
        client=web_client,
        authorize=authorize,
        token_verification_enabled=False,
        process_before_response=process_before_response,
    )
    app.middleware(_ReloadingClientMiddleware(loader))

    def get_bot_user_id() -> Optional[str]:
        if state["bot_user_id"]:
            return state["bot_user_id"]
        uid, _, _ = resolve_bot_identity(loader())
        return uid

    @app.event("app_mention")
    def handle_app_mention(event, say):
        text = format_echo_text(event.get("text", ""), get_bot_user_id())
        thread_ts = event.get("thread_ts")
        if thread_ts:
            say(text=text, thread_ts=thread_ts)
        else:
            say(text=text)

    @app.event("message")
    def handle_message(event, say):
        if event.get("subtype") or event.get("bot_id"):
            return

        uid = get_bot_user_id()
        if uid and event.get("user") == uid:
            return

        channel_type = event.get("channel_type")
        thread_ts = event.get("thread_ts")
        raw_text = event.get("text", "")

        # チャンネル直下 (メンションなし・スレッド外) の発言は無視し、DM またはスレッド返信に反応する
        if channel_type != "im" and not thread_ts:
            return

        # チャンネルスレッド内で bot へのメンション (<@U...> および <@U...|label>) を含む発言は
        # app_mention 側で処理済みのため重複返信しない
        if channel_type != "im" and thread_ts and uid and _contains_user_mention(raw_text, uid):
            return

        text = format_echo_text(raw_text, uid)
        if thread_ts:
            say(text=text, thread_ts=thread_ts)
        else:
            say(text=text)

    return app


class _HealthHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"ok")
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format, *args):
        return


def start_health_server(port: int) -> HTTPServer:
    server = HTTPServer(("0.0.0.0", port), _HealthHandler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    return server


def main() -> None:
    bot_token_file = os.environ.get("SLACK_BOT_TOKEN_FILE", "").strip()
    app_token_file = os.environ.get("SLACK_APP_TOKEN_FILE", "").strip()
    bot_token = os.environ.get("SLACK_BOT_TOKEN", "").strip()
    app_token = os.environ.get("SLACK_APP_TOKEN", "").strip()
    api_base_url = os.environ.get("SLACK_API_BASE_URL", "").strip()
    health_port = int(os.environ.get("HEALTH_PORT", "8080"))

    if not api_base_url or (not bot_token_file and not bot_token) or (not app_token_file and not app_token):
        logger.error(
            "SLACK_API_BASE_URL and (SLACK_BOT_TOKEN_FILE or SLACK_BOT_TOKEN) and (SLACK_APP_TOKEN_FILE or SLACK_APP_TOKEN) must be set"
        )
        sys.exit(1)

    if not api_base_url.endswith("/"):
        api_base_url += "/"

    bot_token_loader = make_token_loader(bot_token_file, bot_token, "bot")
    app_token_loader = make_token_loader(app_token_file, app_token, "app")

    start_health_server(health_port)
    logger.info("health server started on port %d", health_port)

    # spiffe-helper が初回の JWT-SVID を書き出すまで待機しつつ、auth.test が通るまで待つ
    client: Optional[TokenFileWebClient] = None
    bot_user_id: Optional[str] = None
    bot_id: Optional[str] = None
    while bot_user_id is None:
        try:
            if client is None:
                client = TokenFileWebClient(bot_token_loader=bot_token_loader, base_url=api_base_url)
            auth = client.auth_test()
            bot_user_id = auth.get("user_id")
            bot_id = auth.get("bot_id")
            logger.info("authenticated via auth.test as user_id=%s bot_id=%s", bot_user_id, bot_id)
        except Exception as exc:
            logger.warning("waiting for token / backend auth.test (%s): %s", api_base_url, exc)
            time.sleep(3)

    app = create_app(
        api_base_url=api_base_url,
        client=client,
        bot_user_id=bot_user_id,
        bot_id=bot_id,
        bot_token_loader=bot_token_loader,
    )
    handler = TokenFileSocketModeHandler(app, app_token_loader=app_token_loader)
    logger.info("starting TokenFileSocketModeHandler against %s", api_base_url)
    handler.start()


if __name__ == "__main__":
    main()

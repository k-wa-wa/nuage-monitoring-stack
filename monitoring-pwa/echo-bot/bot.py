import logging
import os
import re
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Optional

from slack_bolt import App
from slack_bolt.adapter.socket_mode import SocketModeHandler
from slack_sdk import WebClient

logging.basicConfig(
    level=os.environ.get("LOG_LEVEL", "INFO"),
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
logger = logging.getLogger("echo-bot")

MENTION_RE = re.compile(r"<@[A-Z0-9]+>\s*")


def format_echo_text(raw_text: str, bot_user_id: Optional[str] = None) -> str:
    """メンション表記を取り除いたオウム返し用テキストを整形する。"""
    text = (raw_text or "").strip()
    if bot_user_id:
        text = text.replace(f"<@{bot_user_id}>", "").strip()
    else:
        text = MENTION_RE.sub("", text).strip()
    if not text:
        return "(空のメッセージ)"
    return text


def create_app(
    bot_token: str,
    api_base_url: str,
    client: Optional[WebClient] = None,
    bot_user_id: Optional[str] = None,
) -> App:
    """Slack Bolt アプリとオウム返しハンドラを構築する。"""
    web_client = client or WebClient(token=bot_token, base_url=api_base_url)
    app = App(client=web_client, token_verification_enabled=False)

    state = {"bot_user_id": bot_user_id}

    def get_bot_user_id() -> Optional[str]:
        if state["bot_user_id"]:
            return state["bot_user_id"]
        try:
            res = web_client.auth_test()
            state["bot_user_id"] = res.get("user_id")
        except Exception:
            pass
        return state["bot_user_id"]

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

        # チャンネルスレッド内で bot へのメンションを含む発言は app_mention 側で処理済みのため重複返信しない
        if channel_type != "im" and thread_ts and uid and f"<@{uid}>" in raw_text:
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
    bot_token = os.environ.get("SLACK_BOT_TOKEN", "").strip()
    app_token = os.environ.get("SLACK_APP_TOKEN", "").strip()
    api_base_url = os.environ.get("SLACK_API_BASE_URL", "").strip()
    health_port = int(os.environ.get("HEALTH_PORT", "8080"))

    if not bot_token or not app_token or not api_base_url:
        logger.error("SLACK_BOT_TOKEN, SLACK_APP_TOKEN, and SLACK_API_BASE_URL must be set")
        sys.exit(1)

    if not api_base_url.endswith("/"):
        api_base_url += "/"

    start_health_server(health_port)
    logger.info("health server started on port %d", health_port)

    client = WebClient(token=bot_token, base_url=api_base_url)

    bot_user_id: Optional[str] = None
    while bot_user_id is None:
        try:
            auth = client.auth_test()
            bot_user_id = auth.get("user_id")
            logger.info("authenticated via auth.test as user_id=%s bot_id=%s", bot_user_id, auth.get("bot_id"))
        except Exception as exc:
            logger.warning("waiting for backend auth.test (%s): %s", api_base_url, exc)
            time.sleep(3)

    app = create_app(bot_token=bot_token, api_base_url=api_base_url, client=client, bot_user_id=bot_user_id)
    handler = SocketModeHandler(app, app_token=app_token)
    logger.info("starting SocketModeHandler against %s", api_base_url)
    handler.start()


if __name__ == "__main__":
    main()

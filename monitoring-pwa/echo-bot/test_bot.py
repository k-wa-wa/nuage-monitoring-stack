import base64
import hashlib
import json
import os
import socket
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from unittest.mock import patch

from slack_bolt import BoltRequest
from slack_sdk import WebClient
from slack_sdk.web import SlackResponse

from bot import (
    TokenFileSocketModeHandler,
    TokenFileWebClient,
    create_app,
    format_echo_text,
    load_token,
    make_token_loader,
)


def _encode_ws_text_frame(payload: str) -> bytes:
    data = payload.encode("utf-8")
    n = len(data)
    if n < 126:
        return bytes([0x81, n]) + data
    return bytes([0x81, 126, (n >> 8) & 0xFF, n & 0xFF]) + data


class EchoBotUnitTest(unittest.TestCase):
    def _build_app(self, bot_token_loader=None):
        loader = bot_token_loader or (lambda: "xoxb-test")
        client = TokenFileWebClient(
            bot_token_loader=loader,
            base_url="http://localhost:8080/slack/api/",
        )
        auth_payload = SlackResponse(
            client=client,
            http_verb="POST",
            api_url="http://localhost:8080/slack/api/auth.test",
            req_args={},
            data={
                "ok": True,
                "team_id": "T01NUAGE",
                "user_id": "U02ECHOBOT",
                "bot_id": "B02ECHOBOT",
            },
            headers={"x-oauth-scopes": "chat:write,app_mentions:read"},
            status_code=200,
        )
        app = create_app(
            api_base_url="http://localhost:8080/slack/api/",
            client=client,
            bot_user_id="U02ECHOBOT",
            bot_id="B02ECHOBOT",
            process_before_response=True,
            bot_token_loader=loader,
        )
        return app, auth_payload

    def _dispatch_event(self, app, auth_payload, event):
        body = {
            "type": "event_callback",
            "team_id": "T01NUAGE",
            "event_id": "EvTest",
            "event_time": int(time.time()),
            "event": event,
        }
        with (
            patch.object(WebClient, "auth_test", return_value=auth_payload),
            patch.object(
                WebClient, "chat_postMessage", return_value={"ok": True, "ts": "999.000001"}
            ) as mock_post,
        ):
            resp = app.dispatch(BoltRequest(body=body, mode="socket_mode"))
        self.assertEqual(resp.status, 200)
        return mock_post

    def test_format_echo_text(self):
        self.assertEqual(format_echo_text("<@U01BOT> こんにちは", "U01BOT"), "こんにちは")
        self.assertEqual(format_echo_text("<@U01BOT|nuage-bot> こんにちは", "U01BOT"), "こんにちは")
        self.assertEqual(format_echo_text("<@U01BOT>", "U01BOT"), "(空のメッセージ)")
        self.assertEqual(format_echo_text("<@U01BOT|nuage-bot>", "U01BOT"), "(空のメッセージ)")
        self.assertEqual(format_echo_text("  hello world  ", "U01BOT"), "hello world")

    def test_load_token_retries_when_missing_or_empty_and_never_falls_back(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tok_path = os.path.join(tmpdir, "jwt.token")

            # 一時的に空のファイルが数回リトライ後に書き込まれると成功する
            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("")

            def write_delayed():
                time.sleep(0.03)
                with open(tok_path, "w", encoding="utf-8") as f:
                    f.write("jwt-recovered\n")

            t = threading.Thread(target=write_delayed, daemon=True)
            t.start()
            self.assertEqual(
                load_token(tok_path, "xoxb-fallback", "bot", retry_delays=(0.02, 0.02, 0.02)),
                "jwt-recovered",
            )
            t.join(timeout=1)

            # 最後まで空ならフォールバックせず例外にする
            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("   \n")
            with self.assertRaises(ValueError):
                load_token(tok_path, "xoxb-fallback", "bot", retry_delays=(0.01, 0.01))

            # 最後までファイル不在ならフォールバックせず例外にする
            missing_path = os.path.join(tmpdir, "missing.token")
            with self.assertRaises(FileNotFoundError):
                load_token(missing_path, "xoxb-fallback", "bot", retry_delays=(0.01,))

            # ファイル未指定時は fallback を使う
            self.assertEqual(load_token("", "xoxb-fallback", "bot"), "xoxb-fallback")

    def test_say_reloads_bot_token_even_when_updated_after_authorize_within_same_event(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tok_path = os.path.join(tmpdir, "bot.jwt")
            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("jwt-before-authorize\n")

            seen_auth_headers = []
            seen_loader_calls = []

            class _Handler(BaseHTTPRequestHandler):
                def do_POST(self):
                    seen_auth_headers.append(self.headers.get("Authorization"))
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(json.dumps({"ok": True, "ts": "100.1"}).encode())

                def log_message(self, format, *args):
                    return

            srv = HTTPServer(("127.0.0.1", 0), _Handler)
            t = threading.Thread(target=srv.serve_forever, daemon=True)
            t.start()
            try:
                base_url = f"http://127.0.0.1:{srv.server_port}/slack/api/"
                raw_loader = make_token_loader(tok_path, "", "bot", retry_delays=(0.01,))

                def tracking_loader():
                    val = raw_loader()
                    seen_loader_calls.append(val)
                    # authorize (1 回目の呼び出し) の直後に JWT ファイルが更新された状況を模擬
                    if len(seen_loader_calls) == 1:
                        with open(tok_path, "w", encoding="utf-8") as f:
                            f.write("jwt-updated-before-say\n")
                    return val

                client = TokenFileWebClient(bot_token_loader=tracking_loader, base_url=base_url)
                app = create_app(
                    api_base_url=base_url,
                    client=client,
                    bot_user_id="U02ECHOBOT",
                    bot_id="B02ECHOBOT",
                    process_before_response=True,
                    bot_token_loader=tracking_loader,
                )

                body = {
                    "type": "event_callback",
                    "team_id": "T01NUAGE",
                    "event_id": "EvSameEvent",
                    "event_time": int(time.time()),
                    "event": {
                        "type": "app_mention",
                        "user": "U01USER",
                        "channel": "C05GENERAL",
                        "channel_type": "channel",
                        "text": "<@U02ECHOBOT> ping",
                        "ts": "100.1",
                    },
                }
                resp = app.dispatch(BoltRequest(body=body, mode="socket_mode"))
                self.assertEqual(resp.status, 200)

                # authorize が jwt-before-authorize を読んだ後に更新され、say() 送信時は jwt-updated-before-say になること
                self.assertEqual(seen_loader_calls, ["jwt-before-authorize", "jwt-updated-before-say"])
                self.assertEqual(seen_auth_headers, ["Bearer jwt-updated-before-say"])
            finally:
                srv.shutdown()
                srv.server_close()

    def test_socket_mode_auto_reconnect_reloads_app_token_from_file(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            app_tok_path = os.path.join(tmpdir, "app.jwt")
            with open(app_tok_path, "w", encoding="utf-8") as f:
                f.write("app-jwt-1\n")

            seen_open_tokens = []
            ws_connections = threading.Event()
            second_connected = threading.Event()
            stop_ws = threading.Event()
            conn_count = 0

            # 1 回目の接続は hello 送出後にサーバー側から閉じ、SDK の自動再接続を発火させる
            ws_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            ws_sock.bind(("127.0.0.1", 0))
            ws_sock.listen(5)
            ws_port = ws_sock.getsockname()[1]

            def ws_server_loop():
                nonlocal conn_count
                ws_sock.settimeout(0.2)
                while not stop_ws.is_set():
                    try:
                        conn, _ = ws_sock.accept()
                    except socket.timeout:
                        continue
                    except OSError:
                        break
                    conn_count += 1
                    cur_idx = conn_count
                    try:
                        req = conn.recv(4096).decode("utf-8", errors="ignore")
                        key = ""
                        for line in req.splitlines():
                            if line.lower().startswith("sec-websocket-key:"):
                                key = line.split(":", 1)[1].strip()
                        accept = base64.b64encode(
                            hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()
                        ).decode()
                        conn.sendall(
                            (
                                "HTTP/1.1 101 Switching Protocols\r\n"
                                "Upgrade: websocket\r\n"
                                "Connection: Upgrade\r\n"
                                f"Sec-WebSocket-Accept: {accept}\r\n\r\n"
                            ).encode()
                        )
                        conn.sendall(_encode_ws_text_frame(json.dumps({"type": "hello", "num_connections": cur_idx})))
                        if cur_idx == 1:
                            ws_connections.set()
                            # ファイル更新後に切断したいので、少し待つか close フレームで切断する
                            time.sleep(0.15)
                            conn.sendall(b"\x88\x00")
                            conn.close()
                        else:
                            second_connected.set()
                            while not stop_ws.is_set():
                                time.sleep(0.05)
                            conn.close()
                    except Exception:
                        conn.close()

            ws_thread = threading.Thread(target=ws_server_loop, daemon=True)
            ws_thread.start()

            class _HTTPHandler(BaseHTTPRequestHandler):
                def do_POST(self):
                    seen_open_tokens.append(self.headers.get("Authorization"))
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(
                        json.dumps({"ok": True, "url": f"ws://127.0.0.1:{ws_port}/slack/socket?ticket=t{len(seen_open_tokens)}"}).encode()
                    )

                def log_message(self, format, *args):
                    return

            srv = HTTPServer(("127.0.0.1", 0), _HTTPHandler)
            threading.Thread(target=srv.serve_forever, daemon=True).start()
            handler = None
            try:
                base_url = f"http://127.0.0.1:{srv.server_port}/slack/api/"
                bot_loader = lambda: "bot-jwt-dummy"  # noqa: E731
                app_loader = make_token_loader(app_tok_path, "", "app", retry_delays=(0.01,))
                client = TokenFileWebClient(bot_token_loader=bot_loader, base_url=base_url)
                app = create_app(
                    api_base_url=base_url,
                    client=client,
                    bot_user_id="U02ECHOBOT",
                    bot_id="B02ECHOBOT",
                    process_before_response=True,
                    bot_token_loader=bot_loader,
                )
                handler = TokenFileSocketModeHandler(app, app_token_loader=app_loader)
                handler.connect()

                self.assertTrue(ws_connections.wait(timeout=3), "initial websocket connection did not establish")
                with open(app_tok_path, "w", encoding="utf-8") as f:
                    f.write("app-jwt-2\n")

                self.assertTrue(second_connected.wait(timeout=5), "SDK auto-reconnect did not establish second connection")
                self.assertGreaterEqual(len(seen_open_tokens), 2)
                self.assertEqual(seen_open_tokens[0], "Bearer app-jwt-1")
                self.assertEqual(seen_open_tokens[-1], "Bearer app-jwt-2")
            finally:
                stop_ws.set()
                if handler is not None:
                    handler.close()
                ws_sock.close()
                srv.shutdown()
                srv.server_close()

    def test_app_mention_channel_and_thread_via_bolt_dispatch(self):
        app, auth_payload = self._build_app()

        # チャンネル直下メンション -> thread_ts なしで返す
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "app_mention",
                "user": "U01USER",
                "channel": "C05GENERAL",
                "channel_type": "channel",
                "text": "<@U02ECHOBOT> ping",
                "ts": "100.1",
            },
        )
        mock_post.assert_called_once()
        self.assertEqual(mock_post.call_args.kwargs["channel"], "C05GENERAL")
        self.assertEqual(mock_post.call_args.kwargs["text"], "ping")
        self.assertIsNone(mock_post.call_args.kwargs.get("thread_ts"))

        # スレッド内メンション -> thread_ts 付きで返す (<@U...|label> 形式も整形)
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "app_mention",
                "user": "U01USER",
                "channel": "C05GENERAL",
                "channel_type": "channel",
                "text": "<@U02ECHOBOT|echo-bot> thread ping",
                "ts": "100.2",
                "thread_ts": "100.1",
            },
        )
        mock_post.assert_called_once()
        self.assertEqual(mock_post.call_args.kwargs["channel"], "C05GENERAL")
        self.assertEqual(mock_post.call_args.kwargs["text"], "thread ping")
        self.assertEqual(mock_post.call_args.kwargs.get("thread_ts"), "100.1")

    def test_message_dm_thread_and_self_ignore_via_bolt_dispatch(self):
        app, auth_payload = self._build_app()

        # DM -> そのまま返す
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "message",
                "channel": "D01USER",
                "channel_type": "im",
                "user": "U01USER",
                "text": "dm hello",
                "ts": "101.1",
            },
        )
        mock_post.assert_called_once()
        self.assertEqual(mock_post.call_args.kwargs["channel"], "D01USER")
        self.assertEqual(mock_post.call_args.kwargs["text"], "dm hello")
        self.assertIsNone(mock_post.call_args.kwargs.get("thread_ts"))

        # チャンネル直下 (メンションなし・スレッドなし) -> 無視
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "message",
                "channel": "C05GENERAL",
                "channel_type": "channel",
                "user": "U01USER",
                "text": "just chat",
                "ts": "102.1",
            },
        )
        mock_post.assert_not_called()

        # チャンネルスレッド (メンションなし) -> スレッドに返す
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "message",
                "channel": "C05GENERAL",
                "channel_type": "channel",
                "user": "U01USER",
                "text": "follow up",
                "ts": "102.2",
                "thread_ts": "102.1",
            },
        )
        mock_post.assert_called_once()
        self.assertEqual(mock_post.call_args.kwargs["channel"], "C05GENERAL")
        self.assertEqual(mock_post.call_args.kwargs["text"], "follow up")
        self.assertEqual(mock_post.call_args.kwargs.get("thread_ts"), "102.1")

        # チャンネルスレッド (bot メンションあり: <@U...> と <@U...|label> の両方) -> app_mention との重複防止でスキップ
        for mention_text in ("<@U02ECHOBOT> follow up", "<@U02ECHOBOT|echo-bot> follow up"):
            mock_post = self._dispatch_event(
                app,
                auth_payload,
                {
                    "type": "message",
                    "channel": "C05GENERAL",
                    "channel_type": "channel",
                    "user": "U01USER",
                    "text": mention_text,
                    "ts": "102.3",
                    "thread_ts": "102.1",
                },
            )
            mock_post.assert_not_called()

        # bot 自身のメッセージ -> IgnoringSelfEvents ミドルウェア及びハンドラで無視
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "message",
                "channel": "D01USER",
                "channel_type": "im",
                "user": "U02ECHOBOT",
                "bot_id": "B02ECHOBOT",
                "text": "echo",
                "ts": "101.2",
            },
        )
        mock_post.assert_not_called()


if __name__ == "__main__":
    unittest.main()

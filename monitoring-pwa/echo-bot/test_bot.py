import os
import tempfile
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import threading
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
        self.assertEqual(format_echo_text("<@U01BOT>", "U01BOT"), "(空のメッセージ)")
        self.assertEqual(format_echo_text("  hello world  ", "U01BOT"), "hello world")

    def test_load_token_prefers_file_and_reads_updates(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tok_path = os.path.join(tmpdir, "jwt.token")
            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("jwt-v1\n")

            self.assertEqual(load_token(tok_path, "xoxb-fallback", "bot"), "jwt-v1")

            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("jwt-v2\n")
            self.assertEqual(load_token(tok_path, "xoxb-fallback", "bot"), "jwt-v2")

            # ファイルが空ならフォールバックせずエラーにする
            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("   \n")
            with self.assertRaises(ValueError):
                load_token(tok_path, "xoxb-fallback", "bot")

            # ファイル未指定時は fallback を使う
            self.assertEqual(load_token("", "xoxb-fallback", "bot"), "xoxb-fallback")

    def test_authorize_and_webclient_reload_bot_token_per_call(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tok_path = os.path.join(tmpdir, "bot.jwt")
            with open(tok_path, "w", encoding="utf-8") as f:
                f.write("jwt-token-1\n")

            seen_auth_headers = []

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
                loader = make_token_loader(tok_path, "", "bot")
                client = TokenFileWebClient(bot_token_loader=loader, base_url=base_url)
                app = create_app(
                    api_base_url=base_url,
                    client=client,
                    bot_user_id="U02ECHOBOT",
                    bot_id="B02ECHOBOT",
                    process_before_response=True,
                    bot_token_loader=loader,
                )

                body = {
                    "type": "event_callback",
                    "team_id": "T01NUAGE",
                    "event_id": "Ev1",
                    "event_time": int(time.time()),
                    "event": {
                        "type": "app_mention",
                        "user": "U01USER",
                        "channel": "C05GENERAL",
                        "channel_type": "channel",
                        "text": "<@U02ECHOBOT> ping 1",
                        "ts": "100.1",
                    },
                }
                resp1 = app.dispatch(BoltRequest(body=body, mode="socket_mode"))
                self.assertEqual(resp1.status, 200)

                # ファイル更新後、2 回目のイベント処理は新しい JWT を使うこと
                with open(tok_path, "w", encoding="utf-8") as f:
                    f.write("jwt-token-2\n")

                body["event_id"] = "Ev2"
                body["event"]["text"] = "<@U02ECHOBOT> ping 2"
                body["event"]["ts"] = "100.2"
                resp2 = app.dispatch(BoltRequest(body=body, mode="socket_mode"))
                self.assertEqual(resp2.status, 200)

                self.assertEqual(seen_auth_headers, ["Bearer jwt-token-1", "Bearer jwt-token-2"])
            finally:
                srv.shutdown()
                srv.server_close()

    def test_socket_mode_reconnect_reloads_app_token_from_file(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            app_tok_path = os.path.join(tmpdir, "app.jwt")
            with open(app_tok_path, "w", encoding="utf-8") as f:
                f.write("app-jwt-1\n")

            seen_open_tokens = []

            class _Handler(BaseHTTPRequestHandler):
                def do_POST(self):
                    seen_open_tokens.append(self.headers.get("Authorization"))
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(
                        json.dumps({"ok": True, "url": "ws://127.0.0.1:1/slack/socket?ticket=t"}).encode()
                    )

                def log_message(self, format, *args):
                    return

            srv = HTTPServer(("127.0.0.1", 0), _Handler)
            t = threading.Thread(target=srv.serve_forever, daemon=True)
            t.start()
            handler = None
            try:
                base_url = f"http://127.0.0.1:{srv.server_port}/slack/api/"
                bot_loader = lambda: "bot-jwt-dummy"  # noqa: E731
                app_loader = make_token_loader(app_tok_path, "", "app")
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

                wss_1 = handler.client.issue_new_wss_url()
                self.assertTrue(wss_1.startswith("ws://"))

                with open(app_tok_path, "w", encoding="utf-8") as f:
                    f.write("app-jwt-2\n")

                wss_2 = handler.client.issue_new_wss_url()
                self.assertTrue(wss_2.startswith("ws://"))
                self.assertEqual(seen_open_tokens, ["Bearer app-jwt-1", "Bearer app-jwt-2"])
            finally:
                if handler is not None:
                    handler.client.close()
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

        # スレッド内メンション -> thread_ts 付きで返す
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "app_mention",
                "user": "U01USER",
                "channel": "C05GENERAL",
                "channel_type": "channel",
                "text": "<@U02ECHOBOT> thread ping",
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

        # チャンネルスレッド (bot メンションあり) -> app_mention との重複防止でスキップ
        mock_post = self._dispatch_event(
            app,
            auth_payload,
            {
                "type": "message",
                "channel": "C05GENERAL",
                "channel_type": "channel",
                "user": "U01USER",
                "text": "<@U02ECHOBOT> follow up",
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

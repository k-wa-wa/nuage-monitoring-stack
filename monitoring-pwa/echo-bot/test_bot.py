import time
import unittest
from unittest.mock import patch

from slack_bolt import BoltRequest
from slack_sdk import WebClient
from slack_sdk.web import SlackResponse

from bot import create_app, format_echo_text


class EchoBotUnitTest(unittest.TestCase):
    def _build_app(self):
        client = WebClient(token="xoxb-test", base_url="http://localhost:8080/slack/api/")
        auth_payload = SlackResponse(
            client=client,
            http_verb="POST",
            api_url="http://localhost:8080/slack/api/auth.test",
            req_args={},
            data={
                "ok": True,
                "team_id": "T01NUAGE",
                "user_id": "U01BOT",
                "bot_id": "B01BOT",
            },
            headers={"x-oauth-scopes": "chat:write,app_mentions:read"},
            status_code=200,
        )
        app = create_app(
            "xoxb-test",
            "http://localhost:8080/slack/api/",
            client=client,
            bot_user_id="U01BOT",
            process_before_response=True,
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
                "text": "<@U01BOT> ping",
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
                "text": "<@U01BOT> thread ping",
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
                "text": "<@U01BOT> follow up",
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
                "user": "U01BOT",
                "bot_id": "B01BOT",
                "text": "echo",
                "ts": "101.2",
            },
        )
        mock_post.assert_not_called()


if __name__ == "__main__":
    unittest.main()

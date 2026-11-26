import unittest
from unittest.mock import MagicMock

from slack_sdk import WebClient

from bot import create_app, format_echo_text


class EchoBotUnitTest(unittest.TestCase):
    def _build_app(self):
        client = MagicMock(spec=WebClient)
        client.token = "xoxb-test"
        client.auth_test.return_value = {"ok": True, "user_id": "U01BOT", "bot_id": "B01BOT"}
        app = create_app("xoxb-test", "http://localhost:8080/slack/api/", client=client, bot_user_id="U01BOT")
        return app

    def test_format_echo_text(self):
        self.assertEqual(format_echo_text("<@U01BOT> こんにちは", "U01BOT"), "こんにちは")
        self.assertEqual(format_echo_text("<@U01BOT>", "U01BOT"), "(空のメッセージ)")
        self.assertEqual(format_echo_text("  hello world  ", "U01BOT"), "hello world")

    def test_app_mention_channel_and_thread(self):
        app = self._build_app()
        listener = app._listeners[-2]  # app_mention listener
        say = MagicMock()

        # チャンネル直下メンション -> thread_ts なしで返す
        listener.ack_function(
            event={"type": "app_mention", "user": "U01USER", "text": "<@U01BOT> ping", "ts": "100.1"},
            say=say,
        )
        say.assert_called_once_with(text="ping")

        # スレッド内メンション -> thread_ts 付きで返す
        say.reset_mock()
        listener.ack_function(
            event={
                "type": "app_mention",
                "user": "U01USER",
                "text": "<@U01BOT> thread ping",
                "ts": "100.2",
                "thread_ts": "100.1",
            },
            say=say,
        )
        say.assert_called_once_with(text="thread ping", thread_ts="100.1")

    def test_message_dm_and_thread_behavior(self):
        app = self._build_app()
        listener = app._listeners[-1]  # message listener
        say = MagicMock()

        # DM -> そのまま返す
        listener.ack_function(
            event={"type": "message", "channel_type": "im", "user": "U01USER", "text": "dm hello", "ts": "101.1"},
            say=say,
        )
        say.assert_called_once_with(text="dm hello")

        # チャンネル直下 (メンションなし・スレッドなし) -> 無視
        say.reset_mock()
        listener.ack_function(
            event={"type": "message", "channel_type": "channel", "user": "U01USER", "text": "just chat", "ts": "102.1"},
            say=say,
        )
        say.assert_not_called()

        # チャンネルスレッド (メンションなし) -> スレッドに返す
        say.reset_mock()
        listener.ack_function(
            event={
                "type": "message",
                "channel_type": "channel",
                "user": "U01USER",
                "text": "follow up",
                "ts": "102.2",
                "thread_ts": "102.1",
            },
            say=say,
        )
        say.assert_called_once_with(text="follow up", thread_ts="102.1")

        # チャンネルスレッド (bot メンションあり) -> app_mention との重複防止でスキップ
        say.reset_mock()
        listener.ack_function(
            event={
                "type": "message",
                "channel_type": "channel",
                "user": "U01USER",
                "text": "<@U01BOT> follow up",
                "ts": "102.3",
                "thread_ts": "102.1",
            },
            say=say,
        )
        say.assert_not_called()

        # bot 自身のメッセージ -> 無視
        say.reset_mock()
        listener.ack_function(
            event={"type": "message", "channel_type": "im", "bot_id": "B01BOT", "text": "echo", "ts": "101.2"},
            say=say,
        )
        say.assert_not_called()


if __name__ == "__main__":
    unittest.main()

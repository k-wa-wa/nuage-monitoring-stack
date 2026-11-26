import http.cookiejar
import json
import os
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.request

from slack_bolt.adapter.socket_mode import SocketModeHandler
from slack_sdk import WebClient

import bot as echo_bot


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class BoltGoBackendE2ETest(unittest.TestCase):
    def test_bolt_socket_mode_and_web_api_against_go_backend(self):
        here = os.path.dirname(os.path.abspath(__file__))
        backend_dir = os.path.abspath(os.path.join(here, "..", "backend"))
        port = _free_port()
        base_url = f"http://127.0.0.1:{port}"

        bot_token = "xoxb-ci-e2e-bot-token"
        app_token = "xapp-ci-e2e-app-token"
        passphrase = "nuage-preview"
        users_json = json.dumps(
            [
                {
                    "id": "U01PREVIEW",
                    "name": "preview",
                    "display_name": "Preview User",
                    "passphrase_hash": "$2a$10$RepBxy1AUCcxSMy1ji9PxueGMS.2UR5E8L.HtDpEzL9EvVGFl8Omu",
                }
            ]
        )

        with tempfile.TemporaryDirectory() as tmpdir:
            server_bin = os.path.join(tmpdir, "server")
            subprocess.run(
                ["go", "build", "-o", server_bin, "./cmd/server"],
                cwd=backend_dir,
                check=True,
            )

            env = os.environ.copy()
            env.update(
                {
                    "PORT": str(port),
                    "DB_PATH": os.path.join(tmpdir, "e2e.db"),
                    "VAPID_PUBLIC_KEY": "test-vapid-public-key",
                    "VAPID_PRIVATE_KEY": "test-vapid-private-key",
                    "VAPID_SUBJECT": "mailto:test@example.com",
                    "COOKIE_SIGNING_KEY": "e2e-test-cookie-signing-key-32bytes!",
                    "USERS_JSON": users_json,
                    "SLACK_BOT_TOKEN": bot_token,
                    "SLACK_APP_TOKEN": app_token,
                }
            )

            srv = subprocess.Popen(
                [server_bin], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE
            )
            sm_handler = None
            try:
                for _ in range(50):
                    try:
                        with urllib.request.urlopen(f"{base_url}/health", timeout=1) as r:
                            if r.status == 200:
                                break
                    except Exception:
                        time.sleep(0.1)
                else:
                    self.fail("Go backend server did not become healthy in time")

                client = WebClient(token=bot_token, base_url=f"{base_url}/slack/api/")
                auth = client.auth_test()
                self.assertTrue(auth["ok"])
                self.assertEqual(auth["user_id"], "U01BOT")

                app = echo_bot.create_app(
                    bot_token,
                    f"{base_url}/slack/api/",
                    client=client,
                    bot_user_id=auth["user_id"],
                )
                sm_handler = SocketModeHandler(app, app_token=app_token)
                sm_handler.connect()

                cj = http.cookiejar.CookieJar()
                opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

                def api_req(path, data=None):
                    body = json.dumps(data).encode() if data is not None else None
                    headers = {"Content-Type": "application/json"} if data is not None else {}
                    req = urllib.request.Request(f"{base_url}{path}", data=body, headers=headers)
                    with opener.open(req, timeout=5) as resp:
                        return json.loads(resp.read().decode())

                login_res = api_req("/api/auth/login", {"passphrase": passphrase})
                self.assertEqual(login_res["user"]["id"], "U01PREVIEW")

                boot = api_req("/api/bootstrap")
                self.assertTrue(boot["bot"]["online"])

                # 1. DM オウム返し
                dm_post = api_req("/api/channels/D01PREVIEW/messages", {"text": "DMテスト"})
                self.assertTrue(dm_post["delivered_to_bot"])

                # 2. チャンネルメンション オウム返し
                ch_post = api_req(
                    "/api/channels/C05GENERAL/messages", {"text": "@bot メンションテスト"}
                )
                self.assertTrue(ch_post["delivered_to_bot"])
                parent_ts = ch_post["message"]["ts"]

                # 3. スレッド返信 オウム返し
                th_post = api_req(
                    "/api/channels/C05GENERAL/messages",
                    {"text": "スレッドテスト", "thread_ts": parent_ts},
                )
                self.assertTrue(th_post["delivered_to_bot"])

                deadline = time.time() + 5
                while time.time() < deadline:
                    dm_msgs = api_req("/api/channels/D01PREVIEW/messages")["messages"]
                    ch_msgs = api_req("/api/channels/C05GENERAL/messages")["messages"]
                    th_res = api_req(f"/api/channels/C05GENERAL/threads/{parent_ts}")
                    if len(dm_msgs) >= 2 and len(ch_msgs) >= 2 and len(th_res["replies"]) >= 2:
                        break
                    time.sleep(0.1)

                dm_msgs = api_req("/api/channels/D01PREVIEW/messages")["messages"]
                self.assertEqual([m["text"] for m in dm_msgs], ["DMテスト", "DMテスト"])
                self.assertEqual(dm_msgs[1]["bot_id"], "B01BOT")

                ch_msgs = api_req("/api/channels/C05GENERAL/messages")["messages"]
                self.assertEqual(ch_msgs[1]["text"], "メンションテスト")

                th_res = api_req(f"/api/channels/C05GENERAL/threads/{parent_ts}")
                self.assertEqual(
                    [r["text"] for r in th_res["replies"]],
                    ["スレッドテスト", "スレッドテスト"],
                )

                # 4. Slack Web API 各種検証
                convs = client.conversations_list(types="public_channel,im")
                self.assertTrue(any(c["id"] == "D01PREVIEW" for c in convs["channels"]))

                upd = client.chat_update(
                    channel="D01PREVIEW", ts=dm_msgs[1]["ts"], text="更新済みDM"
                )
                self.assertEqual(upd["text"], "更新済みDM")

                uinfo = client.users_info(user="U01PREVIEW")
                self.assertEqual(uinfo["user"]["profile"]["display_name"], "Preview User")
            finally:
                if sm_handler is not None:
                    sm_handler.close()
                srv.terminate()
                srv.wait(timeout=5)
                if srv.stdout:
                    srv.stdout.close()
                if srv.stderr:
                    srv.stderr.close()


if __name__ == "__main__":
    unittest.main()

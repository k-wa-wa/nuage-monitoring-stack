import http.cookiejar
import json
import os
import socket
import subprocess
import tempfile
import threading
import time
import unittest
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

from slack_sdk.errors import SlackApiError

import bot as echo_bot


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


_JWT_TOOL_GO = r'''package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"
)

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func signES256(priv *ecdsa.PrivateKey, kid, sub, aud string, exp time.Time) (string, error) {
	hdr, _ := json.Marshal(map[string]string{
		"alg": "ES256",
		"typ": "JWT",
		"kid": kid,
	})
	claims, _ := json.Marshal(map[string]interface{}{
		"sub": sub,
		"aud": []string{aud},
		"exp": exp.Unix(),
		"iat": time.Now().Unix(),
	})
	signingInput := b64url(hdr) + "." + b64url(claims)
	sum := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + b64url(sig), nil
}

func main() {
	aud := os.Args[1]
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	xb := make([]byte, 32)
	yb := make([]byte, 32)
	priv.PublicKey.X.FillBytes(xb)
	priv.PublicKey.Y.FillBytes(yb)
	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "EC",
				"kid": "e2e-key-1",
				"use": "sig",
				"alg": "ES256",
				"crv": "P-256",
				"x":   b64url(xb),
				"y":   b64url(yb),
			},
		},
	}
	exp := time.Now().Add(10 * time.Minute)
	echoJWT1, err := signES256(priv, "e2e-key-1", "spiffe://nuage/ns/preview-test/sa/echo-bot", aud, exp)
	if err != nil {
		panic(err)
	}
	echoJWT2, err := signES256(priv, "e2e-key-1", "spiffe://nuage/ns/preview-test/sa/echo-bot", aud, exp.Add(time.Minute))
	if err != nil {
		panic(err)
	}
	atelierJWT, err := signES256(priv, "e2e-key-1", "spiffe://nuage/host/atelier/bot", aud, exp)
	if err != nil {
		panic(err)
	}
	_ = big.NewInt(0)
	out, _ := json.Marshal(map[string]interface{}{
		"jwks":        jwks,
		"echo_jwt_1":  echoJWT1,
		"echo_jwt_2":  echoJWT2,
		"atelier_jwt": atelierJWT,
	})
	fmt.Println(string(out))
}
'''


def _generate_test_spiffe_material(tmpdir: str, audience: str) -> dict:
    src_path = os.path.join(tmpdir, "jwttool.go")
    with open(src_path, "w", encoding="utf-8") as f:
        f.write(_JWT_TOOL_GO)
    res = subprocess.run(
        ["go", "run", src_path, audience],
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(res.stdout)


class BoltGoBackendE2ETest(unittest.TestCase):
    def test_bolt_socket_mode_and_web_api_against_go_backend(self):
        here = os.path.dirname(os.path.abspath(__file__))
        backend_dir = os.path.abspath(os.path.join(here, "..", "backend"))
        port = _free_port()
        base_url = f"http://127.0.0.1:{port}"
        audience = "monitoring-pwa.preview-test"

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
        bot_users_json = json.dumps(
            [
                {
                    "sub": "spiffe://nuage/ns/preview-test/sa/echo-bot",
                    "user_id": "U02ECHOBOT",
                    "bot_id": "B02ECHOBOT",
                    "name": "echo-bot",
                    "is_default": True,
                },
                {
                    "sub": "spiffe://nuage/host/atelier/bot",
                    "user_id": "U01BOT",
                    "bot_id": "B01BOT",
                    "name": "nuage-bot",
                },
            ]
        )

        with tempfile.TemporaryDirectory() as tmpdir:
            material = _generate_test_spiffe_material(tmpdir, audience)
            jwks_bytes = json.dumps(material["jwks"]).encode("utf-8")

            class _JWKSHandler(BaseHTTPRequestHandler):
                def do_GET(self):
                    if self.path == "/keys":
                        self.send_response(200)
                        self.send_header("Content-Type", "application/json")
                        self.end_headers()
                        self.wfile.write(jwks_bytes)
                    else:
                        self.send_response(404)
                        self.end_headers()

                def log_message(self, format, *args):
                    return

            jwks_srv = HTTPServer(("127.0.0.1", 0), _JWKSHandler)
            threading.Thread(target=jwks_srv.serve_forever, daemon=True).start()

            token_file = os.path.join(tmpdir, "monitoring-pwa.jwt")
            with open(token_file, "w", encoding="utf-8") as f:
                f.write(material["echo_jwt_1"])

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
                    "SPIFFE_JWKS_URL": f"http://127.0.0.1:{jwks_srv.server_port}/keys",
                    "SPIFFE_JWT_AUDIENCE": audience,
                    "BOT_USERS_JSON": bot_users_json,
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

                bot_loader = echo_bot.make_token_loader(token_file, "", "bot")
                app_loader = echo_bot.make_token_loader(token_file, "", "app")
                client = echo_bot.TokenFileWebClient(
                    bot_token_loader=bot_loader,
                    base_url=f"{base_url}/slack/api/",
                )
                auth = client.auth_test()
                self.assertTrue(auth["ok"])
                self.assertEqual(auth["user_id"], "U02ECHOBOT")
                self.assertEqual(auth["bot_id"], "B02ECHOBOT")

                app = echo_bot.create_app(
                    api_base_url=f"{base_url}/slack/api/",
                    client=client,
                    bot_user_id=auth["user_id"],
                    bot_id=auth["bot_id"],
                    bot_token_loader=bot_loader,
                )
                sm_handler = echo_bot.TokenFileSocketModeHandler(app, app_token_loader=app_loader)
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

                boot = None
                for _ in range(30):
                    boot = api_req("/api/bootstrap")
                    if boot["bot"]["online"]:
                        break
                    time.sleep(0.1)
                self.assertTrue(boot["bot"]["online"])
                self.assertEqual(boot["bot"]["user_id"], "U02ECHOBOT")

                # トークンファイルを更新して、以降の呼び出しと再接続 URL 発行が新しい JWT を使うこと
                with open(token_file, "w", encoding="utf-8") as f:
                    f.write(material["echo_jwt_2"])

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
                self.assertEqual(dm_msgs[1]["bot_id"], "B02ECHOBOT")

                ch_msgs = api_req("/api/channels/C05GENERAL/messages")["messages"]
                self.assertEqual(ch_msgs[1]["text"], "メンションテスト")

                th_res = api_req(f"/api/channels/C05GENERAL/threads/{parent_ts}")
                self.assertEqual(
                    [r["text"] for r in th_res["replies"]],
                    ["スレッドテスト", "スレッドテスト"],
                )

                # 4. Slack Web API 各種検証と別 bot による chat.update 拒否
                convs = client.conversations_list(types="public_channel,im")
                self.assertTrue(any(c["id"] == "D01PREVIEW" for c in convs["channels"]))

                upd = client.chat_update(
                    channel="D01PREVIEW", ts=dm_msgs[1]["ts"], text="更新済みDM"
                )
                self.assertEqual(upd["text"], "更新済みDM")

                atelier_client = echo_bot.TokenFileWebClient(
                    bot_token_loader=lambda: material["atelier_jwt"],
                    base_url=f"{base_url}/slack/api/",
                )
                with self.assertRaises(SlackApiError) as cm:
                    atelier_client.chat_update(
                        channel="D01PREVIEW", ts=dm_msgs[1]["ts"], text="横取り"
                    )
                self.assertEqual(cm.exception.response["error"], "cant_update_message")

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
                jwks_srv.shutdown()
                jwks_srv.server_close()


if __name__ == "__main__":
    unittest.main()

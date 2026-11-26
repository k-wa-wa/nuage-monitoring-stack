package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/auth"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

func setupSlackTestEnv(t *testing.T) (config.Config, *push.DB, *realtime.Hub, *mockSender, *echo.Echo) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)
	cfg := config.Config{
		Port:             "8080",
		VAPIDPublicKey:   "pub",
		VAPIDPrivateKey:  "priv",
		CookieSigningKey: strings.Repeat("k", 32),
		SlackBotToken:    "xoxb-test-bot-token",
		SlackAppToken:    "xapp-test-app-token",
		Users: []config.UserConfig{
			{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hash)},
			{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: string(hash)},
		},
	}

	db, err := push.InitDB(filepath.Join(t.TempDir(), "slack_test.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureUserChannels(cfg.Users); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	hub := realtime.NewHub()
	sender := &mockSender{}
	slackH := NewSlackHandler(cfg, db, hub, sender)
	chatH := NewChatHandler(cfg, db, hub)

	e := echo.New()
	e.POST("/slack/api/:method", slackH.DispatchMethod)
	e.GET("/slack/socket", slackH.SocketModeWS)

	api := e.Group("/api")
	api.Use(auth.RequireSessionMiddleware([]byte(cfg.CookieSigningKey), cfg.Users))
	api.POST("/channels/:id/messages", chatH.PostMessage)
	api.GET("/channels/:id/messages", chatH.ListMessages)

	return cfg, db, hub, sender, e
}

func callSlackMethod(t *testing.T, e *echo.Echo, method, token string, body interface{}) map[string]interface{} {
	t.Helper()
	var req *http.Request
	switch v := body.(type) {
	case url.Values:
		req = httptest.NewRequest(http.MethodPost, "/slack/api/"+method, strings.NewReader(v.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	case string:
		req = httptest.NewRequest(http.MethodPost, "/slack/api/"+method, strings.NewReader(v))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	default:
		b, _ := json.Marshal(v)
		req = httptest.NewRequest(http.MethodPost, "/slack/api/"+method, strings.NewReader(string(b)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d: %s", method, rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal response for %s failed: %v", method, err)
	}
	return out
}

func TestSlackWebAPIMethods(t *testing.T) {
	cfg, _, _, sender, e := setupSlackTestEnv(t)

	t.Run("認証エラーとunknown_method", func(t *testing.T) {
		resNoAuth := callSlackMethod(t, e, "auth.test", "xoxb-wrong", map[string]interface{}{})
		if resNoAuth["ok"] != false || resNoAuth["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth, got %+v", resNoAuth)
		}

		resOpenBad := callSlackMethod(t, e, "apps.connections.open", cfg.SlackBotToken, map[string]interface{}{})
		if resOpenBad["ok"] != false || resOpenBad["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth when using bot token for connections.open, got %+v", resOpenBad)
		}

		resUnknown := callSlackMethod(t, e, "emoji.list", cfg.SlackBotToken, map[string]interface{}{})
		if resUnknown["ok"] != false || resUnknown["error"] != "unknown_method" {
			t.Fatalf("expected unknown_method, got %+v", resUnknown)
		}
	})

	t.Run("auth.test, conversations.*, chat.postMessage/update, users.info", func(t *testing.T) {
		authRes := callSlackMethod(t, e, "auth.test", cfg.SlackBotToken, map[string]interface{}{})
		if authRes["ok"] != true || authRes["bot_id"] != push.SlackBotID || authRes["user_id"] != push.SlackBotUserID {
			t.Fatalf("unexpected auth.test: %+v", authRes)
		}

		// conversations.list (form-urlencoded でも動くこと)
		listForm := url.Values{}
		listForm.Set("types", "public_channel,im")
		listRes := callSlackMethod(t, e, "conversations.list", cfg.SlackBotToken, listForm)
		if listRes["ok"] != true {
			t.Fatalf("conversations.list failed: %+v", listRes)
		}
		channels := listRes["channels"].([]interface{})
		if len(channels) < 6 { // 5 public + 2 DM
			t.Fatalf("expected >= 6 channels, got %d", len(channels))
		}

		// conversations.info
		infoRes := callSlackMethod(t, e, "conversations.info", cfg.SlackBotToken, map[string]string{"channel": push.ChannelAlertmanager})
		if infoRes["ok"] != true {
			t.Fatalf("conversations.info failed: %+v", infoRes)
		}

		// chat.postMessage (親 + スレッド返信 + Push 発火)
		postRes := callSlackMethod(t, e, "chat.postMessage", cfg.SlackBotToken, map[string]interface{}{
			"channel": push.ChannelGeneral,
			"text":    "初期メッセージ",
			"blocks":  []map[string]interface{}{{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": "*初期メッセージ*"}}},
		})
		if postRes["ok"] != true {
			t.Fatalf("chat.postMessage failed: %+v", postRes)
		}
		ts := postRes["ts"].(string)

		replyRes := callSlackMethod(t, e, "chat.postMessage", cfg.SlackBotToken, map[string]interface{}{
			"channel":   push.ChannelGeneral,
			"thread_ts": ts,
			"text":      "スレッド返信",
		})
		if replyRes["ok"] != true {
			t.Fatalf("chat.postMessage reply failed: %+v", replyRes)
		}

		if len(sender.payloads) != 2 {
			t.Fatalf("expected 2 push payloads from bot posts, got %d", len(sender.payloads))
		}
		if sender.payloads[0].URL != "/channels/"+push.ChannelGeneral {
			t.Fatalf("unexpected push URL: %s", sender.payloads[0].URL)
		}
		if sender.payloads[1].URL != "/channels/"+push.ChannelGeneral+"/threads/"+ts {
			t.Fatalf("unexpected thread push URL: %s", sender.payloads[1].URL)
		}

		// chat.update
		updRes := callSlackMethod(t, e, "chat.update", cfg.SlackBotToken, map[string]interface{}{
			"channel": push.ChannelGeneral,
			"ts":      ts,
			"text":    "更新後メッセージ",
		})
		if updRes["ok"] != true || updRes["text"] != "更新後メッセージ" {
			t.Fatalf("chat.update failed: %+v", updRes)
		}

		// conversations.history
		histRes := callSlackMethod(t, e, "conversations.history", cfg.SlackBotToken, map[string]interface{}{
			"channel": push.ChannelGeneral,
		})
		msgs := histRes["messages"].([]interface{})
		if len(msgs) != 1 {
			t.Fatalf("expected 1 root message in history, got %d", len(msgs))
		}
		parentMsg := msgs[0].(map[string]interface{})
		if parentMsg["text"] != "更新後メッセージ" || parentMsg["reply_count"].(float64) != 1 {
			t.Fatalf("unexpected parent message in history: %+v", parentMsg)
		}

		// conversations.replies
		repRes := callSlackMethod(t, e, "conversations.replies", cfg.SlackBotToken, map[string]interface{}{
			"channel": push.ChannelGeneral,
			"ts":      ts,
		})
		repMsgs := repRes["messages"].([]interface{})
		if len(repMsgs) != 2 {
			t.Fatalf("expected parent + 1 reply in conversations.replies, got %d", len(repMsgs))
		}

		// users.info (人間ユーザーと bot ユーザー)
		uRes := callSlackMethod(t, e, "users.info", cfg.SlackBotToken, map[string]string{"user": "U01ALICE"})
		if uRes["ok"] != true {
			t.Fatalf("users.info failed: %+v", uRes)
		}
		uObj := uRes["user"].(map[string]interface{})
		if uObj["id"] != "U01ALICE" || uObj["is_bot"] != false {
			t.Fatalf("unexpected user object: %+v", uObj)
		}

		// DM への chat.postMessage は該当ユーザー (U01ALICE) の購読にのみ Push 送信すること
		prevBroadcastCount := len(sender.payloads)
		dmPost := callSlackMethod(t, e, "chat.postMessage", cfg.SlackBotToken, map[string]interface{}{
			"channel": push.DMChannelID("U01ALICE"),
			"text":    "Alice 宛の DM",
		})
		if dmPost["ok"] != true {
			t.Fatalf("DM chat.postMessage failed: %+v", dmPost)
		}
		if len(sender.payloads) != prevBroadcastCount {
			t.Fatalf("DM push must not broadcast to all users")
		}
		if len(sender.userPayloads["U01ALICE"]) != 1 || len(sender.userPayloads["U02BOB"]) != 0 {
			t.Fatalf("expected DM push only to U01ALICE, got %+v", sender.userPayloads)
		}
	})

	t.Run("不正なblocksはinvalid_blocksで拒否しチャンネル取得を壊さない", func(t *testing.T) {
		validPost := callSlackMethod(t, e, "chat.postMessage", cfg.SlackBotToken, map[string]interface{}{
			"channel": push.ChannelGeneral,
			"text":    "正常なメッセージ",
		})
		if validPost["ok"] != true {
			t.Fatalf("valid post failed: %+v", validPost)
		}
		validTS := validPost["ts"].(string)

		// form 経由の壊れた JSON 文字列
		badForm := url.Values{}
		badForm.Set("channel", push.ChannelGeneral)
		badForm.Set("blocks", `[{"type":"section"`)
		badPostRes := callSlackMethod(t, e, "chat.postMessage", cfg.SlackBotToken, badForm)
		if badPostRes["ok"] != false || badPostRes["error"] != "invalid_blocks" {
			t.Fatalf("expected invalid_blocks for broken JSON form blocks, got %+v", badPostRes)
		}

		// JSON 配列でない値（オブジェクト / 数値）や null 要素・type 欠落要素も拒否されること
		badPayloads := []map[string]interface{}{
			{"channel": push.ChannelGeneral, "text": "obj", "blocks": map[string]string{"type": "section"}},
			{"channel": push.ChannelGeneral, "text": "null elem", "blocks": []interface{}{nil}},
			{"channel": push.ChannelGeneral, "text": "no type", "blocks": []interface{}{map[string]interface{}{}}},
			{"channel": push.ChannelGeneral, "text": "null ctx", "blocks": []interface{}{map[string]interface{}{"type": "context", "elements": []interface{}{nil}}}},
			{"channel": push.ChannelGeneral, "text": "null field", "blocks": []interface{}{map[string]interface{}{"type": "section", "fields": []interface{}{nil}}}},
			{"channel": push.ChannelGeneral, "text": "null text", "blocks": []interface{}{map[string]interface{}{"type": "section", "text": nil}}},
		}
		for idx, payload := range badPayloads {
			badObjRes := callSlackMethod(t, e, "chat.postMessage", cfg.SlackBotToken, payload)
			if badObjRes["ok"] != false || badObjRes["error"] != "invalid_blocks" {
				t.Fatalf("case %d: expected invalid_blocks, got %+v", idx, badObjRes)
			}
		}

		// chat.update でも同様に拒否されること
		badUpdateForm := url.Values{}
		badUpdateForm.Set("channel", push.ChannelGeneral)
		badUpdateForm.Set("ts", validTS)
		badUpdateForm.Set("blocks", `[{"type":"section"`)
		badUpdRes := callSlackMethod(t, e, "chat.update", cfg.SlackBotToken, badUpdateForm)
		if badUpdRes["ok"] != false || badUpdRes["error"] != "invalid_blocks" {
			t.Fatalf("expected invalid_blocks on chat.update, got %+v", badUpdRes)
		}

		// 既存の人間向けメッセージ一覧 API が 200 を返し続けること
		token, _ := auth.IssueSessionToken([]byte(cfg.CookieSigningKey), "U01ALICE", time.Now())
		listReq := httptest.NewRequest(http.MethodGet, "/api/channels/"+push.ChannelGeneral+"/messages", nil)
		listReq.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
		listRec := httptest.NewRecorder()
		e.ServeHTTP(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("expected 200 on /api/channels/%s/messages, got %d: %s", push.ChannelGeneral, listRec.Code, listRec.Body.String())
		}
	})
}

func TestSocketModeLifecycleAndEvents(t *testing.T) {
	cfg, _, hub, _, e := setupSlackTestEnv(t)

	srv := httptest.NewServer(e)
	defer srv.Close()

	// 1. apps.connections.open で WS URL 取得
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/slack/api/apps.connections.open", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.SlackAppToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("apps.connections.open request failed: %v", err)
	}
	defer resp.Body.Close()

	var openRes struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openRes); err != nil || !openRes.OK {
		t.Fatalf("unexpected apps.connections.open response: %+v, err=%v", openRes, err)
	}
	if !strings.HasPrefix(openRes.URL, "ws://") {
		t.Fatalf("expected ws:// URL, got %s", openRes.URL)
	}

	// 2. WebSocket 接続して hello を受信
	ws, _, err := websocket.DefaultDialer.Dial(openRes.URL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer ws.Close()

	var hello map[string]interface{}
	if err := ws.ReadJSON(&hello); err != nil {
		t.Fatalf("failed to read hello: %v", err)
	}
	if hello["type"] != "hello" {
		t.Fatalf("expected hello frame, got %+v", hello)
	}
	if !hub.IsBotOnline() {
		t.Fatal("expected bot to be online after hello")
	}

	// 3. 人間ユーザーとしてチャンネルにメンション投稿 -> message と app_mention が届くこと
	token, _ := auth.IssueSessionToken([]byte(cfg.CookieSigningKey), "U01ALICE", time.Now())
	postReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/channels/"+push.ChannelGeneral+"/messages", strings.NewReader(`{"text":"@bot ステータス教えて"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil || postResp.StatusCode != http.StatusCreated {
		t.Fatalf("user post failed: status=%v err=%v", postResp, err)
	}
	postResp.Body.Close()

	seenTypes := map[string]string{}
	for i := 0; i < 2; i++ {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		var env realtime.SocketEnvelope
		if err := ws.ReadJSON(&env); err != nil {
			t.Fatalf("failed to read envelope %d: %v", i, err)
		}
		if env.Type != "events_api" {
			t.Fatalf("expected events_api, got %s", env.Type)
		}
		ev := env.Payload["event"].(map[string]interface{})
		evType := ev["type"].(string)
		seenTypes[evType] = env.EnvelopeID
		if ev["user"] != "U01ALICE" || ev["channel_type"] != "channel" {
			t.Fatalf("unexpected event payload: %+v", ev)
		}

		// ack を返す
		if err := ws.WriteJSON(map[string]string{"envelope_id": env.EnvelopeID}); err != nil {
			t.Fatalf("failed to send ack: %v", err)
		}
	}
	if seenTypes["message"] == "" || seenTypes["app_mention"] == "" {
		t.Fatalf("expected both message and app_mention events, got %+v", seenTypes)
	}

	// ack が処理されるまで少し待つ
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.IsAcked(seenTypes["message"]) && hub.IsAcked(seenTypes["app_mention"]) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !hub.IsAcked(seenTypes["message"]) || !hub.IsAcked(seenTypes["app_mention"]) {
		t.Fatal("expected server to record acks for both envelopes")
	}

	// 4. disconnect 通知を受信できること
	hub.DisconnectAll("test_refresh")
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var disc map[string]interface{}
	if err := ws.ReadJSON(&disc); err != nil {
		t.Fatalf("failed to read disconnect: %v", err)
	}
	if disc["type"] != "disconnect" || disc["reason"] != "test_refresh" {
		t.Fatalf("unexpected disconnect message: %+v", disc)
	}
}

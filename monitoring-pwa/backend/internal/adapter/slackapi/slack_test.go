package slackapi_test

import (
	"context"
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

	httpadapter "github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/adapter/http"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/adapter/slackapi"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/realtime"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/sqlite"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

type mockSender struct {
	payloads     []domain.PushNotification
	userPayloads map[string][]domain.PushNotification
}

func (m *mockSender) Broadcast(_ context.Context, n domain.PushNotification) {
	m.payloads = append(m.payloads, n)
}

func (m *mockSender) BroadcastToUser(_ context.Context, userID string, n domain.PushNotification) {
	if m.userPayloads == nil {
		m.userPayloads = make(map[string][]domain.PushNotification)
	}
	m.userPayloads[userID] = append(m.userPayloads[userID], n)
}

const (
	testBotToken = "xoxb-test-bot-token"
	testAppToken = "xapp-test-app-token"
)

func setupSlackTestEnv(t *testing.T) (*usecase.AuthUsecase, *realtime.Hub, *mockSender, *echo.Echo) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)
	users := []domain.User{
		{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hash)},
		{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: string(hash)},
	}

	db, err := sqlite.InitDB(filepath.Join(t.TempDir(), "slack_test.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureUserChannels(users); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	hub := realtime.NewHub()
	sender := &mockSender{}
	authUC := usecase.NewAuthUsecase([]byte(strings.Repeat("k", 32)), users)
	chatUC := usecase.NewChatUsecase(db, db, hub, hub, sender)
	alertUC := usecase.NewAlertUsecase(db, db, hub, sender, realtime.NewLocalLocker())
	notifyUC := usecase.NewNotificationUsecase(db, hub, sender)
	subUC := usecase.NewSubscriptionUsecase("", db, sender)
	socketUC := usecase.NewSocketUsecase(hub, testBotToken, testAppToken)
	slackH := slackapi.NewHandler("8080", chatUC, authUC, socketUC)
	chatH := httpadapter.NewChatHandler(chatUC)
	webhookH := httpadapter.NewWebhookHandler(alertUC, notifyUC, subUC)

	e := echo.New()
	e.POST("/slack/api/:method", slackH.DispatchMethod)
	e.GET("/slack/socket", slackH.SocketModeWS)
	e.POST("/webhook/alertmanager", webhookH.AlertmanagerWebhook)
	e.POST("/webhook/generic", webhookH.GenericWebhook)
	e.POST("/webhook/keel", webhookH.KeelWebhook)

	api := e.Group("/api")
	api.Use(httpadapter.RequireSessionMiddleware(authUC))
	api.POST("/channels/:id/messages", chatH.PostMessage)
	api.GET("/channels/:id/messages", chatH.ListMessages)

	return authUC, hub, sender, e
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
	authUC, _, sender, e := setupSlackTestEnv(t)

	t.Run("認証エラーとunknown_method", func(t *testing.T) {
		resNoAuth := callSlackMethod(t, e, "auth.test", "xoxb-wrong", map[string]interface{}{})
		if resNoAuth["ok"] != false || resNoAuth["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth, got %+v", resNoAuth)
		}

		resOpenBad := callSlackMethod(t, e, "apps.connections.open", testBotToken, map[string]interface{}{})
		if resOpenBad["ok"] != false || resOpenBad["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth when using bot token for connections.open, got %+v", resOpenBad)
		}

		resUnknown := callSlackMethod(t, e, "emoji.list", testBotToken, map[string]interface{}{})
		if resUnknown["ok"] != false || resUnknown["error"] != "unknown_method" {
			t.Fatalf("expected unknown_method, got %+v", resUnknown)
		}
	})

	t.Run("auth.test, conversations.*, chat.postMessage/update, users.info", func(t *testing.T) {
		authRes := callSlackMethod(t, e, "auth.test", testBotToken, map[string]interface{}{})
		if authRes["ok"] != true || authRes["bot_id"] != domain.SlackBotID || authRes["user_id"] != domain.SlackBotUserID {
			t.Fatalf("unexpected auth.test: %+v", authRes)
		}

		// conversations.list (form-urlencoded でも動くこと)
		listForm := url.Values{}
		listForm.Set("types", "public_channel,im")
		listRes := callSlackMethod(t, e, "conversations.list", testBotToken, listForm)
		if listRes["ok"] != true {
			t.Fatalf("conversations.list failed: %+v", listRes)
		}
		channels := listRes["channels"].([]interface{})
		if len(channels) < 6 { // 5 public + 2 DM
			t.Fatalf("expected >= 6 channels, got %d", len(channels))
		}

		// conversations.info
		infoRes := callSlackMethod(t, e, "conversations.info", testBotToken, map[string]string{"channel": domain.ChannelAlertmanager})
		if infoRes["ok"] != true {
			t.Fatalf("conversations.info failed: %+v", infoRes)
		}

		// chat.postMessage (親 + スレッド返信 + Push 発火)
		postRes := callSlackMethod(t, e, "chat.postMessage", testBotToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"text":    "初期メッセージ",
			"blocks":  []map[string]interface{}{{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": "*初期メッセージ*"}}},
		})
		if postRes["ok"] != true {
			t.Fatalf("chat.postMessage failed: %+v", postRes)
		}
		ts := postRes["ts"].(string)

		replyRes := callSlackMethod(t, e, "chat.postMessage", testBotToken, map[string]interface{}{
			"channel":   domain.ChannelGeneral,
			"thread_ts": ts,
			"text":      "スレッド返信",
		})
		if replyRes["ok"] != true {
			t.Fatalf("chat.postMessage reply failed: %+v", replyRes)
		}

		if len(sender.payloads) != 2 {
			t.Fatalf("expected 2 push payloads from bot posts, got %d", len(sender.payloads))
		}
		if sender.payloads[0].URL != "/channels/"+domain.ChannelGeneral {
			t.Fatalf("unexpected push URL: %s", sender.payloads[0].URL)
		}
		if sender.payloads[1].URL != "/channels/"+domain.ChannelGeneral+"/threads/"+ts {
			t.Fatalf("unexpected thread push URL: %s", sender.payloads[1].URL)
		}

		// chat.update
		updRes := callSlackMethod(t, e, "chat.update", testBotToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"ts":      ts,
			"text":    "更新後メッセージ",
		})
		if updRes["ok"] != true || updRes["text"] != "更新後メッセージ" {
			t.Fatalf("chat.update failed: %+v", updRes)
		}

		// conversations.history
		histRes := callSlackMethod(t, e, "conversations.history", testBotToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
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
		repRes := callSlackMethod(t, e, "conversations.replies", testBotToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"ts":      ts,
		})
		repMsgs := repRes["messages"].([]interface{})
		if len(repMsgs) != 2 {
			t.Fatalf("expected parent + 1 reply in conversations.replies, got %d", len(repMsgs))
		}

		// users.info (人間ユーザーと bot ユーザー)
		uRes := callSlackMethod(t, e, "users.info", testBotToken, map[string]string{"user": "U01ALICE"})
		if uRes["ok"] != true {
			t.Fatalf("users.info failed: %+v", uRes)
		}
		uObj := uRes["user"].(map[string]interface{})
		if uObj["id"] != "U01ALICE" || uObj["is_bot"] != false {
			t.Fatalf("unexpected user object: %+v", uObj)
		}

		// DM への chat.postMessage は該当ユーザー (U01ALICE) の購読にのみ Push 送信すること
		prevBroadcastCount := len(sender.payloads)
		dmPost := callSlackMethod(t, e, "chat.postMessage", testBotToken, map[string]interface{}{
			"channel": domain.DMChannelID("U01ALICE"),
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
		validPost := callSlackMethod(t, e, "chat.postMessage", testBotToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"text":    "正常なメッセージ",
		})
		if validPost["ok"] != true {
			t.Fatalf("valid post failed: %+v", validPost)
		}
		validTS := validPost["ts"].(string)

		// form 経由の壊れた JSON 文字列
		badForm := url.Values{}
		badForm.Set("channel", domain.ChannelGeneral)
		badForm.Set("blocks", `[{"type":"section"`)
		badPostRes := callSlackMethod(t, e, "chat.postMessage", testBotToken, badForm)
		if badPostRes["ok"] != false || badPostRes["error"] != "invalid_blocks" {
			t.Fatalf("expected invalid_blocks for broken JSON form blocks, got %+v", badPostRes)
		}

		// JSON 配列でない値（オブジェクト / 数値）や null 要素・type 欠落要素も拒否されること
		badPayloads := []map[string]interface{}{
			{"channel": domain.ChannelGeneral, "text": "obj", "blocks": map[string]string{"type": "section"}},
			{"channel": domain.ChannelGeneral, "text": "null elem", "blocks": []interface{}{nil}},
			{"channel": domain.ChannelGeneral, "text": "no type", "blocks": []interface{}{map[string]interface{}{}}},
			{"channel": domain.ChannelGeneral, "text": "null ctx", "blocks": []interface{}{map[string]interface{}{"type": "context", "elements": []interface{}{nil}}}},
			{"channel": domain.ChannelGeneral, "text": "null field", "blocks": []interface{}{map[string]interface{}{"type": "section", "fields": []interface{}{nil}}}},
			{"channel": domain.ChannelGeneral, "text": "null text", "blocks": []interface{}{map[string]interface{}{"type": "section", "text": nil}}},
		}
		for idx, payload := range badPayloads {
			badObjRes := callSlackMethod(t, e, "chat.postMessage", testBotToken, payload)
			if badObjRes["ok"] != false || badObjRes["error"] != "invalid_blocks" {
				t.Fatalf("case %d: expected invalid_blocks, got %+v", idx, badObjRes)
			}
		}

		// chat.update でも同様に拒否されること
		badUpdateForm := url.Values{}
		badUpdateForm.Set("channel", domain.ChannelGeneral)
		badUpdateForm.Set("ts", validTS)
		badUpdateForm.Set("blocks", `[{"type":"section"`)
		badUpdRes := callSlackMethod(t, e, "chat.update", testBotToken, badUpdateForm)
		if badUpdRes["ok"] != false || badUpdRes["error"] != "invalid_blocks" {
			t.Fatalf("expected invalid_blocks on chat.update, got %+v", badUpdRes)
		}

		// 既存の人間向けメッセージ一覧 API が 200 を返し続けること
		token, _ := authUC.IssueSessionToken("U01ALICE", time.Now())
		listReq := httptest.NewRequest(http.MethodGet, "/api/channels/"+domain.ChannelGeneral+"/messages", nil)
		listReq.AddCookie(&http.Cookie{Name: usecase.SessionCookieName, Value: token})
		listRec := httptest.NewRecorder()
		e.ServeHTTP(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("expected 200 on /api/channels/%s/messages, got %d: %s", domain.ChannelGeneral, listRec.Code, listRec.Body.String())
		}
	})
}

func TestSocketModeLifecycleAndEvents(t *testing.T) {
	authUC, hub, _, e := setupSlackTestEnv(t)

	srv := httptest.NewServer(e)
	defer srv.Close()

	// 1. apps.connections.open で WS URL 取得
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/slack/api/apps.connections.open", nil)
	req.Header.Set("Authorization", "Bearer "+testAppToken)
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
	token, _ := authUC.IssueSessionToken("U01ALICE", time.Now())
	postReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/channels/"+domain.ChannelGeneral+"/messages", strings.NewReader(`{"text":"@bot ステータス教えて"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.AddCookie(&http.Cookie{Name: usecase.SessionCookieName, Value: token})
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

func postWebhookJSON(t *testing.T, e *echo.Echo, path string, body interface{}) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal webhook body failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook %s returned %d: %s", path, rec.Code, rec.Body.String())
	}
}

func TestSlackAlertAndNotificationMetadata(t *testing.T) {
	_, _, _, e := setupSlackTestEnv(t)

	srv := httptest.NewServer(e)
	defer srv.Close()

	// Socket Mode を接続してリアルタイム配信イベントのメタデータも検証する
	openReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/slack/api/apps.connections.open", nil)
	openReq.Header.Set("Authorization", "Bearer "+testAppToken)
	openResp, err := http.DefaultClient.Do(openReq)
	if err != nil {
		t.Fatalf("apps.connections.open failed: %v", err)
	}
	defer openResp.Body.Close()

	var openRes struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(openResp.Body).Decode(&openRes); err != nil || !openRes.OK {
		t.Fatalf("unexpected apps.connections.open response: %+v, err=%v", openRes, err)
	}

	ws, _, err := websocket.DefaultDialer.Dial(openRes.URL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer ws.Close()

	var hello map[string]interface{}
	if err := ws.ReadJSON(&hello); err != nil {
		t.Fatalf("failed to read hello: %v", err)
	}

	readSocketEvent := func() map[string]interface{} {
		t.Helper()
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		var env realtime.SocketEnvelope
		if err := ws.ReadJSON(&env); err != nil {
			t.Fatalf("failed to read socket envelope: %v", err)
		}
		if err := ws.WriteJSON(map[string]string{"envelope_id": env.EnvelopeID}); err != nil {
			t.Fatalf("failed to send ack: %v", err)
		}
		return env.Payload["event"].(map[string]interface{})
	}

	assertAlertDetails := func(t *testing.T, label string, obj map[string]interface{}, wantFingerprint string) {
		t.Helper()
		raw, ok := obj["details"].(string)
		if !ok || raw == "" {
			t.Fatalf("%s: expected non-empty string details, got %#v", label, obj["details"])
		}
		var detailsObj map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &detailsObj); err != nil {
			t.Fatalf("%s: expected valid JSON details: %v", label, err)
		}
		if detailsObj["fingerprint"] != wantFingerprint {
			t.Fatalf("%s: unexpected fingerprint in details: %+v", label, detailsObj)
		}
	}

	t.Run("Alertmanager アラートの発報・解決とスレッド返信", func(t *testing.T) {
		alertPayload := func(status string) map[string]interface{} {
			return map[string]interface{}{
				"status": status,
				"alerts": []map[string]interface{}{
					{
						"fingerprint": "fp-slack-1",
						"status":      status,
						"labels": map[string]string{
							"alertname": "NodeDown",
							"severity":  "critical",
						},
						"annotations": map[string]string{
							"summary": "Node is unreachable",
						},
						"generatorURL": "http://vm-grafana.local/explore?alert=NodeDown",
					},
				},
			}
		}

		// 1. firing 投入 -> Socket Mode イベントに全メタデータが含まれる
		postWebhookJSON(t, e, "/webhook/alertmanager", alertPayload("firing"))
		firingEv := readSocketEvent()
		if firingEv["url"] != "http://vm-grafana.local/explore?alert=NodeDown" ||
			firingEv["source_kind"] != "alert" ||
			firingEv["alert_status"] != "firing" ||
			firingEv["level"] != "error" {
			t.Fatalf("missing alert metadata in socket event: %+v", firingEv)
		}
		assertAlertDetails(t, "socket event", firingEv, "fp-slack-1")

		// 2. conversations.history で firing アラートのメタデータが返る
		histRes := callSlackMethod(t, e, "conversations.history", testBotToken, map[string]interface{}{
			"channel": domain.ChannelAlertmanager,
		})
		histMsgs := histRes["messages"].([]interface{})
		if len(histMsgs) != 1 {
			t.Fatalf("expected 1 alert message in history, got %d", len(histMsgs))
		}
		root := histMsgs[0].(map[string]interface{})
		rootTS := root["ts"].(string)
		if root["url"] != "http://vm-grafana.local/explore?alert=NodeDown" ||
			root["source_kind"] != "alert" ||
			root["alert_status"] != "firing" ||
			root["level"] != "error" {
			t.Fatalf("missing alert metadata in conversations.history: %+v", root)
		}
		assertAlertDetails(t, "conversations.history", root, "fp-slack-1")

		// 3. resolved 投入 -> Socket Mode に解決イベントが届く
		postWebhookJSON(t, e, "/webhook/alertmanager", alertPayload("resolved"))
		resolvedEv := readSocketEvent()
		if resolvedEv["source_kind"] != "alert" ||
			resolvedEv["alert_status"] != "resolved" ||
			resolvedEv["level"] != "success" ||
			resolvedEv["thread_ts"] != rootTS {
			t.Fatalf("unexpected resolved socket event: %+v", resolvedEv)
		}
		for _, omitted := range []string{"url", "details"} {
			if _, ok := resolvedEv[omitted]; ok {
				t.Fatalf("resolved reply must omit empty %q: %+v", omitted, resolvedEv)
			}
		}

		// 4. スレッドへ通常の bot 返信を追加（通常投稿はメタデータが出ないことも確認）
		botReply := callSlackMethod(t, e, "chat.postMessage", testBotToken, map[string]interface{}{
			"channel":   domain.ChannelAlertmanager,
			"thread_ts": rootTS,
			"text":      "調査を開始しました",
		})
		if botReply["ok"] != true {
			t.Fatalf("chat.postMessage reply failed: %+v", botReply)
		}
		_ = readSocketEvent()

		// 5. conversations.replies で親アラート（url/details 維持 + resolved/success）と解決返信が返る
		repRes := callSlackMethod(t, e, "conversations.replies", testBotToken, map[string]interface{}{
			"channel": domain.ChannelAlertmanager,
			"ts":      rootTS,
		})
		repMsgs := repRes["messages"].([]interface{})
		if len(repMsgs) != 3 {
			t.Fatalf("expected 3 messages (root + resolved + bot reply) in replies, got %d", len(repMsgs))
		}

		repRoot := repMsgs[0].(map[string]interface{})
		if repRoot["url"] != "http://vm-grafana.local/explore?alert=NodeDown" ||
			repRoot["source_kind"] != "alert" ||
			repRoot["alert_status"] != "resolved" ||
			repRoot["level"] != "success" {
			t.Fatalf("unexpected parent metadata in conversations.replies: %+v", repRoot)
		}
		assertAlertDetails(t, "conversations.replies parent", repRoot, "fp-slack-1")

		repResolved := repMsgs[1].(map[string]interface{})
		if repResolved["source_kind"] != "alert" ||
			repResolved["alert_status"] != "resolved" ||
			repResolved["level"] != "success" {
			t.Fatalf("unexpected resolved reply metadata in conversations.replies: %+v", repResolved)
		}

		repBot := repMsgs[2].(map[string]interface{})
		for _, omitted := range []string{"url", "details", "source_kind", "alert_status", "level"} {
			if _, ok := repBot[omitted]; ok {
				t.Fatalf("normal bot reply must omit %q: %+v", omitted, repBot)
			}
		}
	})

	t.Run("Webhook 通知のメタデータ", func(t *testing.T) {
		postWebhookJSON(t, e, "/webhook/generic", map[string]interface{}{
			"title":   "[ArgoCD] nuage-app synced",
			"body":    "Deployment completed",
			"url":     "https://argocd.example/apps/nuage",
			"level":   "success",
			"details": `{"revision":"abc123"}`,
		})

		notifEv := readSocketEvent()
		if notifEv["url"] != "https://argocd.example/apps/nuage" ||
			notifEv["details"] != `{"revision":"abc123"}` ||
			notifEv["source_kind"] != "notification" ||
			notifEv["level"] != "success" {
			t.Fatalf("missing notification metadata in socket event: %+v", notifEv)
		}
		if _, ok := notifEv["alert_status"]; ok {
			t.Fatalf("notification event must omit empty alert_status: %+v", notifEv)
		}

		histRes := callSlackMethod(t, e, "conversations.history", testBotToken, map[string]interface{}{
			"channel": domain.ChannelArgoCD,
		})
		histMsgs := histRes["messages"].([]interface{})
		if len(histMsgs) != 1 {
			t.Fatalf("expected 1 message in argocd history, got %d", len(histMsgs))
		}
		msg := histMsgs[0].(map[string]interface{})
		msgTS := msg["ts"].(string)
		if msg["url"] != "https://argocd.example/apps/nuage" ||
			msg["details"] != `{"revision":"abc123"}` ||
			msg["source_kind"] != "notification" ||
			msg["level"] != "success" {
			t.Fatalf("missing notification metadata in conversations.history: %+v", msg)
		}
		if _, ok := msg["alert_status"]; ok {
			t.Fatalf("notification history item must omit empty alert_status: %+v", msg)
		}

		repRes := callSlackMethod(t, e, "conversations.replies", testBotToken, map[string]interface{}{
			"channel": domain.ChannelArgoCD,
			"ts":      msgTS,
		})
		repMsgs := repRes["messages"].([]interface{})
		if len(repMsgs) != 1 {
			t.Fatalf("expected 1 message in argocd replies, got %d", len(repMsgs))
		}
		repMsg := repMsgs[0].(map[string]interface{})
		if repMsg["url"] != "https://argocd.example/apps/nuage" ||
			repMsg["details"] != `{"revision":"abc123"}` ||
			repMsg["source_kind"] != "notification" ||
			repMsg["level"] != "success" {
			t.Fatalf("missing notification metadata in conversations.replies: %+v", repMsg)
		}
	})
}

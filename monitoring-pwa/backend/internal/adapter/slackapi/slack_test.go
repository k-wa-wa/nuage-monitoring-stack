package slackapi_test

import (
	"context"
	"encoding/json"
	"errors"
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

type stubBotVerifier struct {
	subs map[string]string
}

func (s stubBotVerifier) Verify(_ context.Context, token string) (string, error) {
	sub, ok := s.subs[token]
	if !ok {
		return "", errors.New("invalid jwt")
	}
	return sub, nil
}

const (
	testNuageJWTToken = "jwt-nuage-bot"
	testEchoJWTToken  = "jwt-echo-bot"
)

func setupSlackTestEnv(t *testing.T) (*usecase.AuthUsecase, *realtime.Hub, *mockSender, *echo.Echo) {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-pass"), bcrypt.MinCost)
	users := []domain.User{
		{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hash)},
		{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: string(hash)},
	}
	bots := []domain.BotIdentity{
		{Sub: "spiffe://nuage/ns/pr-1/sa/echo-bot", UserID: "U02ECHOBOT", BotID: "B02ECHOBOT", Name: "echo-bot", IsDefault: true},
		{Sub: "spiffe://nuage/host/atelier/bot", UserID: "U01BOT", BotID: "B01BOT", Name: "nuage-bot"},
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
	verifier := stubBotVerifier{
		subs: map[string]string{
			testEchoJWTToken:  "spiffe://nuage/ns/pr-1/sa/echo-bot",
			testNuageJWTToken: "spiffe://nuage/host/atelier/bot",
			"jwt-unlisted":    "spiffe://nuage/ns/other-pr/sa/echo-bot",
		},
	}
	authUC := usecase.NewAuthUsecase([]byte(strings.Repeat("k", 32)), users)
	chatUC := usecase.NewChatUsecase(db, db, hub, hub, sender, bots...)
	socketUC := usecase.NewSocketUsecase(hub, verifier, bots)
	slackH := slackapi.NewHandler("8080", chatUC, authUC, socketUC)
	chatH := httpadapter.NewChatHandler(chatUC)

	e := echo.New()
	e.POST("/slack/api/:method", slackH.DispatchMethod)
	e.GET("/slack/socket", slackH.SocketModeWS)

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
		resNoAuth := callSlackMethod(t, e, "auth.test", "jwt-wrong", map[string]interface{}{})
		if resNoAuth["ok"] != false || resNoAuth["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth, got %+v", resNoAuth)
		}

		resUnlisted := callSlackMethod(t, e, "auth.test", "jwt-unlisted", map[string]interface{}{})
		if resUnlisted["ok"] != false || resUnlisted["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth for unlisted sub, got %+v", resUnlisted)
		}

		// form の token パラメータだけでは認可されないこと
		formOnly := url.Values{}
		formOnly.Set("token", testNuageJWTToken)
		resFormOnly := callSlackMethod(t, e, "auth.test", "", formOnly)
		if resFormOnly["ok"] != false || resFormOnly["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth when token passed only in form, got %+v", resFormOnly)
		}

		resOpenBad := callSlackMethod(t, e, "apps.connections.open", "jwt-unlisted", map[string]interface{}{})
		if resOpenBad["ok"] != false || resOpenBad["error"] != "invalid_auth" {
			t.Fatalf("expected invalid_auth for connections.open with unlisted sub, got %+v", resOpenBad)
		}

		resUnknown := callSlackMethod(t, e, "emoji.list", testNuageJWTToken, map[string]interface{}{})
		if resUnknown["ok"] != false || resUnknown["error"] != "unknown_method" {
			t.Fatalf("expected unknown_method, got %+v", resUnknown)
		}
	})

	t.Run("複数botのauth.test, conversations.*, chat.postMessage/update(cant_update_message), users.info", func(t *testing.T) {
		authNuage := callSlackMethod(t, e, "auth.test", testNuageJWTToken, map[string]interface{}{})
		if authNuage["ok"] != true || authNuage["bot_id"] != "B01BOT" || authNuage["user_id"] != "U01BOT" || authNuage["user"] != "nuage-bot" {
			t.Fatalf("unexpected nuage auth.test: %+v", authNuage)
		}

		authEcho := callSlackMethod(t, e, "auth.test", testEchoJWTToken, map[string]interface{}{})
		if authEcho["ok"] != true || authEcho["bot_id"] != "B02ECHOBOT" || authEcho["user_id"] != "U02ECHOBOT" || authEcho["user"] != "echo-bot" {
			t.Fatalf("unexpected echo auth.test: %+v", authEcho)
		}

		// conversations.list (form-urlencoded でも動くこと)
		listForm := url.Values{}
		listForm.Set("types", "public_channel,im")
		listRes := callSlackMethod(t, e, "conversations.list", testNuageJWTToken, listForm)
		if listRes["ok"] != true {
			t.Fatalf("conversations.list failed: %+v", listRes)
		}
		channels := listRes["channels"].([]interface{})
		if len(channels) < 6 {
			t.Fatalf("expected >= 6 channels, got %d", len(channels))
		}

		// conversations.info
		infoRes := callSlackMethod(t, e, "conversations.info", testNuageJWTToken, map[string]string{"channel": domain.ChannelAlertmanager})
		if infoRes["ok"] != true {
			t.Fatalf("conversations.info failed: %+v", infoRes)
		}

		// chat.postMessage (親 + スレッド返信 + Push 発火)
		postRes := callSlackMethod(t, e, "chat.postMessage", testNuageJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"text":    "初期メッセージ",
			"blocks":  []map[string]interface{}{{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": "*初期メッセージ*"}}},
		})
		if postRes["ok"] != true {
			t.Fatalf("chat.postMessage failed: %+v", postRes)
		}
		ts := postRes["ts"].(string)

		replyRes := callSlackMethod(t, e, "chat.postMessage", testEchoJWTToken, map[string]interface{}{
			"channel":   domain.ChannelGeneral,
			"thread_ts": ts,
			"text":      "スレッド返信",
		})
		if replyRes["ok"] != true {
			t.Fatalf("chat.postMessage reply failed: %+v", replyRes)
		}
		replyMsg := replyRes["message"].(map[string]interface{})
		if replyMsg["bot_id"] != "B02ECHOBOT" || replyMsg["user"] != "U02ECHOBOT" || replyMsg["username"] != "echo-bot" {
			t.Fatalf("unexpected echo-bot reply message metadata: %+v", replyMsg)
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

		// chat.update: 自分の投稿は更新成功
		updRes := callSlackMethod(t, e, "chat.update", testNuageJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"ts":      ts,
			"text":    "更新後メッセージ",
		})
		if updRes["ok"] != true || updRes["text"] != "更新後メッセージ" {
			t.Fatalf("chat.update failed: %+v", updRes)
		}

		// chat.update: 他 bot の投稿は cant_update_message で拒否
		cantUpdOther := callSlackMethod(t, e, "chat.update", testEchoJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"ts":      ts,
			"text":    "他botの改ざん",
		})
		if cantUpdOther["ok"] != false || cantUpdOther["error"] != "cant_update_message" {
			t.Fatalf("expected cant_update_message for other bot's message, got %+v", cantUpdOther)
		}

		// 人間の投稿に対する chat.update も cant_update_message で拒否
		humanToken, _ := authUC.IssueSessionToken("U01ALICE", time.Now())
		humanReq := httptest.NewRequest(http.MethodPost, "/api/channels/"+domain.ChannelGeneral+"/messages", strings.NewReader(`{"text":"human msg"}`))
		humanReq.Header.Set("Content-Type", "application/json")
		humanReq.AddCookie(&http.Cookie{Name: usecase.SessionCookieName, Value: humanToken})
		humanRec := httptest.NewRecorder()
		e.ServeHTTP(humanRec, humanReq)
		var humanPost struct {
			Message struct {
				TS string `json:"ts"`
			} `json:"message"`
		}
		_ = json.Unmarshal(humanRec.Body.Bytes(), &humanPost)

		cantUpdHuman := callSlackMethod(t, e, "chat.update", testNuageJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"ts":      humanPost.Message.TS,
			"text":    "人間投稿の改ざん",
		})
		if cantUpdHuman["ok"] != false || cantUpdHuman["error"] != "cant_update_message" {
			t.Fatalf("expected cant_update_message for human message, got %+v", cantUpdHuman)
		}

		// conversations.history
		histRes := callSlackMethod(t, e, "conversations.history", testNuageJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
		})
		msgs := histRes["messages"].([]interface{})
		if len(msgs) != 2 {
			t.Fatalf("expected 2 root messages in history, got %d", len(msgs))
		}
		parentMsg := msgs[1].(map[string]interface{})
		if parentMsg["text"] != "更新後メッセージ" || parentMsg["reply_count"].(float64) != 1 {
			t.Fatalf("unexpected parent message in history: %+v", parentMsg)
		}

		// conversations.replies
		repRes := callSlackMethod(t, e, "conversations.replies", testNuageJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"ts":      ts,
		})
		repMsgs := repRes["messages"].([]interface{})
		if len(repMsgs) != 2 {
			t.Fatalf("expected parent + 1 reply in conversations.replies, got %d", len(repMsgs))
		}

		// users.info (人間ユーザー・両 bot・システム bot)
		uRes := callSlackMethod(t, e, "users.info", testNuageJWTToken, map[string]string{"user": "U01ALICE"})
		if uRes["ok"] != true || uRes["user"].(map[string]interface{})["is_bot"] != false {
			t.Fatalf("unexpected human user object: %+v", uRes)
		}
		echoInfo := callSlackMethod(t, e, "users.info", testNuageJWTToken, map[string]string{"user": "U02ECHOBOT"})
		if echoInfo["ok"] != true || echoInfo["user"].(map[string]interface{})["name"] != "echo-bot" {
			t.Fatalf("unexpected echo-bot users.info: %+v", echoInfo)
		}
		sysInfo := callSlackMethod(t, e, "users.info", testNuageJWTToken, map[string]string{"user": domain.SystemBotUserID})
		if sysInfo["ok"] != true || sysInfo["user"].(map[string]interface{})["name"] != domain.SystemBotName {
			t.Fatalf("unexpected system bot users.info: %+v", sysInfo)
		}

		// DM への chat.postMessage は該当ユーザー (U01ALICE) の購読にのみ Push 送信すること
		prevBroadcastCount := len(sender.payloads)
		dmPost := callSlackMethod(t, e, "chat.postMessage", testNuageJWTToken, map[string]interface{}{
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
		validPost := callSlackMethod(t, e, "chat.postMessage", testNuageJWTToken, map[string]interface{}{
			"channel": domain.ChannelGeneral,
			"text":    "正常なメッセージ",
		})
		if validPost["ok"] != true {
			t.Fatalf("valid post failed: %+v", validPost)
		}
		validTS := validPost["ts"].(string)

		badForm := url.Values{}
		badForm.Set("channel", domain.ChannelGeneral)
		badForm.Set("blocks", `[{"type":"section"`)
		badPostRes := callSlackMethod(t, e, "chat.postMessage", testNuageJWTToken, badForm)
		if badPostRes["ok"] != false || badPostRes["error"] != "invalid_blocks" {
			t.Fatalf("expected invalid_blocks for broken JSON form blocks, got %+v", badPostRes)
		}

		badPayloads := []map[string]interface{}{
			{"channel": domain.ChannelGeneral, "text": "obj", "blocks": map[string]string{"type": "section"}},
			{"channel": domain.ChannelGeneral, "text": "null elem", "blocks": []interface{}{nil}},
			{"channel": domain.ChannelGeneral, "text": "no type", "blocks": []interface{}{map[string]interface{}{}}},
			{"channel": domain.ChannelGeneral, "text": "null ctx", "blocks": []interface{}{map[string]interface{}{"type": "context", "elements": []interface{}{nil}}}},
			{"channel": domain.ChannelGeneral, "text": "null field", "blocks": []interface{}{map[string]interface{}{"type": "section", "fields": []interface{}{nil}}}},
			{"channel": domain.ChannelGeneral, "text": "null text", "blocks": []interface{}{map[string]interface{}{"type": "section", "text": nil}}},
		}
		for idx, payload := range badPayloads {
			badObjRes := callSlackMethod(t, e, "chat.postMessage", testNuageJWTToken, payload)
			if badObjRes["ok"] != false || badObjRes["error"] != "invalid_blocks" {
				t.Fatalf("case %d: expected invalid_blocks, got %+v", idx, badObjRes)
			}
		}

		badUpdateForm := url.Values{}
		badUpdateForm.Set("channel", domain.ChannelGeneral)
		badUpdateForm.Set("ts", validTS)
		badUpdateForm.Set("blocks", `[{"type":"section"`)
		badUpdRes := callSlackMethod(t, e, "chat.update", testNuageJWTToken, badUpdateForm)
		if badUpdRes["ok"] != false || badUpdRes["error"] != "invalid_blocks" {
			t.Fatalf("expected invalid_blocks on chat.update, got %+v", badUpdRes)
		}

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

	// 1. 静的トークン (xapp-...) では /slack/socket に繋がらないこと
	if _, resp, err := websocket.DefaultDialer.Dial(strings.Replace(srv.URL, "http://", "ws://", 1)+"/slack/socket", http.Header{
		"Authorization": []string{"Bearer xapp-static"},
	}); err == nil || (resp != nil && resp.StatusCode != http.StatusUnauthorized) {
		t.Fatalf("expected 401 when connecting without ticket, resp=%v err=%v", resp, err)
	}

	// 2. apps.connections.open で echo-bot と nuage-bot の WS URL を取得
	openSocket := func(jwtTok string) *websocket.Conn {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/slack/api/apps.connections.open", nil)
		req.Header.Set("Authorization", "Bearer "+jwtTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("apps.connections.open request failed: %v", err)
		}
		defer resp.Body.Close()

		var openRes struct {
			OK  bool   `json:"ok"`
			URL string `json:"url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&openRes); err != nil || !openRes.OK || !strings.HasPrefix(openRes.URL, "ws://") {
			t.Fatalf("unexpected apps.connections.open response: %+v, err=%v", openRes, err)
		}

		ws, _, err := websocket.DefaultDialer.Dial(openRes.URL, nil)
		if err != nil {
			t.Fatalf("websocket dial failed: %v", err)
		}
		var hello map[string]interface{}
		if err := ws.ReadJSON(&hello); err != nil || hello["type"] != "hello" {
			t.Fatalf("failed to read hello: %+v err=%v", hello, err)
		}
		return ws
	}

	wsEcho := openSocket(testEchoJWTToken)
	defer wsEcho.Close()
	wsNuage := openSocket(testNuageJWTToken)
	defer wsNuage.Close()

	if !hub.IsBotOnline() {
		t.Fatal("expected bot to be online after hello")
	}

	// 3. 人間ユーザーとしてチャンネルに @bot (default: echo-bot) メンション投稿
	//    -> 両方に message が届き、echo-bot にだけ app_mention が届く
	token, _ := authUC.IssueSessionToken("U01ALICE", time.Now())
	postReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/channels/"+domain.ChannelGeneral+"/messages", strings.NewReader(`{"text":"@bot ステータス教えて"}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.AddCookie(&http.Cookie{Name: usecase.SessionCookieName, Value: token})
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil || postResp.StatusCode != http.StatusCreated {
		t.Fatalf("user post failed: status=%v err=%v", postResp, err)
	}
	postResp.Body.Close()

	seenEcho := map[string]string{}
	for i := 0; i < 2; i++ {
		_ = wsEcho.SetReadDeadline(time.Now().Add(3 * time.Second))
		var env realtime.SocketEnvelope
		if err := wsEcho.ReadJSON(&env); err != nil {
			t.Fatalf("failed to read echo envelope %d: %v", i, err)
		}
		ev := env.Payload["event"].(map[string]interface{})
		seenEcho[ev["type"].(string)] = env.EnvelopeID
		if err := wsEcho.WriteJSON(map[string]string{"envelope_id": env.EnvelopeID}); err != nil {
			t.Fatalf("failed to send ack: %v", err)
		}
	}
	if seenEcho["message"] == "" || seenEcho["app_mention"] == "" {
		t.Fatalf("expected both message and app_mention for echo-bot, got %+v", seenEcho)
	}

	_ = wsNuage.SetReadDeadline(time.Now().Add(3 * time.Second))
	var nuageEnv realtime.SocketEnvelope
	if err := wsNuage.ReadJSON(&nuageEnv); err != nil {
		t.Fatalf("failed to read nuage envelope: %v", err)
	}
	if nuageEnv.Payload["event"].(map[string]interface{})["type"] != "message" {
		t.Fatalf("expected message for nuage-bot, got %+v", nuageEnv)
	}
	_ = wsNuage.WriteJSON(map[string]string{"envelope_id": nuageEnv.EnvelopeID})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.IsAcked(seenEcho["message"]) && hub.IsAcked(seenEcho["app_mention"]) && hub.IsAcked(nuageEnv.EnvelopeID) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !hub.IsAcked(seenEcho["message"]) || !hub.IsAcked(seenEcho["app_mention"]) || !hub.IsAcked(nuageEnv.EnvelopeID) {
		t.Fatal("expected server to record acks for all envelopes")
	}

	// 4. disconnect 通知を受信できること
	hub.DisconnectAll("test_refresh")
	_ = wsEcho.SetReadDeadline(time.Now().Add(3 * time.Second))
	var disc map[string]interface{}
	if err := wsEcho.ReadJSON(&disc); err != nil {
		t.Fatalf("failed to read disconnect: %v", err)
	}
	if disc["type"] != "disconnect" || disc["reason"] != "test_refresh" {
		t.Fatalf("unexpected disconnect message: %+v", disc)
	}
}

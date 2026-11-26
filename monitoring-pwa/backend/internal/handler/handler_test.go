package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/notify"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

type mockSender struct {
	payloads     []notify.PushPayload
	userPayloads map[string][]notify.PushPayload
}

func (m *mockSender) Broadcast(payload []byte) {
	var p notify.PushPayload
	if err := json.Unmarshal(payload, &p); err == nil {
		m.payloads = append(m.payloads, p)
	}
}

func (m *mockSender) BroadcastToUser(userID string, payload []byte) {
	var p notify.PushPayload
	if err := json.Unmarshal(payload, &p); err == nil {
		if m.userPayloads == nil {
			m.userPayloads = make(map[string][]notify.PushPayload)
		}
		m.userPayloads[userID] = append(m.userPayloads[userID], p)
	}
}

func setupTestDB(t *testing.T) *push.DB {
	t.Helper()
	db, err := push.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func postJSON(t *testing.T, fn func(echo.Context) error, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := fn(echo.New().NewContext(req, rec)); err != nil {
		if he, ok := err.(*echo.HTTPError); ok {
			rec.Code = he.Code
			return rec
		}
		t.Fatalf("%s failed: %v", path, err)
	}
	return rec
}

func TestWebhookHandlers(t *testing.T) {
	t.Run("Alertmanager の発生と解決を 1 本のスレッドにまとめ、Push はスレッドへ飛ぶ", func(t *testing.T) {
		db := setupTestDB(t)
		sender := &mockSender{}
		h := NewWebhookHandler(notify.New(db, nil, sender), sender)

		alert := func(status string) string {
			return `{"status":"` + status + `","alerts":[{"status":"` + status + `",
				"labels":{"alertname":"PodCrashLooping","namespace":"default","severity":"critical"},
				"annotations":{"summary":"Pod is crash looping"},
				"generatorURL":"http://vm-grafana.nuage-monitoring-stack.svc/explore"}]}`
		}
		if rec := postJSON(t, h.AlertmanagerWebhook, "/webhook/alertmanager", alert("firing")); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		msgs, _, _ := db.ListChannelMessagesAsc(push.ChannelAlertmanager, "", 10)
		if len(msgs) != 1 || msgs[0].AlertStatus != "firing" {
			t.Fatalf("expected one firing thread, got %+v", msgs)
		}
		if len(sender.payloads) != 1 || sender.payloads[0].URL != notify.ThreadURL(push.ChannelAlertmanager, msgs[0].TS) ||
			sender.payloads[0].TargetURL != "http://vm-grafana.nuage-monitoring-stack.svc/explore" {
			t.Fatalf("unexpected push: %+v", sender.payloads)
		}

		if rec := postJSON(t, h.AlertmanagerWebhook, "/webhook/alertmanager", alert("resolved")); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		parent, replies, _ := db.GetThreadMessages(push.ChannelAlertmanager, msgs[0].TS)
		if parent.AlertStatus != "resolved" || len(replies) != 1 {
			t.Fatalf("expected resolved thread with one reply, got %+v / %+v", parent, replies)
		}
		if len(sender.payloads) != 1 {
			t.Fatalf("resolved should not push, got %d", len(sender.payloads))
		}
	})

	t.Run("汎用と Keel の通知は送り手のチャンネルに投稿し、Push はスレッドへ飛ぶ", func(t *testing.T) {
		db := setupTestDB(t)
		sender := &mockSender{}
		h := NewWebhookHandler(notify.New(db, nil, sender), sender)

		// source 未指定でも、タイトルから Argo CD と判断する
		g := `{"title":"[ArgoCD] nuage-app synced","body":"Deployment completed","url":"https://argocd.wpcapp.net/applications/nuage","direct_open":true}`
		if rec := postJSON(t, h.GenericWebhook, "/webhook/generic", g); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		k := `{"name":"monitoring-pwa","message":"updated to v1.2.3","type":"update"}`
		if rec := postJSON(t, h.KeelWebhook, "/webhook/keel", k); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		argo, _, _ := db.ListChannelMessagesAsc(push.ChannelArgoCD, "", 10)
		keel, _, _ := db.ListChannelMessagesAsc(push.ChannelKeel, "", 10)
		if len(argo) != 1 || argo[0].SourceKind != "notification" || argo[0].URL != "https://argocd.wpcapp.net/applications/nuage" {
			t.Fatalf("unexpected argocd messages: %+v", argo)
		}
		if len(keel) != 1 || !strings.Contains(keel[0].Text, "updated to v1.2.3") {
			t.Fatalf("unexpected keel messages: %+v", keel)
		}
		if len(sender.payloads) != 2 {
			t.Fatalf("expected 2 pushes, got %d", len(sender.payloads))
		}
		if p := sender.payloads[0]; p.URL != notify.ThreadURL(push.ChannelArgoCD, argo[0].TS) || !p.DirectOpen {
			t.Fatalf("unexpected generic push: %+v", p)
		}
		if p := sender.payloads[1]; p.URL != notify.ThreadURL(push.ChannelKeel, keel[0].TS) || p.DirectOpen {
			t.Fatalf("unexpected keel push: %+v", p)
		}
	})

	t.Run("汎用 Webhook は title と body が無ければ 400", func(t *testing.T) {
		db := setupTestDB(t)
		sender := &mockSender{}
		h := NewWebhookHandler(notify.New(db, nil, sender), sender)
		if rec := postJSON(t, h.GenericWebhook, "/webhook/generic", `{"title":"only title"}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestActivityAPI(t *testing.T) {
	db := setupTestDB(t)
	sender := &mockSender{}
	svc := notify.New(db, nil, sender)
	if err := svc.HandleAlert(notify.IncomingAlert{Status: "firing", Labels: map[string]string{"alertname": "A"}}); err != nil {
		t.Fatalf("HandleAlert failed: %v", err)
	}
	if _, err := svc.PostNotification(notify.Notification{Source: "keel", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("PostNotification failed: %v", err)
	}
	h := NewChatHandler(config.Config{}, db, realtime.NewHub())

	get := func(query string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
		rec := httptest.NewRecorder()
		err := h.Activity(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/activity"+query, nil), rec))
		if he, ok := err.(*echo.HTTPError); ok {
			rec.Code = he.Code
			return rec, nil
		}
		if err != nil {
			t.Fatalf("Activity failed: %v", err)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		return rec, body
	}

	_, body := get("")
	var items []push.ActivityItem
	_ = json.Unmarshal(body["items"], &items)
	if len(items) != 2 || string(body["firing_count"]) != "1" {
		t.Fatalf("expected 2 items and firing_count 1, got %d items / %s", len(items), body["firing_count"])
	}
	_, body = get("?filter=firing")
	_ = json.Unmarshal(body["items"], &items)
	if len(items) != 1 || items[0].Message.AlertStatus != "firing" || items[0].ChannelName != "alertmanager" {
		t.Fatalf("unexpected firing items: %+v", items)
	}
	if rec, _ := get("?filter=bogus"); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown filter, got %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("valid-pass"), bcrypt.MinCost)
	h := NewAuthHandler([]byte(strings.Repeat("k", 32)), []config.UserConfig{
		{ID: "U01ADMIN", DisplayName: "Admin", PassphraseHash: string(hash)},
	})
	now := time.Unix(1700000000, 0)
	h.nowFunc = func() time.Time { return now }

	var waits []time.Duration
	h.sleepFunc = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	e := echo.New()
	send := func(ip, pass string) (int, error) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"`+pass+`"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		err := h.Login(e.NewContext(req, rec))
		if err != nil {
			if httpErr, ok := err.(*echo.HTTPError); ok {
				return httpErr.Code, err
			}
			return 0, err
		}
		return rec.Code, nil
	}

	// 1. 失敗回数が増えるにつれてバックオフ遅延が増加すること (2回までは 0ms)
	for i := 0; i < 6; i++ {
		code, err := send("198.51.100.10", "wrong")
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got code=%d err=%v", i, code, err)
		}
	}
	// 3回目失敗後の4回目(i=3)に500ms、5回目(i=4)に1s、6回目(i=5)に2s が設定されること
	if len(waits) != 3 || waits[0] != 500*time.Millisecond || waits[1] != 1*time.Second || waits[2] != 2*time.Second {
		t.Fatalf("unexpected backoff waits: %v", waits)
	}

	// 2. 失敗が連続した後でも、正規の合言葉を送れば締め出されず 200 でログインできること
	codeOK, errOK := send("198.51.100.10", "valid-pass")
	if errOK != nil || codeOK != http.StatusOK {
		t.Fatalf("expected 200 for valid passphrase after failures, got code=%d err=%v", codeOK, errOK)
	}

	// 3. 待機キューが上限 (loginMaxQueueWait) を超える集中攻撃のみ 429 で弾かれること
	waits = nil
	for i := 0; i < 6; i++ {
		_, _ = send("198.51.100.11", "wrong")
	}
	// 4s 遅延状態のまま時間を進めず連続投入すると待機キュー上限超過で 429 になる
	got429 := false
	for i := 0; i < 6; i++ {
		code, _ := send("198.51.100.11", "wrong")
		if code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("expected 429 when concurrent/burst queue wait exceeds limit")
	}

	// 別 IP は遅延なくログイン可能であること
	waitsBefore := len(waits)
	codeOther, errOther := send("198.51.100.20", "valid-pass")
	if errOther != nil || codeOther != http.StatusOK {
		t.Fatalf("expected 200 from another IP, got code=%d err=%v", codeOther, errOther)
	}
	if len(waits) != waitsBefore {
		t.Fatalf("expected no wait for another IP, got %v", waits[waitsBefore:])
	}
}

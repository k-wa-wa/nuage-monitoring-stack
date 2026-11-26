package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

type mockSender struct {
	payloads     []notificationPayload
	userPayloads map[string][]notificationPayload
}

func (m *mockSender) Broadcast(payload []byte) {
	var p notificationPayload
	if err := json.Unmarshal(payload, &p); err == nil {
		m.payloads = append(m.payloads, p)
	}
}

func (m *mockSender) BroadcastToUser(userID string, payload []byte) {
	var p notificationPayload
	if err := json.Unmarshal(payload, &p); err == nil {
		if m.userPayloads == nil {
			m.userPayloads = make(map[string][]notificationPayload)
		}
		m.userPayloads[userID] = append(m.userPayloads[userID], p)
	}
}

type mockActiveFetcher struct {
	alerts []push.Alert
	err    error
}

func (m *mockActiveFetcher) GetActiveAlerts(ctx context.Context) ([]push.Alert, error) {
	return m.alerts, m.err
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

func TestWebhookHandlers(t *testing.T) {
	t.Run("AlertmanagerWebhookはalertsのみに保存しPushURLに/alerts/:idを設定する", func(t *testing.T) {
		db := setupTestDB(t)
		sender := &mockSender{}
		h := NewWebhookHandler(sender, db)
		e := echo.New()

		body := `{
			"status": "firing",
			"alerts": [{
				"status": "firing",
				"labels": {"alertname": "PodCrashLooping", "namespace": "default", "severity": "critical"},
				"annotations": {"summary": "Pod is crash looping"},
				"generatorURL": "http://vm-grafana.nuage-monitoring-stack.svc/explore"
			}]
		}`
		req := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		if err := h.AlertmanagerWebhook(c); err != nil {
			t.Fatalf("AlertmanagerWebhook failed: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		// notifications テーブルへ二重保存しないこと
		notes, err := db.ListNotifications(10)
		if err != nil {
			t.Fatalf("ListNotifications failed: %v", err)
		}
		if len(notes) != 0 {
			t.Fatalf("expected 0 notifications for alertmanager webhook, got %d", len(notes))
		}

		alerts, err := db.ListAlertHistory(10)
		if err != nil {
			t.Fatalf("ListAlertHistory failed: %v", err)
		}
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert, got %d", len(alerts))
		}
		if len(sender.payloads) != 1 {
			t.Fatalf("expected 1 push payload, got %d", len(sender.payloads))
		}
		if sender.payloads[0].URL != "/alerts/1" {
			t.Fatalf("expected push url /alerts/1, got %s", sender.payloads[0].URL)
		}
		if sender.payloads[0].TargetURL != "http://vm-grafana.nuage-monitoring-stack.svc/explore" {
			t.Fatalf("unexpected target_url: %s", sender.payloads[0].TargetURL)
		}
	})

	t.Run("GenericWebhookとKeelWebhookは/notifications/:idを設定する", func(t *testing.T) {
		db := setupTestDB(t)
		sender := &mockSender{}
		h := NewWebhookHandler(sender, db)
		e := echo.New()

		// Generic Webhook (with url & direct_open)
		gBody := `{"title": "Argo CD Sync", "body": "nuage-app synced", "url": "https://argocd.wpcapp.net/applications/nuage", "source": "argocd", "direct_open": true}`
		gReq := httptest.NewRequest(http.MethodPost, "/webhook/generic", strings.NewReader(gBody))
		gReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		gRec := httptest.NewRecorder()
		if err := h.GenericWebhook(e.NewContext(gReq, gRec)); err != nil {
			t.Fatalf("GenericWebhook failed: %v", err)
		}
		if gRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", gRec.Code)
		}

		// Keel Webhook
		kBody := `{"name": "monitoring-pwa", "message": "updated to v1.2.3", "type": "update"}`
		kReq := httptest.NewRequest(http.MethodPost, "/webhook/keel", strings.NewReader(kBody))
		kReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		kRec := httptest.NewRecorder()
		if err := h.KeelWebhook(e.NewContext(kReq, kRec)); err != nil {
			t.Fatalf("KeelWebhook failed: %v", err)
		}
		if kRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", kRec.Code)
		}

		if len(sender.payloads) != 2 {
			t.Fatalf("expected 2 payloads, got %d", len(sender.payloads))
		}
		if sender.payloads[0].URL != "/notifications/1" || !sender.payloads[0].DirectOpen || sender.payloads[0].TargetURL != "https://argocd.wpcapp.net/applications/nuage" {
			t.Fatalf("unexpected generic push payload: %+v", sender.payloads[0])
		}
		if sender.payloads[1].URL != "/notifications/2" || sender.payloads[1].DirectOpen {
			t.Fatalf("unexpected keel push payload: %+v", sender.payloads[1])
		}
	})
}

func TestUnifiedNotificationsAndDetail(t *testing.T) {
	db := setupTestDB(t)
	e := echo.New()

	// 1) Alert ID=1 を DB に保存（resolved にしておき、ActiveFetcher で firing として返すケースを検証）
	labels := map[string]string{"alertname": "NodeMemoryHigh", "node": "n1", "severity": "warning"}
	fp := push.GenerateFingerprint(labels)
	savedAlert, err := db.UpsertAlert(fp, "NodeMemoryHigh", "resolved", "warning", "Memory high", "desc", "http://vm-grafana.nuage-monitoring-stack.svc/explore", `{"labels":{"alertname":"NodeMemoryHigh","node":"n1"}}`)
	if err != nil {
		t.Fatalf("UpsertAlert failed: %v", err)
	}

	// 2) ID 衝突テストのため notifications にも ID=1 で汎用通知を保存
	noteID, err := db.SaveNotificationWithSource("Argo CD Synced", "Synced revision abc", "https://argocd.wpcapp.net", "success", "details", "argocd")
	if err != nil {
		t.Fatalf("SaveNotificationWithSource failed: %v", err)
	}
	if savedAlert.ID != noteID {
		t.Fatalf("expected colliding IDs for test, got alert=%d note=%d", savedAlert.ID, noteID)
	}

	// 3) 過去の二重保存互換で notifications に紛れ込んだ Alertmanager 通知を再現（統合一覧では除外されること）
	if _, err := db.SaveNotification("[firing] NodeMemoryHigh", "Memory high", "http://vm-grafana/explore", "warning", `{"labels":{"alertname":"NodeMemoryHigh"},"annotations":{"summary":"Memory high"}}`); err != nil {
		t.Fatalf("SaveNotification legacy failed: %v", err)
	}

	// 4) Alertmanager API から同じ fingerprint の active alert が返る
	amFetcher := &mockActiveFetcher{
		alerts: []push.Alert{
			{
				ID:          999999,
				Fingerprint: fp,
				Alertname:   "NodeMemoryHigh",
				Status:      "firing",
				Level:       "warning",
				Summary:     "Memory high (live)",
				FiredAt:     time.Now(),
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			},
		},
	}

	h := NewHistoryHandler(db, amFetcher)
	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	rec := httptest.NewRecorder()
	if err := h.UnifiedNotifications(e.NewContext(req, rec)); err != nil {
		t.Fatalf("UnifiedNotifications failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var items []UnifiedNotification
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// レガシー重複レコードは除外され、NodeMemoryHigh(firing, alert-1) と Argo CD Synced(notice, notification-1) の2件になること
	if len(items) != 2 {
		t.Fatalf("expected 2 unified items, got %d: %+v", len(items), items)
	}
	if items[0].UID != "alert-1" || items[0].Status != "firing" || items[0].ID != 1 {
		t.Fatalf("expected first item to be firing alert-1 merged with DB ID, got %+v", items[0])
	}
	if items[1].UID != "notification-1" || items[1].Status != "notice" || items[1].Source != "argocd" {
		t.Fatalf("expected second item to be notification-1, got %+v", items[1])
	}

	// Alertmanager 接続エラー時も DB 側データで 200 を返すこと
	hErr := NewHistoryHandler(db, &mockActiveFetcher{err: errors.New("connection refused")})
	recErr := httptest.NewRecorder()
	if err := hErr.UnifiedNotifications(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/notifications", nil), recErr)); err != nil {
		t.Fatalf("UnifiedNotifications on AM error failed: %v", err)
	}
	if recErr.Code != http.StatusOK {
		t.Fatalf("expected 200 on AM fallback, got %d", recErr.Code)
	}

	// 個別詳細エンドポイントが alert と notification で衝突しないこと
	recA := httptest.NewRecorder()
	cA := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/notifications/alert/1", nil), recA)
	cA.SetParamNames("kind", "id")
	cA.SetParamValues("alert", "1")
	if err := h.GetNotificationDetail(cA); err != nil {
		t.Fatalf("GetNotificationDetail alert failed: %v", err)
	}
	var detailA UnifiedNotification
	if err := json.Unmarshal(recA.Body.Bytes(), &detailA); err != nil {
		t.Fatalf("unmarshal detailA failed: %v", err)
	}
	if detailA.UID != "alert-1" || detailA.Title != "NodeMemoryHigh" {
		t.Fatalf("unexpected alert detail: %+v", detailA)
	}

	recN := httptest.NewRecorder()
	cN := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/notifications/notification/1", nil), recN)
	cN.SetParamNames("kind", "id")
	cN.SetParamValues("notification", "1")
	if err := h.GetNotificationDetail(cN); err != nil {
		t.Fatalf("GetNotificationDetail notification failed: %v", err)
	}
	var detailN UnifiedNotification
	if err := json.Unmarshal(recN.Body.Bytes(), &detailN); err != nil {
		t.Fatalf("unmarshal detailN failed: %v", err)
	}
	if detailN.UID != "notification-1" || detailN.Title != "Argo CD Synced" {
		t.Fatalf("unexpected notification detail: %+v", detailN)
	}
}

func TestSourceInferenceConsistency(t *testing.T) {
	db := setupTestDB(t)
	sender := &mockSender{}
	wh := NewWebhookHandler(sender, db)
	hh := NewHistoryHandler(db, &mockActiveFetcher{})
	e := echo.New()

	// source 未指定の Argo CD 通知を GenericWebhook に送信
	body := `{"title": "[ArgoCD] nuage-app synced", "body": "Deployment completed", "url": "https://argocd.wpcapp.net"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook/generic", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := wh.GenericWebhook(e.NewContext(req, rec)); err != nil {
		t.Fatalf("GenericWebhook failed: %v", err)
	}

	// すでに "generic" として保存された既存データ相当のレコードも混在させる
	if _, err := db.SaveNotificationWithSource("[ArgoCD] legacy synced", "body", "", "info", "", "generic"); err != nil {
		t.Fatalf("SaveNotificationWithSource failed: %v", err)
	}

	listRec := httptest.NewRecorder()
	if err := hh.UnifiedNotifications(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/notifications", nil), listRec)); err != nil {
		t.Fatalf("UnifiedNotifications failed: %v", err)
	}
	var items []UnifiedNotification
	if err := json.Unmarshal(listRec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for _, it := range items {
		if it.Source != "argocd" {
			t.Fatalf("expected source argocd for %q, got %q", it.Title, it.Source)
		}
	}
}

func TestAlertmanagerEndToEndFingerprintAndOutOfLimitMatch(t *testing.T) {
	db := setupTestDB(t)
	sender := &mockSender{}
	wh := NewWebhookHandler(sender, db)
	e := echo.New()

	// 1) AlertmanagerWebhook でアラート1（古い）を firing として保存
	whBody1 := `{
		"status": "firing",
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "OldAlert", "namespace": "kube-system", "severity": "critical"},
			"annotations": {"summary": "Old alert firing"},
			"generatorURL": "http://vm-grafana.nuage-monitoring-stack.svc/explore"
		}]
	}`
	req1 := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(whBody1))
	req1.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if err := wh.AlertmanagerWebhook(e.NewContext(req1, httptest.NewRecorder())); err != nil {
		t.Fatalf("AlertmanagerWebhook 1 failed: %v", err)
	}

	// 2) より新しい別アラートを2件追加して、OldAlert を limit=1 の範囲外に押し出す
	time.Sleep(5 * time.Millisecond)
	whBody2 := `{
		"status": "resolved",
		"alerts": [
			{"status": "resolved", "labels": {"alertname": "NewResolved1"}, "annotations": {"summary": "ok1"}},
			{"status": "resolved", "labels": {"alertname": "NewResolved2"}, "annotations": {"summary": "ok2"}}
		]
	}`
	req2 := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(whBody2))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if err := wh.AlertmanagerWebhook(e.NewContext(req2, httptest.NewRecorder())); err != nil {
		t.Fatalf("AlertmanagerWebhook 2 failed: %v", err)
	}

	// 3) 実物の alertmanager.Client が Alertmanager API (16進fingerprint・ラベル順違い) を取得
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"fingerprint": "deadbeef12345678",
			"labels": {"severity": "critical", "namespace": "kube-system", "alertname": "OldAlert"},
			"annotations": {"summary": "Old alert firing"},
			"generatorURL": "http://vm-grafana.nuage-monitoring-stack.svc/explore",
			"startsAt": "2026-11-19T00:00:00Z",
			"updatedAt": "2026-11-19T00:05:00Z",
			"status": {"state": "active"}
		}]`))
	}))
	defer ts.Close()

	amClient := alertmanager.NewClient(ts.URL)
	hh := NewHistoryHandler(db, amClient)

	// limit=1 で取得しても OldAlert が DB の ID=1 (uid: "alert-1") と照合されること
	listRec := httptest.NewRecorder()
	if err := hh.UnifiedNotifications(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/notifications?limit=1", nil), listRec)); err != nil {
		t.Fatalf("UnifiedNotifications failed: %v", err)
	}
	var items []UnifiedNotification
	if err := json.Unmarshal(listRec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item with limit=1, got %d", len(items))
	}
	if items[0].UID != "alert-1" || items[0].ID != 1 || items[0].Status != "firing" {
		t.Fatalf("expected active OldAlert to merge with DB record alert-1, got %+v", items[0])
	}
}

func TestAlertmanagerRestartKeepsSingleThread(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart.db")
	db1, err := push.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB 1 failed: %v", err)
	}

	e := echo.New()
	wh1 := NewWebhookHandler(&mockSender{}, db1)
	firingBody := `{
		"status": "firing",
		"alerts": [{
			"status": "firing",
			"labels": {"alertname": "NodeDown", "instance": "n1", "severity": "critical"},
			"annotations": {"summary": "Node n1 is down"}
		}]
	}`
	req1 := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(firingBody))
	req1.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if err := wh1.AlertmanagerWebhook(e.NewContext(req1, httptest.NewRecorder())); err != nil {
		t.Fatalf("firing 1 failed: %v", err)
	}
	_ = db1.Close()

	// Pod 再起動をシミュレート (InitDB が alerts を一律 resolved にする)
	db2, err := push.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB 2 failed: %v", err)
	}
	defer db2.Close()

	wh2 := NewWebhookHandler(&mockSender{}, db2)
	req2 := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(firingBody))
	req2.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if err := wh2.AlertmanagerWebhook(e.NewContext(req2, httptest.NewRecorder())); err != nil {
		t.Fatalf("firing 2 failed: %v", err)
	}

	// 親メッセージが 2 つに分裂せず 1 つのまま維持されること
	parents, _, err := db2.ListChannelMessagesAsc(push.ChannelAlertmanager, "", 20)
	if err != nil {
		t.Fatalf("ListChannelMessagesAsc failed: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("expected 1 parent thread after restart + re-fire, got %d", len(parents))
	}
	if parents[0].AlertStatus != "firing" || parents[0].ReplyCount != 1 {
		t.Fatalf("expected firing parent with 1 reply, got %+v", parents[0])
	}

	// resolved が来たら同じ親スレッドが resolved になること
	resolvedBody := `{
		"status": "resolved",
		"alerts": [{
			"status": "resolved",
			"labels": {"alertname": "NodeDown", "instance": "n1", "severity": "critical"},
			"annotations": {"summary": "Node n1 is down"}
		}]
	}`
	req3 := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(resolvedBody))
	req3.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if err := wh2.AlertmanagerWebhook(e.NewContext(req3, httptest.NewRecorder())); err != nil {
		t.Fatalf("resolved failed: %v", err)
	}

	parents, _, _ = db2.ListChannelMessagesAsc(push.ChannelAlertmanager, "", 20)
	if len(parents) != 1 || parents[0].AlertStatus != "resolved" || parents[0].ReplyCount != 2 {
		t.Fatalf("expected resolved single parent with 2 replies, got %+v", parents)
	}
}

func TestLoginRateLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("valid-pass"), bcrypt.MinCost)
	h := NewAuthHandler([]byte(strings.Repeat("k", 32)), []config.UserConfig{
		{ID: "U01ADMIN", DisplayName: "Admin", PassphraseHash: string(hash)},
	})
	e := echo.New()

	for i := 0; i < loginRateMaxFailures; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"wrong"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.RemoteAddr = "198.51.100.10:12345"
		err := h.Login(e.NewContext(req, httptest.NewRecorder()))
		httpErr, ok := err.(*echo.HTTPError)
		if !ok || httpErr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %v", i, err)
		}
	}

	// 上限到達後は 429
	reqBlocked := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"valid-pass"}`))
	reqBlocked.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	reqBlocked.RemoteAddr = "198.51.100.10:12345"
	err := h.Login(e.NewContext(reqBlocked, httptest.NewRecorder()))
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after rate limit, got %v", err)
	}

	// 別 IP はログイン可能であること
	reqOther := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"valid-pass"}`))
	reqOther.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	reqOther.RemoteAddr = "198.51.100.20:12345"
	recOther := httptest.NewRecorder()
	if err := h.Login(e.NewContext(reqOther, recOther)); err != nil || recOther.Code != http.StatusOK {
		t.Fatalf("expected 200 from another IP, got code=%d err=%v", recOther.Code, err)
	}
}

func TestAlertmanagerWebhookDeduplicatesSameStatusReplies(t *testing.T) {
	db := setupTestDB(t)
	wh := NewWebhookHandler(&mockSender{}, db)
	e := echo.New()

	postAlert := func(status, summary string) {
		body := `{
			"status": "` + status + `",
			"alerts": [{
				"status": "` + status + `",
				"labels": {"alertname": "HighCPU", "instance": "node01", "severity": "warning"},
				"annotations": {"summary": "` + summary + `"}
			}]
		}`
		req := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		if err := wh.AlertmanagerWebhook(e.NewContext(req, httptest.NewRecorder())); err != nil {
			t.Fatalf("AlertmanagerWebhook(%s) failed: %v", status, err)
		}
	}

	// firing を 3 回連続送信してもスレッド返信が増えないこと
	postAlert("firing", "CPU 90%")
	postAlert("firing", "CPU 92%")
	postAlert("firing", "CPU 95%")

	parents, _, err := db.ListChannelMessagesAsc(push.ChannelAlertmanager, "", 20)
	if err != nil {
		t.Fatalf("ListChannelMessagesAsc failed: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("expected 1 parent message, got %d", len(parents))
	}
	if parents[0].ReplyCount != 0 || parents[0].AlertStatus != "firing" {
		t.Fatalf("expected reply_count=0 and firing on repeated firing, got %+v", parents[0])
	}

	// resolved へ遷移したら返信が 1 件だけ付くこと
	postAlert("resolved", "CPU normal")
	// resolved の再送では返信が増えないこと
	postAlert("resolved", "CPU normal")

	parents, _, err = db.ListChannelMessagesAsc(push.ChannelAlertmanager, "", 20)
	if err != nil {
		t.Fatalf("ListChannelMessagesAsc after resolved failed: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("expected 1 parent message after resolved, got %d", len(parents))
	}
	if parents[0].ReplyCount != 1 || parents[0].AlertStatus != "resolved" {
		t.Fatalf("expected reply_count=1 and resolved without duplicate reply, got %+v", parents[0])
	}
}

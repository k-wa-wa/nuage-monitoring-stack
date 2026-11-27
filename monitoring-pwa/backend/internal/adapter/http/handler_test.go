package httpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	httpadapter "github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/adapter/http"
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

func setupTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.InitDB(filepath.Join(t.TempDir(), "test.db"))
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
		alertUC := usecase.NewAlertUsecase(db, db, nil, sender, realtime.NewLocalLocker())
		notifyUC := usecase.NewNotificationUsecase(db, nil, sender)
		subUC := usecase.NewSubscriptionUsecase("pub", db, sender)
		h := httpadapter.NewWebhookHandler(alertUC, notifyUC, subUC)

		alert := func(status string) string {
			return `{"status":"` + status + `","alerts":[{"fingerprint":"f0e1d2c3b4a59687","status":"` + status + `",
				"labels":{"alertname":"PodCrashLooping","namespace":"default","severity":"critical"},
				"annotations":{"summary":"Pod is crash looping"},
				"generatorURL":"http://vm-grafana.nuage-monitoring-stack.svc/explore"}]}`
		}
		if rec := postJSON(t, h.AlertmanagerWebhook, "/webhook/alertmanager", alert("firing")); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		msgs, _, _ := db.ListChannelMessagesAsc(domain.ChannelAlertmanager, "", 10)
		if len(msgs) != 1 || msgs[0].AlertStatus != "firing" {
			t.Fatalf("expected one firing thread, got %+v", msgs)
		}
		if len(sender.payloads) != 1 || sender.payloads[0].URL != domain.ThreadURL(domain.ChannelAlertmanager, msgs[0].TS) ||
			sender.payloads[0].TargetURL != "http://vm-grafana.nuage-monitoring-stack.svc/explore" {
			t.Fatalf("unexpected push: %+v", sender.payloads)
		}

		if rec := postJSON(t, h.AlertmanagerWebhook, "/webhook/alertmanager", alert("resolved")); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		parent, replies, _ := db.GetThreadMessages(domain.ChannelAlertmanager, msgs[0].TS)
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
		alertUC := usecase.NewAlertUsecase(db, db, nil, sender, realtime.NewLocalLocker())
		notifyUC := usecase.NewNotificationUsecase(db, nil, sender)
		subUC := usecase.NewSubscriptionUsecase("pub", db, sender)
		h := httpadapter.NewWebhookHandler(alertUC, notifyUC, subUC)

		// source 未指定でも、タイトルから Argo CD と判断する
		g := `{"title":"[ArgoCD] nuage-app synced","body":"Deployment completed","url":"https://argocd.wpcapp.net/applications/nuage","direct_open":true}`
		if rec := postJSON(t, h.GenericWebhook, "/webhook/generic", g); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		k := `{"name":"monitoring-pwa","message":"updated to v1.2.3","type":"update"}`
		if rec := postJSON(t, h.KeelWebhook, "/webhook/keel", k); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		argo, _, _ := db.ListChannelMessagesAsc(domain.ChannelArgoCD, "", 10)
		keel, _, _ := db.ListChannelMessagesAsc(domain.ChannelKeel, "", 10)
		if len(argo) != 1 || argo[0].SourceKind != "notification" || argo[0].URL != "https://argocd.wpcapp.net/applications/nuage" {
			t.Fatalf("unexpected argocd messages: %+v", argo)
		}
		if len(keel) != 1 || !strings.Contains(keel[0].Text, "updated to v1.2.3") {
			t.Fatalf("unexpected keel messages: %+v", keel)
		}
		if len(sender.payloads) != 2 {
			t.Fatalf("expected 2 pushes, got %d", len(sender.payloads))
		}
		if p := sender.payloads[0]; p.URL != domain.ThreadURL(domain.ChannelArgoCD, argo[0].TS) || !p.DirectOpen {
			t.Fatalf("unexpected generic push: %+v", p)
		}
		if p := sender.payloads[1]; p.URL != domain.ThreadURL(domain.ChannelKeel, keel[0].TS) || p.DirectOpen {
			t.Fatalf("unexpected keel push: %+v", p)
		}
	})

	t.Run("汎用 Webhook は title と body が無ければ 400", func(t *testing.T) {
		db := setupTestDB(t)
		sender := &mockSender{}
		h := httpadapter.NewWebhookHandler(
			usecase.NewAlertUsecase(db, db, nil, sender, realtime.NewLocalLocker()),
			usecase.NewNotificationUsecase(db, nil, sender),
			usecase.NewSubscriptionUsecase("pub", db, sender),
		)
		if rec := postJSON(t, h.GenericWebhook, "/webhook/generic", `{"title":"only title"}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestActivityAPI(t *testing.T) {
	db := setupTestDB(t)
	sender := &mockSender{}
	alertUC := usecase.NewAlertUsecase(db, db, nil, sender, realtime.NewLocalLocker())
	notifyUC := usecase.NewNotificationUsecase(db, nil, sender)
	if err := alertUC.HandleAlert(context.Background(), domain.IncomingAlert{Fingerprint: "0123456789abcdef", Status: "firing", Labels: map[string]string{"alertname": "A"}}); err != nil {
		t.Fatalf("HandleAlert failed: %v", err)
	}
	if _, err := notifyUC.PostNotification(context.Background(), domain.Notification{Source: "keel", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("PostNotification failed: %v", err)
	}
	h := httpadapter.NewActivityHandler(usecase.NewActivityUsecase(db))

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
	var items []httpadapter.ActivityItemDTO
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

type failingActivityRepo struct {
	usecase.MessageRepository
	listErr  error
	countErr error
}

func (f failingActivityRepo) ListActivity(domain.ActivityFilter, string, int) ([]domain.ActivityItem, bool, error) {
	return nil, false, f.listErr
}

func (f failingActivityRepo) CountFiringAlerts() (int, error) {
	return 0, f.countErr
}

func TestActivityAPI_Errors(t *testing.T) {
	cases := []struct {
		name    string
		repo    failingActivityRepo
		wantMsg string
	}{
		{
			name:    "ListActivity failure",
			repo:    failingActivityRepo{listErr: errors.New("db down")},
			wantMsg: "failed to list activity",
		},
		{
			name:    "CountFiringAlerts failure",
			repo:    failingActivityRepo{countErr: errors.New("db down")},
			wantMsg: "failed to count firing alerts",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := httpadapter.NewActivityHandler(usecase.NewActivityUsecase(tc.repo))
			rec := httptest.NewRecorder()
			err := h.Activity(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/activity", nil), rec))
			he, ok := err.(*echo.HTTPError)
			if !ok {
				t.Fatalf("expected *echo.HTTPError, got %T (%v)", err, err)
			}
			if he.Code != http.StatusInternalServerError || he.Message != tc.wantMsg {
				t.Fatalf("got (%d, %v), want (%d, %q)", he.Code, he.Message, http.StatusInternalServerError, tc.wantMsg)
			}
		})
	}
}

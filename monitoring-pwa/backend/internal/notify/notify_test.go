package notify

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

type recordingSender struct {
	mu       sync.Mutex
	payloads []PushPayload
}

func (r *recordingSender) Broadcast(payload []byte) {
	var p PushPayload
	_ = json.Unmarshal(payload, &p)
	r.mu.Lock()
	r.payloads = append(r.payloads, p)
	r.mu.Unlock()
}

func (r *recordingSender) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.payloads)
}

type fakeLister struct {
	alerts []alertmanager.Alert
	err    error
}

func (f fakeLister) ListAlerts(context.Context) ([]alertmanager.Alert, error) { return f.alerts, f.err }

func newService(t *testing.T) (*Service, *push.DB, *recordingSender) {
	t.Helper()
	db, err := push.InitDB(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	sender := &recordingSender{}
	return New(db, nil, sender, NewLocalLocker()), db, sender
}

var diskLabels = map[string]string{"alertname": "DiskFull", "severity": "critical", "instance": "node-1"}

const diskFP = "a1b2c3d4e5f60718"

func diskAlert(status string) IncomingAlert {
	return IncomingAlert{
		Fingerprint:  diskFP,
		Status:       status,
		Labels:       diskLabels,
		Annotations:  map[string]string{"summary": "Disk 95%"},
		GeneratorURL: "http://vmalert/graph",
	}
}

func roots(t *testing.T, db *push.DB) []push.Message {
	t.Helper()
	msgs, _, err := db.ListChannelMessagesAsc(push.ChannelAlertmanager, "", 50)
	if err != nil {
		t.Fatalf("ListChannelMessagesAsc failed: %v", err)
	}
	return msgs
}

func TestAlertLifecycle(t *testing.T) {
	svc, db, sender := newService(t)

	// 発生: スレッドができ、Push はそのスレッドへ飛ぶ
	if err := svc.HandleAlert(context.Background(), diskAlert("firing")); err != nil {
		t.Fatalf("firing failed: %v", err)
	}
	rs := roots(t, db)
	if len(rs) != 1 || rs[0].AlertStatus != "firing" || rs[0].Level != "error" || rs[0].SourceKind != "alert" {
		t.Fatalf("expected one firing thread, got %+v", rs)
	}
	thread := rs[0].TS
	if sender.count() != 1 || sender.payloads[0].URL != ThreadURL(push.ChannelAlertmanager, thread) {
		t.Fatalf("push should point to the thread, got %+v", sender.payloads)
	}

	// 再通知 (repeat_interval): 同じスレッドのまま、返信は増やさない
	if err := svc.HandleAlert(context.Background(), diskAlert("firing")); err != nil {
		t.Fatalf("repeat firing failed: %v", err)
	}
	if rs := roots(t, db); len(rs) != 1 || rs[0].ReplyCount != 0 {
		t.Fatalf("repeat firing should keep one thread without replies, got %+v", rs)
	}

	// 解決: 返信が付き、親が解決済みになる。Push は送らない
	if err := svc.HandleAlert(context.Background(), diskAlert("resolved")); err != nil {
		t.Fatalf("resolved failed: %v", err)
	}
	parent, replies, err := db.GetThreadMessages(push.ChannelAlertmanager, thread)
	if err != nil {
		t.Fatalf("GetThreadMessages failed: %v", err)
	}
	if parent.AlertStatus != "resolved" || parent.ResolvedAt == nil || len(replies) != 1 || replies[0].AlertStatus != "resolved" {
		t.Fatalf("expected resolved thread with one reply, got parent=%+v replies=%+v", parent, replies)
	}
	if sender.count() != 2 {
		t.Fatalf("resolved should not push, got %d pushes", sender.count())
	}

	// 解決済みの後に届いた resolved は捨てる
	if err := svc.HandleAlert(context.Background(), diskAlert("resolved")); err != nil {
		t.Fatalf("late resolved failed: %v", err)
	}
	if _, replies, _ := db.GetThreadMessages(push.ChannelAlertmanager, thread); len(replies) != 1 {
		t.Fatalf("late resolved should be ignored, got %d replies", len(replies))
	}

	// 2 回目の発生は新しいスレッドになり、Push はその回のスレッドへ飛ぶ
	if err := svc.HandleAlert(context.Background(), diskAlert("firing")); err != nil {
		t.Fatalf("second firing failed: %v", err)
	}
	rs = roots(t, db)
	if len(rs) != 2 || rs[1].TS == thread || rs[1].AlertStatus != "firing" {
		t.Fatalf("second occurrence should open a new thread, got %+v", rs)
	}
	last := sender.payloads[len(sender.payloads)-1]
	if last.URL != ThreadURL(push.ChannelAlertmanager, rs[1].TS) {
		t.Fatalf("push for second occurrence should point to its own thread, got %s", last.URL)
	}
}

func TestReconcile(t *testing.T) {
	ctx := context.Background()
	fp := diskFP

	t.Run("Alertmanager から消えた発生中のスレッドを解決済みにし、理由を返信に残す", func(t *testing.T) {
		svc, db, _ := newService(t)
		if err := svc.HandleAlert(context.Background(), diskAlert("firing")); err != nil {
			t.Fatalf("firing failed: %v", err)
		}
		if err := svc.Reconcile(ctx, fakeLister{}); err != nil {
			t.Fatalf("Reconcile failed: %v", err)
		}
		rs := roots(t, db)
		parent, replies, _ := db.GetThreadMessages(push.ChannelAlertmanager, rs[0].TS)
		if parent.AlertStatus != "resolved" || len(replies) != 1 || !strings.Contains(replies[0].Text, "解決の通知は届いていない") {
			t.Fatalf("expected stale thread closed with reason, got parent=%+v replies=%+v", parent, replies)
		}
		if st, _ := db.GetAlertState(fp); st == nil || st.OpenThreadTS != "" {
			t.Fatalf("alert state should be closed, got %+v", st)
		}
	})

	t.Run("Alertmanager にあるのにスレッドが無い発生は、新しいスレッドにして Push を送る", func(t *testing.T) {
		svc, db, sender := newService(t)
		lister := fakeLister{alerts: []alertmanager.Alert{
			{Fingerprint: fp, Alertname: "DiskFull", Labels: diskLabels, Annotations: map[string]string{"summary": "Disk 95%"}},
			// PWA に送らない (route が null) もの・通知が止められているものは拾わない
			{Fingerprint: "fp-watchdog", Alertname: "Watchdog", Labels: map[string]string{"alertname": "Watchdog"}},
			{Fingerprint: "fp-silenced", Alertname: "Silenced", Labels: map[string]string{"alertname": "Silenced"}, Suppressed: true},
		}}
		if err := svc.Reconcile(ctx, lister); err != nil {
			t.Fatalf("Reconcile failed: %v", err)
		}
		rs := roots(t, db)
		if len(rs) != 1 || rs[0].AlertStatus != "firing" || !strings.Contains(rs[0].Text, "取りこぼした") {
			t.Fatalf("expected one missed thread, got %+v", rs)
		}
		if sender.count() != 1 {
			t.Fatalf("expected one push, got %d", sender.count())
		}
		// もう一度突き合わせても増えない
		if err := svc.Reconcile(ctx, lister); err != nil {
			t.Fatalf("second Reconcile failed: %v", err)
		}
		if rs := roots(t, db); len(rs) != 1 {
			t.Fatalf("reconcile should be idempotent, got %d threads", len(rs))
		}
	})

	t.Run("サイレンス中でも Alertmanager にある発生中のスレッドは閉じない", func(t *testing.T) {
		svc, db, _ := newService(t)
		if err := svc.HandleAlert(context.Background(), diskAlert("firing")); err != nil {
			t.Fatalf("firing failed: %v", err)
		}
		lister := fakeLister{alerts: []alertmanager.Alert{{Fingerprint: fp, Alertname: "DiskFull", Labels: diskLabels, Suppressed: true}}}
		if err := svc.Reconcile(ctx, lister); err != nil {
			t.Fatalf("Reconcile failed: %v", err)
		}
		if rs := roots(t, db); rs[0].AlertStatus != "firing" {
			t.Fatalf("silenced alert should stay firing, got %+v", rs[0])
		}
	})

	t.Run("Alertmanager から一覧を取れない時は何も変えない", func(t *testing.T) {
		svc, db, _ := newService(t)
		if err := svc.HandleAlert(context.Background(), diskAlert("firing")); err != nil {
			t.Fatalf("firing failed: %v", err)
		}
		if err := svc.Reconcile(ctx, fakeLister{err: errors.New("down")}); err == nil {
			t.Fatal("expected error to be returned")
		}
		if rs := roots(t, db); rs[0].AlertStatus != "firing" || rs[0].ReplyCount != 0 {
			t.Fatalf("nothing should change on lister error, got %+v", rs[0])
		}
	})
}

func TestHandleAlertRequiresFingerprint(t *testing.T) {
	svc, db, _ := newService(t)
	a := diskAlert("firing")
	a.Fingerprint = ""
	if err := svc.HandleAlert(context.Background(), a); err == nil {
		t.Fatal("expected an error for an alert without fingerprint")
	}
	if rs := roots(t, db); len(rs) != 0 {
		t.Fatalf("nothing should be posted, got %+v", rs)
	}
}

func TestPostNotification(t *testing.T) {
	svc, db, sender := newService(t)
	msg, err := svc.PostNotification(Notification{
		Source: "argocd", Title: "Sync succeeded", Body: "monitoring-pwa", Level: "success", URL: "https://argocd.example/app",
	})
	if err != nil {
		t.Fatalf("PostNotification failed: %v", err)
	}
	if msg.ChannelID != push.ChannelArgoCD || msg.Username != "Argo CD" || msg.SourceKind != "notification" || msg.URL != "https://argocd.example/app" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if sender.count() != 1 || sender.payloads[0].URL != ThreadURL(push.ChannelArgoCD, msg.TS) || sender.payloads[0].TargetURL != "https://argocd.example/app" {
		t.Fatalf("unexpected push: %+v", sender.payloads)
	}
	items, _, _ := db.ListActivity(push.ActivityNotice, "", 10)
	if len(items) != 1 || items[0].Message.TS != msg.TS {
		t.Fatalf("notification should appear in activity, got %+v", items)
	}
}

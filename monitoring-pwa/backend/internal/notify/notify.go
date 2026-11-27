// Package notify はアラート・通知を #チャンネルのメッセージとして投稿し、画面 (SSE)・bot (Socket Mode)・
// Web Push へ知らせる。アラートの中身はスレッドの親メッセージを正とし、Alertmanager との突き合わせもここで行う。
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

// PushSender は Web Push を送る (push.Sender が満たす)。
type PushSender interface {
	Broadcast(payload []byte)
}

// AlertLister は Alertmanager に今あるアラートを返す (alertmanager.Client が満たす)。
type AlertLister interface {
	ListAlerts(ctx context.Context) ([]alertmanager.Alert, error)
}

// ignoredAlertnames は Alertmanager の route で PWA に送らないアラートである
// (k8s/base/helm/victoria-stack-custom.yaml の "null" receiver と揃える)。
// 突き合わせで「取りこぼした発生」を作る時に、これらを拾わないようにする。
var ignoredAlertnames = map[string]bool{"Watchdog": true, "InfoInhibitor": true}

// Locker は key ごとの排他である。アラートの状態遷移 (Webhook と突き合わせ) を 1 つずつ処理し、
// 同じ回のスレッドを 2 本作らないようにする。今は Pod の中の排他 (LocalLocker) で、
// Pod を増やす時は Postgres の advisory lock などの Pod をまたぐ排他に差し替える。
type Locker interface {
	Lock(ctx context.Context, key string) (unlock func(), err error)
}

// LocalLocker は Pod の中だけで効く Locker である。
type LocalLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewLocalLocker は LocalLocker を作る。
func NewLocalLocker() *LocalLocker {
	return &LocalLocker{locks: map[string]*sync.Mutex{}}
}

// Lock は key の排他を取る。
func (l *LocalLocker) Lock(_ context.Context, key string) (func(), error) {
	l.mu.Lock()
	m, ok := l.locks[key]
	if !ok {
		m = &sync.Mutex{}
		l.locks[key] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock, nil
}

// alertsLockKey はアラートの状態遷移すべてで共有する排他の鍵である。
// Webhook (1 件) と突き合わせ (全件) が同じアラートを同時に触らないよう、細かく分けずに 1 つにしている。
const alertsLockKey = "notify:alerts"

// Service はアラート・通知の投稿をまとめる。
type Service struct {
	db     *push.DB
	hub    *realtime.Hub
	sender PushSender
	locker Locker
}

// New は Service を作る。hub が nil なら画面・bot へは知らせない (テスト用)。
func New(db *push.DB, hub *realtime.Hub, sender PushSender, locker Locker) *Service {
	return &Service{db: db, hub: hub, sender: sender, locker: locker}
}

// IncomingAlert は Alertmanager の Webhook で届いたアラート 1 件である。
type IncomingAlert struct {
	// Fingerprint は Alertmanager が振るアラートの識別子。発生の回をまたいで同じアラートを指す
	Fingerprint  string            `json:"fingerprint"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	GeneratorURL string            `json:"generatorURL"`
}

type alertView struct {
	fingerprint string
	name        string
	summary     string
	level       string
	details     string
	url         string
}

func viewOf(a IncomingAlert) alertView {
	name := a.Labels["alertname"]
	if name == "" {
		name = "Alert"
	}
	summary := a.Annotations["summary"]
	if summary == "" {
		summary = a.Annotations["description"]
	}
	if summary == "" {
		summary = name
	}
	level := "warning"
	if a.Labels["severity"] == "critical" {
		level = "error"
	}
	details, _ := json.Marshal(a)
	return alertView{
		fingerprint: a.Fingerprint,
		name:        name,
		summary:     summary,
		level:       level,
		details:     string(details),
		url:         a.GeneratorURL,
	}
}

// HandleAlert は Webhook で届いたアラートを、その回のスレッドに反映する。
//   - firing: 発生中のスレッドが無ければ新しく作り、あれば親メッセージの詳細を更新する。どちらも Push を送る
//   - resolved: 発生中のスレッドに「解決した」と返信し、親メッセージを解決済みにする。Push は送らない
func (s *Service) HandleAlert(ctx context.Context, a IncomingAlert) error {
	if strings.TrimSpace(a.Fingerprint) == "" {
		return fmt.Errorf("alert %q has no fingerprint", a.Labels["alertname"])
	}
	v := viewOf(a)
	unlock, err := s.locker.Lock(ctx, alertsLockKey)
	if err != nil {
		return err
	}
	defer unlock()

	st, err := s.db.GetAlertState(v.fingerprint)
	if err != nil {
		return err
	}
	open := st != nil && st.OpenThreadTS != ""

	switch a.Status {
	case "firing":
		if open {
			root, err := s.db.UpdateAlertRoot(push.ChannelAlertmanager, st.OpenThreadTS, push.AlertRootUpdate{
				Status: "firing", Level: v.level, Details: v.details, URL: v.url,
			})
			if err != nil {
				return err
			}
			s.emit(push.ChannelAlertmanager, nil, root)
			s.pushAlert(v, root.TS)
			return nil
		}
		root, err := s.openAlertThread(v, "")
		if err != nil {
			return err
		}
		s.pushAlert(v, root.TS)
		return nil
	case "resolved":
		if !open {
			// 発生中の回が無い (突き合わせで解決済みにした後に届いた、DB を作り直した等)。記録するスレッドが無いので捨てる
			slog.Info("resolved alert without open thread; ignored", "fingerprint", v.fingerprint)
			return nil
		}
		return s.closeAlertThread(v.fingerprint, v.name, st.OpenThreadTS, "アラートが解決した。")
	default:
		return fmt.Errorf("unknown alert status %q", a.Status)
	}
}

func (s *Service) openAlertThread(v alertView, note string) (*push.Message, error) {
	now := time.Now()
	text := fmt.Sprintf("🔥 **[firing] %s**\n%s", v.name, v.summary)
	if note != "" {
		text += "\n\n_" + note + "_"
	}
	root, err := s.db.CreateMessage(push.CreateMessageParams{
		ChannelID:   push.ChannelAlertmanager,
		UserID:      push.SystemBotUserID,
		BotID:       push.SystemBotID,
		Username:    "Alertmanager",
		Text:        text,
		Level:       v.level,
		SourceKind:  "alert",
		Details:     v.details,
		URL:         v.url,
		AlertStatus: "firing",
		FiredAt:     &now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.db.SetAlertOpenThread(v.fingerprint, v.name, root.TS); err != nil {
		return nil, err
	}
	s.emit(push.ChannelAlertmanager, root, nil)
	return root, nil
}

func (s *Service) closeAlertThread(fingerprint, name, threadTS, reason string) error {
	now := time.Now()
	reply, err := s.db.CreateMessage(push.CreateMessageParams{
		ChannelID:   push.ChannelAlertmanager,
		ThreadTS:    threadTS,
		UserID:      push.SystemBotUserID,
		BotID:       push.SystemBotID,
		Username:    "Alertmanager",
		Text:        fmt.Sprintf("✅ **[resolved] %s**\n%s", name, reason),
		Level:       "success",
		SourceKind:  "alert",
		AlertStatus: "resolved",
		ResolvedAt:  &now,
	})
	if err != nil {
		return err
	}
	root, err := s.db.UpdateAlertRoot(push.ChannelAlertmanager, threadTS, push.AlertRootUpdate{
		Status: "resolved", Level: "success", ResolvedAt: &now,
	})
	if err != nil {
		return err
	}
	if err := s.db.SetAlertOpenThread(fingerprint, name, ""); err != nil {
		return err
	}
	s.emit(push.ChannelAlertmanager, reply, root)
	return nil
}

// Reconcile は Alertmanager に今あるアラートと、発生中のスレッドを突き合わせる。
//   - スレッドは発生中なのに Alertmanager に無い: 解決の Webhook を取りこぼしたとみなし、解決済みにする
//   - Alertmanager で発生中 (通知が止められていない) なのにスレッドが無い: 発生の Webhook を取りこぼしたとみなし、スレッドを作る
//
// Alertmanager から一覧を取れなかった時は何も変えずにエラーを返す (全部を解決済みにしてしまわないため)。
func (s *Service) Reconcile(ctx context.Context, lister AlertLister) error {
	current, err := lister.ListAlerts(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]alertmanager.Alert, len(current))
	for _, a := range current {
		present[a.Fingerprint] = a
	}

	unlock, err := s.locker.Lock(ctx, alertsLockKey)
	if err != nil {
		return err
	}
	defer unlock()

	openAlerts, err := s.db.ListOpenAlerts()
	if err != nil {
		return err
	}
	for _, st := range openAlerts {
		if _, ok := present[st.Fingerprint]; ok {
			continue
		}
		if err := s.closeAlertThread(st.Fingerprint, st.Alertname, st.OpenThreadTS,
			"Alertmanager 上で発生中ではなくなった (解決の通知は届いていない)。"); err != nil {
			return err
		}
		slog.Info("reconcile: closed stale alert thread", "fingerprint", st.Fingerprint)
	}

	for _, a := range current {
		if a.Suppressed || ignoredAlertnames[a.Alertname] {
			continue
		}
		st, err := s.db.GetAlertState(a.Fingerprint)
		if err != nil {
			return err
		}
		if st != nil && st.OpenThreadTS != "" {
			continue
		}
		v := viewOf(IncomingAlert{Fingerprint: a.Fingerprint, Status: "firing", Labels: a.Labels, Annotations: a.Annotations, GeneratorURL: a.GeneratorURL})
		root, err := s.openAlertThread(v, "発生の通知を取りこぼしたため、Alertmanager との突き合わせで作成した。")
		if err != nil {
			return err
		}
		s.pushAlert(v, root.TS)
		slog.Info("reconcile: opened missed alert thread", "fingerprint", a.Fingerprint)
	}
	return nil
}

// RunReconciler は起動時と interval ごとに Reconcile を行う。ctx が終わるまで戻らない。
func (s *Service) RunReconciler(ctx context.Context, lister AlertLister, interval time.Duration) {
	run := func() {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := s.Reconcile(rctx, lister); err != nil {
			slog.Warn("alert reconcile failed; nothing changed", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// Notification は Argo CD・Keel・汎用 Webhook からの通知 1 件である。
type Notification struct {
	Source     string // argocd / keel / generic
	Title      string
	Body       string
	Level      string
	Details    string
	URL        string // 通知の対象 (Push を押した時に直接開くこともある)
	DirectOpen bool
}

// PostNotification は通知をその送り手のチャンネルに投稿し、Push を送る。
func (s *Service) PostNotification(n Notification) (*push.Message, error) {
	chID := push.ChannelForSource(n.Source)
	username := "Nuage Notify"
	switch n.Source {
	case "argocd":
		username = "Argo CD"
	case "keel":
		username = "Keel"
	}
	level := n.Level
	if level == "" {
		level = "info"
	}
	title := strings.TrimSpace(n.Title)
	body := strings.TrimSpace(n.Body)
	text := fmt.Sprintf("**%s**\n%s", title, body)
	if body == "" || body == title {
		text = fmt.Sprintf("**%s**", title)
	}

	msg, err := s.db.CreateMessage(push.CreateMessageParams{
		ChannelID:  chID,
		UserID:     push.SystemBotUserID,
		BotID:      push.SystemBotID,
		Username:   username,
		Text:       text,
		Level:      level,
		SourceKind: "notification",
		Details:    n.Details,
		URL:        n.URL,
	})
	if err != nil {
		return nil, err
	}
	s.emit(chID, msg, nil)
	s.send(PushPayload{
		Title:      n.Title,
		Body:       n.Body,
		URL:        ThreadURL(chID, msg.TS),
		TargetURL:  n.URL,
		DirectOpen: n.DirectOpen,
		Level:      level,
		Tag:        fmt.Sprintf("msg-%s-%s", chID, msg.TS),
	})
	return msg, nil
}

// PushPayload は Service Worker が受け取る Web Push の中身である。
type PushPayload struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url,omitempty"`
	TargetURL  string `json:"target_url,omitempty"`
	DirectOpen bool   `json:"direct_open,omitempty"`
	Level      string `json:"level,omitempty"`
	Tag        string `json:"tag,omitempty"`
}

// ThreadURL は Push 通知から開くスレッドの URL (画面側でタブの中のパスへ移す)。
func ThreadURL(channelID, ts string) string {
	return fmt.Sprintf("/channels/%s/threads/%s", url.PathEscape(channelID), url.PathEscape(ts))
}

func (s *Service) pushAlert(v alertView, threadTS string) {
	s.send(PushPayload{
		Title:     fmt.Sprintf("[firing] %s", v.name),
		Body:      v.summary,
		URL:       ThreadURL(push.ChannelAlertmanager, threadTS),
		TargetURL: v.url,
		Level:     v.level,
		// 同じ回の再通知 (repeat_interval) は端末上で 1 件にまとめる
		Tag: "alert-" + threadTS,
	})
}

func (s *Service) send(p PushPayload) {
	if s.sender == nil {
		return
	}
	payload, err := json.Marshal(p)
	if err != nil {
		slog.Error("failed to encode push payload", "error", err)
		return
	}
	s.sender.Broadcast(payload)
}

// emit は新しいメッセージ (created) と更新した親メッセージ (updated) を画面と bot へ知らせる。
func (s *Service) emit(channelID string, created, updated *push.Message) {
	if s.hub == nil {
		return
	}
	if created != nil {
		s.hub.PublishBrowser("", realtime.BrowserEvent{Type: "message_created", ChannelID: channelID, Payload: created})
	}
	if updated != nil && (created == nil || updated.TS != created.TS) {
		s.hub.PublishBrowser("", realtime.BrowserEvent{Type: "message_updated", ChannelID: channelID, Payload: updated})
	}
	if created == nil {
		return
	}
	ev := map[string]interface{}{
		"type":         "message",
		"channel":      channelID,
		"channel_type": "channel",
		"user":         created.UserID,
		"bot_id":       created.BotID,
		"text":         created.Text,
		"ts":           created.TS,
		"event_ts":     created.TS,
	}
	if created.ThreadTS != "" {
		ev["thread_ts"] = created.ThreadTS
	}
	s.hub.SendEventCallback(ev)
}

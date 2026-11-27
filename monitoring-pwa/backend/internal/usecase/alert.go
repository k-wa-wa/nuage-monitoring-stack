package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// alertsLockKey はアラートの状態遷移すべてで共有する排他の鍵である。
// Webhook (1 件) と突き合わせ (全件) が同じアラートを同時に触らないよう、細かく分けずに 1 つにしている。
const alertsLockKey = "notify:alerts"

type alertDetailsPayload struct {
	Fingerprint  string            `json:"fingerprint"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	GeneratorURL string            `json:"generatorURL"`
}

// AlertUsecase は Alertmanager Webhook の処理と Alertmanager API との突き合わせを扱う。
type AlertUsecase struct {
	messages MessageRepository
	states   AlertStateRepository
	events   EventPublisher
	push     PushSender
	locker   Locker
}

// NewAlertUsecase は新しい AlertUsecase を生成する。
func NewAlertUsecase(
	messages MessageRepository,
	states AlertStateRepository,
	events EventPublisher,
	push PushSender,
	locker Locker,
) *AlertUsecase {
	return &AlertUsecase{
		messages: messages,
		states:   states,
		events:   events,
		push:     push,
		locker:   locker,
	}
}

var alertChannel = domain.Channel{
	ID:        domain.ChannelAlertmanager,
	Name:      "alertmanager",
	IsChannel: true,
}

// HandleAlert は Webhook で届いたアラートを、その回のスレッドに反映する。
//   - firing: 発生中のスレッドが無ければ新しく作り、あれば親メッセージの詳細を更新する。どちらも Push を送る
//   - resolved: 発生中のスレッドに「解決した」と返信し、親メッセージを解決済みにする。Push は送らない
func (u *AlertUsecase) HandleAlert(ctx context.Context, a domain.IncomingAlert) error {
	if strings.TrimSpace(a.Fingerprint) == "" {
		return fmt.Errorf("alert %q has no fingerprint", a.Labels["alertname"])
	}
	if a.Details == "" {
		b, _ := json.Marshal(alertDetailsPayload{
			Fingerprint:  a.Fingerprint,
			Status:       a.Status,
			Labels:       a.Labels,
			Annotations:  a.Annotations,
			GeneratorURL: a.GeneratorURL,
		})
		a.Details = string(b)
	}
	v := domain.ViewOfAlert(a)
	unlock, err := u.locker.Lock(ctx, alertsLockKey)
	if err != nil {
		return err
	}
	defer unlock()

	st, err := u.states.GetAlertState(v.Fingerprint)
	if err != nil {
		return err
	}
	open := st != nil && st.OpenThreadTS != ""

	switch a.Status {
	case "firing":
		if open {
			root, err := u.messages.UpdateAlertRoot(domain.ChannelAlertmanager, st.OpenThreadTS, domain.AlertRootUpdate{
				Status: "firing", Level: v.Level, Details: v.Details, URL: v.URL,
			})
			if err != nil {
				return err
			}
			pushNotif := alertPushNotification(v, root.TS)
			publishMessageUpdated(ctx, u.events, u.push, alertChannel, *root, &pushNotif)
			return nil
		}
		_, err := u.openAlertThread(ctx, v, "")
		return err
	case "resolved":
		if !open {
			// 発生中の回が無い (突き合わせで解決済みにした後に届いた、DB を作り直した等)。記録するスレッドが無いので捨てる
			slog.Info("resolved alert without open thread; ignored", "fingerprint", v.Fingerprint)
			return nil
		}
		return u.closeAlertThread(ctx, v.Fingerprint, v.Name, st.OpenThreadTS, "アラートが解決した。")
	default:
		return fmt.Errorf("unknown alert status %q", a.Status)
	}
}

func (u *AlertUsecase) openAlertThread(ctx context.Context, v domain.AlertView, note string) (*domain.Message, error) {
	now := time.Now()
	text := fmt.Sprintf("🔥 **[firing] %s**\n%s", v.Name, v.Summary)
	if note != "" {
		text += "\n\n_" + note + "_"
	}
	root, err := u.messages.CreateMessage(domain.CreateMessageParams{
		ChannelID:   domain.ChannelAlertmanager,
		UserID:      domain.SystemBotUserID,
		BotID:       domain.SystemBotID,
		Username:    "Alertmanager",
		Text:        text,
		Level:       v.Level,
		SourceKind:  "alert",
		Details:     v.Details,
		URL:         v.URL,
		AlertStatus: "firing",
		FiredAt:     &now,
	})
	if err != nil {
		return nil, err
	}
	if err := u.states.SetAlertOpenThread(v.Fingerprint, v.Name, root.TS); err != nil {
		return nil, err
	}
	pushNotif := alertPushNotification(v, root.TS)
	publishMessageCreated(ctx, u.events, u.push, alertChannel, *root, &pushNotif)
	return root, nil
}

func (u *AlertUsecase) closeAlertThread(ctx context.Context, fingerprint, name, threadTS, reason string) error {
	now := time.Now()
	reply, err := u.messages.CreateMessage(domain.CreateMessageParams{
		ChannelID:   domain.ChannelAlertmanager,
		ThreadTS:    threadTS,
		UserID:      domain.SystemBotUserID,
		BotID:       domain.SystemBotID,
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
	root, err := u.messages.UpdateAlertRoot(domain.ChannelAlertmanager, threadTS, domain.AlertRootUpdate{
		Status: "resolved", Level: "success", ResolvedAt: &now,
	})
	if err != nil {
		return err
	}
	if err := u.states.SetAlertOpenThread(fingerprint, name, ""); err != nil {
		return err
	}
	publishMessageCreated(ctx, u.events, nil, alertChannel, *reply, nil)
	publishMessageUpdated(ctx, u.events, nil, alertChannel, *root, nil)
	return nil
}

// Reconcile は Alertmanager に今あるアラートと、発生中のスレッドを突き合わせる。
//   - スレッドは発生中なのに Alertmanager に無い: 解決の Webhook を取りこぼしたとみなし、解決済みにする
//   - Alertmanager で発生中 (通知が止められていない) なのにスレッドが無い: 発生の Webhook を取りこぼしたとみなし、スレッドを作る
//
// Alertmanager から一覧を取れなかった時は何も変えずにエラーを返す (全部を解決済みにしてしまわないため)。
func (u *AlertUsecase) Reconcile(ctx context.Context, lister AlertLister) error {
	current, err := lister.ListAlerts(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]domain.ActiveAlert, len(current))
	for _, a := range current {
		present[a.Fingerprint] = a
	}

	unlock, err := u.locker.Lock(ctx, alertsLockKey)
	if err != nil {
		return err
	}
	defer unlock()

	openAlerts, err := u.states.ListOpenAlerts()
	if err != nil {
		return err
	}
	for _, st := range openAlerts {
		if _, ok := present[st.Fingerprint]; ok {
			continue
		}
		if err := u.closeAlertThread(ctx, st.Fingerprint, st.Alertname, st.OpenThreadTS,
			"Alertmanager 上で発生中ではなくなった (解決の通知は届いていない)。"); err != nil {
			return err
		}
		slog.Info("reconcile: closed stale alert thread", "fingerprint", st.Fingerprint)
	}

	for _, a := range current {
		if a.Suppressed || domain.IgnoredAlertnames[a.Alertname] {
			continue
		}
		st, err := u.states.GetAlertState(a.Fingerprint)
		if err != nil {
			return err
		}
		if st != nil && st.OpenThreadTS != "" {
			continue
		}
		detailsBytes, _ := json.Marshal(alertDetailsPayload{
			Fingerprint:  a.Fingerprint,
			Status:       "firing",
			Labels:       a.Labels,
			Annotations:  a.Annotations,
			GeneratorURL: a.GeneratorURL,
		})
		v := domain.ViewOfAlert(domain.IncomingAlert{
			Fingerprint:  a.Fingerprint,
			Status:       "firing",
			Labels:       a.Labels,
			Annotations:  a.Annotations,
			GeneratorURL: a.GeneratorURL,
			Details:      string(detailsBytes),
		})
		if _, err := u.openAlertThread(ctx, v, "発生の通知を取りこぼしたため、Alertmanager との突き合わせで作成した。"); err != nil {
			return err
		}
		slog.Info("reconcile: opened missed alert thread", "fingerprint", a.Fingerprint)
	}
	return nil
}

// RunReconciler は起動時と interval ごとに Reconcile を行う。ctx が終わるまで戻らない。
func (u *AlertUsecase) RunReconciler(ctx context.Context, lister AlertLister, interval time.Duration) {
	run := func() {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := u.Reconcile(rctx, lister); err != nil {
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

func alertPushNotification(v domain.AlertView, threadTS string) domain.PushNotification {
	return domain.PushNotification{
		Title:     fmt.Sprintf("[firing] %s", v.Name),
		Body:      v.Summary,
		URL:       domain.ThreadURL(domain.ChannelAlertmanager, threadTS),
		TargetURL: v.URL,
		Level:     v.Level,
		// 同じ回の再通知 (repeat_interval) は端末上で 1 件にまとめる
		Tag: "alert-" + threadTS,
	}
}

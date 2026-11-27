package realtime

import (
	"testing"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

type spySocketSession struct {
	sent []interface{}
}

func (s *spySocketSession) SendJSON(v interface{}) error {
	s.sent = append(s.sent, v)
	return nil
}

func TestBuildSocketEventMetadata(t *testing.T) {
	ch := domain.Channel{ID: domain.ChannelAlertmanager, IsChannel: true}

	t.Run("通常チャットメッセージではメタデータを出力しない", func(t *testing.T) {
		humanEv := buildSocketEvent("message", ch, domain.Message{
			TS:     "1700000000.000001",
			UserID: "U01ALICE",
			Text:   "hello",
		})
		for _, k := range []string{"url", "details", "source_kind", "alert_status", "level"} {
			if _, ok := humanEv[k]; ok {
				t.Fatalf("unexpected key %q in human chat event: %+v", k, humanEv)
			}
		}

		// 通常ボット投稿 (PostBotMessage) は SourceKind="" で Level="info" が入るが、互換性維持のため level を出さない
		botEv := buildSocketEvent("message", ch, domain.Message{
			TS:     "1700000000.000002",
			UserID: domain.SlackBotUserID,
			BotID:  domain.SlackBotID,
			Text:   "bot reply",
			Level:  "info",
		})
		for _, k := range []string{"url", "details", "source_kind", "alert_status", "level"} {
			if _, ok := botEv[k]; ok {
				t.Fatalf("unexpected key %q in bot chat event: %+v", k, botEv)
			}
		}
	})

	t.Run("Alertmanager アラートでは全メタデータを出力する", func(t *testing.T) {
		ev := buildSocketEvent("message", ch, domain.Message{
			TS:          "1700000000.000003",
			UserID:      domain.SystemBotUserID,
			BotID:       domain.SystemBotID,
			Text:        "firing alert",
			URL:         "http://vm-grafana/explore",
			Details:     `{"fingerprint":"fp-1"}`,
			SourceKind:  "alert",
			AlertStatus: "firing",
			Level:       "error",
		})
		want := map[string]string{
			"url":          "http://vm-grafana/explore",
			"details":      `{"fingerprint":"fp-1"}`,
			"source_kind":  "alert",
			"alert_status": "firing",
			"level":        "error",
		}
		for k, v := range want {
			if ev[k] != v {
				t.Fatalf("ev[%q] = %v, want %q", k, ev[k], v)
			}
		}
	})

	t.Run("Webhook 通知では非空のメタデータのみ出力する", func(t *testing.T) {
		notifCh := domain.Channel{ID: domain.ChannelArgoCD, IsChannel: true}
		ev := buildSocketEvent("message", notifCh, domain.Message{
			TS:         "1700000000.000004",
			UserID:     domain.SystemBotUserID,
			BotID:      domain.SystemBotID,
			Text:       "synced",
			URL:        "https://argocd.example/apps/nuage",
			Details:    `{"app":"nuage"}`,
			SourceKind: "notification",
			Level:      "success",
		})
		if ev["url"] != "https://argocd.example/apps/nuage" ||
			ev["details"] != `{"app":"nuage"}` ||
			ev["source_kind"] != "notification" ||
			ev["level"] != "success" {
			t.Fatalf("unexpected notification event: %+v", ev)
		}
		if _, ok := ev["alert_status"]; ok {
			t.Fatalf("alert_status must be omitted when empty: %+v", ev)
		}
	})
}

func TestPublishMessageCreatedDeliversAlertMetadataOverSocket(t *testing.T) {
	hub := NewHub()
	sess := &spySocketSession{}
	_, cleanup, err := hub.RegisterSocket(sess)
	if err != nil {
		t.Fatalf("RegisterSocket failed: %v", err)
	}
	defer cleanup()

	ch := domain.Channel{ID: domain.ChannelAlertmanager, IsChannel: true}
	delivered := hub.PublishMessageCreated(ch, domain.Message{
		TS:          "1700000000.000010",
		UserID:      domain.SystemBotUserID,
		BotID:       domain.SystemBotID,
		Text:        "firing alert",
		URL:         "http://vmalert/graph",
		Details:     `{"fingerprint":"fp-2"}`,
		SourceKind:  "alert",
		AlertStatus: "firing",
		Level:       "warning",
	})
	if !delivered {
		t.Fatal("expected event to be delivered to connected socket")
	}
	// sent[0] is hello, sent[1] is events_api
	if len(sess.sent) != 2 {
		t.Fatalf("expected 2 frames (hello + event), got %d", len(sess.sent))
	}
	env, ok := sess.sent[1].(SocketEnvelope)
	if !ok {
		t.Fatalf("expected SocketEnvelope, got %T", sess.sent[1])
	}
	ev := env.Payload["event"].(map[string]interface{})
	if ev["url"] != "http://vmalert/graph" ||
		ev["details"] != `{"fingerprint":"fp-2"}` ||
		ev["source_kind"] != "alert" ||
		ev["alert_status"] != "firing" ||
		ev["level"] != "warning" {
		t.Fatalf("unexpected socket envelope event metadata: %+v", ev)
	}
}

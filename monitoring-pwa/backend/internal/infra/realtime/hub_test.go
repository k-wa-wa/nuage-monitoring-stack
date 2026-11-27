package realtime_test

import (
	"sync"
	"testing"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/realtime"
)

type recordingSession struct {
	mu     sync.Mutex
	frames []map[string]interface{}
}

func (r *recordingSession) SendJSON(v interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := v.(map[string]interface{}); ok {
		r.frames = append(r.frames, m)
	} else if env, ok := v.(realtime.SocketEnvelope); ok {
		r.frames = append(r.frames, map[string]interface{}{
			"type":        env.Type,
			"envelope_id": env.EnvelopeID,
			"payload":     env.Payload,
		})
	}
	return nil
}

func (r *recordingSession) events() []map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]interface{}
	for _, f := range r.frames {
		if f["type"] == "events_api" {
			payload := f["payload"].(map[string]interface{})
			out = append(out, payload["event"].(map[string]interface{}))
		}
	}
	return out
}

func TestHubPerBotRoutingAndMentions(t *testing.T) {
	hub := realtime.NewHub()
	botEcho := domain.BotIdentity{Sub: "spiffe://nuage/ns/pr-1/sa/echo-bot", UserID: "U02ECHOBOT", BotID: "B02ECHOBOT", Name: "echo-bot"}
	botNuage := domain.BotIdentity{Sub: "spiffe://nuage/host/atelier/bot", UserID: "U01BOT", BotID: "B01BOT", Name: "nuage-bot"}

	echoSess1 := &recordingSession{}
	echoSess2 := &recordingSession{}
	nuageSess := &recordingSession{}

	_, cleanEcho1, err := hub.RegisterSocket(botEcho, echoSess1)
	if err != nil {
		t.Fatalf("RegisterSocket echo1 failed: %v", err)
	}
	defer cleanEcho1()
	_, cleanEcho2, err := hub.RegisterSocket(botEcho, echoSess2)
	if err != nil {
		t.Fatalf("RegisterSocket echo2 failed: %v", err)
	}
	defer cleanEcho2()
	_, cleanNuage, err := hub.RegisterSocket(botNuage, nuageSess)
	if err != nil {
		t.Fatalf("RegisterSocket nuage failed: %v", err)
	}
	defer cleanNuage()

	ch := domain.Channel{ID: domain.ChannelGeneral, Name: "general", IsChannel: true}

	// 1. echo-bot のみメンションした人間投稿:
	//    - message は echo-bot に 1 本、nuage-bot に 1 本届く
	//    - app_mention は echo-bot にだけ 1 本届き、nuage-bot には届かない
	delivered := hub.PublishMessageCreated(ch, domain.Message{
		ChannelID: ch.ID,
		TS:        "1700000001.000001",
		UserID:    "U01ALICE",
		Text:      "hello <@U02ECHOBOT>",
	})
	if !delivered {
		t.Fatal("expected delivered=true")
	}

	echoTotal := append(echoSess1.events(), echoSess2.events()...)
	if len(echoTotal) != 2 {
		t.Fatalf("expected 2 events total across echo-bot connections (message + app_mention), got %d: %+v", len(echoTotal), echoTotal)
	}
	echoTypes := map[string]int{}
	for _, ev := range echoTotal {
		echoTypes[ev["type"].(string)]++
	}
	if echoTypes["message"] != 1 || echoTypes["app_mention"] != 1 {
		t.Fatalf("expected 1 message and 1 app_mention for echo-bot, got %+v", echoTypes)
	}

	nuageEvents := nuageSess.events()
	if len(nuageEvents) != 1 || nuageEvents[0]["type"] != "message" {
		t.Fatalf("expected only 1 message event for nuage-bot, got %+v", nuageEvents)
	}

	// 2. 同一 bot に複数本接続がある場合、ラウンドロビンで 1 本ずつに分散し両方へ二重送信しない
	//    さらに bot 投稿時はメンションを含んでも app_mention を生成しない
	prevEcho1 := len(echoSess1.events())
	prevEcho2 := len(echoSess2.events())
	for i := 0; i < 4; i++ {
		hub.PublishMessageCreated(ch, domain.Message{
			ChannelID: ch.ID,
			TS:        "1700000002.00000" + string(rune('1'+i)),
			UserID:    botNuage.UserID,
			BotID:     botNuage.BotID,
			Text:      "bot reply <@U02ECHOBOT>",
		})
	}
	gotEcho1 := len(echoSess1.events()) - prevEcho1
	gotEcho2 := len(echoSess2.events()) - prevEcho2
	if gotEcho1+gotEcho2 != 4 || gotEcho1 == 0 || gotEcho2 == 0 {
		t.Fatalf("expected 4 message events round-robined across echo-bot conns without app_mention, got (%d, %d)", gotEcho1, gotEcho2)
	}

	// 3. 両 bot メンションした人間投稿 -> 両方に app_mention が届く
	prevNuage := len(nuageSess.events())
	prevEchoTotal := len(echoSess1.events()) + len(echoSess2.events())
	hub.PublishMessageCreated(ch, domain.Message{
		ChannelID: ch.ID,
		TS:        "1700000003.000001",
		UserID:    "U01ALICE",
		Text:      "<@U01BOT> <@U02ECHOBOT> both",
	})
	if len(nuageSess.events())-prevNuage != 2 {
		t.Fatalf("expected 2 events (message + app_mention) for nuage-bot, got %d", len(nuageSess.events())-prevNuage)
	}
	if (len(echoSess1.events())+len(echoSess2.events()))-prevEchoTotal != 2 {
		t.Fatalf("expected 2 events (message + app_mention) for echo-bot, got %d", (len(echoSess1.events())+len(echoSess2.events()))-prevEchoTotal)
	}
}

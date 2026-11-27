package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

type fakeVerifier struct {
	tokens map[string]string
	err    error
}

func (f fakeVerifier) Verify(_ context.Context, token string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	sub, ok := f.tokens[token]
	if !ok {
		return "", errors.New("invalid token")
	}
	return sub, nil
}

type fakeSocketHub struct {
	tickets map[string]domain.BotIdentity
	seq     int
}

func (f *fakeSocketHub) IssueTicket(bot domain.BotIdentity) string {
	f.seq++
	t := "ticket-" + bot.UserID
	if f.tickets == nil {
		f.tickets = make(map[string]domain.BotIdentity)
	}
	f.tickets[t] = bot
	return t
}

func (f *fakeSocketHub) ConsumeTicket(ticket string) (domain.BotIdentity, bool) {
	b, ok := f.tickets[ticket]
	if !ok {
		return domain.BotIdentity{}, false
	}
	delete(f.tickets, ticket)
	return b, true
}

func (f *fakeSocketHub) RegisterSocket(domain.BotIdentity, usecase.SocketSession) (uint64, func(), error) {
	return 1, func() {}, nil
}

func (f *fakeSocketHub) HandleAck([]byte) (string, bool) { return "", false }
func (f *fakeSocketHub) IsBotOnline() bool               { return false }

func TestSocketUsecase_VerifyAndTickets(t *testing.T) {
	bots := []domain.BotIdentity{
		{Sub: "spiffe://nuage/ns/pr-1/sa/echo-bot", UserID: "U02ECHOBOT", BotID: "B02ECHOBOT", Name: "echo-bot", IsDefault: true},
		{Sub: "spiffe://nuage/host/atelier/bot", UserID: "U01BOT", BotID: "B01BOT", Name: "nuage-bot"},
	}
	verifier := fakeVerifier{
		tokens: map[string]string{
			"jwt-echo":    "spiffe://nuage/ns/pr-1/sa/echo-bot",
			"jwt-atelier": "spiffe://nuage/host/atelier/bot",
			"jwt-stranger": "spiffe://nuage/ns/other-pr/sa/echo-bot",
		},
	}
	hub := &fakeSocketHub{}
	uc := usecase.NewSocketUsecase(hub, verifier, bots)
	ctx := context.Background()

	b1, err := uc.VerifyBotToken(ctx, "jwt-echo")
	if err != nil || b1.UserID != "U02ECHOBOT" || b1.BotID != "B02ECHOBOT" {
		t.Fatalf("unexpected bot 1: %+v err=%v", b1, err)
	}
	b2, err := uc.VerifyBotToken(ctx, "jwt-atelier")
	if err != nil || b2.UserID != "U01BOT" {
		t.Fatalf("unexpected bot 2: %+v err=%v", b2, err)
	}

	// 許可リストに無い sub は拒否
	if _, err := uc.VerifyBotToken(ctx, "jwt-stranger"); !errors.Is(err, usecase.ErrInvalidAuth) {
		t.Fatalf("expected ErrInvalidAuth for unlisted sub, got %v", err)
	}
	// 空・不正トークンも拒否
	if _, err := uc.VerifyBotToken(ctx, ""); !errors.Is(err, usecase.ErrInvalidAuth) {
		t.Fatalf("expected ErrInvalidAuth for empty token, got %v", err)
	}

	// チケット発行とワンタイム消費
	ticket, err := uc.OpenConnectionTicket(ctx, "jwt-echo")
	if err != nil || ticket == "" {
		t.Fatalf("OpenConnectionTicket failed: %v", err)
	}
	gotBot, ok := uc.AuthorizeSocket(ticket)
	if !ok || gotBot.UserID != "U02ECHOBOT" {
		t.Fatalf("AuthorizeSocket failed: bot=%+v ok=%v", gotBot, ok)
	}
	if _, ok := uc.AuthorizeSocket(ticket); ok {
		t.Fatal("ticket must be single-use")
	}
}

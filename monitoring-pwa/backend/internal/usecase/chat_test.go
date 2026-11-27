package usecase_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/realtime"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/sqlite"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

func TestChatUsecase_MultiBotPostUpdateAndBootstrap(t *testing.T) {
	db, err := sqlite.InitDB(filepath.Join(t.TempDir(), "chat_uc.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	user := domain.User{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: "$2a$04$dummy"}
	if err := db.EnsureUserChannels([]domain.User{user}); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	botEcho := domain.BotIdentity{Sub: "spiffe://nuage/ns/pr-1/sa/echo-bot", UserID: "U02ECHOBOT", BotID: "B02ECHOBOT", Name: "echo-bot", IsDefault: true}
	botNuage := domain.BotIdentity{Sub: "spiffe://nuage/host/atelier/bot", UserID: "U01BOT", BotID: "B01BOT", Name: "nuage-bot"}

	hub := realtime.NewHub()
	chatUC := usecase.NewChatUsecase(db, db, hub, hub, &recordingSender{}, botEcho, botNuage)
	ctx := context.Background()

	// Bootstrap は代表 bot (echo-bot) を返す
	boot, err := chatUC.Bootstrap(user)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	if boot.Bot.UserID != "U02ECHOBOT" || boot.Bot.ID != "B02ECHOBOT" || boot.Bot.Name != "echo-bot" {
		t.Fatalf("unexpected default bot in Bootstrap: %+v", boot.Bot)
	}

	// 各 bot が投稿するとそれぞれの UserID / BotID / Name が記録される
	echoMsg, err := chatUC.PostBotMessage(ctx, botEcho, domain.ChannelGeneral, "", "", "hello from echo", "")
	if err != nil {
		t.Fatalf("PostBotMessage echo failed: %v", err)
	}
	if echoMsg.UserID != "U02ECHOBOT" || echoMsg.BotID != "B02ECHOBOT" || echoMsg.Username != "echo-bot" {
		t.Fatalf("unexpected echo message author: %+v", echoMsg)
	}

	nuageMsg, err := chatUC.PostBotMessage(ctx, botNuage, domain.ChannelGeneral, "", "", "hello from nuage", "")
	if err != nil {
		t.Fatalf("PostBotMessage nuage failed: %v", err)
	}
	if nuageMsg.UserID != "U01BOT" || nuageMsg.BotID != "B01BOT" || nuageMsg.Username != "nuage-bot" {
		t.Fatalf("unexpected nuage message author: %+v", nuageMsg)
	}

	humanRes, err := chatUC.PostHumanMessage(ctx, user, domain.ChannelGeneral, "ping @bot and @nuage-bot", "")
	if err != nil {
		t.Fatalf("PostHumanMessage failed: %v", err)
	}
	if humanRes.Message.Text != "ping <@U02ECHOBOT> and <@U01BOT>" {
		t.Fatalf("unexpected normalized mention text: %q", humanRes.Message.Text)
	}

	// 自分の投稿は更新可能
	updated, err := chatUC.UpdateBotMessage(ctx, botEcho, domain.ChannelGeneral, echoMsg.TS, "updated by echo", "")
	if err != nil || updated.Text != "updated by echo" {
		t.Fatalf("expected own message update to succeed, got %+v err=%v", updated, err)
	}

	// 他 bot の投稿や人間の投稿は ErrCantUpdateMessage で拒否
	if _, err := chatUC.UpdateBotMessage(ctx, botEcho, domain.ChannelGeneral, nuageMsg.TS, "hijack", ""); !errors.Is(err, usecase.ErrCantUpdateMessage) {
		t.Fatalf("expected ErrCantUpdateMessage updating another bot's message, got %v", err)
	}
	if _, err := chatUC.UpdateBotMessage(ctx, botEcho, domain.ChannelGeneral, humanRes.Message.TS, "hijack", ""); !errors.Is(err, usecase.ErrCantUpdateMessage) {
		t.Fatalf("expected ErrCantUpdateMessage updating human message, got %v", err)
	}
}

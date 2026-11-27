package domain

import (
	"reflect"
	"testing"
)

func TestDefaultBotAndMentions(t *testing.T) {
	bots := []BotIdentity{
		{Sub: "spiffe://nuage/ns/pr-1/sa/echo-bot", UserID: "U02ECHOBOT", BotID: "B02ECHOBOT", Name: "echo-bot", IsDefault: true},
		{Sub: "spiffe://nuage/host/atelier/bot", UserID: "U01BOT", BotID: "B01BOT", Name: "nuage-bot"},
	}

	if got := DefaultBot(bots); got.UserID != "U02ECHOBOT" {
		t.Fatalf("expected U02ECHOBOT as default bot, got %+v", got)
	}
	if got := DefaultBot(bots[1:]); got.UserID != "U01BOT" {
		t.Fatalf("expected first bot when is_default unset, got %+v", got)
	}

	gotText := NormalizeBotMentions("ping @bot and @nuage-bot and @ECHO-BOT plus user@bot.example", bots)
	wantText := "ping <@U02ECHOBOT> and <@U01BOT> and <@U02ECHOBOT> plus user@bot.example"
	if gotText != wantText {
		t.Fatalf("got %q, want %q", gotText, wantText)
	}

	ids := ExtractMentionedUserIDs("hi <@U02ECHOBOT> and <@U01BOT|nuage-bot> and <@U02ECHOBOT>!")
	wantIDs := []string{"U02ECHOBOT", "U01BOT"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("ExtractMentionedUserIDs = %v, want %v", ids, wantIDs)
	}
}

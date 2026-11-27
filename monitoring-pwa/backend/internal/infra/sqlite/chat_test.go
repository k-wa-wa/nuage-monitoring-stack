package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

func TestChatDBOperations(t *testing.T) {
	t.Run("TS単調増加・未読管理・検索・ユーザー別DM分離が動作すること", func(t *testing.T) {
		db, err := InitDB(filepath.Join(t.TempDir(), "chat.db"))
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		users := []domain.User{
			{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: "$2a$04$dummy"},
			{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: "$2a$04$dummy"},
		}
		if err := db.EnsureUserChannels(users); err != nil {
			t.Fatalf("EnsureUserChannels failed: %v", err)
		}

		// DM にメッセージを投稿
		dmAlice := domain.DMChannelID("U01ALICE")
		dmBob := domain.DMChannelID("U02BOB")
		m1, err := db.CreateMessage(domain.CreateMessageParams{
			ChannelID: dmAlice,
			UserID:    domain.SlackBotUserID,
			BotID:     domain.SlackBotID,
			Username:  domain.SlackBotName,
			Text:      "secret for alice",
		})
		if err != nil {
			t.Fatalf("CreateMessage failed: %v", err)
		}
		m2, err := db.CreateMessage(domain.CreateMessageParams{
			ChannelID: dmBob,
			UserID:    domain.SlackBotUserID,
			BotID:     domain.SlackBotID,
			Username:  domain.SlackBotName,
			Text:      "secret for bob",
		})
		if err != nil {
			t.Fatalf("CreateMessage 2 failed: %v", err)
		}
		if m1.TS >= m2.TS {
			t.Fatalf("expected strictly increasing ts: %s >= %s", m1.TS, m2.TS)
		}

		// Alice には Bob の DM が見えず、検索にも引っかからないこと
		aliceChannels, err := db.ListChannelsForUser("U01ALICE")
		if err != nil {
			t.Fatalf("ListChannelsForUser failed: %v", err)
		}
		for _, ch := range aliceChannels {
			if ch.ID == dmBob {
				t.Fatalf("Alice should not see Bob's DM channel")
			}
			if ch.ID == dmAlice && ch.UnreadCount != 1 {
				t.Fatalf("expected 1 unread in Alice DM, got %d", ch.UnreadCount)
			}
		}

		res, err := db.SearchMessages("secret", "", "U01ALICE", 20)
		if err != nil {
			t.Fatalf("SearchMessages failed: %v", err)
		}
		if len(res) != 1 || res[0].ChannelID != dmAlice {
			t.Fatalf("expected only Alice's DM in search, got %+v", res)
		}

		// 既読更新で未読 0 になること
		if err := db.MarkChannelRead("U01ALICE", dmAlice, m1.TS); err != nil {
			t.Fatalf("MarkChannelRead failed: %v", err)
		}
		aliceChannels, _ = db.ListChannelsForUser("U01ALICE")
		for _, ch := range aliceChannels {
			if ch.ID == dmAlice && ch.UnreadCount != 0 {
				t.Fatalf("expected 0 unread after MarkChannelRead, got %d", ch.UnreadCount)
			}
		}
	})

	t.Run("ts は実際の時刻 (µs) で振られ、同じ µs に重なっても書き直して一意になること", func(t *testing.T) {
		db, err := InitDB(filepath.Join(t.TempDir(), "ts.db"))
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		fixed := time.Date(2026, 11, 26, 12, 0, 0, 123456000, time.UTC)
		nowFunc = func() time.Time { return fixed }
		t.Cleanup(func() { nowFunc = time.Now })

		var tss []string
		for i := 0; i < 5; i++ {
			m, err := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelGeneral, Username: "a", Text: "x"})
			if err != nil {
				t.Fatalf("CreateMessage failed: %v", err)
			}
			tss = append(tss, m.TS)
		}
		want := []string{"1795694400.123456", "1795694400.123457", "1795694400.123458", "1795694400.123459", "1795694400.123460"}
		for i := range want {
			if tss[i] != want[i] {
				t.Fatalf("ts = %v, want %v", tss, want)
			}
		}
		// 別のチャンネルなら同じ ts を使える (Slack と同じく ts はチャンネルの中で一意)
		m, err := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelKeel, Username: "a", Text: "x"})
		if err != nil || m.TS != "1795694400.123456" {
			t.Fatalf("expected the same ts in another channel, got %+v (err=%v)", m, err)
		}
	})

	t.Run("通知タブの一覧は、アラート・通知のスレッドの親だけを絞り込んで返すこと", func(t *testing.T) {
		db, err := InitDB(filepath.Join(t.TempDir(), "activity.db"))
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		now := time.Now()
		firing, _ := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelAlertmanager, Username: "Alertmanager", Text: "firing", SourceKind: "alert", AlertStatus: "firing", FiredAt: &now})
		resolved, _ := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelAlertmanager, Username: "Alertmanager", Text: "was firing", SourceKind: "alert", AlertStatus: "firing", FiredAt: &now})
		// 解決の返信 (スレッドの中) は一覧に出ない
		if _, err := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelAlertmanager, ThreadTS: resolved.TS, Username: "Alertmanager", Text: "resolved", SourceKind: "alert", AlertStatus: "resolved"}); err != nil {
			t.Fatalf("reply failed: %v", err)
		}
		if _, err := db.UpdateAlertRoot(domain.ChannelAlertmanager, resolved.TS, domain.AlertRootUpdate{Status: "resolved", Level: "success", ResolvedAt: &now}); err != nil {
			t.Fatalf("UpdateAlertRoot failed: %v", err)
		}
		notice, _ := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelArgoCD, Username: "Argo CD", Text: "synced", SourceKind: "notification"})
		// 普通の会話は出ない
		if _, err := db.CreateMessage(domain.CreateMessageParams{ChannelID: domain.ChannelGeneral, Username: "a", Text: "hello"}); err != nil {
			t.Fatalf("chat failed: %v", err)
		}

		check := func(filter domain.ActivityFilter, want ...string) {
			t.Helper()
			items, _, err := db.ListActivity(filter, "", 10)
			if err != nil {
				t.Fatalf("ListActivity(%s) failed: %v", filter, err)
			}
			var got []string
			for _, it := range items {
				got = append(got, it.Message.TS)
			}
			if len(got) != len(want) {
				t.Fatalf("ListActivity(%s) = %v, want %v", filter, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("ListActivity(%s) = %v, want %v", filter, got, want)
				}
			}
		}
		check(domain.ActivityAll, notice.TS, resolved.TS, firing.TS)
		check(domain.ActivityFiring, firing.TS)
		check(domain.ActivityResolved, resolved.TS)
		check(domain.ActivityNotice, notice.TS)

		items, _, _ := db.ListActivity(domain.ActivityNotice, "", 10)
		if items[0].ChannelName != "argocd" {
			t.Fatalf("expected channel name argocd, got %q", items[0].ChannelName)
		}
		if n, err := db.CountFiringAlerts(); err != nil || n != 1 {
			t.Fatalf("expected 1 firing alert, got %d (err=%v)", n, err)
		}
		// before より古いものだけを返す (ページ送り)
		page, hasMore, err := db.ListActivity(domain.ActivityAll, notice.TS, 1)
		if err != nil || len(page) != 1 || page[0].Message.TS != resolved.TS || !hasMore {
			t.Fatalf("unexpected page: %+v hasMore=%v err=%v", page, hasMore, err)
		}
	})
}

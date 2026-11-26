package push

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

func TestChatDBOperationsAndLegacyBackfill(t *testing.T) {
	t.Run("既存DBのalertsとnotificationsがマイグレーション時にメッセージへ取り込まれること", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "legacy_chat.db")

		// 先に旧データを作成
		conn, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("sql.Open failed: %v", err)
		}
		_, err = conn.Exec(`
			CREATE TABLE subscriptions (endpoint TEXT PRIMARY KEY, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at DATETIME NOT NULL);
			CREATE TABLE notifications (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, body TEXT NOT NULL, url TEXT, level TEXT NOT NULL, details TEXT, source TEXT, created_at DATETIME NOT NULL);
			CREATE TABLE alerts (id INTEGER PRIMARY KEY AUTOINCREMENT, fingerprint TEXT UNIQUE NOT NULL, alertname TEXT NOT NULL, status TEXT NOT NULL, level TEXT NOT NULL, summary TEXT NOT NULL, description TEXT, url TEXT, details TEXT, fired_at DATETIME NOT NULL, resolved_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
			INSERT INTO alerts (fingerprint, alertname, status, level, summary, fired_at, resolved_at, created_at, updated_at)
			VALUES ('OldAlert', 'OldAlert', 'resolved', 'success', 'CPU high', '2026-11-01T00:00:00Z', '2026-11-01T00:10:00Z', '2026-11-01T00:00:00Z', '2026-11-01T00:10:00Z');
			INSERT INTO notifications (title, body, url, level, source, created_at)
			VALUES ('[ArgoCD] app synced', 'synced', 'https://argocd.wpcapp.net', 'info', 'argocd', '2026-11-02T00:00:00Z');
		`)
		if err != nil {
			t.Fatalf("setup failed: %v", err)
		}
		conn.Close()

		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		amMsgs, _, err := db.ListChannelMessagesAsc(ChannelAlertmanager, "", 20)
		if err != nil {
			t.Fatalf("ListChannelMessagesAsc failed: %v", err)
		}
		if len(amMsgs) != 1 || amMsgs[0].ReplyCount != 1 || amMsgs[0].AlertStatus != "resolved" {
			t.Fatalf("expected 1 alert parent with 1 resolved reply, got %+v", amMsgs)
		}

		argoMsgs, _, err := db.ListChannelMessagesAsc(ChannelArgoCD, "", 20)
		if err != nil {
			t.Fatalf("ListChannelMessagesAsc argocd failed: %v", err)
		}
		if len(argoMsgs) != 1 || argoMsgs[0].SourceKind != "notification" {
			t.Fatalf("expected 1 argocd message, got %+v", argoMsgs)
		}
	})

	t.Run("TS単調増加・未読管理・検索・ユーザー別DM分離が動作すること", func(t *testing.T) {
		db, err := InitDB(filepath.Join(t.TempDir(), "chat.db"))
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		users := []config.UserConfig{
			{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: "$2a$04$dummy"},
			{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: "$2a$04$dummy"},
		}
		if err := db.EnsureUserChannels(users); err != nil {
			t.Fatalf("EnsureUserChannels failed: %v", err)
		}

		// DM にメッセージを投稿
		dmAlice := DMChannelID("U01ALICE")
		dmBob := DMChannelID("U02BOB")
		m1, err := db.CreateMessage(CreateMessageParams{
			ChannelID: dmAlice,
			UserID:    SlackBotUserID,
			BotID:     SlackBotID,
			Username:  SlackBotName,
			Text:      "secret for alice",
		})
		if err != nil {
			t.Fatalf("CreateMessage failed: %v", err)
		}
		m2, err := db.CreateMessage(CreateMessageParams{
			ChannelID: dmBob,
			UserID:    SlackBotUserID,
			BotID:     SlackBotID,
			Username:  SlackBotName,
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

	t.Run("アラートの発生と解決が1スレッドにまとまること", func(t *testing.T) {
		db, err := InitDB(filepath.Join(t.TempDir(), "alert_thread.db"))
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		a1, err := db.UpsertAlert("DiskFull", "DiskFull", "firing", "error", "Disk 95%", "desc", "", "{}")
		if err != nil {
			t.Fatalf("UpsertAlert firing failed: %v", err)
		}
		msg1, parent1, err := db.PostAlertChatMessage(a1, true, "DiskFull", "Disk 95%", "{}", "")
		if err != nil {
			t.Fatalf("PostAlertChatMessage firing failed: %v", err)
		}
		if msg1.TS != parent1.TS || parent1.AlertStatus != "firing" {
			t.Fatalf("unexpected firing parent: %+v", parent1)
		}

		time.Sleep(2 * time.Millisecond)
		a2, err := db.UpsertAlert("DiskFull", "DiskFull", "resolved", "success", "Disk 95%", "desc", "", "{}")
		if err != nil {
			t.Fatalf("UpsertAlert resolved failed: %v", err)
		}
		reply, parent2, err := db.PostAlertChatMessage(a2, false, "DiskFull", "Disk 95%", "{}", "")
		if err != nil {
			t.Fatalf("PostAlertChatMessage resolved failed: %v", err)
		}
		if reply.ThreadTS != parent1.TS || parent2.AlertStatus != "resolved" {
			t.Fatalf("expected reply in thread %s, got reply=%+v parent=%+v", parent1.TS, reply, parent2)
		}

		p, replies, err := db.GetThreadMessages(ChannelAlertmanager, parent1.TS)
		if err != nil {
			t.Fatalf("GetThreadMessages failed: %v", err)
		}
		if p.ReplyCount != 1 || len(replies) != 1 || replies[0].TS != reply.TS {
			t.Fatalf("unexpected thread structure: parent=%+v replies=%+v", p, replies)
		}
	})

	t.Run("1000件を超える既存履歴も上限なくバックフィルされること", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "many_legacy.db")
		conn, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("sql.Open failed: %v", err)
		}
		_, err = conn.Exec(`
			CREATE TABLE subscriptions (endpoint TEXT PRIMARY KEY, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at DATETIME NOT NULL);
			CREATE TABLE notifications (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, body TEXT NOT NULL, url TEXT, level TEXT NOT NULL, details TEXT, source TEXT, created_at DATETIME NOT NULL);
			CREATE TABLE alerts (id INTEGER PRIMARY KEY AUTOINCREMENT, fingerprint TEXT UNIQUE NOT NULL, alertname TEXT NOT NULL, status TEXT NOT NULL, level TEXT NOT NULL, summary TEXT NOT NULL, description TEXT, url TEXT, details TEXT, fired_at DATETIME NOT NULL, resolved_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
		`)
		if err != nil {
			t.Fatalf("create tables failed: %v", err)
		}
		tx, _ := conn.Begin()
		stmt, _ := tx.Prepare(`INSERT INTO notifications (title, body, url, level, source, created_at) VALUES (?, 'body', '', 'info', 'generic', '2026-11-01T00:00:00Z')`)
		for i := 0; i < 1005; i++ {
			_, _ = stmt.Exec("note")
		}
		_ = stmt.Close()
		_ = tx.Commit()
		_ = conn.Close()

		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		ref, err := db.GetMessageBySourceRef("notification", 1)
		if err != nil || ref == nil {
			t.Fatalf("expected oldest notification id=1 to be backfilled, err=%v", err)
		}
	})
}

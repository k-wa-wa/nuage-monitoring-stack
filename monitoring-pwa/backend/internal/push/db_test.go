package push

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("sqlite_master query failed: %v", err)
	}
	return n > 0
}

func TestInitDB(t *testing.T) {
	t.Run("新しい DB を作り、版を記録すること", func(t *testing.T) {
		db, err := InitDB(filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		for _, table := range []string{"subscriptions", "channels", "messages", "channel_reads", "alerts"} {
			if !tableExists(t, db, table) {
				t.Errorf("table %s should exist", table)
			}
		}
		if tableExists(t, db, "notifications") {
			t.Errorf("notifications table should not exist")
		}
		var version int
		if err := db.conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
			t.Fatalf("expected user_version %d, got %d (err=%v)", schemaVersion, version, err)
		}
	})

	t.Run("古い作りの DB は、購読だけ残して作り直すこと", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "legacy.db")
		conn, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("sql.Open failed: %v", err)
		}
		// master の版の作り (user_id の無い subscriptions、notifications、id を持つ alerts)
		_, err = conn.Exec(`
			CREATE TABLE subscriptions (endpoint TEXT PRIMARY KEY, p256dh TEXT NOT NULL, auth TEXT NOT NULL, created_at DATETIME NOT NULL);
			INSERT INTO subscriptions VALUES ('https://push.example/1', 'p', 'a', '2026-01-01T00:00:00Z');
			CREATE TABLE notifications (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, body TEXT NOT NULL, url TEXT, level TEXT NOT NULL, created_at DATETIME NOT NULL);
			INSERT INTO notifications (title, body, url, level, created_at) VALUES ('t', 'b', '', 'info', '2026-01-01T00:00:00Z');
			CREATE TABLE alerts (id INTEGER PRIMARY KEY AUTOINCREMENT, fingerprint TEXT UNIQUE NOT NULL, alertname TEXT NOT NULL, status TEXT NOT NULL);
			INSERT INTO alerts (fingerprint, alertname, status) VALUES ('fp', 'A', 'firing');
		`)
		conn.Close()
		if err != nil {
			t.Fatalf("legacy setup failed: %v", err)
		}

		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB on legacy DB failed: %v", err)
		}
		defer db.Close()

		if tableExists(t, db, "notifications") {
			t.Errorf("legacy notifications table should be dropped")
		}
		if open, err := db.ListOpenAlerts(); err != nil || len(open) != 0 {
			t.Errorf("legacy alerts should be dropped: %+v (err=%v)", open, err)
		}
		subs := db.List()
		if len(subs) != 1 || subs[0].Endpoint != "https://push.example/1" {
			t.Fatalf("subscriptions should be kept, got %+v", subs)
		}
		// user_id 列が足されていて、ユーザーに紐づけられること
		if err := db.AddForUser(webpush.Subscription{Endpoint: "https://push.example/1"}, "U01ALICE"); err != nil {
			t.Fatalf("AddForUser failed: %v", err)
		}
		if got := db.ListByUser("U01ALICE"); len(got) != 1 {
			t.Fatalf("expected subscription bound to user, got %+v", got)
		}
	})

	t.Run("同じ版の DB は作り直さないこと", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "keep.db")
		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		if _, err := db.CreateMessage(CreateMessageParams{ChannelID: ChannelGeneral, Username: "a", Text: "keep me"}); err != nil {
			t.Fatalf("CreateMessage failed: %v", err)
		}
		db.Close()

		db, err = InitDB(dbPath)
		if err != nil {
			t.Fatalf("re-InitDB failed: %v", err)
		}
		defer db.Close()
		msgs, _, err := db.ListChannelMessagesAsc(ChannelGeneral, "", 10)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("messages should survive restart, got %d (err=%v)", len(msgs), err)
		}
	})

	t.Run("読み取り専用の場合はエラーになること", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test_readonly.db")

		// 事前に DB を作成して閉じる
		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("setup InitDB failed: %v", err)
		}
		db.Close()

		// ファイルと親ディレクトリを読み取り専用にする
		if err := os.Chmod(dbPath, 0400); err != nil {
			t.Fatalf("chmod db failed: %v", err)
		}
		if err := os.Chmod(tmpDir, 0500); err != nil {
			t.Fatalf("chmod dir failed: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(tmpDir, 0700)
			_ = os.Chmod(dbPath, 0600)
		})

		// 読み取り専用環境で InitDB を実行するとエラーになること
		_, err = InitDB(dbPath)
		if err == nil {
			t.Fatalf("expected error for readonly DB, got nil")
		}
	})
}

func TestFingerprintIsOrderIndependent(t *testing.T) {
	fp1 := GenerateFingerprint(map[string]string{"alertname": "HighCPU", "pod": "api-1", "namespace": "default"})
	fp2 := GenerateFingerprint(map[string]string{"namespace": "default", "alertname": "HighCPU", "pod": "api-1"})
	if fp1 != fp2 {
		t.Fatalf("fingerprint should be order-independent: %s != %s", fp1, fp2)
	}
}

func TestAlertState(t *testing.T) {
	db, err := InitDB(filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	if st, err := db.GetAlertState("fp-1"); err != nil || st != nil {
		t.Fatalf("unknown fingerprint should return nil, got %+v (err=%v)", st, err)
	}
	if err := db.SetAlertOpenThread("fp-1", "HighCPU", "1700000000.000001"); err != nil {
		t.Fatalf("SetAlertOpenThread failed: %v", err)
	}
	if err := db.SetAlertOpenThread("fp-2", "DiskFull", ""); err != nil {
		t.Fatalf("SetAlertOpenThread failed: %v", err)
	}
	open, err := db.ListOpenAlerts()
	if err != nil || len(open) != 1 || open[0].Fingerprint != "fp-1" || open[0].OpenThreadTS != "1700000000.000001" {
		t.Fatalf("expected only fp-1 to be open, got %+v (err=%v)", open, err)
	}
	if err := db.SetAlertOpenThread("fp-1", "HighCPU", ""); err != nil {
		t.Fatalf("closing failed: %v", err)
	}
	st, err := db.GetAlertState("fp-1")
	if err != nil || st == nil || st.OpenThreadTS != "" {
		t.Fatalf("expected fp-1 closed, got %+v (err=%v)", st, err)
	}
}

package push

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitDB(t *testing.T) {
	t.Run("正常に初期化できること", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB failed: %v", err)
		}
		defer db.Close()

		// 正常にテーブルが作られ、書き込み可能であること
		id, err := db.SaveNotificationWithSource("title", "body", "https://example.com", "info", "details", "generic")
		if err != nil {
			t.Fatalf("SaveNotificationWithSource failed: %v", err)
		}

		got, err := db.GetNotificationByID(id)
		if err != nil {
			t.Fatalf("GetNotificationByID failed: %v", err)
		}
		if got.Title != "title" || got.URL != "https://example.com" || got.Source != "generic" {
			t.Fatalf("unexpected notification: %+v", got)
		}
	})

	t.Run("既存DBからの後方互換マイグレーションができること", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "legacy.db")

		// details / source カラムを持たない旧スキーマの DB を事前作成する
		conn, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("sql.Open failed: %v", err)
		}
		_, err = conn.Exec(`
			CREATE TABLE subscriptions (
				endpoint TEXT PRIMARY KEY,
				p256dh TEXT NOT NULL,
				auth TEXT NOT NULL,
				created_at DATETIME NOT NULL
			);
			CREATE TABLE notifications (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				title TEXT NOT NULL,
				body TEXT NOT NULL,
				url TEXT,
				level TEXT NOT NULL,
				created_at DATETIME NOT NULL
			);
			INSERT INTO notifications (title, body, url, level, created_at)
			VALUES ('legacy-title', 'legacy-body', 'https://argocd.wpcapp.net', 'info', '2026-01-01T00:00:00Z');
		`)
		if err != nil {
			conn.Close()
			t.Fatalf("legacy setup failed: %v", err)
		}
		conn.Close()

		db, err := InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB on legacy DB failed: %v", err)
		}
		defer db.Close()

		notes, err := db.ListNotifications(10)
		if err != nil {
			t.Fatalf("ListNotifications failed: %v", err)
		}
		if len(notes) != 1 || notes[0].Title != "legacy-title" || notes[0].URL != "https://argocd.wpcapp.net" {
			t.Fatalf("legacy data was lost or corrupted: %+v", notes)
		}

		// マイグレーション後の新規書き込みも動作すること
		if _, err := db.SaveNotificationWithSource("new", "body", "", "info", "det", "keel"); err != nil {
			t.Fatalf("SaveNotificationWithSource after migration failed: %v", err)
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

func TestAlertAndFingerprint(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := InitDB(filepath.Join(tmpDir, "alerts.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	fp1 := GenerateFingerprint(map[string]string{"alertname": "HighCPU", "pod": "api-1", "namespace": "default"})
	fp2 := GenerateFingerprint(map[string]string{"namespace": "default", "alertname": "HighCPU", "pod": "api-1"})
	if fp1 != fp2 {
		t.Fatalf("fingerprint should be order-independent: %s != %s", fp1, fp2)
	}

	saved, err := db.UpsertAlert(fp1, "HighCPU", "firing", "error", "CPU high", "desc", "http://grafana/explore", `{"labels":{"alertname":"HighCPU"}}`)
	if err != nil {
		t.Fatalf("UpsertAlert failed: %v", err)
	}

	got, err := db.GetAlertByID(saved.ID)
	if err != nil {
		t.Fatalf("GetAlertByID failed: %v", err)
	}
	if got.Fingerprint != fp1 || got.Status != "firing" {
		t.Fatalf("unexpected alert: %+v", got)
	}

	time.Sleep(5 * time.Millisecond)
	resolved, err := db.UpsertAlert(fp1, "HighCPU", "resolved", "success", "CPU high", "desc", "http://grafana/explore", `{"labels":{"alertname":"HighCPU"}}`)
	if err != nil {
		t.Fatalf("UpsertAlert resolve failed: %v", err)
	}
	if resolved.ID != saved.ID || resolved.Status != "resolved" || resolved.ResolvedAt == nil {
		t.Fatalf("unexpected resolved alert: %+v", resolved)
	}
}

package push

import (
	"os"
	"path/filepath"
	"testing"
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
		if _, err := db.SaveNotification("title", "body", "", "info", ""); err != nil {
			t.Fatalf("SaveNotification failed: %v", err)
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

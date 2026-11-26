package config

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoadConfigValidation(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("test-passphrase"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt failed: %v", err)
	}
	usersJSON, _ := json.Marshal([]UserConfig{
		{
			ID:             "U01TEST",
			DisplayName:    "Tester",
			PassphraseHash: string(hash),
		},
	})

	setValidEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("VAPID_PUBLIC_KEY", "pub-key")
		t.Setenv("VAPID_PRIVATE_KEY", "priv-key")
		t.Setenv("COOKIE_SIGNING_KEY", strings.Repeat("a", 32))
		t.Setenv("USERS_JSON", string(usersJSON))
		t.Setenv("SLACK_BOT_TOKEN", "xoxb-test-token")
		t.Setenv("SLACK_APP_TOKEN", "xapp-test-token")
	}

	t.Run("すべての設定が揃っていれば正常に読み込めること", func(t *testing.T) {
		setValidEnv(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(cfg.Users) != 1 || cfg.Users[0].ID != "U01TEST" {
			t.Fatalf("unexpected users: %+v", cfg.Users)
		}
	})

	t.Run("署名鍵が欠落または短い場合はエラーになること", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("COOKIE_SIGNING_KEY", "short")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for short COOKIE_SIGNING_KEY")
		}
	})

	t.Run("ユーザー定義が無い場合はエラーになること", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("USERS_JSON", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for empty USERS_JSON")
		}
	})

	t.Run("トークン形式が不正または欠落の場合はエラーになること", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("SLACK_BOT_TOKEN", "invalid")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for invalid SLACK_BOT_TOKEN")
		}

		setValidEnv(t)
		t.Setenv("SLACK_APP_TOKEN", "")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for empty SLACK_APP_TOKEN")
		}
	})

	t.Run("不正なbcryptハッシュやID形式はエラーになること", func(t *testing.T) {
		setValidEnv(t)
		badUsers, _ := json.Marshal([]UserConfig{
			{ID: "X01BAD", DisplayName: "Bad", PassphraseHash: "not-a-bcrypt-hash"},
		})
		t.Setenv("USERS_JSON", string(badUsers))
		if _, err := Load(); err == nil {
			t.Fatal("expected error for invalid user id and hash")
		}
	})
}

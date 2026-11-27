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
	botsJSON, _ := json.Marshal([]BotUserConfig{
		{
			Sub:       "spiffe://nuage/host/atelier/bot",
			UserID:    "U01BOT",
			BotID:     "B01BOT",
			Name:      "nuage-bot",
			IsDefault: true,
		},
	})

	setValidEnv := func(t *testing.T) {
		t.Helper()
		t.Setenv("VAPID_PUBLIC_KEY", "pub-key")
		t.Setenv("VAPID_PRIVATE_KEY", "priv-key")
		t.Setenv("COOKIE_SIGNING_KEY", strings.Repeat("a", 32))
		t.Setenv("USERS_JSON", string(usersJSON))
		t.Setenv("SPIFFE_JWKS_URL", "http://spiffe-oidc-discovery-provider.spire.svc.cluster.local/keys")
		t.Setenv("SPIFFE_JWT_AUDIENCE", "monitoring-pwa")
		t.Setenv("BOT_USERS_JSON", string(botsJSON))
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
		if len(cfg.BotUsers) != 1 || cfg.BotUsers[0].Sub != "spiffe://nuage/host/atelier/bot" {
			t.Fatalf("unexpected bot users: %+v", cfg.BotUsers)
		}
	})

	t.Run("ALERT_RECONCILE は既定で on、off で止まり、それ以外はエラーになること", func(t *testing.T) {
		setValidEnv(t)
		cfg, err := Load()
		if err != nil || !cfg.AlertReconcile {
			t.Fatalf("expected reconcile on by default, got %v (err=%v)", cfg.AlertReconcile, err)
		}
		t.Setenv("ALERT_RECONCILE", "off")
		if cfg, err := Load(); err != nil || cfg.AlertReconcile {
			t.Fatalf("expected reconcile off, got %v (err=%v)", cfg.AlertReconcile, err)
		}
		t.Setenv("ALERT_RECONCILE", "false")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for unknown ALERT_RECONCILE value")
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

	t.Run("SPIFFE_JWKS_URL や SPIFFE_JWT_AUDIENCE が不正または欠落の場合はエラーになること", func(t *testing.T) {
		for _, badURL := range []string{"", "not-a-url", "ftp://example.com/keys", "http:///keys"} {
			setValidEnv(t)
			t.Setenv("SPIFFE_JWKS_URL", badURL)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for invalid SPIFFE_JWKS_URL %q", badURL)
			}
		}

		setValidEnv(t)
		t.Setenv("SPIFFE_JWT_AUDIENCE", "   ")
		if _, err := Load(); err == nil {
			t.Fatal("expected error for empty SPIFFE_JWT_AUDIENCE")
		}
	})

	t.Run("BOT_USERS_JSON の検証ルールに反する場合はエラーになること", func(t *testing.T) {
		cases := []struct {
			name string
			raw  string
		}{
			{name: "空", raw: ""},
			{name: "0件配列", raw: "[]"},
			{name: "不正JSON", raw: "{"},
			{name: "subがspiffe://でない", raw: `[{"sub":"https://example.com","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a"}]`},
			{name: "subがspiffe://のみ", raw: `[{"sub":"spiffe://","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a"}]`},
			{name: "subに前後空白がある", raw: `[{"sub":" spiffe://nuage/bot ","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a"}]`},
			{name: "user_idがU始まりでない", raw: `[{"sub":"spiffe://nuage/bot","user_id":"X01BOT","bot_id":"B01BOT","name":"bot-a"}]`},
			{name: "user_idに小文字が含まれる", raw: `[{"sub":"spiffe://nuage/bot","user_id":"Uecho","bot_id":"B01BOT","name":"echo-bot"}]`},
			{name: "bot_idがB始まりでない", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"X01BOT","name":"bot-a"}]`},
			{name: "bot_idに小文字が含まれる", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"Becho","name":"bot-a"}]`},
			{name: "nameが空", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B01BOT","name":" "}]`},
			{name: "nameに空白や記号が含まれる", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B01BOT","name":"echo bot"}]`},
			{name: "予約名bot", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B01BOT","name":"bot"}]`},
			{name: "予約名BOT(大文字)", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B01BOT","name":"BOT"}]`},
			{name: "予約名nuage-notify", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B01BOT","name":"nuage-notify"}]`},
			{name: "システム予約user_idと衝突", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U00SYSTEM","bot_id":"B01BOT","name":"bot-a"}]`},
			{name: "システム予約bot_idと衝突", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B00SYSTEM","name":"bot-a"}]`},
			{name: "人間ユーザーIDと衝突", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01TEST","bot_id":"B01BOT","name":"bot-a"}]`},
			{name: "sub重複", raw: `[{"sub":"spiffe://nuage/bot","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a"},{"sub":"spiffe://nuage/bot","user_id":"U02BOT","bot_id":"B02BOT","name":"bot-b"}]`},
			{name: "user_id重複", raw: `[{"sub":"spiffe://nuage/a","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a"},{"sub":"spiffe://nuage/b","user_id":"U01BOT","bot_id":"B02BOT","name":"bot-b"}]`},
			{name: "bot_id重複", raw: `[{"sub":"spiffe://nuage/a","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a"},{"sub":"spiffe://nuage/b","user_id":"U02BOT","bot_id":"B01BOT","name":"bot-b"}]`},
			{name: "nameの大文字小文字重複", raw: `[{"sub":"spiffe://nuage/a","user_id":"U01BOT","bot_id":"B01BOT","name":"Echo-Bot"},{"sub":"spiffe://nuage/b","user_id":"U02BOT","bot_id":"B02BOT","name":"echo-bot"}]`},
			{name: "is_defaultが2件", raw: `[{"sub":"spiffe://nuage/a","user_id":"U01BOT","bot_id":"B01BOT","name":"bot-a","is_default":true},{"sub":"spiffe://nuage/b","user_id":"U02BOT","bot_id":"B02BOT","name":"bot-b","is_default":true}]`},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				setValidEnv(t)
				t.Setenv("BOT_USERS_JSON", tc.raw)
				if _, err := Load(); err == nil {
					t.Fatalf("expected error for BOT_USERS_JSON case %s", tc.name)
				}
			})
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

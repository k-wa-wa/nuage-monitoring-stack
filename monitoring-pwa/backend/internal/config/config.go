package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// UserConfig は SOPS 等で宣言される人間ユーザーの定義である。
type UserConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	DisplayName    string `json:"display_name"`
	PassphraseHash string `json:"passphrase_hash"`
}

// EffectiveName は Slack 互換の username 相当（小文字識別子）を返す。
func (u UserConfig) EffectiveName() string {
	if strings.TrimSpace(u.Name) != "" {
		return strings.TrimSpace(u.Name)
	}
	return strings.ToLower(strings.TrimSpace(u.DisplayName))
}

// Config はアプリケーションの設定情報を保持する構造体である。
type Config struct {
	Port             string
	VAPIDPublicKey   string
	VAPIDPrivateKey  string
	VAPIDSubject     string
	DBPath           string
	AlertmanagerURL  string
	CookieSigningKey string
	Users            []UserConfig
	SlackBotToken    string
	SlackAppToken    string
}

// Load は環境変数から設定情報をロードする。必須の秘密情報についてはデフォルト値へフォールバックしない。
func Load() (Config, error) {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	dbPath := strings.TrimSpace(os.Getenv("DB_PATH"))
	if dbPath == "" {
		dbPath = "./monitoring-pwa.db"
	}

	alertmanagerURL := strings.TrimSpace(os.Getenv("ALERTMANAGER_URL"))
	if alertmanagerURL == "" {
		alertmanagerURL = "http://vmalertmanager-vm:9093"
	}

	cfg := Config{
		Port:             port,
		VAPIDPublicKey:   strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
		VAPIDPrivateKey:  strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
		VAPIDSubject:     strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
		DBPath:           dbPath,
		AlertmanagerURL:  alertmanagerURL,
		CookieSigningKey: strings.TrimSpace(os.Getenv("COOKIE_SIGNING_KEY")),
		SlackBotToken:    strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN")),
		SlackAppToken:    strings.TrimSpace(os.Getenv("SLACK_APP_TOKEN")),
	}

	usersRaw := strings.TrimSpace(os.Getenv("USERS_JSON"))
	if usersRaw != "" {
		var users []UserConfig
		if err := json.Unmarshal([]byte(usersRaw), &users); err != nil {
			return Config{}, fmt.Errorf("USERS_JSON の JSON パースに失敗した: %w", err)
		}
		cfg.Users = users
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate は起動に必要な秘密情報・ユーザー定義がすべて揃っているか検証する。
func (c Config) Validate() error {
	var errs []string

	if c.VAPIDPublicKey == "" || c.VAPIDPrivateKey == "" {
		errs = append(errs, "VAPID_PUBLIC_KEY と VAPID_PRIVATE_KEY の設定が必須である")
	}
	if len(c.CookieSigningKey) < 32 {
		errs = append(errs, "COOKIE_SIGNING_KEY は 32 文字以上で設定する必要がある")
	}
	if !strings.HasPrefix(c.SlackBotToken, "xoxb-") || len(c.SlackBotToken) <= len("xoxb-") {
		errs = append(errs, "SLACK_BOT_TOKEN は xoxb- で始まる有効なトークンの設定が必須である")
	}
	if !strings.HasPrefix(c.SlackAppToken, "xapp-") || len(c.SlackAppToken) <= len("xapp-") {
		errs = append(errs, "SLACK_APP_TOKEN は xapp- で始まる有効なトークンの設定が必須である")
	}
	if len(c.Users) == 0 {
		errs = append(errs, "USERS_JSON には 1 人以上のユーザー定義が必須である")
	}

	seenIDs := make(map[string]bool, len(c.Users))
	for i, u := range c.Users {
		id := strings.TrimSpace(u.ID)
		if !strings.HasPrefix(id, "U") || len(id) < 2 {
			errs = append(errs, fmt.Sprintf("USERS_JSON[%d].id (%q) は 'U' で始まるユーザー ID である必要がある", i, u.ID))
		} else if seenIDs[id] {
			errs = append(errs, fmt.Sprintf("USERS_JSON[%d].id (%q) が重複している", i, id))
		}
		seenIDs[id] = true

		if strings.TrimSpace(u.DisplayName) == "" {
			errs = append(errs, fmt.Sprintf("USERS_JSON[%d].display_name が空である", i))
		}
		if _, err := bcrypt.Cost([]byte(u.PassphraseHash)); err != nil {
			errs = append(errs, fmt.Sprintf("USERS_JSON[%d].passphrase_hash は有効な bcrypt ハッシュである必要がある: %v", i, err))
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

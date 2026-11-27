package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	reservedSystemBotUserID = "U00SYSTEM"
	reservedSystemBotID     = "B00SYSTEM"
)

var (
	botUserIDRe = regexp.MustCompile(`^U[A-Z0-9]+$`)
	botIDRe     = regexp.MustCompile(`^B[A-Z0-9]+$`)
	botNameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

	reservedBotNames = map[string]bool{
		"bot":          true,
		"nuage-notify": true,
	}
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

// BotUserConfig は BOT_USERS_JSON で宣言される許可 bot の定義である。
type BotUserConfig struct {
	Sub       string `json:"sub"`
	UserID    string `json:"user_id"`
	BotID     string `json:"bot_id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default,omitempty"`
}

// Config はアプリケーションの設定情報を保持する構造体である。
type Config struct {
	Port            string
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	DBPath          string
	AlertmanagerURL string
	// AlertReconcile は Alertmanager との突き合わせを行うか (ALERT_RECONCILE=on|off、既定は on)。
	// preview は NetworkPolicy で本番の Alertmanager へ通れないため off にする。
	AlertReconcile    bool
	CookieSigningKey  string
	Users             []UserConfig
	SPIFFEJWKSURL     string
	SPIFFEJWTAudience string
	BotUsers          []BotUserConfig
}

// Load は環境変数から設定情報をロードする。必須の秘密情報・SPIFFE 設定についてはデフォルト値へフォールバックしない。
func Load() (Config, error) {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	dbPath := strings.TrimSpace(os.Getenv("DB_PATH"))
	if dbPath == "" {
		dbPath = "./monitoring-pwa.db"
	}

	alertReconcile := true
	switch v := strings.TrimSpace(os.Getenv("ALERT_RECONCILE")); v {
	case "", "on":
	case "off":
		alertReconcile = false
	default:
		return Config{}, fmt.Errorf("ALERT_RECONCILE は on か off で指定する (%q は使えない)", v)
	}

	alertmanagerURL := strings.TrimSpace(os.Getenv("ALERTMANAGER_URL"))
	if alertmanagerURL == "" {
		alertmanagerURL = "http://vmalertmanager-vm:9093"
	}

	cfg := Config{
		Port:              port,
		VAPIDPublicKey:    strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
		VAPIDPrivateKey:   strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
		VAPIDSubject:      strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
		DBPath:            dbPath,
		AlertmanagerURL:   alertmanagerURL,
		AlertReconcile:    alertReconcile,
		CookieSigningKey:  strings.TrimSpace(os.Getenv("COOKIE_SIGNING_KEY")),
		SPIFFEJWKSURL:     strings.TrimSpace(os.Getenv("SPIFFE_JWKS_URL")),
		SPIFFEJWTAudience: strings.TrimSpace(os.Getenv("SPIFFE_JWT_AUDIENCE")),
	}

	usersRaw := strings.TrimSpace(os.Getenv("USERS_JSON"))
	if usersRaw != "" {
		var users []UserConfig
		if err := json.Unmarshal([]byte(usersRaw), &users); err != nil {
			return Config{}, fmt.Errorf("USERS_JSON の JSON パースに失敗した: %w", err)
		}
		cfg.Users = users
	}

	botUsersRaw := strings.TrimSpace(os.Getenv("BOT_USERS_JSON"))
	if botUsersRaw != "" {
		var bots []BotUserConfig
		if err := json.Unmarshal([]byte(botUsersRaw), &bots); err != nil {
			return Config{}, fmt.Errorf("BOT_USERS_JSON の JSON パースに失敗した: %w", err)
		}
		cfg.BotUsers = bots
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate は起動に必要な秘密情報・ユーザー定義・SPIFFE 設定がすべて揃っているか検証する。
func (c Config) Validate() error {
	var errs []string

	if c.VAPIDPublicKey == "" || c.VAPIDPrivateKey == "" {
		errs = append(errs, "VAPID_PUBLIC_KEY と VAPID_PRIVATE_KEY の設定が必須である")
	}
	if len(c.CookieSigningKey) < 32 {
		errs = append(errs, "COOKIE_SIGNING_KEY は 32 文字以上で設定する必要がある")
	}

	if err := validateHTTPURL(c.SPIFFEJWKSURL); err != nil {
		errs = append(errs, fmt.Sprintf("SPIFFE_JWKS_URL が不正である: %v", err))
	}
	if strings.TrimSpace(c.SPIFFEJWTAudience) == "" {
		errs = append(errs, "SPIFFE_JWT_AUDIENCE の設定が必須である")
	}

	humanUserIDs, userErrs := validateUsers(c.Users)
	errs = append(errs, userErrs...)
	errs = append(errs, validateBotUsers(c.BotUsers, humanUserIDs)...)

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("http:// または https:// で始まる有効な URL である必要がある")
	}
	return nil
}

func validateUsers(users []UserConfig) (map[string]bool, []string) {
	var errs []string
	seenIDs := make(map[string]bool, len(users))
	if len(users) == 0 {
		return seenIDs, []string{"USERS_JSON には 1 人以上のユーザー定義が必須である"}
	}

	for i, u := range users {
		id := strings.TrimSpace(u.ID)
		if !strings.HasPrefix(id, "U") || len(id) < 2 {
			errs = append(errs, fmt.Sprintf("USERS_JSON[%d].id (%q) は 'U' で始まるユーザー ID である必要がある", i, u.ID))
		} else if id == reservedSystemBotUserID {
			errs = append(errs, fmt.Sprintf("USERS_JSON[%d].id (%q) はシステム予約 ID のため使用できない", i, id))
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
	return seenIDs, errs
}

func validateBotUsers(bots []BotUserConfig, humanUserIDs map[string]bool) []string {
	var errs []string
	if len(bots) == 0 {
		return []string{"BOT_USERS_JSON には 1 件以上の bot 定義が必須である"}
	}

	seenSubs := make(map[string]bool, len(bots))
	seenUserIDs := make(map[string]bool, len(bots))
	seenBotIDs := make(map[string]bool, len(bots))
	seenNames := make(map[string]bool, len(bots))
	defaultCount := 0

	for i, b := range bots {
		if b.Sub != strings.TrimSpace(b.Sub) || !strings.HasPrefix(b.Sub, "spiffe://") || len(b.Sub) <= len("spiffe://") {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].sub (%q) は前後に空白のない 'spiffe://' で始まる有効な SPIFFE ID である必要がある", i, b.Sub))
		} else if seenSubs[b.Sub] {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].sub (%q) が重複している", i, b.Sub))
		}
		seenSubs[b.Sub] = true

		if b.UserID != strings.TrimSpace(b.UserID) || !botUserIDRe.MatchString(b.UserID) {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].user_id (%q) は 'U' + 大文字英数字である必要がある", i, b.UserID))
		} else if b.UserID == reservedSystemBotUserID || humanUserIDs[b.UserID] {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].user_id (%q) はシステムまたは人間ユーザー ID と衝突している", i, b.UserID))
		} else if seenUserIDs[b.UserID] {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].user_id (%q) が重複している", i, b.UserID))
		}
		seenUserIDs[b.UserID] = true

		if b.BotID != strings.TrimSpace(b.BotID) || !botIDRe.MatchString(b.BotID) {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].bot_id (%q) は 'B' + 大文字英数字である必要がある", i, b.BotID))
		} else if b.BotID == reservedSystemBotID {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].bot_id (%q) はシステム予約 bot ID と衝突している", i, b.BotID))
		} else if seenBotIDs[b.BotID] {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].bot_id (%q) が重複している", i, b.BotID))
		}
		seenBotIDs[b.BotID] = true

		lowerName := strings.ToLower(b.Name)
		if b.Name != strings.TrimSpace(b.Name) || !botNameRe.MatchString(b.Name) {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].name (%q) は英数字始まりかつ英数字・'-'・'_' のみで構成する必要がある", i, b.Name))
		} else if reservedBotNames[lowerName] {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].name (%q) は予約名のため使用できない", i, b.Name))
		} else if seenNames[lowerName] {
			errs = append(errs, fmt.Sprintf("BOT_USERS_JSON[%d].name (%q) が大文字小文字を区別せず重複している", i, b.Name))
		}
		seenNames[lowerName] = true

		if b.IsDefault {
			defaultCount++
		}
	}

	if defaultCount > 1 {
		errs = append(errs, "BOT_USERS_JSON で is_default: true にできるのは高々 1 件である")
	}
	return errs
}

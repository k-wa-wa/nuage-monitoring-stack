package domain

import "strings"

const (
	TeamID = "T01NUAGE"
	AppID  = "A01NUAGE"

	SystemBotID     = "B00SYSTEM"
	SystemBotUserID = "U00SYSTEM"
	SystemBotName   = "nuage-notify"

	SlackBotID     = "B01BOT"
	SlackBotUserID = "U01BOT"
	SlackBotName   = "nuage-bot"
)

// User は認証・チャット・Slack 互換 API で共通して扱う人間ユーザーである。
type User struct {
	ID             string
	Name           string
	DisplayName    string
	PassphraseHash string
}

// EffectiveName は Slack 互換の username 相当（小文字識別子）を返す。
func (u User) EffectiveName() string {
	if strings.TrimSpace(u.Name) != "" {
		return strings.TrimSpace(u.Name)
	}
	return strings.ToLower(strings.TrimSpace(u.DisplayName))
}

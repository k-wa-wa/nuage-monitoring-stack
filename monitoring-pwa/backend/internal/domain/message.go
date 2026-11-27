package domain

import (
	"regexp"
	"strings"
	"time"
)

var botMentionAliasRe = regexp.MustCompile(`(?i)@(?:bot|nuage-bot)\b`)

// Message はチャンネルまたはスレッド内の 1 件のメッセージを表す。
type Message struct {
	ID          int64
	ChannelID   string
	TS          string
	ThreadTS    string
	UserID      string
	BotID       string
	Username    string
	Text        string
	Blocks      string
	Level       string
	SourceKind  string
	Details     string
	URL         string
	AlertStatus string
	FiredAt     *time.Time
	ResolvedAt  *time.Time
	ReplyCount  int
	LatestReply string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateMessageParams は新規メッセージ作成のパラメータである。
type CreateMessageParams struct {
	ChannelID   string
	ThreadTS    string
	UserID      string
	BotID       string
	Username    string
	Text        string
	Blocks      string
	Level       string
	SourceKind  string
	Details     string
	URL         string
	AlertStatus string
	FiredAt     *time.Time
	ResolvedAt  *time.Time
}

// SearchResult はキーワード検索のヒット結果である。
type SearchResult struct {
	ChannelID   string
	ChannelName string
	IsIM        bool
	Message     Message
}

// NormalizeBotMentions は @bot / @nuage-bot を Slack 互換メンション形式 <@U01BOT> に正規化する。
func NormalizeBotMentions(text string) string {
	return botMentionAliasRe.ReplaceAllString(text, "<@"+SlackBotUserID+">")
}

// MentionsBot はメッセージ本文が bot へのメンションを含むかを返す。
func MentionsBot(text string) bool {
	return strings.Contains(text, "<@"+SlackBotUserID+">")
}

package domain

import (
	"regexp"
	"strings"
	"time"
)

var (
	mentionAliasRe  = regexp.MustCompile(`(?i)@([a-z0-9][a-z0-9_-]*)\b`)
	slackUserMentRe = regexp.MustCompile(`<@(U[A-Z0-9]+)(?:\|[^>]*)?>`)
)

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

// NormalizeBotMentions は @<bot-name> と省略形の @bot をそれぞれの Slack 互換メンション形式 <@U...> に正規化する。
// @bot は代表 bot (is_default: true または先頭要素) へ置換する。
func NormalizeBotMentions(text string, bots []BotIdentity) string {
	def := DefaultBot(bots)
	aliases := make(map[string]string, len(bots)+1)
	aliases["bot"] = def.UserID
	for _, b := range bots {
		if name := strings.ToLower(strings.TrimSpace(b.Name)); name != "" {
			aliases[name] = b.UserID
		}
	}

	var b strings.Builder
	last := 0
	for _, loc := range mentionAliasRe.FindAllStringSubmatchIndex(text, -1) {
		fullStart, fullEnd := loc[0], loc[1]
		// メールアドレス等の途中 (直前が英数字・アンダースコア・ドット・ハイフン) はメンション扱いしない。
		if fullStart > 0 && isIdentifierChar(text[fullStart-1]) {
			continue
		}
		name := strings.ToLower(text[loc[2]:loc[3]])
		uid, ok := aliases[name]
		if !ok {
			continue
		}
		b.WriteString(text[last:fullStart])
		b.WriteString("<@" + uid + ">")
		last = fullEnd
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

func isIdentifierChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-'
}

// ExtractMentionedUserIDs はメッセージ本文に含まれる <@U...> 形式のメンション先ユーザー ID 一覧（重複除去・出現順）を返す。
func ExtractMentionedUserIDs(text string) []string {
	matches := slackUserMentRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		uid := m[1]
		if !seen[uid] {
			seen[uid] = true
			ids = append(ids, uid)
		}
	}
	return ids
}

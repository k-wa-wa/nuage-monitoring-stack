package slackapi

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

func slackOK(c echo.Context, fields map[string]interface{}) error {
	res := map[string]interface{}{"ok": true}
	for k, v := range fields {
		res[k] = v
	}
	return c.JSON(http.StatusOK, res)
}

func slackErr(c echo.Context, errCode string) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"ok":    false,
		"error": errCode,
	})
}

func formatSlackChannel(ch domain.Channel) map[string]interface{} {
	m := map[string]interface{}{
		"id":          ch.ID,
		"name":        ch.Name,
		"is_channel":  ch.IsChannel,
		"is_group":    false,
		"is_im":       ch.IsIM,
		"is_private":  false,
		"is_archived": false,
		"is_member":   true,
		"created":     ch.CreatedAt.Unix(),
		"topic": map[string]interface{}{
			"value":    ch.Topic,
			"creator":  domain.SlackBotUserID,
			"last_set": ch.CreatedAt.Unix(),
		},
		"purpose": map[string]interface{}{
			"value":    ch.Purpose,
			"creator":  domain.SlackBotUserID,
			"last_set": ch.CreatedAt.Unix(),
		},
	}
	if ch.IsIM {
		m["user"] = ch.DMUserID
	}
	return m
}

func formatSlackMessage(m domain.Message) map[string]interface{} {
	obj := map[string]interface{}{
		"type":     "message",
		"ts":       m.TS,
		"text":     m.Text,
		"username": m.Username,
	}
	if m.UserID != "" {
		obj["user"] = m.UserID
	}
	if m.BotID != "" {
		obj["bot_id"] = m.BotID
		obj["subtype"] = "bot_message"
	}
	if m.ThreadTS != "" {
		obj["thread_ts"] = m.ThreadTS
	} else if m.ReplyCount > 0 {
		obj["thread_ts"] = m.TS
		obj["reply_count"] = m.ReplyCount
		obj["latest_reply"] = m.LatestReply
	}
	if m.Blocks != "" {
		var blocks interface{}
		if err := json.Unmarshal([]byte(m.Blocks), &blocks); err == nil {
			obj["blocks"] = blocks
		}
	}
	if m.URL != "" {
		obj["url"] = m.URL
	}
	if m.Details != "" {
		obj["details"] = m.Details
	}
	if m.SourceKind != "" {
		obj["source_kind"] = m.SourceKind
	}
	if m.AlertStatus != "" {
		obj["alert_status"] = m.AlertStatus
	}
	// 通常ボット投稿 (PostBotMessage) は SourceKind が空のまま Level="info" を持つため、
	// 既存チャット契約との互換性を保つ目的でアラート・通知メッセージに限って level を出力する。
	if m.SourceKind != "" && m.Level != "" {
		obj["level"] = m.Level
	}
	return obj
}

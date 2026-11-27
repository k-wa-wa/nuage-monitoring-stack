package httpadapter

import (
	"encoding/json"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// AuthUserDTO は認証 API・Bootstrap で返すユーザー情報である。
type AuthUserDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

func toAuthUserDTO(u *domain.User) AuthUserDTO {
	return AuthUserDTO{
		ID:          u.ID,
		Name:        u.EffectiveName(),
		DisplayName: u.DisplayName,
	}
}

// ChannelDTO はチャットチャンネル・DM の API レスポンス表現である。
type ChannelDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	IsChannel   bool      `json:"is_channel"`
	IsIM        bool      `json:"is_im"`
	DMUserID    string    `json:"dm_user_id,omitempty"`
	Topic       string    `json:"topic,omitempty"`
	Purpose     string    `json:"purpose,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UnreadCount int       `json:"unread_count"`
	LastReadTS  string    `json:"last_read_ts,omitempty"`
	LatestTS    string    `json:"latest_ts,omitempty"`
}

func toChannelDTO(ch domain.Channel) ChannelDTO {
	return ChannelDTO{
		ID:          ch.ID,
		Name:        ch.Name,
		IsChannel:   ch.IsChannel,
		IsIM:        ch.IsIM,
		DMUserID:    ch.DMUserID,
		Topic:       ch.Topic,
		Purpose:     ch.Purpose,
		CreatedAt:   ch.CreatedAt,
		UnreadCount: ch.UnreadCount,
		LastReadTS:  ch.LastReadTS,
		LatestTS:    ch.LatestTS,
	}
}

func toChannelDTOs(chs []domain.Channel) []ChannelDTO {
	out := make([]ChannelDTO, 0, len(chs))
	for _, ch := range chs {
		out = append(out, toChannelDTO(ch))
	}
	return out
}

// MessageDTO はメッセージの API レスポンス表現である。
type MessageDTO struct {
	ChannelID   string          `json:"channel_id"`
	TS          string          `json:"ts"`
	ThreadTS    string          `json:"thread_ts,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	BotID       string          `json:"bot_id,omitempty"`
	Username    string          `json:"username"`
	Text        string          `json:"text"`
	Blocks      json.RawMessage `json:"blocks,omitempty"`
	Level       string          `json:"level,omitempty"`
	SourceKind  string          `json:"source_kind,omitempty"`
	Details     string          `json:"details,omitempty"`
	URL         string          `json:"url,omitempty"`
	AlertStatus string          `json:"alert_status,omitempty"`
	FiredAt     *time.Time      `json:"fired_at,omitempty"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
	ReplyCount  int             `json:"reply_count,omitempty"`
	LatestReply string          `json:"latest_reply,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func toMessageDTO(m domain.Message) MessageDTO {
	var blocks json.RawMessage
	if m.Blocks != "" {
		blocks = json.RawMessage(m.Blocks)
	}
	return MessageDTO{
		ChannelID:   m.ChannelID,
		TS:          m.TS,
		ThreadTS:    m.ThreadTS,
		UserID:      m.UserID,
		BotID:       m.BotID,
		Username:    m.Username,
		Text:        m.Text,
		Blocks:      blocks,
		Level:       m.Level,
		SourceKind:  m.SourceKind,
		Details:     m.Details,
		URL:         m.URL,
		AlertStatus: m.AlertStatus,
		FiredAt:     m.FiredAt,
		ResolvedAt:  m.ResolvedAt,
		ReplyCount:  m.ReplyCount,
		LatestReply: m.LatestReply,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func toMessageDTOs(msgs []domain.Message) []MessageDTO {
	out := make([]MessageDTO, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toMessageDTO(m))
	}
	return out
}

// SearchResultDTO は検索結果の API レスポンス表現である。
type SearchResultDTO struct {
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	IsIM        bool       `json:"is_im"`
	Message     MessageDTO `json:"message"`
}

func toSearchResultDTOs(results []domain.SearchResult) []SearchResultDTO {
	out := make([]SearchResultDTO, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResultDTO{
			ChannelID:   r.ChannelID,
			ChannelName: r.ChannelName,
			IsIM:        r.IsIM,
			Message:     toMessageDTO(r.Message),
		})
	}
	return out
}

// ActivityItemDTO は通知タブ一覧要素の API レスポンス表現である。
type ActivityItemDTO struct {
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	Message     MessageDTO `json:"message"`
}

func toActivityItemDTOs(items []domain.ActivityItem) []ActivityItemDTO {
	out := make([]ActivityItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, ActivityItemDTO{
			ChannelID:   it.ChannelID,
			ChannelName: it.ChannelName,
			Message:     toMessageDTO(it.Message),
		})
	}
	return out
}

// BrowserEventDTO はブラウザ向け SSE ストリームで送る JSON ペイロードである。
type BrowserEventDTO struct {
	Type      string      `json:"type"`
	ChannelID string      `json:"channel_id,omitempty"`
	Payload   interface{} `json:"payload"`
}

func toBrowserEventDTO(ev domain.BrowserEvent) BrowserEventDTO {
	var payload interface{}
	if ev.Message != nil {
		payload = toMessageDTO(*ev.Message)
	} else if ev.BotOnline != nil {
		payload = map[string]bool{"online": *ev.BotOnline}
	}
	return BrowserEventDTO{
		Type:      string(ev.Type),
		ChannelID: ev.ChannelID,
		Payload:   payload,
	}
}

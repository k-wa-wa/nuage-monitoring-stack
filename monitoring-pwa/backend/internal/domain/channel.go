package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	ChannelAlertmanager = "C01ALERTMGR"
	ChannelArgoCD       = "C02ARGOCD"
	ChannelKeel         = "C03KEEL"
	ChannelGeneric      = "C04GENERIC"
	ChannelGeneral      = "C05GENERAL"
)

// Channel はチャットチャンネルまたは DM を表す。
type Channel struct {
	ID          string
	Name        string
	IsChannel   bool
	IsIM        bool
	DMUserID    string
	Topic       string
	Purpose     string
	CreatedAt   time.Time
	UnreadCount int
	LastReadTS  string
	LatestTS    string
}

// ChannelType は Slack 互換のチャンネル種別文字列を返す。
func (c Channel) ChannelType() string {
	if c.IsIM {
		return "im"
	}
	return "channel"
}

// DMChannelID はユーザー ID から一意な DM チャンネル ID (D…) を決定的に生成する。
func DMChannelID(userID string) string {
	trimmed := strings.TrimPrefix(userID, "U")
	return "D" + trimmed
}

// DefaultChannels は初期構築時に用意する既定のパブリックチャンネル一覧を返す。
func DefaultChannels(now time.Time) []Channel {
	return []Channel{
		{ID: ChannelAlertmanager, Name: "alertmanager", IsChannel: true, Topic: "Alertmanager アラート", Purpose: "Prometheus / Alertmanager の発報と解決スレッド", CreatedAt: now},
		{ID: ChannelArgoCD, Name: "argocd", IsChannel: true, Topic: "Argo CD 同期通知", Purpose: "Argo CD アプリケーション同期イベント", CreatedAt: now},
		{ID: ChannelKeel, Name: "keel", IsChannel: true, Topic: "Keel 更新通知", Purpose: "Keel コンテナイメージ自動更新イベント", CreatedAt: now},
		{ID: ChannelGeneric, Name: "generic", IsChannel: true, Topic: "汎用 Webhook 通知", Purpose: "汎用 Webhook から送信された通知", CreatedAt: now},
		{ID: ChannelGeneral, Name: "general", IsChannel: true, Topic: "一般チャット", Purpose: "Bot との会話や運用メモ", CreatedAt: now},
	}
}

// DMChannelForUser は人間ユーザーに対応する bot DM チャンネル定義を返す。
func DMChannelForUser(u User, now time.Time) Channel {
	topic := fmt.Sprintf("%s と %s のダイレクトメッセージ", u.DisplayName, SlackBotName)
	return Channel{
		ID:        DMChannelID(u.ID),
		Name:      "dm-" + u.EffectiveName(),
		IsChannel: false,
		IsIM:      true,
		DMUserID:  u.ID,
		Topic:     topic,
		Purpose:   topic,
		CreatedAt: now,
	}
}

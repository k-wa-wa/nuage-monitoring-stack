package domain

import (
	"fmt"
	"net/url"
	"strings"
)

// Notification は Argo CD・Keel・汎用 Webhook からの通知 1 件である。
type Notification struct {
	Source     string // argocd / keel / generic
	Title      string
	Body       string
	Level      string
	Details    string
	URL        string // 通知の対象 (Push を押した時に直接開くこともある)
	DirectOpen bool
}

// PushNotification は Web Push で端末へ送る通知内容である。
type PushNotification struct {
	Title      string
	Body       string
	URL        string
	TargetURL  string
	DirectOpen bool
	Level      string
	Tag        string
}

// ChannelForSource は通知ソース文字列から対応するデフォルトチャンネル ID を返す。
func ChannelForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "alertmanager":
		return ChannelAlertmanager
	case "argocd":
		return ChannelArgoCD
	case "keel":
		return ChannelKeel
	default:
		return ChannelGeneric
	}
}

// InferNotificationSource は明示指定またはタイトルから通知の送信元種別を判定する。
func InferNotificationSource(title, explicitSource string) string {
	s := strings.TrimSpace(explicitSource)
	if s != "" && s != "generic" {
		return s
	}
	if strings.HasPrefix(title, "[Keel]") {
		return "keel"
	}
	if strings.Contains(strings.ToLower(title), "argo") {
		return "argocd"
	}
	return "generic"
}

// ThreadURL は Push 通知から開くスレッドの URL (画面側でタブの中のパスへ移す)。
func ThreadURL(channelID, ts string) string {
	return fmt.Sprintf("/channels/%s/threads/%s", url.PathEscape(channelID), url.PathEscape(ts))
}

// ChannelURL は Push 通知から開くチャンネルの URL を返す。
func ChannelURL(channelID string) string {
	return fmt.Sprintf("/channels/%s", url.PathEscape(channelID))
}

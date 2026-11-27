package domain

import "strings"

// ActivityFilter は通知タブの絞り込みである。
type ActivityFilter string

const (
	ActivityAll      ActivityFilter = "all"
	ActivityFiring   ActivityFilter = "firing"
	ActivityResolved ActivityFilter = "resolved"
	ActivityNotice   ActivityFilter = "notice"
)

// ParseActivityFilter はクエリ文字列を ActivityFilter に解釈する。空は ActivityAll になる。
func ParseActivityFilter(raw string) (ActivityFilter, bool) {
	f := ActivityFilter(strings.TrimSpace(raw))
	switch f {
	case "":
		return ActivityAll, true
	case ActivityAll, ActivityFiring, ActivityResolved, ActivityNotice:
		return f, true
	default:
		return "", false
	}
}

// ActivityItem は通知タブの 1 件 (アラート・通知のスレッドの親メッセージ) である。
type ActivityItem struct {
	ChannelID   string
	ChannelName string
	Message     Message
}

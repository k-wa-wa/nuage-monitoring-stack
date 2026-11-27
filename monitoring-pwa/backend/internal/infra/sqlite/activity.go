package sqlite

import (
	"strings"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// ListActivity はアラート・通知のスレッドの親メッセージを新しい順に返す。beforeTS より古いものだけを返す。
func (db *DB) ListActivity(filter domain.ActivityFilter, beforeTS string, limit int) ([]domain.ActivityItem, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	conds := []string{"m.thread_ts = ''", "m.source_kind != ''"}
	var args []interface{}
	switch filter {
	case domain.ActivityFiring:
		conds = append(conds, "m.source_kind = 'alert'", "m.alert_status = 'firing'")
	case domain.ActivityResolved:
		conds = append(conds, "m.source_kind = 'alert'", "m.alert_status = 'resolved'")
	case domain.ActivityNotice:
		conds = append(conds, "m.source_kind = 'notification'")
	}
	if beforeTS != "" {
		conds = append(conds, "m.ts < ?")
		args = append(args, beforeTS)
	}
	args = append(args, limit+1)

	rows, err := db.conn.Query(
		`SELECT c.name, `+messageSelectCols+`
		 FROM messages m JOIN channels c ON c.id = m.channel_id
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY m.ts DESC LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var items []domain.ActivityItem
	for rows.Next() {
		var name string
		m, err := scanMessageWithPrefix(rows, &name)
		if err != nil {
			return nil, false, err
		}
		items = append(items, domain.ActivityItem{ChannelID: m.ChannelID, ChannelName: name, Message: m})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// CountFiringAlerts は発生中のアラート (スレッドの親メッセージ) の数を返す。
func (db *DB) CountFiringAlerts() (int, error) {
	var n int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE thread_ts = '' AND source_kind = 'alert' AND alert_status = 'firing'`,
	).Scan(&n)
	return n, err
}

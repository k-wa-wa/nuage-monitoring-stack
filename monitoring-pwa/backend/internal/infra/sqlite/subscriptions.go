package sqlite

import (
	"log/slog"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// AddForUser はユーザー ID を紐づけて購読情報を登録または更新する。
func (db *DB) AddForUser(sub domain.PushSubscription, userID string) error {
	_, err := db.conn.Exec(
		`INSERT INTO subscriptions (endpoint, p256dh, auth, user_id, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_id = CASE WHEN excluded.user_id != '' THEN excluded.user_id ELSE subscriptions.user_id END,
			created_at = excluded.created_at`,
		sub.Endpoint, sub.P256dh, sub.Auth, userID, time.Now(),
	)
	return err
}

// Remove は指定された endpoint の購読情報をデータベースから削除する。
func (db *DB) Remove(endpoint string) {
	_, err := db.conn.Exec(`DELETE FROM subscriptions WHERE endpoint = ?`, endpoint)
	if err != nil {
		slog.Error("failed to remove subscription", "endpoint", endpoint, "error", err)
	}
}

// List は全購読情報を取得する。
func (db *DB) List() []domain.PushSubscription {
	return db.listSubscriptionsWhere("", nil)
}

// ListByUser は指定ユーザーに紐づく購読情報一覧を取得する。
func (db *DB) ListByUser(userID string) []domain.PushSubscription {
	return db.listSubscriptionsWhere("WHERE user_id = ?", []interface{}{userID})
}

func (db *DB) listSubscriptionsWhere(where string, args []interface{}) []domain.PushSubscription {
	rows, err := db.conn.Query(`SELECT endpoint, p256dh, auth, user_id FROM subscriptions `+where, args...)
	if err != nil {
		slog.Error("failed to list subscriptions", "error", err)
		return nil
	}
	defer rows.Close()

	var subs []domain.PushSubscription
	for rows.Next() {
		var sub domain.PushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.UserID); err != nil {
			slog.Error("failed to scan subscription", "error", err)
			continue
		}
		subs = append(subs, sub)
	}
	return subs
}

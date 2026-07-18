package push

import (
	"database/sql"
	"log/slog"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	_ "modernc.org/sqlite"
)

// DB は SQLite データベースへの接続を管理する構造体である。
type DB struct {
	conn *sql.DB
}

// Notification は通知履歴の各レコードを表す。
type Notification struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	URL       string    `json:"url,omitempty"`
	Level     string    `json:"level"`
	CreatedAt time.Time `json:"created_at"`
}

// InitDB は指定されたパスの SQLite データベースを初期化し、DB 構造体を返す。
func InitDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

// Close はデータベース接続を閉じる。
func (db *DB) Close() error {
	return db.conn.Close()
}

// migrate は必要なテーブルを初期作成する。
func (db *DB) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS subscriptions (
			endpoint TEXT PRIMARY KEY,
			p256dh TEXT NOT NULL,
			auth TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			url TEXT,
			level TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
	}

	for _, q := range queries {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// Add は購読情報をデータベースに登録、または更新する。
func (db *DB) Add(sub webpush.Subscription) error {
	_, err := db.conn.Exec(
		`INSERT INTO subscriptions (endpoint, p256dh, auth, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			created_at = excluded.created_at`,
		sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth, time.Now(),
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
func (db *DB) List() []webpush.Subscription {
	rows, err := db.conn.Query(`SELECT endpoint, p256dh, auth FROM subscriptions`)
	if err != nil {
		slog.Error("failed to list subscriptions", "error", err)
		return nil
	}
	defer rows.Close()

	var subs []webpush.Subscription
	for rows.Next() {
		var sub webpush.Subscription
		if err := rows.Scan(&sub.Endpoint, &sub.Keys.P256dh, &sub.Keys.Auth); err != nil {
			slog.Error("failed to scan subscription", "error", err)
			continue
		}
		subs = append(subs, sub)
	}
	return subs
}

// SaveNotification は通知履歴を保存する。
func (db *DB) SaveNotification(title, body, url, level string) error {
	_, err := db.conn.Exec(
		`INSERT INTO notifications (title, body, url, level, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		title, body, url, level, time.Now(),
	)
	return err
}

// ListNotifications は通知履歴を指定件数ロードする。
func (db *DB) ListNotifications(limit int) ([]Notification, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, body, url, level, created_at
		 FROM notifications
		 ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Notification
	for rows.Next() {
		var n Notification
		var urlVal sql.NullString
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &urlVal, &n.Level, &n.CreatedAt); err != nil {
			return nil, err
		}
		if urlVal.Valid {
			n.URL = urlVal.String
		}
		notes = append(notes, n)
	}
	return notes, nil
}

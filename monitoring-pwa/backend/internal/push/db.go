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
	Details   string    `json:"details,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Alert はスレッド化されたアラートの状態を表す構造体である。
type Alert struct {
	ID          int64      `json:"id"`
	Fingerprint string     `json:"fingerprint"`
	Alertname   string     `json:"alertname"`
	Status      string     `json:"status"` // firing, resolved
	Level       string     `json:"level"`  // error, warning, info, success
	Summary     string     `json:"summary"`
	Description string     `json:"description,omitempty"`
	URL         string     `json:"url,omitempty"`
	Details     string     `json:"details,omitempty"`
	FiredAt     time.Time  `json:"fired_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
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
			details TEXT,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT UNIQUE NOT NULL,
			alertname TEXT NOT NULL,
			status TEXT NOT NULL,
			level TEXT NOT NULL,
			summary TEXT NOT NULL,
			description TEXT,
			url TEXT,
			details TEXT,
			fired_at DATETIME NOT NULL,
			resolved_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);`,
	}

	for _, q := range queries {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}

	// details カラムが未追加の場合はマイグレーションを実行する。
	rows, err := db.conn.Query(`PRAGMA table_info(notifications)`)
	if err == nil {
		defer rows.Close()
		hasDetails := false
		for rows.Next() {
			var cid int
			var name string
			var typeStr string
			var notnull int
			var dfltVal interface{}
			var pk int
			if err := rows.Scan(&cid, &name, &typeStr, &notnull, &dfltVal, &pk); err == nil {
				if name == "details" {
					hasDetails = true
					break
				}
			}
		}
		if !hasDetails {
			if _, err := db.conn.Exec(`ALTER TABLE notifications ADD COLUMN details TEXT`); err != nil {
				return err
			}
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

// SaveNotification は通知履歴を保存し、自動生成された ID を返す。
func (db *DB) SaveNotification(title, body, url, level, details string) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO notifications (title, body, url, level, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		title, body, url, level, details, time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListNotifications は通知履歴を指定件数ロードする。
func (db *DB) ListNotifications(limit int) ([]Notification, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, body, url, level, details, created_at
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
		var detailsVal sql.NullString
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &urlVal, &n.Level, &detailsVal, &n.CreatedAt); err != nil {
			return nil, err
		}
		if urlVal.Valid {
			n.URL = urlVal.String
		}
		if detailsVal.Valid {
			n.Details = detailsVal.String
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// UpsertAlert はアラートの状態（firing / resolved）を保存・更新し、Alert レコードを返す。
func (db *DB) UpsertAlert(fingerprint, alertname, status, level, summary, description, url, details string) (*Alert, error) {
	now := time.Now()

	var existingID int64
	var existingStatus string
	var existingFiredAt time.Time
	var existingCreatedAt time.Time

	err := db.conn.QueryRow(
		`SELECT id, status, fired_at, created_at FROM alerts WHERE fingerprint = ?`,
		fingerprint,
	).Scan(&existingID, &existingStatus, &existingFiredAt, &existingCreatedAt)

	if err == sql.ErrNoRows {
		// 新規挿入
		var res sql.Result
		var insertErr error
		if status == "firing" {
			res, insertErr = db.conn.Exec(
				`INSERT INTO alerts (fingerprint, alertname, status, level, summary, description, url, details, fired_at, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				fingerprint, alertname, status, level, summary, description, url, details, now, now, now,
			)
		} else {
			// いきなり resolved がきた場合
			res, insertErr = db.conn.Exec(
				`INSERT INTO alerts (fingerprint, alertname, status, level, summary, description, url, details, fired_at, resolved_at, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				fingerprint, alertname, status, level, summary, description, url, details, now, now, now, now,
			)
		}
		if insertErr != nil {
			return nil, insertErr
		}
		id, _ := res.LastInsertId()

		var resolvedAt *time.Time
		if status == "resolved" {
			resolvedAt = &now
		}

		return &Alert{
			ID:          id,
			Fingerprint: fingerprint,
			Alertname:   alertname,
			Status:      status,
			Level:       level,
			Summary:     summary,
			Description: description,
			URL:         url,
			Details:     details,
			FiredAt:     now,
			ResolvedAt:  resolvedAt,
			CreatedAt:   now,
			UpdatedAt:   now,
		}, nil
	} else if err != nil {
		return nil, err
	}

	// 既存更新
	if status == "firing" {
		// 再度 firing になった場合
		_, updateErr := db.conn.Exec(
			`UPDATE alerts SET status = ?, level = ?, summary = ?, description = ?, url = ?, details = ?, fired_at = ?, resolved_at = NULL, updated_at = ?
			 WHERE id = ?`,
			status, level, summary, description, url, details, now, now, existingID,
		)
		if updateErr != nil {
			return nil, updateErr
		}
		return &Alert{
			ID:          existingID,
			Fingerprint: fingerprint,
			Alertname:   alertname,
			Status:      status,
			Level:       level,
			Summary:     summary,
			Description: description,
			URL:         url,
			Details:     details,
			FiredAt:     now,
			ResolvedAt:  nil,
			CreatedAt:   existingCreatedAt,
			UpdatedAt:   now,
		}, nil
	} else {
		// resolved になった場合
		effectiveLevel := "success"
		_, updateErr := db.conn.Exec(
			`UPDATE alerts SET status = ?, level = ?, resolved_at = ?, updated_at = ?
			 WHERE id = ?`,
			status, effectiveLevel, now, now, existingID,
		)
		if updateErr != nil {
			return nil, updateErr
		}
		return &Alert{
			ID:          existingID,
			Fingerprint: fingerprint,
			Alertname:   alertname,
			Status:      status,
			Level:       effectiveLevel,
			Summary:     summary,
			Description: description,
			URL:         url,
			Details:     details,
			FiredAt:     existingFiredAt,
			ResolvedAt:  &now,
			CreatedAt:   existingCreatedAt,
			UpdatedAt:   now,
		}, nil
	}
}

// ListActiveAlerts は現在 firing 状態のアラート一覧を取得する。
func (db *DB) ListActiveAlerts() ([]Alert, error) {
	rows, err := db.conn.Query(
		`SELECT id, fingerprint, alertname, status, level, summary, description, url, details, fired_at, resolved_at, created_at, updated_at
		 FROM alerts
		 WHERE status = 'firing'
		 ORDER BY fired_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// ListAlertHistory は更新日時順に全アラート履歴（スレッド情報）を取得する。
func (db *DB) ListAlertHistory(limit int) ([]Alert, error) {
	rows, err := db.conn.Query(
		`SELECT id, fingerprint, alertname, status, level, summary, description, url, details, fired_at, resolved_at, created_at, updated_at
		 FROM alerts
		 ORDER BY updated_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAlerts(rows)
}

func scanAlerts(rows *sql.Rows) ([]Alert, error) {
	var alerts []Alert
	for rows.Next() {
		var a Alert
		var descVal, urlVal, detailsVal sql.NullString
		var resolvedAtVal sql.NullTime

		if err := rows.Scan(
			&a.ID, &a.Fingerprint, &a.Alertname, &a.Status, &a.Level, &a.Summary,
			&descVal, &urlVal, &detailsVal, &a.FiredAt, &resolvedAtVal, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if descVal.Valid {
			a.Description = descVal.String
		}
		if urlVal.Valid {
			a.URL = urlVal.String
		}
		if detailsVal.Valid {
			a.Details = detailsVal.String
		}
		if resolvedAtVal.Valid {
			a.ResolvedAt = &resolvedAtVal.Time
		}

		alerts = append(alerts, a)
	}
	return alerts, nil
}


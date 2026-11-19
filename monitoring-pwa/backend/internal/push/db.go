package push

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// GenerateFingerprint はアラートラベルから一意に識別するキーを生成する。
func GenerateFingerprint(labels map[string]string) string {
	alertname := labels["alertname"]
	if alertname == "" {
		alertname = "Alert"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(alertname)
	for _, k := range keys {
		if k == "alertname" {
			continue
		}
		sb.WriteString(fmt.Sprintf(";%s=%s", k, labels[k]))
	}
	return sb.String()
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

	if err := db.checkWritable(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("database is not writable: %w", err)
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
			source TEXT,
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

	// details / source カラムが未追加の場合はマイグレーションを実行する。
	if err := db.ensureNotificationColumns([]string{"details", "source"}); err != nil {
		return err
	}

	// 過去の取りこぼしアラート（firing のまま残存していたゴーストアラート）を resolved に更新する。
	// アクティブアラートは Alertmanager API からリアルタイム取得するため、DB 上の未解決レコードを解消する。
	_, _ = db.conn.Exec(`UPDATE alerts SET status = 'resolved', level = 'success', resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE status = 'firing'`)

	return nil
}

// ensureNotificationColumns は notifications テーブルに不足している TEXT カラムを追加する。
func (db *DB) ensureNotificationColumns(requiredCols []string) error {
	rows, err := db.conn.Query(`PRAGMA table_info(notifications)`)
	if err != nil {
		return err
	}

	existing := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typeStr string
		var notnull int
		var dfltVal interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typeStr, &notnull, &dfltVal, &pk); err == nil {
			existing[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, col := range requiredCols {
		if !existing[col] {
			if _, err := db.conn.Exec(fmt.Sprintf(`ALTER TABLE notifications ADD COLUMN %s TEXT`, col)); err != nil {
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
	return db.SaveNotificationWithSource(title, body, url, level, details, "")
}

// SaveNotificationWithSource は送信元（source）付きで通知履歴を保存し、自動生成された ID を返す。
func (db *DB) SaveNotificationWithSource(title, body, url, level, details, source string) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO notifications (title, body, url, level, details, source, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		title, body, url, level, details, source, time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListNotifications は通知履歴を指定件数ロードする。
func (db *DB) ListNotifications(limit int) ([]Notification, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, body, url, level, details, source, created_at
		 FROM notifications
		 ORDER BY created_at DESC, id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// GetNotificationByID は指定 ID の通知履歴を取得する。見つからない場合は nil, sql.ErrNoRows を返す。
func (db *DB) GetNotificationByID(id int64) (*Notification, error) {
	row := db.conn.QueryRow(
		`SELECT id, title, body, url, level, details, source, created_at
		 FROM notifications
		 WHERE id = ?`,
		id,
	)
	n, err := scanNotification(row)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanNotification(s rowScanner) (Notification, error) {
	var n Notification
	var urlVal, detailsVal, sourceVal sql.NullString
	if err := s.Scan(&n.ID, &n.Title, &n.Body, &urlVal, &n.Level, &detailsVal, &sourceVal, &n.CreatedAt); err != nil {
		return Notification{}, err
	}
	if urlVal.Valid {
		n.URL = urlVal.String
	}
	if detailsVal.Valid {
		n.Details = detailsVal.String
	}
	if sourceVal.Valid {
		n.Source = sourceVal.String
	}
	return n, nil
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

// GetAlertByID は指定 ID のアラートを取得する。見つからない場合は nil, sql.ErrNoRows を返す。
func (db *DB) GetAlertByID(id int64) (*Alert, error) {
	row := db.conn.QueryRow(
		`SELECT id, fingerprint, alertname, status, level, summary, description, url, details, fired_at, resolved_at, created_at, updated_at
		 FROM alerts
		 WHERE id = ?`,
		id,
	)
	a, err := scanAlert(row)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func scanAlerts(rows *sql.Rows) ([]Alert, error) {
	var alerts []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func scanAlert(s rowScanner) (Alert, error) {
	var a Alert
	var descVal, urlVal, detailsVal sql.NullString
	var resolvedAtVal sql.NullTime

	if err := s.Scan(
		&a.ID, &a.Fingerprint, &a.Alertname, &a.Status, &a.Level, &a.Summary,
		&descVal, &urlVal, &detailsVal, &a.FiredAt, &resolvedAtVal, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return Alert{}, err
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
	return a, nil
}

// checkWritable はデータベースが書き込み可能であるかを検査する。
// SQLite が読み取り専用モードで開かれた場合にサイレントに起動して後続の更新が失敗するのを防ぐ。
func (db *DB) checkWritable() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 実際に書き込みを行って物理ファイルへの書き込み権限を検証する。
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS _permission_check (id INTEGER PRIMARY KEY);
		INSERT OR REPLACE INTO _permission_check (id) VALUES (1);
		DELETE FROM _permission_check WHERE id = 1;
	`); err != nil {
		return err
	}

	return tx.Commit()
}



package push

import (
	"database/sql"
	"errors"
	"time"
)

// AlertState はアラート (fingerprint) ごとの「今発生中の回のスレッド」を覚える。
// アラートの中身 (状態・詳細・リンク) はスレッドの親メッセージ (messages) を正とし、ここには写さない。
type AlertState struct {
	// Fingerprint は Alertmanager が振るアラートの識別子 (Webhook と API の両方に入っている)
	Fingerprint string
	Alertname   string
	// OpenThreadTS は発生中の回のスレッド (#alertmanager の親メッセージの ts)。解決したら空になる。
	OpenThreadTS string
	UpdatedAt    time.Time
}

func (db *DB) migrateAlertSchema() error {
	_, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS alerts (
		fingerprint TEXT PRIMARY KEY,
		alertname TEXT NOT NULL,
		open_thread_ts TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP NOT NULL
	);`)
	return err
}

// GetAlertState は fingerprint の状態を返す。一度も見たことが無ければ nil を返す。
func (db *DB) GetAlertState(fingerprint string) (*AlertState, error) {
	var st AlertState
	err := db.conn.QueryRow(
		`SELECT fingerprint, alertname, open_thread_ts, updated_at FROM alerts WHERE fingerprint = ?`,
		fingerprint,
	).Scan(&st.Fingerprint, &st.Alertname, &st.OpenThreadTS, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// SetAlertOpenThread は fingerprint の発生中のスレッドを記録する。threadTS を空にすると「発生中ではない」になる。
func (db *DB) SetAlertOpenThread(fingerprint, alertname, threadTS string) error {
	_, err := db.conn.Exec(
		`INSERT INTO alerts (fingerprint, alertname, open_thread_ts, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(fingerprint) DO UPDATE SET
			alertname = excluded.alertname,
			open_thread_ts = excluded.open_thread_ts,
			updated_at = excluded.updated_at`,
		fingerprint, alertname, threadTS, time.Now(),
	)
	return err
}

// ListOpenAlerts は発生中のスレッドを持つアラートを返す (Alertmanager との突き合わせに使う)。
func (db *DB) ListOpenAlerts() ([]AlertState, error) {
	rows, err := db.conn.Query(
		`SELECT fingerprint, alertname, open_thread_ts, updated_at FROM alerts WHERE open_thread_ts != '' ORDER BY fingerprint`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertState
	for rows.Next() {
		var st AlertState
		if err := rows.Scan(&st.Fingerprint, &st.Alertname, &st.OpenThreadTS, &st.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

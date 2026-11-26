package push

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	_ "modernc.org/sqlite"
)

// DB は SQLite データベースへの接続を管理する構造体である。
type DB struct {
	conn *sql.DB
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

	// SQLite の同時書き込みによる SQLITE_BUSY を防ぐ
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
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

// schemaVersion は DB の作りの版である。上がった時は、古い作りのデータを捨てて作り直す (後方互換は持たない)。
//
//	2: アラート・通知の中身を messages に一本化し、notifications テーブルを廃止した。
//	3: ts を実際の時刻 (µs) で振り、アラートの鍵を Alertmanager の fingerprint にした。
//	   版の記録を SQLite 専用の PRAGMA user_version から schema_migrations テーブルに移した。
const schemaVersion = 3

// migrate は必要なテーブルを作る。Web Push の購読 (subscriptions) だけは版が上がっても残す。
// 版は schema_migrations テーブルに記録する (どの DB でも使える形にしておき、将来 Postgres へ移せるようにする)。
func (db *DB) migrate() error {
	if _, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS subscriptions (
		endpoint TEXT PRIMARY KEY,
		p256dh TEXT NOT NULL,
		auth TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL
	);`); err != nil {
		return err
	}
	if err := db.ensureTableColumns("subscriptions", []string{"user_id"}); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	);`); err != nil {
		return err
	}

	var version int
	if err := db.conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than this server (%d)", version, schemaVersion)
	}
	if version < schemaVersion {
		slog.Warn("schema version changed; dropping old chat and alert data", "from", version, "to", schemaVersion)
		for _, table := range []string{"notifications", "alerts", "channel_reads", "messages", "channels"} {
			if _, err := db.conn.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, table)); err != nil {
				return err
			}
		}
	}

	if err := db.migrateChatSchema(); err != nil {
		return err
	}
	if err := db.migrateAlertSchema(); err != nil {
		return err
	}

	if version < schemaVersion {
		if _, err := db.conn.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, schemaVersion, time.Now(),
		); err != nil {
			return err
		}
	}
	return nil
}

// ensureTableColumns は指定テーブルに不足している TEXT カラムを追加する。
func (db *DB) ensureTableColumns(table string, requiredCols []string) error {
	rows, err := db.conn.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
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
			if _, err := db.conn.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, table, col)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Add は購読情報をデータベースに登録、または更新する。
func (db *DB) Add(sub webpush.Subscription) error {
	return db.AddForUser(sub, "")
}

// AddForUser はユーザー ID を紐づけて購読情報を登録または更新する。
func (db *DB) AddForUser(sub webpush.Subscription, userID string) error {
	_, err := db.conn.Exec(
		`INSERT INTO subscriptions (endpoint, p256dh, auth, user_id, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_id = CASE WHEN excluded.user_id != '' THEN excluded.user_id ELSE subscriptions.user_id END,
			created_at = excluded.created_at`,
		sub.Endpoint, sub.Keys.P256dh, sub.Keys.Auth, userID, time.Now(),
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
	return db.listSubscriptionsWhere("", nil)
}

// ListByUser は指定ユーザーに紐づく購読情報一覧を取得する。
func (db *DB) ListByUser(userID string) []webpush.Subscription {
	return db.listSubscriptionsWhere("WHERE user_id = ?", []interface{}{userID})
}

func (db *DB) listSubscriptionsWhere(where string, args []interface{}) []webpush.Subscription {
	rows, err := db.conn.Query(`SELECT endpoint, p256dh, auth FROM subscriptions `+where, args...)
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

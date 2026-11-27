package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

func (db *DB) migrateChatSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			is_channel INTEGER NOT NULL DEFAULT 1,
			is_im INTEGER NOT NULL DEFAULT 0,
			dm_user_id TEXT NOT NULL DEFAULT '',
			topic TEXT NOT NULL DEFAULT '',
			purpose TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id TEXT NOT NULL,
			ts TEXT NOT NULL,
			thread_ts TEXT NOT NULL DEFAULT '',
			user_id TEXT NOT NULL DEFAULT '',
			bot_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL,
			blocks TEXT NOT NULL DEFAULT '',
			level TEXT NOT NULL DEFAULT '',
			source_kind TEXT NOT NULL DEFAULT '',
			details TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			alert_status TEXT NOT NULL DEFAULT '',
			fired_at TIMESTAMP,
			resolved_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			UNIQUE(channel_id, ts)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_channel_thread_ts ON messages(channel_id, thread_ts, ts);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_activity ON messages(source_kind, thread_ts, ts);`,
		`CREATE TABLE IF NOT EXISTS channel_reads (
			user_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			last_read_ts TEXT NOT NULL,
			PRIMARY KEY (user_id, channel_id)
		);`,
	}

	for _, q := range queries {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}

	// デフォルトの通知・チャットチャンネルを作成
	now := time.Now()
	for _, ch := range domain.DefaultChannels(now) {
		if _, err := db.conn.Exec(
			`INSERT OR IGNORE INTO channels (id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at)
			 VALUES (?, ?, 1, 0, '', ?, ?, ?)`,
			ch.ID, ch.Name, ch.Topic, ch.Purpose, ch.CreatedAt,
		); err != nil {
			return err
		}
	}

	return nil
}

// EnsureUserChannels は宣言された全人間ユーザーに対応する bot DM チャンネルを作成する。
func (db *DB) EnsureUserChannels(users []domain.User) error {
	now := time.Now()
	for _, u := range users {
		ch := domain.DMChannelForUser(u, now)
		if _, err := db.conn.Exec(
			`INSERT INTO channels (id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at)
			 VALUES (?, ?, 0, 1, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				dm_user_id = excluded.dm_user_id,
				topic = excluded.topic,
				purpose = excluded.purpose`,
			ch.ID, ch.Name, ch.DMUserID, ch.Topic, ch.Purpose, ch.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

// GetChannelByID は指定 ID のチャンネルを取得する。
func (db *DB) GetChannelByID(id string) (*domain.Channel, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at
		 FROM channels WHERE id = ?`,
		id,
	)
	var ch domain.Channel
	var isCh, isIM int
	if err := row.Scan(&ch.ID, &ch.Name, &isCh, &isIM, &ch.DMUserID, &ch.Topic, &ch.Purpose, &ch.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, usecase.ErrNotFound
		}
		return nil, err
	}
	ch.IsChannel = isCh != 0
	ch.IsIM = isIM != 0
	return &ch, nil
}

// ListChannelsForUser は指定ユーザーがアクセス可能なチャンネル一覧と未読数を取得する。
func (db *DB) ListChannelsForUser(userID string) ([]domain.Channel, error) {
	rows, err := db.conn.Query(
		`SELECT c.id, c.name, c.is_channel, c.is_im, c.dm_user_id, c.topic, c.purpose, c.created_at,
		        COALESCE(r.last_read_ts, '') AS last_read_ts,
		        COALESCE((SELECT MAX(m.ts) FROM messages m WHERE m.channel_id = c.id), '') AS latest_ts,
		        (SELECT COUNT(*) FROM messages m
		         WHERE m.channel_id = c.id
		           AND m.user_id != ?
		           AND m.ts > COALESCE(r.last_read_ts, '')) AS unread_count
		 FROM channels c
		 LEFT JOIN channel_reads r ON r.channel_id = c.id AND r.user_id = ?
		 WHERE c.is_channel = 1 OR c.dm_user_id = ?
		 ORDER BY c.is_im ASC, c.id ASC`,
		userID, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []domain.Channel
	for rows.Next() {
		var ch domain.Channel
		var isCh, isIM int
		if err := rows.Scan(
			&ch.ID, &ch.Name, &isCh, &isIM, &ch.DMUserID, &ch.Topic, &ch.Purpose, &ch.CreatedAt,
			&ch.LastReadTS, &ch.LatestTS, &ch.UnreadCount,
		); err != nil {
			return nil, err
		}
		ch.IsChannel = isCh != 0
		ch.IsIM = isIM != 0
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// ListAllChannels は Slack API (`conversations.list`) 向けにチャンネル一覧を取得する。
func (db *DB) ListAllChannels(includePublic, includeIM bool) ([]domain.Channel, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at
		 FROM channels
		 ORDER BY is_im ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []domain.Channel
	for rows.Next() {
		var ch domain.Channel
		var isCh, isIM int
		if err := rows.Scan(&ch.ID, &ch.Name, &isCh, &isIM, &ch.DMUserID, &ch.Topic, &ch.Purpose, &ch.CreatedAt); err != nil {
			return nil, err
		}
		ch.IsChannel = isCh != 0
		ch.IsIM = isIM != 0
		if (ch.IsChannel && includePublic) || (ch.IsIM && includeIM) {
			channels = append(channels, ch)
		}
	}
	return channels, rows.Err()
}

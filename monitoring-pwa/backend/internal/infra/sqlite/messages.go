package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

const createMessageMaxAttempts = 16

// nowFunc は ts を振る時刻である (テストで差し替える)。
var nowFunc = time.Now

// formatTS は µs の時刻を Slack の ts の形 ("1700000000.000100") にする。
func formatTS(unixMicro int64) string {
	return fmt.Sprintf("%010d.%06d", unixMicro/1_000_000, unixMicro%1_000_000)
}

// isUniqueViolation は一意制約の違反かを判定する。
// SQLite (modernc) はエラーコードを型で公開していないため文言で判定する。Postgres へ移す時は SQLSTATE 23505 で判定する。
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateMessage は新規メッセージを保存し、振った ts を持つ Message を返す。
//
// ts は Slack と同じく実際の時刻 "秒.マイクロ秒" で振る。同じチャンネルで同じ ts が既にあれば
// (同じ µs に書き込まれた、別の Pod が先に書いた等) 1µs ずらして書き直す。
// プロセスのメモリにも DB の連番にも頼らないので、Pod が増えても DB を替えても同じ作りで動く。
func (db *DB) CreateMessage(p domain.CreateMessageParams) (*domain.Message, error) {
	now := nowFunc()
	ts := now.UnixMicro()
	for attempt := 0; attempt < createMessageMaxAttempts; attempt++ {
		tsStr := formatTS(ts)
		res, err := db.conn.Exec(
			`INSERT INTO messages (
				channel_id, ts, thread_ts, user_id, bot_id, username, text, blocks, level,
				source_kind, details, url, alert_status, fired_at, resolved_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ChannelID, tsStr, p.ThreadTS, p.UserID, p.BotID, p.Username, p.Text, p.Blocks, p.Level,
			p.SourceKind, p.Details, p.URL, p.AlertStatus, p.FiredAt, p.ResolvedAt, now, now,
		)
		if err != nil {
			if isUniqueViolation(err) {
				ts++
				continue
			}
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		return db.GetMessageByID(id)
	}
	return nil, fmt.Errorf("could not allocate a unique ts in channel %s after %d attempts", p.ChannelID, createMessageMaxAttempts)
}

// UpdateMessage は既存メッセージの本文・Blocks を更新する。
func (db *DB) UpdateMessage(channelID, ts, text, blocks string) (*domain.Message, error) {
	now := time.Now()
	res, err := db.conn.Exec(
		`UPDATE messages SET text = ?, blocks = ?, updated_at = ? WHERE channel_id = ? AND ts = ?`,
		text, blocks, now, channelID, ts,
	)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, usecase.ErrNotFound
	}
	return db.GetMessageByTS(channelID, ts)
}

// UpdateAlertRoot はアラートのスレッドの親メッセージの状態を更新する。
func (db *DB) UpdateAlertRoot(channelID, ts string, u domain.AlertRootUpdate) (*domain.Message, error) {
	res, err := db.conn.Exec(
		`UPDATE messages
		 SET alert_status = ?, level = ?, resolved_at = ?,
		     details = CASE WHEN ? != '' THEN ? ELSE details END,
		     url = CASE WHEN ? != '' THEN ? ELSE url END,
		     updated_at = ?
		 WHERE channel_id = ? AND ts = ? AND thread_ts = ''`,
		u.Status, u.Level, u.ResolvedAt,
		u.Details, u.Details,
		u.URL, u.URL,
		time.Now(), channelID, ts,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, usecase.ErrNotFound
	}
	return db.GetMessageByTS(channelID, ts)
}

const messageSelectCols = `
	m.id, m.channel_id, m.ts, m.thread_ts, m.user_id, m.bot_id, m.username, m.text, m.blocks, m.level,
	m.source_kind, m.details, m.url, m.alert_status, m.fired_at, m.resolved_at,
	(SELECT COUNT(*) FROM messages r WHERE r.channel_id = m.channel_id AND r.thread_ts = m.ts) AS reply_count,
	COALESCE((SELECT MAX(r.ts) FROM messages r WHERE r.channel_id = m.channel_id AND r.thread_ts = m.ts), '') AS latest_reply,
	m.created_at, m.updated_at`

// GetMessageByID は内部 ID からメッセージを取得する。
func (db *DB) GetMessageByID(id int64) (*domain.Message, error) {
	row := db.conn.QueryRow(`SELECT `+messageSelectCols+` FROM messages m WHERE m.id = ?`, id)
	msg, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, usecase.ErrNotFound
		}
		return nil, err
	}
	return &msg, nil
}

// GetMessageByTS は (channel_id, ts) からメッセージを取得する。
func (db *DB) GetMessageByTS(channelID, ts string) (*domain.Message, error) {
	row := db.conn.QueryRow(
		`SELECT `+messageSelectCols+` FROM messages m WHERE m.channel_id = ? AND m.ts = ?`,
		channelID, ts,
	)
	msg, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, usecase.ErrNotFound
		}
		return nil, err
	}
	return &msg, nil
}

// ListChannelMessagesDesc はチャンネルのルートメッセージを新しい順（Slack API 準拠）で返す。
func (db *DB) ListChannelMessagesDesc(channelID, oldest, latest string, inclusive bool, limit int) ([]domain.Message, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	conds := []string{"m.channel_id = ?", "m.thread_ts = ''"}
	args := []interface{}{channelID}
	if oldest != "" {
		op := ">"
		if inclusive {
			op = ">="
		}
		conds = append(conds, fmt.Sprintf("m.ts %s ?", op))
		args = append(args, oldest)
	}
	if latest != "" {
		op := "<"
		if inclusive {
			op = "<="
		}
		conds = append(conds, fmt.Sprintf("m.ts %s ?", op))
		args = append(args, latest)
	}
	args = append(args, limit+1)

	query := `SELECT ` + messageSelectCols + ` FROM messages m WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY m.ts DESC LIMIT ?`
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := false
	if len(msgs) > limit {
		hasMore = true
		msgs = msgs[:limit]
	}
	return msgs, hasMore, nil
}

// ListChannelMessagesAsc はチャット UI 向けに古い→新しい順でルートメッセージを返す。
func (db *DB) ListChannelMessagesAsc(channelID, beforeTS string, limit int) ([]domain.Message, bool, error) {
	msgsDesc, hasMore, err := db.ListChannelMessagesDesc(channelID, "", beforeTS, false, limit)
	if err != nil {
		return nil, false, err
	}
	for i, j := 0, len(msgsDesc)-1; i < j; i, j = i+1, j-1 {
		msgsDesc[i], msgsDesc[j] = msgsDesc[j], msgsDesc[i]
	}
	return msgsDesc, hasMore, nil
}

// GetThreadMessages は親メッセージと返信一覧（古い順）を返す。
func (db *DB) GetThreadMessages(channelID, parentTS string) (*domain.Message, []domain.Message, error) {
	parent, err := db.GetMessageByTS(channelID, parentTS)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.conn.Query(
		`SELECT `+messageSelectCols+`
		 FROM messages m
		 WHERE m.channel_id = ? AND m.thread_ts = ?
		 ORDER BY m.ts ASC`,
		channelID, parentTS,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var replies []domain.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, nil, err
		}
		replies = append(replies, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return parent, replies, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanMessage(s rowScanner) (domain.Message, error) {
	return scanMessageWithPrefix(s)
}

// scanMessageWithPrefix は、messageSelectCols の前に並べた列 (prefix) も一緒に読む。
func scanMessageWithPrefix(s rowScanner, prefix ...interface{}) (domain.Message, error) {
	var m domain.Message
	var firedAtVal, resolvedAtVal sql.NullTime

	dest := append(prefix,
		&m.ID, &m.ChannelID, &m.TS, &m.ThreadTS, &m.UserID, &m.BotID, &m.Username, &m.Text,
		&m.Blocks, &m.Level, &m.SourceKind, &m.Details, &m.URL, &m.AlertStatus,
		&firedAtVal, &resolvedAtVal, &m.ReplyCount, &m.LatestReply, &m.CreatedAt, &m.UpdatedAt,
	)
	if err := s.Scan(dest...); err != nil {
		return domain.Message{}, err
	}
	if firedAtVal.Valid {
		m.FiredAt = &firedAtVal.Time
	}
	if resolvedAtVal.Valid {
		m.ResolvedAt = &resolvedAtVal.Time
	}
	return m, nil
}

package push

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

const (
	ChannelAlertmanager = "C01ALERTMGR"
	ChannelArgoCD       = "C02ARGOCD"
	ChannelKeel         = "C03KEEL"
	ChannelGeneric      = "C04GENERIC"
	ChannelGeneral      = "C05GENERAL"

	SystemBotID     = "B00SYSTEM"
	SystemBotUserID = "U00SYSTEM"

	SlackBotID     = "B01BOT"
	SlackBotUserID = "U01BOT"
	SlackBotName   = "nuage-bot"
)

// Channel はチャットチャンネルまたは DM を表す構造体である。
type Channel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	IsChannel   bool      `json:"is_channel"`
	IsIM        bool      `json:"is_im"`
	DMUserID    string    `json:"dm_user_id,omitempty"`
	Topic       string    `json:"topic,omitempty"`
	Purpose     string    `json:"purpose,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UnreadCount int       `json:"unread_count"`
	LastReadTS  string    `json:"last_read_ts,omitempty"`
	LatestTS    string    `json:"latest_ts,omitempty"`
}

// Message はチャンネルまたはスレッド内の1件のメッセージを表す構造体である。
type Message struct {
	// ID は DB の中だけで使う。メッセージは (ChannelID, TS) で識別する (Slack と同じ) ので API には出さない
	ID          int64           `json:"-"`
	ChannelID   string          `json:"channel_id"`
	TS          string          `json:"ts"`
	ThreadTS    string          `json:"thread_ts,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	BotID       string          `json:"bot_id,omitempty"`
	Username    string          `json:"username"`
	Text        string          `json:"text"`
	Blocks      json.RawMessage `json:"blocks,omitempty"`
	Level       string          `json:"level,omitempty"`
	SourceKind  string          `json:"source_kind,omitempty"`
	Details     string          `json:"details,omitempty"`
	URL         string          `json:"url,omitempty"`
	AlertStatus string          `json:"alert_status,omitempty"`
	FiredAt     *time.Time      `json:"fired_at,omitempty"`
	ResolvedAt  *time.Time      `json:"resolved_at,omitempty"`
	ReplyCount  int             `json:"reply_count,omitempty"`
	LatestReply string          `json:"latest_reply,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// SearchResult はキーワード検索のヒット結果である。
type SearchResult struct {
	ChannelID   string  `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	IsIM        bool    `json:"is_im"`
	Message     Message `json:"message"`
}

// DMChannelID はユーザー ID から一意な DM チャンネル ID (D…) を決定的に生成する。
func DMChannelID(userID string) string {
	trimmed := strings.TrimPrefix(userID, "U")
	return "D" + trimmed
}

// ChannelForSource は通知ソース文字列から対応するデフォルトチャンネル ID を返す。
func ChannelForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "alertmanager":
		return ChannelAlertmanager
	case "argocd":
		return ChannelArgoCD
	case "keel":
		return ChannelKeel
	default:
		return ChannelGeneric
	}
}

// InferNotificationSource は明示指定またはタイトルから通知の送信元種別を判定する。
func InferNotificationSource(title, explicitSource string) string {
	s := strings.TrimSpace(explicitSource)
	if s != "" && s != "generic" {
		return s
	}
	if strings.HasPrefix(title, "[Keel]") {
		return "keel"
	}
	if strings.Contains(strings.ToLower(title), "argo") {
		return "argocd"
	}
	return "generic"
}

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
	defaults := []Channel{
		{ID: ChannelAlertmanager, Name: "alertmanager", IsChannel: true, Topic: "Alertmanager アラート", Purpose: "Prometheus / Alertmanager の発報と解決スレッド"},
		{ID: ChannelArgoCD, Name: "argocd", IsChannel: true, Topic: "Argo CD 同期通知", Purpose: "Argo CD アプリケーション同期イベント"},
		{ID: ChannelKeel, Name: "keel", IsChannel: true, Topic: "Keel 更新通知", Purpose: "Keel コンテナイメージ自動更新イベント"},
		{ID: ChannelGeneric, Name: "generic", IsChannel: true, Topic: "汎用 Webhook 通知", Purpose: "汎用 Webhook から送信された通知"},
		{ID: ChannelGeneral, Name: "general", IsChannel: true, Topic: "一般チャット", Purpose: "Bot との会話や運用メモ"},
	}
	for _, ch := range defaults {
		if _, err := db.conn.Exec(
			`INSERT OR IGNORE INTO channels (id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at)
			 VALUES (?, ?, 1, 0, '', ?, ?, ?)`,
			ch.ID, ch.Name, ch.Topic, ch.Purpose, now,
		); err != nil {
			return err
		}
	}

	return nil
}

// EnsureUserChannels は宣言された全人間ユーザーに対応する bot DM チャンネルを作成する。
func (db *DB) EnsureUserChannels(users []config.UserConfig) error {
	now := time.Now()
	for _, u := range users {
		dmID := DMChannelID(u.ID)
		name := "dm-" + u.EffectiveName()
		topic := fmt.Sprintf("%s と %s のダイレクトメッセージ", u.DisplayName, SlackBotName)
		if _, err := db.conn.Exec(
			`INSERT INTO channels (id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at)
			 VALUES (?, ?, 0, 1, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				dm_user_id = excluded.dm_user_id,
				topic = excluded.topic,
				purpose = excluded.purpose`,
			dmID, name, u.ID, topic, topic, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// GetChannelByID は指定 ID のチャンネルを取得する。
func (db *DB) GetChannelByID(id string) (*Channel, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at
		 FROM channels WHERE id = ?`,
		id,
	)
	var ch Channel
	var isCh, isIM int
	if err := row.Scan(&ch.ID, &ch.Name, &isCh, &isIM, &ch.DMUserID, &ch.Topic, &ch.Purpose, &ch.CreatedAt); err != nil {
		return nil, err
	}
	ch.IsChannel = isCh != 0
	ch.IsIM = isIM != 0
	return &ch, nil
}

// ListChannels は指定フィルタ（チャンネル・DM・特定ユーザー向け）でチャンネル一覧を取得する。
func (db *DB) ListChannelsForUser(userID string) ([]Channel, error) {
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

	var channels []Channel
	for rows.Next() {
		var ch Channel
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
func (db *DB) ListAllChannels(includePublic, includeIM bool) ([]Channel, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, is_channel, is_im, dm_user_id, topic, purpose, created_at
		 FROM channels
		 ORDER BY is_im ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
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

// CreateMessageParams は新規メッセージ作成のパラメータである。
type CreateMessageParams struct {
	ChannelID   string
	ThreadTS    string
	UserID      string
	BotID       string
	Username    string
	Text        string
	Blocks      string
	Level       string
	SourceKind  string
	Details     string
	URL         string
	AlertStatus string
	FiredAt     *time.Time
	ResolvedAt  *time.Time
}

// CreateMessage は新規メッセージを保存し、振った ts を持つ Message を返す。
//
// ts は Slack と同じく実際の時刻 "秒.マイクロ秒" で振る。同じチャンネルで同じ ts が既にあれば
// (同じ µs に書き込まれた、別の Pod が先に書いた等) 1µs ずらして書き直す。
// プロセスのメモリにも DB の連番にも頼らないので、Pod が増えても DB を替えても同じ作りで動く。
func (db *DB) CreateMessage(p CreateMessageParams) (*Message, error) {
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

// UpdateMessage は既存メッセージの本文・Blocks を更新する。
func (db *DB) UpdateMessage(channelID, ts, text, blocks string) (*Message, error) {
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
		return nil, sql.ErrNoRows
	}
	return db.GetMessageByTS(channelID, ts)
}

// AlertRootUpdate はアラートのスレッドの親メッセージ (ルート) に書き戻す状態である。
type AlertRootUpdate struct {
	Status     string
	Level      string
	ResolvedAt *time.Time
	Details    string // 空なら変えない
	URL        string // 空なら変えない
}

// UpdateAlertRoot はアラートのスレッドの親メッセージの状態を更新する。
func (db *DB) UpdateAlertRoot(channelID, ts string, u AlertRootUpdate) (*Message, error) {
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
		return nil, sql.ErrNoRows
	}
	return db.GetMessageByTS(channelID, ts)
}

// ActivityFilter は通知タブの絞り込みである。
type ActivityFilter string

const (
	ActivityAll      ActivityFilter = "all"
	ActivityFiring   ActivityFilter = "firing"
	ActivityResolved ActivityFilter = "resolved"
	ActivityNotice   ActivityFilter = "notice"
)

// ActivityItem は通知タブの 1 件 (アラート・通知のスレッドの親メッセージ) である。
type ActivityItem struct {
	ChannelID   string  `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	Message     Message `json:"message"`
}

// ListActivity はアラート・通知のスレッドの親メッセージを新しい順に返す。beforeTS より古いものだけを返す。
func (db *DB) ListActivity(filter ActivityFilter, beforeTS string, limit int) ([]ActivityItem, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	conds := []string{"m.thread_ts = ''", "m.source_kind != ''"}
	var args []interface{}
	switch filter {
	case ActivityFiring:
		conds = append(conds, "m.source_kind = 'alert'", "m.alert_status = 'firing'")
	case ActivityResolved:
		conds = append(conds, "m.source_kind = 'alert'", "m.alert_status = 'resolved'")
	case ActivityNotice:
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

	var items []ActivityItem
	for rows.Next() {
		var name string
		m, err := scanMessageWithPrefix(rows, &name)
		if err != nil {
			return nil, false, err
		}
		items = append(items, ActivityItem{ChannelID: m.ChannelID, ChannelName: name, Message: m})
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

const messageSelectCols = `
	m.id, m.channel_id, m.ts, m.thread_ts, m.user_id, m.bot_id, m.username, m.text, m.blocks, m.level,
	m.source_kind, m.details, m.url, m.alert_status, m.fired_at, m.resolved_at,
	(SELECT COUNT(*) FROM messages r WHERE r.channel_id = m.channel_id AND r.thread_ts = m.ts) AS reply_count,
	COALESCE((SELECT MAX(r.ts) FROM messages r WHERE r.channel_id = m.channel_id AND r.thread_ts = m.ts), '') AS latest_reply,
	m.created_at, m.updated_at`

// GetMessageByID は内部 ID からメッセージを取得する。
func (db *DB) GetMessageByID(id int64) (*Message, error) {
	row := db.conn.QueryRow(`SELECT `+messageSelectCols+` FROM messages m WHERE m.id = ?`, id)
	msg, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetMessageByTS は (channel_id, ts) からメッセージを取得する。
func (db *DB) GetMessageByTS(channelID, ts string) (*Message, error) {
	row := db.conn.QueryRow(
		`SELECT `+messageSelectCols+` FROM messages m WHERE m.channel_id = ? AND m.ts = ?`,
		channelID, ts,
	)
	msg, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// ListChannelMessagesDesc はチャンネルのルートメッセージを新しい順（Slack API 準拠）で返す。
func (db *DB) ListChannelMessagesDesc(channelID, oldest, latest string, inclusive bool, limit int) ([]Message, bool, error) {
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

	var msgs []Message
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
func (db *DB) ListChannelMessagesAsc(channelID, beforeTS string, limit int) ([]Message, bool, error) {
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
func (db *DB) GetThreadMessages(channelID, parentTS string) (*Message, []Message, error) {
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

	var replies []Message
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

// MarkChannelRead は指定ユーザーのチャンネル既読位置を更新する。
func (db *DB) MarkChannelRead(userID, channelID, ts string) error {
	if ts == "" {
		var maxTS sql.NullString
		if err := db.conn.QueryRow(`SELECT MAX(ts) FROM messages WHERE channel_id = ?`, channelID).Scan(&maxTS); err != nil {
			return err
		}
		if maxTS.Valid {
			ts = maxTS.String
		}
	}
	if ts == "" {
		return nil
	}
	_, err := db.conn.Exec(
		`INSERT INTO channel_reads (user_id, channel_id, last_read_ts)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id, channel_id) DO UPDATE SET
			last_read_ts = CASE WHEN excluded.last_read_ts > channel_reads.last_read_ts THEN excluded.last_read_ts ELSE channel_reads.last_read_ts END`,
		userID, channelID, ts,
	)
	return err
}

// SearchMessages はユーザーが閲覧可能なチャンネル内をキーワード検索する。
func (db *DB) SearchMessages(query, channelID, userID string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(query))
	pattern := "%" + escaped + "%"

	conds := []string{
		"(c.is_channel = 1 OR c.dm_user_id = ?)",
		"(m.text LIKE ? ESCAPE '\\' OR m.username LIKE ? ESCAPE '\\' OR m.details LIKE ? ESCAPE '\\')",
	}
	args := []interface{}{userID, pattern, pattern, pattern}
	if channelID != "" {
		conds = append(conds, "m.channel_id = ?")
		args = append(args, channelID)
	}
	args = append(args, limit)

	sqlQuery := `SELECT c.id, c.name, c.is_im, ` + messageSelectCols + `
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY m.ts DESC
		LIMIT ?`

	rows, err := db.conn.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var isIM int
		var blocksStr string
		var firedAtVal, resolvedAtVal sql.NullTime

		if err := rows.Scan(
			&r.ChannelID, &r.ChannelName, &isIM,
			&r.Message.ID, &r.Message.ChannelID, &r.Message.TS, &r.Message.ThreadTS,
			&r.Message.UserID, &r.Message.BotID, &r.Message.Username, &r.Message.Text,
			&blocksStr, &r.Message.Level, &r.Message.SourceKind,
			&r.Message.Details, &r.Message.URL, &r.Message.AlertStatus,
			&firedAtVal, &resolvedAtVal, &r.Message.ReplyCount, &r.Message.LatestReply,
			&r.Message.CreatedAt, &r.Message.UpdatedAt,
		); err != nil {
			return nil, err
		}
		r.IsIM = isIM != 0
		if blocksStr != "" {
			r.Message.Blocks = json.RawMessage(blocksStr)
		}
		if firedAtVal.Valid {
			r.Message.FiredAt = &firedAtVal.Time
		}
		if resolvedAtVal.Valid {
			r.Message.ResolvedAt = &resolvedAtVal.Time
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanMessage(s rowScanner) (Message, error) {
	return scanMessageWithPrefix(s)
}

// scanMessageWithPrefix は、messageSelectCols の前に並べた列 (prefix) も一緒に読む。
func scanMessageWithPrefix(s rowScanner, prefix ...interface{}) (Message, error) {
	var m Message
	var blocksStr string
	var firedAtVal, resolvedAtVal sql.NullTime

	dest := append(prefix,
		&m.ID, &m.ChannelID, &m.TS, &m.ThreadTS, &m.UserID, &m.BotID, &m.Username, &m.Text,
		&blocksStr, &m.Level, &m.SourceKind, &m.Details, &m.URL, &m.AlertStatus,
		&firedAtVal, &resolvedAtVal, &m.ReplyCount, &m.LatestReply, &m.CreatedAt, &m.UpdatedAt,
	)
	if err := s.Scan(dest...); err != nil {
		return Message{}, err
	}
	if blocksStr != "" {
		m.Blocks = json.RawMessage(blocksStr)
	}
	if firedAtVal.Valid {
		m.FiredAt = &firedAtVal.Time
	}
	if resolvedAtVal.Valid {
		m.ResolvedAt = &resolvedAtVal.Time
	}
	return m, nil
}

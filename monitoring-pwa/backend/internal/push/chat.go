package push

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
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
	ID          int64           `json:"id"`
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
	SourceID    int64           `json:"source_id,omitempty"`
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

// IsLegacyAlertmanagerNotification は過去の二重保存によって notifications テーブルに入った Alertmanager 通知を判定する。
func IsLegacyAlertmanagerNotification(n Notification) bool {
	if n.Source == "alertmanager" {
		return true
	}
	if n.Source != "" {
		return false
	}
	if !strings.HasPrefix(n.Title, "[firing] ") && !strings.HasPrefix(n.Title, "[resolved] ") {
		return false
	}
	if n.Details == "" {
		return false
	}
	var parsed struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal([]byte(n.Details), &parsed); err != nil {
		return false
	}
	return len(parsed.Labels) > 0 || len(parsed.Annotations) > 0
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
			created_at DATETIME NOT NULL
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
			source_id INTEGER NOT NULL DEFAULT 0,
			details TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			alert_status TEXT NOT NULL DEFAULT '',
			fired_at DATETIME,
			resolved_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(channel_id, ts)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_channel_thread_ts ON messages(channel_id, thread_ts, ts);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_source ON messages(source_kind, source_id);`,
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

	if err := db.seedLastTS(); err != nil {
		return err
	}

	if err := db.backfillLegacyRecords(); err != nil {
		return err
	}

	return nil
}

func (db *DB) seedLastTS() error {
	var maxTS sql.NullString
	if err := db.conn.QueryRow(`SELECT MAX(ts) FROM messages`).Scan(&maxTS); err != nil {
		return err
	}
	if maxTS.Valid && maxTS.String != "" {
		sec, seq := parseTS(maxTS.String)
		db.tsMu.Lock()
		db.lastTSSec = sec
		db.lastTSSeq = seq
		db.tsMu.Unlock()
	}
	return nil
}

func parseTS(ts string) (int64, int) {
	parts := strings.SplitN(ts, ".", 2)
	sec, _ := strconv.ParseInt(parts[0], 10, 64)
	var seq int
	if len(parts) == 2 {
		seq, _ = strconv.Atoi(parts[1])
	}
	return sec, seq
}

// NextTS は Slack 互換の `"1700000000.000100"` 形式の一意かつ単調増加するタイムスタンプを生成する。
func (db *DB) NextTS() string {
	return db.nextTSAt(time.Now())
}

func (db *DB) nextTSAt(t time.Time) string {
	db.tsMu.Lock()
	defer db.tsMu.Unlock()

	sec := t.Unix()
	if sec < 0 {
		sec = 0
	}
	if sec > db.lastTSSec {
		db.lastTSSec = sec
		db.lastTSSeq = 1
	} else {
		db.lastTSSeq++
		if db.lastTSSeq >= 1000000 {
			db.lastTSSec++
			db.lastTSSeq = 1
		}
	}
	return fmt.Sprintf("%010d.%06d", db.lastTSSec, db.lastTSSeq)
}

// backfillLegacyRecords は既存 DB の alerts / notifications レコードのうち messages に未取り込みのものを全件補完する。
func (db *DB) backfillLegacyRecords() error {
	// 1. alerts の全件取り込み (古い順)
	alertRows, err := db.conn.Query(
		`SELECT id, fingerprint, alertname, status, level, summary, description, url, details, fired_at, resolved_at, created_at, updated_at
		 FROM alerts
		 ORDER BY id ASC`,
	)
	if err != nil {
		return err
	}
	alerts, err := scanAlerts(alertRows)
	alertRows.Close()
	if err != nil {
		return err
	}

	for _, a := range alerts {
		var count int
		if err := db.conn.QueryRow(
			`SELECT COUNT(*) FROM messages WHERE source_kind = 'alert' AND source_id = ?`, a.ID,
		).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		title := a.Alertname
		if title == "" {
			title = a.Summary
		}
		text := fmt.Sprintf("**[firing] %s**\n%s", title, a.Summary)
		if a.Status == "resolved" && a.ResolvedAt == nil {
			text = fmt.Sprintf("**[resolved] %s**\n%s", title, a.Summary)
		}

		parentTS := db.nextTSAt(a.FiredAt)
		parentLevel := a.Level
		if a.Status == "resolved" {
			parentLevel = "success"
		}

		if _, err := db.conn.Exec(
			`INSERT INTO messages (channel_id, ts, thread_ts, user_id, bot_id, username, text, blocks, level, source_kind, source_id, details, url, alert_status, fired_at, resolved_at, created_at, updated_at)
			 VALUES (?, ?, '', ?, ?, 'Alertmanager', ?, '', ?, 'alert', ?, ?, ?, ?, ?, ?, ?, ?)`,
			ChannelAlertmanager, parentTS, SystemBotUserID, SystemBotID, text, parentLevel, a.ID, a.Details, a.URL, a.Status, a.FiredAt, a.ResolvedAt, a.CreatedAt, a.UpdatedAt,
		); err != nil {
			return err
		}

		if a.Status == "resolved" && a.ResolvedAt != nil {
			replyTS := db.nextTSAt(*a.ResolvedAt)
			replyText := fmt.Sprintf("✅ **[resolved] %s**\nアラートが解決した。", title)
			if _, err := db.conn.Exec(
				`INSERT INTO messages (channel_id, ts, thread_ts, user_id, bot_id, username, text, blocks, level, source_kind, source_id, details, url, alert_status, fired_at, resolved_at, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, 'Alertmanager', ?, '', 'success', 'alert', ?, '', '', 'resolved', ?, ?, ?, ?)`,
				ChannelAlertmanager, replyTS, parentTS, SystemBotUserID, SystemBotID, replyText, a.ID, a.FiredAt, a.ResolvedAt, *a.ResolvedAt, *a.ResolvedAt,
			); err != nil {
				return err
			}
		}
	}

	// 2. notifications の全件取り込み (古い順)
	noteRows, err := db.conn.Query(
		`SELECT id, title, body, url, level, details, source, created_at
		 FROM notifications
		 ORDER BY id ASC`,
	)
	if err != nil {
		return err
	}
	var notes []Notification
	for noteRows.Next() {
		n, scanErr := scanNotification(noteRows)
		if scanErr != nil {
			noteRows.Close()
			return scanErr
		}
		notes = append(notes, n)
	}
	if err := noteRows.Err(); err != nil {
		noteRows.Close()
		return err
	}
	noteRows.Close()

	for _, n := range notes {
		if IsLegacyAlertmanagerNotification(n) {
			continue
		}
		var count int
		if err := db.conn.QueryRow(
			`SELECT COUNT(*) FROM messages WHERE source_kind = 'notification' AND source_id = ?`, n.ID,
		).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		source := InferNotificationSource(n.Title, n.Source)
		chID := ChannelForSource(source)
		username := usernameForSource(source)
		text := formatNotificationText(n.Title, n.Body)
		ts := db.nextTSAt(n.CreatedAt)

		if _, err := db.conn.Exec(
			`INSERT INTO messages (channel_id, ts, thread_ts, user_id, bot_id, username, text, blocks, level, source_kind, source_id, details, url, alert_status, created_at, updated_at)
			 VALUES (?, ?, '', ?, ?, ?, ?, '', ?, 'notification', ?, ?, ?, '', ?, ?)`,
			chID, ts, SystemBotUserID, SystemBotID, username, text, n.Level, n.ID, n.Details, n.URL, n.CreatedAt, n.CreatedAt,
		); err != nil {
			return err
		}
	}

	return nil
}

func usernameForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "argocd":
		return "Argo CD"
	case "keel":
		return "Keel"
	case "alertmanager":
		return "Alertmanager"
	default:
		return "Nuage Notify"
	}
}

func formatNotificationText(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return body
	}
	if body == "" || body == title {
		return fmt.Sprintf("**%s**", title)
	}
	return fmt.Sprintf("**%s**\n%s", title, body)
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
	SourceID    int64
	Details     string
	URL         string
	AlertStatus string
	FiredAt     *time.Time
	ResolvedAt  *time.Time
}

// CreateMessage は新規メッセージを保存し、採番された ts を持つ Message を返す。
func (db *DB) CreateMessage(p CreateMessageParams) (*Message, error) {
	now := time.Now()
	ts := db.nextTSAt(now)
	if p.Blocks == "" {
		p.Blocks = ""
	}
	res, err := db.conn.Exec(
		`INSERT INTO messages (
			channel_id, ts, thread_ts, user_id, bot_id, username, text, blocks, level,
			source_kind, source_id, details, url, alert_status, fired_at, resolved_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ChannelID, ts, p.ThreadTS, p.UserID, p.BotID, p.Username, p.Text, p.Blocks, p.Level,
		p.SourceKind, p.SourceID, p.Details, p.URL, p.AlertStatus, p.FiredAt, p.ResolvedAt, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return db.GetMessageByID(id)
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

// PostAlertChatMessage は Alertmanager の発報・解決を同じスレッドにまとめて保存する。
// 返り値は (投稿したメッセージ, 親メッセージまたは更新された親メッセージ, error) である。
func (db *DB) PostAlertChatMessage(alert *Alert, isNewCycle bool, title, summary, details, targetURL string) (*Message, *Message, error) {
	return db.SyncAlertChatMessage(alert, isNewCycle, false, title, summary, details, targetURL)
}

// SyncAlertChatMessage は Alertmanager の発報・解決をチャットスレッドへ反映する。
// skipReply が true で既存スレッドが存在する場合は返信を作成せず親メッセージのみを更新する。
func (db *DB) SyncAlertChatMessage(alert *Alert, isNewCycle, skipReply bool, title, summary, details, targetURL string) (*Message, *Message, error) {
	if !isNewCycle {
		// 既存の親スレッドを探し、状態遷移時のみ返信を足す
		parent, err := db.GetLatestAlertParentMessage(alert.ID)
		if err == nil && parent != nil {
			var reply *Message
			if !skipReply {
				var replyText string
				var replyLevel string
				if alert.Status == "resolved" {
					replyText = fmt.Sprintf("✅ **[resolved] %s**\nアラートが解決した。", title)
					replyLevel = "success"
				} else {
					replyText = fmt.Sprintf("🔥 **[firing] %s**\n%s", title, summary)
					replyLevel = alert.Level
				}

				reply, err = db.CreateMessage(CreateMessageParams{
					ChannelID:   ChannelAlertmanager,
					ThreadTS:    parent.TS,
					UserID:      SystemBotUserID,
					BotID:       SystemBotID,
					Username:    "Alertmanager",
					Text:        replyText,
					Level:       replyLevel,
					SourceKind:  "alert",
					SourceID:    alert.ID,
					AlertStatus: alert.Status,
					FiredAt:     &alert.FiredAt,
					ResolvedAt:  alert.ResolvedAt,
				})
				if err != nil {
					return nil, nil, err
				}
			}

			updatedParent, err := db.updateAlertParentMessage(parent.ID, alert, details, targetURL)
			if err != nil {
				if reply != nil {
					return reply, parent, nil
				}
				return nil, nil, err
			}
			return reply, updatedParent, nil
		}
	}

	// 新規親メッセージを作成
	var icon string
	if alert.Status == "resolved" {
		icon = "✅"
	} else {
		icon = "🔥"
	}
	text := fmt.Sprintf("%s **[%s] %s**\n%s", icon, alert.Status, title, summary)
	parent, err := db.CreateMessage(CreateMessageParams{
		ChannelID:   ChannelAlertmanager,
		UserID:      SystemBotUserID,
		BotID:       SystemBotID,
		Username:    "Alertmanager",
		Text:        text,
		Level:       alert.Level,
		SourceKind:  "alert",
		SourceID:    alert.ID,
		Details:     details,
		URL:         targetURL,
		AlertStatus: alert.Status,
		FiredAt:     &alert.FiredAt,
		ResolvedAt:  alert.ResolvedAt,
	})
	if err != nil {
		return nil, nil, err
	}
	return parent, parent, nil
}

func (db *DB) updateAlertParentMessage(parentID int64, alert *Alert, details, targetURL string) (*Message, error) {
	parentLevel := alert.Level
	if alert.Status == "resolved" {
		parentLevel = "success"
	}
	now := time.Now()
	if _, err := db.conn.Exec(
		`UPDATE messages
		 SET alert_status = ?, level = ?, resolved_at = ?,
		     details = CASE WHEN ? != '' THEN ? ELSE details END,
		     url = CASE WHEN ? != '' THEN ? ELSE url END,
		     updated_at = ?
		 WHERE id = ?`,
		alert.Status, parentLevel, alert.ResolvedAt,
		details, details,
		targetURL, targetURL,
		now, parentID,
	); err != nil {
		return nil, err
	}
	return db.GetMessageByID(parentID)
}

// GetLatestAlertParentMessage は指定 alert ID に紐づく最新のルートメッセージを取得する。
func (db *DB) GetLatestAlertParentMessage(alertID int64) (*Message, error) {
	return db.GetMessageBySourceRef("alert", alertID)
}

// GetMessageBySourceRef は source_kind と source_id に対応する最新のルートメッセージを取得する。
func (db *DB) GetMessageBySourceRef(kind string, id int64) (*Message, error) {
	row := db.conn.QueryRow(
		`SELECT `+messageSelectCols+`
		 FROM messages m
		 WHERE m.source_kind = ? AND m.source_id = ? AND m.thread_ts = ''
		 ORDER BY m.ts DESC LIMIT 1`,
		kind, id,
	)
	msg, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

const messageSelectCols = `
	m.id, m.channel_id, m.ts, m.thread_ts, m.user_id, m.bot_id, m.username, m.text, m.blocks, m.level,
	m.source_kind, m.source_id, m.details, m.url, m.alert_status, m.fired_at, m.resolved_at,
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
			&blocksStr, &r.Message.Level, &r.Message.SourceKind, &r.Message.SourceID,
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

func scanMessage(s rowScanner) (Message, error) {
	var m Message
	var blocksStr string
	var firedAtVal, resolvedAtVal sql.NullTime

	if err := s.Scan(
		&m.ID, &m.ChannelID, &m.TS, &m.ThreadTS, &m.UserID, &m.BotID, &m.Username, &m.Text,
		&blocksStr, &m.Level, &m.SourceKind, &m.SourceID, &m.Details, &m.URL, &m.AlertStatus,
		&firedAtVal, &resolvedAtVal, &m.ReplyCount, &m.LatestReply, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
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

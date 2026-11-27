package sqlite

import "database/sql"

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

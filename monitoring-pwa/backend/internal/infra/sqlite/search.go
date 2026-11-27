package sqlite

import (
	"strings"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// SearchMessages はユーザーが閲覧可能なチャンネル内をキーワード検索する。
func (db *DB) SearchMessages(query, channelID, userID string, limit int) ([]domain.SearchResult, error) {
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

	var results []domain.SearchResult
	for rows.Next() {
		var chID, chName string
		var isIM int
		m, err := scanMessageWithPrefix(rows, &chID, &chName, &isIM)
		if err != nil {
			return nil, err
		}
		results = append(results, domain.SearchResult{
			ChannelID:   chID,
			ChannelName: chName,
			IsIM:        isIM != 0,
			Message:     m,
		})
	}
	return results, rows.Err()
}

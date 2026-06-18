package sqlite

import (
	"database/sql"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// MessageRepo is the SQLite-backed port.MessageRepository. It keeps at most
// maxPerRoom text rows per room, pruning older ones on write.
type MessageRepo struct {
	db         *sql.DB
	maxPerRoom int
}

func (r *MessageRepo) SaveText(m domain.Message) error {
	// Insert first; prune is best-effort maintenance kept separate so a prune
	// failure never rolls back a delivered message.
	if _, err := r.db.Exec(
		`INSERT INTO messages (room_name, from_user_id, from_username, content, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		m.RoomName, m.FromUserID, m.FromUsername, m.Content, m.Timestamp,
	); err != nil {
		return err
	}

	_, _ = r.db.Exec(`
		DELETE FROM messages
		WHERE  room_name = ?
		AND    id NOT IN (
			SELECT id FROM messages
			WHERE  room_name = ?
			ORDER  BY timestamp DESC, id DESC
			LIMIT  ?
		)`, m.RoomName, m.RoomName, r.maxPerRoom)
	return nil
}

func (r *MessageRepo) Load(roomName string, limit int) ([]domain.HistoryEntry, error) {
	rows, err := r.db.Query(`
		SELECT kind, from_user_id, from_username, content, file_id, filename, mime_type, size, timestamp
		FROM (
			SELECT 'text'      AS kind,
			       from_user_id, from_username,
			       content,
			       ''          AS file_id,
			       ''          AS filename,
			       ''          AS mime_type,
			       0           AS size,
			       timestamp
			FROM   messages WHERE room_name = ?
			UNION ALL
			SELECT 'file',
			       from_user_id, from_username,
			       '',
			       id,
			       filename,
			       mime_type,
			       size,
			       timestamp
			FROM   files WHERE room_name = ?
			ORDER  BY timestamp DESC
			LIMIT  ?
		)
		ORDER BY timestamp ASC`,
		roomName, roomName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.HistoryEntry
	for rows.Next() {
		var e domain.HistoryEntry
		if err := rows.Scan(
			&e.Kind, &e.FromUserID, &e.FromUsername,
			&e.Content, &e.FileID, &e.Filename, &e.MimeType, &e.Size,
			&e.Timestamp,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var _ port.MessageRepository = (*MessageRepo)(nil)

package store

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"

	"darmie/internal/protocol"
)

// MessageStore is a SQLite-backed message history store keyed by room name.
type MessageStore struct {
	db *sql.DB
}

// FileRecord holds the metadata for an uploaded file.
type FileRecord struct {
	ID           string
	RoomName     string
	FromUserID   string
	FromUsername string
	Filename     string
	MimeType     string
	Size         int64
	Timestamp    int64
}

const maxMessagesPerRoom = 200 // rows kept per room; matches hub.maxHistory

// NewMessageStore opens (or creates) the SQLite database at path and
// ensures the schema exists.
func NewMessageStore(path string) (*MessageStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// WAL mode gives better concurrent read/write performance.
	if _, err = db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}

	if _, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			room_name     TEXT    NOT NULL,
			from_user_id  TEXT    NOT NULL,
			from_username TEXT    NOT NULL,
			content       TEXT    NOT NULL,
			timestamp     INTEGER NOT NULL
		)`); err != nil {
		db.Close()
		return nil, err
	}

	if _, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_room_ts
		ON messages (room_name, timestamp)`); err != nil {
		db.Close()
		return nil, err
	}

	if _, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			id            TEXT    PRIMARY KEY,
			room_name     TEXT    NOT NULL,
			from_user_id  TEXT    NOT NULL,
			from_username TEXT    NOT NULL,
			filename      TEXT    NOT NULL,
			mime_type     TEXT    NOT NULL,
			size          INTEGER NOT NULL,
			timestamp     INTEGER NOT NULL
		)`); err != nil {
		db.Close()
		return nil, err
	}

	if _, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_files_room_ts
		ON files (room_name, timestamp)`); err != nil {
		db.Close()
		return nil, err
	}

	return &MessageStore{db: db}, nil
}

// SaveText persists a single text message associated with roomName and prunes old
// rows so each room never exceeds maxMessagesPerRoom entries.
// Errors are logged but not returned — a failed save must never crash the send path.
func (ms *MessageStore) SaveText(roomName string, msg protocol.IncomingTextPayload) {
	// Insert the message first, independently of the prune step that follows.
	// Keeping them separate ensures that a prune failure never rolls back the
	// insert and silently drops a delivered message.
	_, err := ms.db.Exec(
		`INSERT INTO messages (room_name, from_user_id, from_username, content, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		roomName, msg.FromUserID, msg.FromUsername, msg.Content, msg.Timestamp,
	)
	if err != nil {
		log.Printf("messages: save error: %v", err)
		return
	}

	// Prune rows beyond the per-room limit (non-critical maintenance).
	if _, err = ms.db.Exec(`
		DELETE FROM messages
		WHERE  room_name = ?
		AND    id NOT IN (
			SELECT id FROM messages
			WHERE  room_name = ?
			ORDER  BY timestamp DESC, id DESC
			LIMIT  ?
		)`, roomName, roomName, maxMessagesPerRoom); err != nil {
		log.Printf("messages: prune error: %v", err)
	}
}

// SaveFile persists file upload metadata. Returns an error so the caller can
// roll back the on-disk file on failure.
func (ms *MessageStore) SaveFile(rec FileRecord) error {
	_, err := ms.db.Exec(
		`INSERT INTO files (id, room_name, from_user_id, from_username, filename, mime_type, size, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.RoomName, rec.FromUserID, rec.FromUsername,
		rec.Filename, rec.MimeType, rec.Size, rec.Timestamp,
	)
	return err
}

// GetFile returns the metadata for a single file by its UUID.
func (ms *MessageStore) GetFile(fileID string) (*FileRecord, error) {
	var rec FileRecord
	err := ms.db.QueryRow(
		`SELECT id, room_name, from_user_id, from_username, filename, mime_type, size, timestamp
		 FROM files WHERE id = ?`, fileID,
	).Scan(&rec.ID, &rec.RoomName, &rec.FromUserID, &rec.FromUsername,
		&rec.Filename, &rec.MimeType, &rec.Size, &rec.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}
	return &rec, nil
}

// Load returns the most recent limit history entries (text + file) for roomName
// in chronological order.
func (ms *MessageStore) Load(roomName string, limit int) ([]protocol.HistoryEntry, error) {
	rows, err := ms.db.Query(`
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

	var out []protocol.HistoryEntry
	for rows.Next() {
		var e protocol.HistoryEntry
		if err := rows.Scan(
			&e.Kind, &e.FromUserID, &e.FromUsername,
			&e.Content, &e.FileID, &e.Filename, &e.MimeType, &e.Size,
			&e.Timestamp,
		); err != nil {
			return nil, err
		}
		if e.Kind == "file" {
			e.URL = "/files/" + e.FileID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

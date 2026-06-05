package store

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"

	"darmie/internal/protocol"
)

// MessageStore is a SQLite-backed message history store keyed by room name.
type MessageStore struct {
	db *sql.DB
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

	return &MessageStore{db: db}, nil
}

// Save persists a single text message associated with roomName and prunes old
// rows so each room never exceeds maxMessagesPerRoom entries.
// Both operations run in a single transaction so the row limit is enforced
// atomically even under concurrent saves.
// Errors are logged but not returned a failed save must never crash the send path.
func (ms *MessageStore) Save(roomName string, msg protocol.IncomingTextPayload) {
	tx, err := ms.db.Begin()
	if err != nil {
		log.Printf("messages: begin tx: %v", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(
		`INSERT INTO messages (room_name, from_user_id, from_username, content, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		roomName, msg.FromUserID, msg.FromUsername, msg.Content, msg.Timestamp,
	)
	if err != nil {
		log.Printf("messages: save error: %v", err)
		return
	}

	// Prune rows beyond the per-room limit. Ordering by (timestamp DESC, id DESC)
	// is deterministic even when two messages share the same millisecond timestamp.
	_, err = tx.Exec(`
		DELETE FROM messages
		WHERE  room_name = ?
		AND    id NOT IN (
			SELECT id FROM messages
			WHERE  room_name = ?
			ORDER  BY timestamp DESC, id DESC
			LIMIT  ?
		)`, roomName, roomName, maxMessagesPerRoom)
	if err != nil {
		log.Printf("messages: prune error: %v", err)
		return
	}

	if err = tx.Commit(); err != nil {
		log.Printf("messages: commit: %v", err)
	}
}

// Load returns the most recent limit messages for roomName in chronological order.
func (ms *MessageStore) Load(roomName string, limit int) ([]protocol.IncomingTextPayload, error) {
	rows, err := ms.db.Query(`
		SELECT from_user_id, from_username, content, timestamp
		FROM (
			SELECT from_user_id, from_username, content, timestamp
			FROM   messages
			WHERE  room_name = ?
			ORDER  BY timestamp DESC
			LIMIT  ?
		)
		ORDER BY timestamp ASC`,
		roomName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []protocol.IncomingTextPayload
	for rows.Next() {
		var m protocol.IncomingTextPayload
		if err := rows.Scan(&m.FromUserID, &m.FromUsername, &m.Content, &m.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

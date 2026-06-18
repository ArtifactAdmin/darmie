// Package sqlite implements the persistence ports (users, sessions, messages,
// files) on a single SQLite database via modernc.org/sqlite (pure Go, no cgo).
package sqlite

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB and groups the repository constructors so a single
// database file backs every repository.
type DB struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path, enables WAL, and
// applies the schema migrations.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

// Close closes the underlying database.
func (d *DB) Close() error { return d.db.Close() }

// Users returns a UserRepository backed by this database.
func (d *DB) Users() *UserRepo { return &UserRepo{db: d.db} }

// Sessions returns a SessionRepository backed by this database.
func (d *DB) Sessions() *SessionRepo { return &SessionRepo{db: d.db} }

// Messages returns a MessageRepository backed by this database.
func (d *DB) Messages(maxPerRoom int) *MessageRepo {
	return &MessageRepo{db: d.db, maxPerRoom: maxPerRoom}
}

// Files returns a FileRepository backed by this database.
func (d *DB) Files() *FileRepo { return &FileRepo{db: d.db} }

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            TEXT    PRIMARY KEY,
			username      TEXT    NOT NULL UNIQUE,
			password_hash TEXT    NOT NULL,
			created_at    INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT    PRIMARY KEY,
			user_id    TEXT    NOT NULL,
			username   TEXT    NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			room_name     TEXT    NOT NULL,
			from_user_id  TEXT    NOT NULL,
			from_username TEXT    NOT NULL,
			content       TEXT    NOT NULL,
			timestamp     INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_room_ts ON messages (room_name, timestamp)`,
		`CREATE TABLE IF NOT EXISTS files (
			id            TEXT    PRIMARY KEY,
			room_name     TEXT    NOT NULL,
			from_user_id  TEXT    NOT NULL,
			from_username TEXT    NOT NULL,
			filename      TEXT    NOT NULL,
			mime_type     TEXT    NOT NULL,
			size          INTEGER NOT NULL,
			timestamp     INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_room_ts ON files (room_name, timestamp)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

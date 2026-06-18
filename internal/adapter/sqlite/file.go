package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// FileRepo is the SQLite-backed port.FileRepository.
type FileRepo struct {
	db *sql.DB
}

func (r *FileRepo) Save(f domain.File) error {
	_, err := r.db.Exec(
		`INSERT INTO files (id, room_name, from_user_id, from_username, filename, mime_type, size, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.RoomName, f.FromUserID, f.FromUsername,
		f.Filename, f.MimeType, f.Size, f.Timestamp,
	)
	return err
}

func (r *FileRepo) Get(id string) (*domain.File, error) {
	var f domain.File
	err := r.db.QueryRow(
		`SELECT id, room_name, from_user_id, from_username, filename, mime_type, size, timestamp
		 FROM files WHERE id = ?`, id,
	).Scan(&f.ID, &f.RoomName, &f.FromUserID, &f.FromUsername,
		&f.Filename, &f.MimeType, &f.Size, &f.Timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("file not found: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

var _ port.FileRepository = (*FileRepo)(nil)

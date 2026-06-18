package sqlite

import (
	"database/sql"
	"errors"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// SessionRepo is the SQLite-backed port.SessionRepository.
type SessionRepo struct {
	db *sql.DB
}

func (r *SessionRepo) Create(s *domain.Session) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (token, user_id, username, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		s.Token, s.UserID, s.Username, s.CreatedAt, s.ExpiresAt,
	)
	return err
}

func (r *SessionRepo) FindByToken(token string) (*domain.Session, error) {
	var s domain.Session
	err := r.db.QueryRow(
		`SELECT token, user_id, username, created_at, expires_at FROM sessions WHERE token = ?`,
		token,
	).Scan(&s.Token, &s.UserID, &s.Username, &s.CreatedAt, &s.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SessionRepo) Delete(token string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

var _ port.SessionRepository = (*SessionRepo)(nil)

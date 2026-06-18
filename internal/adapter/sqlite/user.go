package sqlite

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// UserRepo is the SQLite-backed port.UserRepository.
type UserRepo struct {
	db *sql.DB
}

func (r *UserRepo) Create(u *domain.User) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	_, err := r.db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Username, u.PasswordHash, u.CreatedAt,
	)
	if isUniqueViolation(err) {
		return domain.ErrUserAlreadyExists
	}
	return err
}

func (r *UserRepo) FindByUsername(username string) (*domain.User, error) {
	return r.scanOne(`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`, username)
}

func (r *UserRepo) FindByID(id string) (*domain.User, error) {
	return r.scanOne(`SELECT id, username, password_hash, created_at FROM users WHERE id = ?`, id)
}

func (r *UserRepo) scanOne(query, arg string) (*domain.User, error) {
	var u domain.User
	err := r.db.QueryRow(query, arg).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

var _ port.UserRepository = (*UserRepo)(nil)

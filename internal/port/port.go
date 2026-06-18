// Package port declares the outbound interfaces (driven ports) that the core
// services depend on. Adapters in internal/adapter implement these; the core
// never imports a concrete adapter, only this package and the domain.
package port

import (
	"io"

	"darmie/internal/domain"
)

// UserRepository persists and retrieves accounts.
type UserRepository interface {
	// Create stores u, assigning u.ID and u.CreatedAt if unset. It returns
	// domain.ErrUserAlreadyExists when the username is taken.
	Create(u *domain.User) error
	// FindByUsername returns domain.ErrUserNotFound when no match exists.
	FindByUsername(username string) (*domain.User, error)
	// FindByID returns domain.ErrUserNotFound when no match exists.
	FindByID(id string) (*domain.User, error)
}

// SessionRepository persists durable auth sessions.
type SessionRepository interface {
	Create(s *domain.Session) error
	// FindByToken returns domain.ErrSessionNotFound when no match exists.
	FindByToken(token string) (*domain.Session, error)
	Delete(token string) error
}

// MessageRepository persists chat history.
type MessageRepository interface {
	SaveText(m domain.Message) error
	// Load returns up to limit recent history entries (text + file) for a room,
	// in chronological order.
	Load(roomName string, limit int) ([]domain.HistoryEntry, error)
}

// FileRepository persists uploaded-file metadata.
type FileRepository interface {
	Save(f domain.File) error
	// Get returns domain.ErrUserNotFound's file analogue via a wrapped error
	// when no match exists.
	Get(id string) (*domain.File, error)
}

// FileStorage persists the raw bytes of uploaded files (blob storage).
type FileStorage interface {
	// Save streams r into the blob identified by id and returns the byte count.
	Save(id string, r io.Reader) (int64, error)
	// Open returns a reader for the stored blob; the caller must close it.
	Open(id string) (io.ReadCloser, error)
	// Remove deletes the stored blob, used to roll back a failed upload.
	Remove(id string) error
}

// PasswordHasher abstracts the password hashing scheme.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare reports a nil error when password matches hash.
	Compare(hash, password string) error
	// DummyCompare performs a comparison of equivalent cost against a fixed
	// hash, so login timing does not reveal whether a username exists.
	DummyCompare(password string)
}

// TokenGenerator produces opaque, cryptographically-random tokens.
type TokenGenerator interface {
	Generate() (string, error)
}

// Clock returns the current time in Unix milliseconds. Injected so core logic
// is deterministic under test.
type Clock func() int64

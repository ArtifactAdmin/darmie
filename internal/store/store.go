// Package store provides an in-memory, thread-safe user store.
package store

import (
	"errors"
	"regexp"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("username already taken")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrInvalidUsername   = errors.New("username must be 3–20 alphanumeric/underscore characters")
	ErrWeakPassword      = errors.New("password must be at least 8 characters")

	usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)

	// dummyHash is a real bcrypt hash used for constant-time rejection of
	// unknown usernames, preventing user-enumeration via timing differences.
	// It is generated once at startup so CompareHashAndPassword takes the same
	// ~200 ms whether the username exists or not.
	dummyHash []byte
)

func init() {
	var err error
	dummyHash, err = bcrypt.GenerateFromPassword([]byte(uuid.New().String()), bcrypt.DefaultCost)
	if err != nil {
		panic("store: failed to generate dummy hash: " + err.Error())
	}
}

// User holds persisted account data.
type User struct {
	ID           string
	Username     string
	passwordHash string
}

// Store is a thread-safe in-memory user registry.
type Store struct {
	mu         sync.RWMutex
	byID       map[string]*User
	byUsername map[string]*User
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		byID:       make(map[string]*User),
		byUsername: make(map[string]*User),
	}
}

// Register creates a new user account.
func (s *Store) Register(username, password string) (*User, error) {
	if !usernameRE.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if len(password) < 8 {
		return nil, ErrWeakPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byUsername[username]; exists {
		return nil, ErrUserAlreadyExists
	}

	u := &User{
		ID:           uuid.New().String(),
		Username:     username,
		passwordHash: string(hash),
	}
	s.byID[u.ID] = u
	s.byUsername[u.Username] = u
	return u, nil
}

// Login validates credentials and returns the User on success.
func (s *Store) Login(username, password string) (*User, error) {
	s.mu.RLock()
	u, exists := s.byUsername[username]
	s.mu.RUnlock()

	if !exists {
		// Constant-time rejection: use a real bcrypt hash so the comparison
		// takes ~200 ms regardless of whether the username exists.
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password)) //nolint:errcheck
		return nil, ErrUserNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidPassword
	}
	return u, nil
}

// GetByID looks up a user by their UUID.
func (s *Store) GetByID(id string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[id]
	return u, ok
}

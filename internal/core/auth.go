// Package core holds the application services — the use-case layer of the
// hexagon. Each service orchestrates domain rules and outbound ports; it has no
// knowledge of WebSockets, HTTP, or SQL.
package core

import (
	"time"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// sessionTTL is how long a session token remains valid after issue.
const sessionTTL = 30 * 24 * time.Hour

// AuthService implements registration, login, and session resumption.
type AuthService struct {
	users    port.UserRepository
	sessions port.SessionRepository
	hasher   port.PasswordHasher
	tokens   port.TokenGenerator
	now      port.Clock
}

// NewAuthService wires an AuthService. If now is nil, wall-clock time is used.
func NewAuthService(
	users port.UserRepository,
	sessions port.SessionRepository,
	hasher port.PasswordHasher,
	tokens port.TokenGenerator,
	now port.Clock,
) *AuthService {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &AuthService{users: users, sessions: sessions, hasher: hasher, tokens: tokens, now: now}
}

// Register creates a new account and an initial session.
func (s *AuthService) Register(username, password string) (*domain.Session, error) {
	if err := domain.ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, err
	}

	u := &domain.User{Username: username, PasswordHash: hash, CreatedAt: s.now()}
	if err := s.users.Create(u); err != nil {
		return nil, err // domain.ErrUserAlreadyExists or storage failure
	}

	return s.issueSession(u)
}

// Login validates credentials and issues a session. The comparison cost is
// constant whether or not the username exists, preventing user enumeration.
func (s *AuthService) Login(username, password string) (*domain.Session, error) {
	u, err := s.users.FindByUsername(username)
	if err != nil {
		s.hasher.DummyCompare(password)
		return nil, domain.ErrInvalidCredentials
	}
	if err := s.hasher.Compare(u.PasswordHash, password); err != nil {
		return nil, domain.ErrInvalidCredentials
	}
	return s.issueSession(u)
}

// Resume validates a session token and returns the live session. It rejects
// expired sessions and sessions whose user no longer exists.
func (s *AuthService) Resume(token string) (*domain.Session, error) {
	sess, err := s.sessions.FindByToken(token)
	if err != nil {
		return nil, domain.ErrSessionNotFound
	}
	if sess.Expired(s.now()) {
		_ = s.sessions.Delete(token)
		return nil, domain.ErrSessionNotFound
	}
	if _, err := s.users.FindByID(sess.UserID); err != nil {
		_ = s.sessions.Delete(token)
		return nil, domain.ErrSessionNotFound
	}
	return sess, nil
}

// Logout invalidates a session token.
func (s *AuthService) Logout(token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(token)
}

func (s *AuthService) issueSession(u *domain.User) (*domain.Session, error) {
	token, err := s.tokens.Generate()
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &domain.Session{
		Token:     token,
		UserID:    u.ID,
		Username:  u.Username,
		CreatedAt: now,
		ExpiresAt: now + sessionTTL.Milliseconds(),
	}
	if err := s.sessions.Create(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

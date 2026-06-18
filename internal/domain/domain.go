// Package domain holds the core entities and business rules of Darmie.
//
// It is the centre of the hexagon: it depends on nothing outside the standard
// library. Every other layer (ports, core services, adapters) depends inward
// on these types, never the reverse.
package domain

import (
	"errors"
	"regexp"
	"unicode/utf8"
)

// ─── Errors ─────────────────────────────────────────────────────────────────

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUserAlreadyExists  = errors.New("username already taken")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidUsername    = errors.New("username must be 3–20 alphanumeric/underscore characters")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
	ErrSessionNotFound    = errors.New("session expired — please sign in again")
	ErrMessageTooLong     = errors.New("message too long")
	ErrRoomNameInvalid    = errors.New("room name must be 1–50 characters")
)

// ─── Invariants ───────────────────────────────────────────────────────────────

const (
	MinPasswordLen = 8
	MaxMessageLen  = 2000
	MaxRoomNameLen = 50
)

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)

// ValidateUsername reports whether name satisfies the account-name policy.
func ValidateUsername(name string) error {
	if !usernameRE.MatchString(name) {
		return ErrInvalidUsername
	}
	return nil
}

// ValidatePassword reports whether the password meets the minimum strength.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

// ValidateMessage reports whether a chat message body is within limits.
func ValidateMessage(content string) error {
	if utf8.RuneCountInString(content) > MaxMessageLen {
		return ErrMessageTooLong
	}
	return nil
}

// ─── Entities ─────────────────────────────────────────────────────────────────

// User is a registered account. PasswordHash is opaque to the domain.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    int64 // Unix milliseconds
}

// Session is a durable authentication context bound to a user. The token
// survives WebSocket disconnects so a client can resume after a page reload.
type Session struct {
	Token     string
	UserID    string
	Username  string
	CreatedAt int64 // Unix milliseconds
	ExpiresAt int64 // Unix milliseconds; 0 means never expires
}

// Expired reports whether the session is past its expiry at time now (ms).
func (s Session) Expired(now int64) bool {
	return s.ExpiresAt != 0 && now >= s.ExpiresAt
}

// Message is a single chat text message scoped to a room.
type Message struct {
	RoomName     string
	FromUserID   string
	FromUsername string
	Content      string
	Timestamp    int64 // Unix milliseconds
}

// File is the metadata of an uploaded file scoped to a room.
type File struct {
	ID           string
	RoomName     string
	FromUserID   string
	FromUsername string
	Filename     string
	MimeType     string
	Size         int64
	Timestamp    int64 // Unix milliseconds
}

// HistoryEntry kinds.
const (
	HistoryText = "text"
	HistoryFile = "file"
)

// HistoryEntry is a unified room-history record covering both text messages
// (Kind == HistoryText) and uploaded files (Kind == HistoryFile).
type HistoryEntry struct {
	Kind         string
	FromUserID   string
	FromUsername string
	Timestamp    int64
	// text-only
	Content string
	// file-only
	FileID   string
	Filename string
	MimeType string
	Size     int64
}

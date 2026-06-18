// Package security provides cryptographic outbound adapters: password hashing
// and random token generation.
package security

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"darmie/internal/port"
)

// BcryptHasher implements port.PasswordHasher using bcrypt.
type BcryptHasher struct {
	dummyHash []byte
}

// NewBcryptHasher returns a hasher with a precomputed dummy hash used for
// constant-time rejection of unknown usernames (anti-enumeration).
func NewBcryptHasher() (*BcryptHasher, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte(uuid.NewString()), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &BcryptHasher{dummyHash: dummy}, nil
}

func (h *BcryptHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *BcryptHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// DummyCompare burns roughly the same CPU time as a real comparison so an
// attacker cannot distinguish "no such user" from "wrong password" by timing.
func (h *BcryptHasher) DummyCompare(password string) {
	_ = bcrypt.CompareHashAndPassword(h.dummyHash, []byte(password))
}

// RandomTokenGenerator implements port.TokenGenerator with crypto/rand.
type RandomTokenGenerator struct {
	nbytes int
}

// NewTokenGenerator returns a generator producing URL-safe tokens of the given
// entropy in bytes (32 = 256 bits).
func NewTokenGenerator(nbytes int) *RandomTokenGenerator {
	if nbytes <= 0 {
		nbytes = 32
	}
	return &RandomTokenGenerator{nbytes: nbytes}
}

func (g *RandomTokenGenerator) Generate() (string, error) {
	b := make([]byte, g.nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

var (
	_ port.PasswordHasher = (*BcryptHasher)(nil)
	_ port.TokenGenerator = (*RandomTokenGenerator)(nil)
)

// Package diskstore is a filesystem-backed implementation of port.FileStorage.
package diskstore

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"darmie/internal/port"
)

// Storage stores file blobs as plain files under a base directory, named by id.
type Storage struct {
	dir string
}

// New ensures dir exists and returns a Storage rooted there.
func New(dir string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Storage{dir: dir}, nil
}

// path resolves a blob id to an on-disk path, rejecting anything that is not a
// single safe path segment (defence in depth against traversal).
func (s *Storage) path(id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", false
	}
	return filepath.Join(s.dir, id), true
}

func (s *Storage) Save(id string, r io.Reader) (int64, error) {
	p, ok := s.path(id)
	if !ok {
		return 0, os.ErrInvalid
	}
	f, err := os.Create(p)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(p)
		return 0, err
	}
	return n, nil
}

func (s *Storage) Open(id string) (io.ReadSeekCloser, error) {
	p, ok := s.path(id)
	if !ok {
		return nil, os.ErrNotExist
	}
	return os.Open(p)
}

func (s *Storage) Remove(id string) error {
	p, ok := s.path(id)
	if !ok {
		return os.ErrInvalid
	}
	return os.Remove(p)
}

var _ port.FileStorage = (*Storage)(nil)

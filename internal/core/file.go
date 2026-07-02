package core

import (
	"io"
	"time"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// FileService stores uploaded files (bytes + metadata) and serves them back.
type FileService struct {
	files   port.FileRepository
	storage port.FileStorage
	now     port.Clock
}

// NewFileService wires a FileService. If now is nil, wall-clock time is used.
func NewFileService(files port.FileRepository, storage port.FileStorage, now port.Clock) *FileService {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &FileService{files: files, storage: storage, now: now}
}

// Save streams r into blob storage and records metadata. The caller supplies
// f.ID (an opaque, collision-free identifier) and the descriptive fields; Save
// fills in Size and Timestamp. On a metadata failure the blob is rolled back so
// storage and metadata never diverge.
func (s *FileService) Save(f domain.File, r io.Reader) (domain.File, error) {
	size, err := s.storage.Save(f.ID, r)
	if err != nil {
		return domain.File{}, err
	}
	f.Size = size
	f.Timestamp = s.now()

	if err := s.files.Save(f); err != nil {
		_ = s.storage.Remove(f.ID)
		return domain.File{}, err
	}
	return f, nil
}

// Open returns the metadata and a seekable byte stream for a stored file. The
// caller must close the returned reader.
func (s *FileService) Open(id string) (*domain.File, io.ReadSeekCloser, error) {
	rec, err := s.files.Get(id)
	if err != nil {
		return nil, nil, err
	}
	rc, err := s.storage.Open(id)
	if err != nil {
		return nil, nil, err
	}
	return rec, rc, nil
}

// Package blob stores file contents on disk, one file per stored version.
//
// Blob paths are derived from the database ID only, never from user input.
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const tmpPrefix = ".tmp-"

// Store manages the blob directory.
type Store struct {
	dir string
}

// New creates the blob directory if needed and removes temporary files left by a crash.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id int64) string {
	return filepath.Join(s.dir, strconv.FormatInt(id, 10))
}

// Temp is an uploaded body written to a temporary file, not yet committed.
type Temp struct {
	path   string
	Size   int64
	SHA256 string
	Head   []byte // first bytes of the content, for MIME sniffing
}

// headWriter keeps the first len(buf) bytes written to it.
type headWriter struct {
	buf []byte
	n   int
}

func (h *headWriter) Write(p []byte) (int, error) {
	if h.n < len(h.buf) {
		h.n += copy(h.buf[h.n:], p)
	}
	return len(p), nil
}

// WriteTemp streams r into a temporary file, computing its size and SHA-256.
func (s *Store) WriteTemp(r io.Reader) (*Temp, error) {
	f, err := os.CreateTemp(s.dir, tmpPrefix+"*")
	if err != nil {
		return nil, err
	}
	t := &Temp{path: f.Name()}

	hash := sha256.New()
	head := &headWriter{buf: make([]byte, 512)}
	n, err := io.Copy(io.MultiWriter(f, hash, head), r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(t.path)
		return nil, err
	}

	t.Size = n
	t.SHA256 = hex.EncodeToString(hash.Sum(nil))
	t.Head = head.buf[:head.n]
	return t, nil
}

// Discard removes the temporary file.
func (t *Temp) Discard() { os.Remove(t.path) }

// Commit atomically moves the temporary file to its final location for id.
func (s *Store) Commit(t *Temp, id int64) error {
	return os.Rename(t.path, s.path(id))
}

// Open opens the blob of id for reading.
func (s *Store) Open(id int64) (*os.File, error) {
	return os.Open(s.path(id))
}

// Remove deletes the blob of id; a missing blob is not an error.
func (s *Store) Remove(id int64) error {
	err := os.Remove(s.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

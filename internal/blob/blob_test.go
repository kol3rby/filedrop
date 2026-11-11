package blob

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycle(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	content := bytes.Repeat([]byte("0123456789"), 100) // 1000 bytes > sniff buffer
	tmp, err := s.WriteTemp(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if tmp.Size != 1000 || tmp.SHA256 != hex.EncodeToString(sum[:]) || len(tmp.Head) != 512 {
		t.Fatalf("temp = size %d sha %s head %d", tmp.Size, tmp.SHA256, len(tmp.Head))
	}

	if err := s.Commit(tmp, 42); err != nil {
		t.Fatal(err)
	}
	f, err := s.Open(42)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, content) {
		t.Fatal("content mismatch")
	}

	if err := s.Remove(42); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(42); err != nil {
		t.Fatalf("removing a missing blob: %v", err)
	}
	if _, err := s.Open(42); err == nil {
		t.Fatal("blob still present")
	}
}

func TestDiscardAndCleanup(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	tmp, err := s.WriteTemp(strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	tmp.Discard()

	// A leftover temp file (simulated crash) is cleaned up on startup.
	os.WriteFile(filepath.Join(dir, tmpPrefix+"orphan"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "7"), []byte("keep"), 0o600)
	if _, err := New(dir); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "7" {
		t.Fatalf("entries after cleanup = %v", entries)
	}
}

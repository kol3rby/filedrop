package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insert(t *testing.T, s *Store, slug, name string) File {
	t.Helper()
	f, err := s.Insert(context.Background(), File{Slug: slug, Filename: name, Size: 3, SHA256: "abc", ContentType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestVersioning(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for want := 1; want <= 3; want++ {
		if f := insert(t, s, "slug", "a.txt"); f.Version != want {
			t.Fatalf("version = %d, want %d", f.Version, want)
		}
	}
	if f := insert(t, s, "slug", "b.txt"); f.Version != 1 {
		t.Fatalf("other file version = %d, want 1", f.Version)
	}
	if f := insert(t, s, "other", "a.txt"); f.Version != 1 {
		t.Fatalf("other slug version = %d, want 1", f.Version)
	}

	latest, err := s.Get(ctx, "slug", "a.txt", 0)
	if err != nil || latest.Version != 3 {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	v1, err := s.Get(ctx, "slug", "a.txt", 1)
	if err != nil || v1.Version != 1 {
		t.Fatalf("v1 = %+v, %v", v1, err)
	}
	if _, err := s.Get(ctx, "slug", "a.txt", 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version err = %v", err)
	}

	list, err := s.List(ctx, "slug")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range list {
		got = append(got, fmt.Sprintf("%s@%d", f.Filename, f.Version))
	}
	want := []string{"a.txt@3", "a.txt@2", "a.txt@1", "b.txt@1"}
	if len(got) != len(want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list = %v, want %v", got, want)
		}
	}
}

func TestDelete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	insert(t, s, "slug", "a.txt")
	insert(t, s, "slug", "a.txt")
	insert(t, s, "slug", "a.txt")

	del, err := s.Delete(ctx, "slug", "a.txt", 2)
	if err != nil || len(del) != 1 || del[0].Version != 2 {
		t.Fatalf("delete v2 = %+v, %v", del, err)
	}
	if _, err := s.Delete(ctx, "slug", "a.txt", 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
	del, err = s.Delete(ctx, "slug", "a.txt", 0)
	if err != nil || len(del) != 2 {
		t.Fatalf("delete all = %+v, %v", del, err)
	}
	if ok, _ := s.SlugExists(ctx, "slug"); ok {
		t.Fatal("slug still exists")
	}
	// Numbering restarts once every version is gone.
	if f := insert(t, s, "slug", "a.txt"); f.Version != 1 {
		t.Fatalf("version after delete = %d", f.Version)
	}
}

func TestConcurrentInserts(t *testing.T) {
	s := newStore(t)
	const n = 20
	versions := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := s.Insert(context.Background(), File{Slug: "s", Filename: "f", SHA256: "x", ContentType: "t"})
			if err != nil {
				t.Error(err)
				return
			}
			versions[i] = f.Version
		}()
	}
	wg.Wait()
	sort.Ints(versions)
	for i, v := range versions {
		if v != i+1 {
			t.Fatalf("versions = %v", versions)
		}
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	insert(t, s, "slug", "a.txt")
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if f := insert(t, s, "slug", "a.txt"); f.Version != 2 {
		t.Fatalf("version after reopen = %d", f.Version)
	}
}

// Package store persists file metadata in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // SQLite driver (pure Go)
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when no matching file exists.
var ErrNotFound = errors.New("not found")

// File is the metadata of one stored version of a file.
type File struct {
	ID          int64     `json:"-"`
	Slug        string    `json:"slug"`
	Filename    string    `json:"filename"`
	Version     int       `json:"version"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store gives access to the metadata database.
type Store struct {
	db *sql.DB
}

// Open opens (and creates if needed) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// A single connection serialises writes and avoids SQLITE_BUSY;
	// the workload (small metadata queries) does not need more.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}

	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", name)
		}
		if v <= current {
			continue
		}
		script, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(script)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const columns = `id, slug, filename, version, size, sha256, content_type, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanFile(sc scanner) (File, error) {
	var f File
	var created string
	if err := sc.Scan(&f.ID, &f.Slug, &f.Filename, &f.Version, &f.Size, &f.SHA256, &f.ContentType, &created); err != nil {
		return File{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return File{}, fmt.Errorf("bad created_at %q: %w", created, err)
	}
	f.CreatedAt = t
	return f, nil
}

// Insert stores a new version of (slug, filename), numbered after the latest one.
// Only Slug, Filename, Size, SHA256 and ContentType are read from f.
func (s *Store) Insert(ctx context.Context, f File) (File, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM files WHERE slug = ? AND filename = ?`,
		f.Slug, f.Filename,
	).Scan(&f.Version); err != nil {
		return File{}, err
	}
	f.CreatedAt = time.Now().UTC()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO files (slug, filename, version, size, sha256, content_type, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.Slug, f.Filename, f.Version, f.Size, f.SHA256, f.ContentType, f.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return File{}, err
	}
	if f.ID, err = res.LastInsertId(); err != nil {
		return File{}, err
	}
	return f, tx.Commit()
}

// Get returns a given version of a file; version 0 means the latest one.
func (s *Store) Get(ctx context.Context, slug, filename string, version int) (File, error) {
	query := `SELECT ` + columns + ` FROM files WHERE slug = ? AND filename = ?`
	args := []any{slug, filename}
	if version > 0 {
		query += ` AND version = ?`
		args = append(args, version)
	}
	query += ` ORDER BY version DESC LIMIT 1`

	f, err := scanFile(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

// List returns every version of every file of a slug, ordered by filename then newest version first.
func (s *Store) List(ctx context.Context, slug string) ([]File, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+columns+` FROM files WHERE slug = ? ORDER BY filename, version DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// Delete removes all versions of a file, or only the given one when version > 0.
// It returns the deleted rows so that their blobs can be removed; ErrNotFound if none matched.
func (s *Store) Delete(ctx context.Context, slug, filename string, version int) ([]File, error) {
	query := `DELETE FROM files WHERE slug = ? AND filename = ?`
	args := []any{slug, filename}
	if version > 0 {
		query += ` AND version = ?`
		args = append(args, version)
	}
	query += ` RETURNING ` + columns

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var deleted []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		deleted = append(deleted, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(deleted) == 0 {
		return nil, ErrNotFound
	}
	return deleted, tx.Commit()
}

// DeleteByID removes a single row; used to roll back a failed upload.
func (s *Store) DeleteByID(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, id)
	return err
}

// SlugExists reports whether at least one file is stored under slug.
func (s *Store) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM files WHERE slug = ?)`, slug).Scan(&exists)
	return exists, err
}

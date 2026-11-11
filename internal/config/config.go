// Package config loads the server configuration from environment variables
// and an optional env file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// Config holds the runtime configuration.
type Config struct {
	Addr          string // listen address
	DataDir       string // directory holding the SQLite database and the blobs
	BaseURL       string // public base URL; empty means "derive from the request"
	MaxUploadSize int64  // maximum request body size, in bytes
	SlugLength    int    // length of generated slugs
}

// Load reads the configuration from the environment, applying defaults.
// When ENV_FILE is set, the KEY=value pairs of that file override the
// environment; a missing file is ignored. Load re-reads the file on each call,
// so it can be used to reload the configuration.
func Load() (Config, error) {
	var file map[string]string
	if path := os.Getenv("ENV_FILE"); path != "" {
		var err error
		file, err = ReadEnvFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("ENV_FILE: %w", err)
		}
	}
	env := func(key, def string) string {
		if v := file[key]; v != "" {
			return v
		}
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Addr:       env("ADDR", ":8080"),
		DataDir:    env("DATA_DIR", "./data"),
		BaseURL:    strings.TrimRight(env("BASE_URL", ""), "/"),
		SlugLength: 8,
	}

	size, err := ParseSize(env("MAX_UPLOAD_SIZE", "100MB"))
	if err != nil {
		return Config{}, fmt.Errorf("MAX_UPLOAD_SIZE: %w", err)
	}
	cfg.MaxUploadSize = size

	if v := env("SLUG_LENGTH", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 4 || n > 64 {
			return Config{}, fmt.Errorf("SLUG_LENGTH: must be an integer between 4 and 64, got %q", v)
		}
		cfg.SlugLength = n
	}

	return cfg, nil
}

// ReadEnvFile parses a file of KEY=value lines. Blank lines and lines starting
// with # are skipped; an "export " prefix and matching surrounding quotes
// around the value are removed.
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vars := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("%s:%d: expected KEY=value", path, n)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		vars[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}

var units = []struct {
	suffix string
	factor int64
}{
	{"GB", 1 << 30},
	{"MB", 1 << 20},
	{"KB", 1 << 10},
	{"G", 1 << 30},
	{"M", 1 << 20},
	{"K", 1 << 10},
	{"B", 1},
}

// ParseSize parses a size such as "100MB", "512K" or "1048576" (binary units).
func ParseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	factor := int64(1)
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			factor = u.factor
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n * factor, nil
}

// FormatSize renders a byte count in a human-readable form (binary units).
func FormatSize(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GB", n>>30)
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%d KB", n>>10)
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

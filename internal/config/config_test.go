package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := map[string]int64{
		"100MB":   100 << 20,
		"512k":    512 << 10,
		"1 GB":    1 << 30,
		"1048576": 1 << 20,
		"10B":     10,
	}
	for in, want := range tests {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-1MB", "0", "1TB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) succeeded", in)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := map[int64]string{
		0:         "0 B",
		999:       "999 B",
		1 << 10:   "1 KB",
		1536:      "1.5 KB",
		100 << 20: "100 MB",
		1 << 30:   "1 GB",
	}
	for in, want := range tests {
		if got := FormatSize(in); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestReadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filedrop.env")
	content := "# comment\n\nBASE_URL = https://a.example\nexport MAX_UPLOAD_SIZE=\"1GB\"\nSLUG_LENGTH='12'\nEMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"BASE_URL": "https://a.example", "MAX_UPLOAD_SIZE": "1GB", "SLUG_LENGTH": "12", "EMPTY": ""}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	if err := os.WriteFile(path, []byte("no equal sign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEnvFile(path); err == nil {
		t.Error("ReadEnvFile accepted a line without '='")
	}
}

func TestLoadEnvFileOverridesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filedrop.env")
	t.Setenv("ENV_FILE", path)
	t.Setenv("ADDR", ":9000")
	t.Setenv("MAX_UPLOAD_SIZE", "1MB")

	// Missing file: environment only.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9000" || cfg.MaxUploadSize != 1<<20 {
		t.Fatalf("without file: %+v", cfg)
	}

	if err := os.WriteFile(path, []byte("MAX_UPLOAD_SIZE=2MB\nBASE_URL=https://b.example/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9000" || cfg.MaxUploadSize != 2<<20 || cfg.BaseURL != "https://b.example" {
		t.Fatalf("with file: %+v", cfg)
	}

	if err := os.WriteFile(path, []byte("SLUG_LENGTH=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("Load accepted an invalid SLUG_LENGTH from the file")
	}
}

package slug

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		s, err := Generate(8)
		if err != nil {
			t.Fatal(err)
		}
		if len(s) != 8 || Validate(s) != nil || strings.ContainsAny(s, "0Oo1lI") {
			t.Fatalf("bad slug %q", s)
		}
		if seen[s] {
			t.Fatalf("duplicate slug %q", s)
		}
		seen[s] = true
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"abc", true},
		{"A-b_9", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
		{"a.b", false},
		{"a/b", false},
		{"..", false},
		{"héhé", false},
		{"healthz", false},
		{"Healthz", false},
		{"health", true},
	}
	for _, tt := range tests {
		if err := Validate(tt.in); (err == nil) != tt.ok {
			t.Errorf("Validate(%q) = %v, want ok=%v", tt.in, err, tt.ok)
		}
	}
}

func TestValidateFilename(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"report.pdf", true},
		{"my file (1).tar.gz", true},
		{".hidden", true},
		{"résumé-日本.txt", true},
		{strings.Repeat("a", 255), true},
		{strings.Repeat("a", 256), false},
		{"", false},
		{".", false},
		{"..", false},
		{"../etc/passwd", false},
		{"a/b", false},
		{`a\b`, false},
		{"a\x00b", false},
		{"a\nb", false},
		{"\xff\xfe", false},
	}
	for _, tt := range tests {
		if err := ValidateFilename(tt.in); (err == nil) != tt.ok {
			t.Errorf("ValidateFilename(%q) = %v, want ok=%v", tt.in, err, tt.ok)
		}
	}
}

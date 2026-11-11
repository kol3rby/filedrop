// Package slug generates and validates slugs and filenames.
package slug

import (
	"crypto/rand"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// alphabet is base62 without visually ambiguous characters (0 O o 1 l I).
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"

var (
	slugRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reserved = map[string]bool{"healthz": true}

	ErrInvalidSlug     = errors.New("invalid slug: use 1-64 characters among A-Z a-z 0-9 _ -")
	ErrReservedSlug    = errors.New("this slug is reserved")
	ErrInvalidFilename = errors.New("invalid filename: must be 1-255 bytes of UTF-8, without '/', '\\' or control characters, and not '.' or '..'")
)

// Generate returns a random slug of length n.
func Generate(n int) (string, error) {
	max := big.NewInt(int64(len(alphabet)))
	b := make([]byte, n)
	for i := range b {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[k.Int64()]
	}
	return string(b), nil
}

// Validate checks that s is an acceptable slug.
func Validate(s string) error {
	if !slugRe.MatchString(s) {
		return ErrInvalidSlug
	}
	if reserved[strings.ToLower(s)] {
		return ErrReservedSlug
	}
	return nil
}

// ValidateFilename checks that name is safe to use as a filename.
func ValidateFilename(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) {
		return ErrInvalidFilename
	}
	if strings.ContainsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\' || unicode.IsControl(r)
	}) {
		return ErrInvalidFilename
	}
	return nil
}

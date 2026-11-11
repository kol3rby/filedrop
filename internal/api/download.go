package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"filedrop/internal/slug"
	"filedrop/internal/store"
)

// handleDownload handles GET /{slug}/{filename}[?version=N].
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	sl, name := r.PathValue("slug"), r.PathValue("filename")
	if slug.Validate(sl) != nil || slug.ValidateFilename(name) != nil {
		s.httpError(w, r, http.StatusNotFound, "file not found")
		return
	}
	version, ok := parseVersion(r)
	if !ok {
		s.httpError(w, r, http.StatusBadRequest, "version must be a positive integer")
		return
	}

	f, err := s.store.Get(r.Context(), sl, name, version)
	if errors.Is(err, store.ErrNotFound) {
		s.httpError(w, r, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		s.internalError(w, r, "get metadata", err)
		return
	}

	fh, err := s.blobs.Open(f.ID)
	if err != nil {
		s.internalError(w, r, "open blob", err)
		return
	}
	defer fh.Close()

	h := w.Header()
	h.Set("Content-Type", f.ContentType)
	h.Set("Content-Disposition", contentDisposition(f.Filename))
	h.Set("ETag", `"`+f.SHA256+`"`)
	h.Set("Cache-Control", "no-cache")
	h.Set("X-File-Version", strconv.Itoa(f.Version))
	// Uploaded content is untrusted: never let it run as a page of this origin.
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	http.ServeContent(w, r, f.Filename, f.CreatedAt, fh)
}

// contentDisposition builds an attachment header with an ASCII fallback
// (filename=, understood by curl -J) and the exact UTF-8 name (filename*, RFC 6266).
func contentDisposition(name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	v := `attachment; filename="` + fallback + `"`
	if fallback != name {
		v += "; filename*=UTF-8''" + rfc5987Escape(name)
	}
	return v
}

// rfc5987Escape percent-encodes everything but RFC 5987 attr-chars.
func rfc5987Escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

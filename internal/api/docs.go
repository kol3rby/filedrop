package api

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"strings"

	"filedrop/internal/config"
)

// handleDocs handles GET /: the Markdown documentation, rendered as HTML for
// browsers or sent raw to curl. ?format=md|html forces the format.
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	var asHTML bool
	switch r.URL.Query().Get("format") {
	case "":
		asHTML = wantsHTML(r)
	case "html":
		asHTML = true
	case "md", "markdown":
		asHTML = false
	default:
		s.httpError(w, r, http.StatusBadRequest, "format must be 'md' or 'html'")
		return
	}

	md := strings.NewReplacer(
		"{{BASE_URL}}", s.baseURL(r),
		"{{MAX_UPLOAD_SIZE}}", config.FormatSize(s.config().MaxUploadSize),
	).Replace(s.docsMD)

	w.Header().Set("Vary", "Accept")
	if !asHTML {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		io.WriteString(w, md)
		return
	}

	var body bytes.Buffer
	if err := s.md.Convert([]byte(md), &body); err != nil {
		s.internalError(w, r, "render markdown", err)
		return
	}
	var page bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&page, "layout.html", map[string]any{
		"Title":   "filedrop",
		"Content": template.HTML(body.String()), // rendered from the embedded docs only; raw HTML is not allowed
	}); err != nil {
		s.internalError(w, r, "render layout", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Write(page.Bytes())
}

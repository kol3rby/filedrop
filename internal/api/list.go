package api

import (
	"fmt"
	"html/template"
	"net/http"
	"text/tabwriter"
	"time"

	"filedrop/internal/config"
	"filedrop/internal/slug"
	"filedrop/internal/store"
)

var templateFuncs = template.FuncMap{
	"size": config.FormatSize,
	"date": func(t time.Time) string { return t.Format("2006-01-02 15:04:05 MST") },
	"short": func(s string) string {
		if len(s) > 12 {
			return s[:12]
		}
		return s
	},
}

type listedFile struct {
	store.File
	URL string `json:"url"`
}

type fileGroup struct {
	Name      string
	LatestURL string
	Versions  []listedFile
}

// handleList handles GET /{slug}: every file with its versions, as HTML, JSON or text.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	sl := r.PathValue("slug")
	var files []store.File
	valid := slug.Validate(sl) == nil
	if valid {
		var err error
		if files, err = s.store.List(r.Context(), sl); err != nil {
			s.internalError(w, r, "list files", err)
			return
		}
	}

	base := s.baseURL(r)
	listed := make([]listedFile, len(files))
	for i, f := range files {
		listed[i] = listedFile{f, base + versionPath(f)}
	}

	w.Header().Set("Vary", "Accept")
	status := http.StatusOK
	if len(files) == 0 {
		status = http.StatusNotFound
	}

	switch {
	case wantsJSON(r):
		if status == http.StatusNotFound {
			s.httpError(w, r, status, "no file in this slug")
			return
		}
		writeJSON(w, status, listed)

	case wantsHTML(r):
		var groups []fileGroup
		for _, f := range listed {
			if n := len(groups); n == 0 || groups[n-1].Name != f.Filename {
				groups = append(groups, fileGroup{Name: f.Filename, LatestURL: filePath(f.Slug, f.Filename)})
			}
			groups[len(groups)-1].Versions = append(groups[len(groups)-1].Versions, f)
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
		w.WriteHeader(status)
		if err := s.tmpl.ExecuteTemplate(w, "list.html", map[string]any{
			"Slug":      sl,
			"BaseURL":   base,
			"Groups":    groups,
			"UploadURL": slugPath(sl),
			"CanUpload": valid,
			"MaxSize":   s.config().MaxUploadSize,
		}); err != nil {
			s.log.Error("render list", "err", err)
		}

	default:
		if status == http.StatusNotFound {
			s.httpError(w, r, status, "no file in this slug")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "FILENAME\tVERSION\tSIZE\tCREATED\tURL")
		for _, f := range listed {
			fmt.Fprintf(tw, "%s\tv%d\t%s\t%s\t%s\n",
				f.Filename, f.Version, config.FormatSize(f.Size), f.CreatedAt.Format(time.RFC3339), f.URL)
		}
		tw.Flush()
	}
}

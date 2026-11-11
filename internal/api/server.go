// Package api implements the HTTP interface.
package api

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"filedrop/internal/blob"
	"filedrop/internal/config"
	"filedrop/internal/store"
)

//go:embed assets
var assets embed.FS

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	cfg    atomic.Pointer[config.Config]
	store  *store.Store
	blobs  *blob.Store
	log    *slog.Logger
	tmpl   *template.Template
	md     goldmark.Markdown
	docsMD string
}

// New builds a Server.
func New(cfg config.Config, st *store.Store, bl *blob.Store, log *slog.Logger) *Server {
	docs, err := assets.ReadFile("assets/docs.md")
	if err != nil {
		panic(err) // embedded at build time: cannot fail
	}
	s := &Server{
		store:  st,
		blobs:  bl,
		log:    log,
		tmpl:   template.Must(template.New("").Funcs(templateFuncs).ParseFS(assets, "assets/*.html")),
		md:     goldmark.New(goldmark.WithExtensions(extension.GFM)),
		docsMD: string(docs),
	}
	s.SetConfig(cfg)
	return s
}

// SetConfig replaces the configuration used by the handlers; requests already
// running keep the one they started with. Addr and DataDir are not used here.
func (s *Server) SetConfig(cfg config.Config) { s.cfg.Store(&cfg) }

func (s *Server) config() *config.Config { return s.cfg.Load() }

// Handler returns the root HTTP handler, routes and middlewares included.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleDocs)
	mux.HandleFunc("POST /{$}", s.handleUploadAuto)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /{slug}", s.handleList)
	mux.HandleFunc("GET /{slug}/{$}", s.handleList)
	mux.HandleFunc("POST /{slug}", s.handleUploadToSlug)
	mux.HandleFunc("POST /{slug}/{$}", s.handleUploadToSlug)
	mux.HandleFunc("POST /{slug}/{filename}", s.handleUpload)
	mux.HandleFunc("GET /{slug}/{filename}", s.handleDownload)
	mux.HandleFunc("DELETE /{slug}/{filename}", s.handleDelete)
	return s.recoverer(s.logger(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok\n")
}

// ---------- helpers ----------

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// baseURL returns the public base URL, without trailing slash.
func (s *Server) baseURL(r *http.Request) string {
	if b := s.config().BaseURL; b != "" {
		return b
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
		scheme = p
	}
	host := r.Host
	if h := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func slugPath(sl string) string { return "/" + url.PathEscape(sl) }

func filePath(sl, name string) string { return slugPath(sl) + "/" + url.PathEscape(name) }

func versionPath(f store.File) string {
	return filePath(f.Slug, f.Filename) + "?version=" + strconv.Itoa(f.Version)
}

// parseVersion reads the optional ?version=N parameter; 0 means "not set".
func parseVersion(r *http.Request) (int, bool) {
	v := r.URL.Query().Get("version")
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// httpError writes an error as JSON or plain text depending on the Accept header.
func (s *Server) httpError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Del("Content-Disposition")
	if wantsJSON(r) {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	io.WriteString(w, "error: "+msg+"\n")
}

// internalError logs err and answers with a generic 500.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.log.Error(msg, "err", err, "request_id", w.Header().Get("X-Request-ID"))
	s.httpError(w, r, http.StatusInternalServerError, "internal server error")
}

// ---------- middlewares ----------

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (rec *statusRecorder) WriteHeader(code int) {
	if rec.status == 0 {
		rec.status = code
	}
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(p []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	n, err := rec.ResponseWriter.Write(p)
	rec.bytes += int64(n)
	return n, err
}

// ReadFrom keeps the sendfile optimisation of the underlying writer for downloads.
func (rec *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	n, err := io.Copy(rec.ResponseWriter, src)
	rec.bytes += n
	return n, err
}

func (rec *statusRecorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := make([]byte, 8)
		rand.Read(id)
		reqID := hex.EncodeToString(id)
		w.Header().Set("X-Request-ID", reqID)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"request_id", reqID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start).Round(time.Microsecond),
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "value", v, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

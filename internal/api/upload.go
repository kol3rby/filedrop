package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"filedrop/internal/slug"
	"filedrop/internal/store"
)

// handleUpload handles POST /{slug}/{filename}.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	sl := r.PathValue("slug")
	if err := slug.Validate(sl); err != nil {
		s.httpError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.upload(w, r, sl, r.PathValue("filename"))
}

// handleUploadToSlug handles POST /{slug}: same as POST /{slug}/{filename} with
// the filename given by requestFilename. Used by the upload zone of the HTML list.
func (s *Server) handleUploadToSlug(w http.ResponseWriter, r *http.Request) {
	sl := r.PathValue("slug")
	if err := slug.Validate(sl); err != nil {
		s.httpError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.upload(w, r, sl, requestFilename(r))
}

// handleUploadAuto handles POST /: same as POST /{slug} with a generated slug.
func (s *Server) handleUploadAuto(w http.ResponseWriter, r *http.Request) {
	sl, err := s.newSlug(r.Context())
	if err != nil {
		s.internalError(w, r, "generate slug", err)
		return
	}
	s.upload(w, r, sl, requestFilename(r))
}

// requestFilename returns the filename of an upload without one in its path:
// ?filename=, then the X-Filename header; empty means "generate one".
func requestFilename(r *http.Request) string {
	if name := r.URL.Query().Get("filename"); name != "" {
		return name
	}
	return r.Header.Get("X-Filename")
}

// newSlug generates a slug that is not used yet.
func (s *Server) newSlug(ctx context.Context) (string, error) {
	for range 10 {
		sl, err := slug.Generate(s.config().SlugLength)
		if err != nil {
			return "", err
		}
		exists, err := s.store.SlugExists(ctx, sl)
		if err != nil {
			return "", err
		}
		if !exists {
			return sl, nil
		}
	}
	return "", errors.New("could not find a free slug")
}

// upload stores the request body as a new version of sl/name.
// An empty name is replaced by a generated one.
func (s *Server) upload(w http.ResponseWriter, r *http.Request, sl, name string) {
	if name != "" {
		if err := slug.ValidateFilename(name); err != nil {
			s.httpError(w, r, http.StatusBadRequest, err.Error())
			return
		}
	}
	maxSize := s.config().MaxUploadSize
	tooLarge := "body exceeds the maximum upload size of " + strconv.FormatInt(maxSize, 10) + " bytes"
	if r.ContentLength > maxSize {
		s.httpError(w, r, http.StatusRequestEntityTooLarge, tooLarge)
		return
	}

	tmp, err := s.blobs.WriteTemp(http.MaxBytesReader(w, r.Body, maxSize))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			s.httpError(w, r, http.StatusRequestEntityTooLarge, tooLarge)
			return
		}
		s.internalError(w, r, "write upload", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			tmp.Discard()
		}
	}()

	if tmp.Size == 0 {
		s.httpError(w, r, http.StatusBadRequest, "empty body: send the file content with curl --data-binary @<file>")
		return
	}

	ctype := contentType(r, tmp.Head)
	if name == "" {
		name = generatedName(ctype)
	}

	f, err := s.store.Insert(r.Context(), store.File{
		Slug:        sl,
		Filename:    name,
		Size:        tmp.Size,
		SHA256:      tmp.SHA256,
		ContentType: ctype,
	})
	if err != nil {
		s.internalError(w, r, "insert metadata", err)
		return
	}
	if err := s.blobs.Commit(tmp, f.ID); err != nil {
		if derr := s.store.DeleteByID(context.WithoutCancel(r.Context()), f.ID); derr != nil {
			s.log.Error("rollback metadata", "err", derr, "id", f.ID)
		}
		s.internalError(w, r, "commit blob", err)
		return
	}
	committed = true

	base := s.baseURL(r)
	fileURL := base + filePath(f.Slug, f.Filename)
	w.Header().Set("Location", fileURL)
	w.Header().Set("X-File-Version", strconv.Itoa(f.Version))

	if wantsJSON(r) {
		writeJSON(w, http.StatusCreated, struct {
			store.File
			URL        string `json:"url"`
			VersionURL string `json:"version_url"`
		}{f, fileURL, base + versionPath(f)})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, fileURL+"\n")
}

// contentType returns the client-provided Content-Type when meaningful, otherwise sniffs it.
// curl --data-binary sends application/x-www-form-urlencoded by default, which is ignored.
func contentType(r *http.Request, head []byte) string {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && mt != "application/x-www-form-urlencoded" && mt != "multipart/form-data" {
		if ct := mime.FormatMediaType(mt, params); ct != "" {
			return ct
		}
	}
	return http.DetectContentType(head)
}

var extensions = map[string]string{
	"text/plain":         ".txt",
	"text/html":          ".html",
	"text/xml":           ".xml",
	"application/json":   ".json",
	"application/pdf":    ".pdf",
	"application/zip":    ".zip",
	"application/x-gzip": ".gz",
	"application/gzip":   ".gz",
	"image/png":          ".png",
	"image/jpeg":         ".jpg",
	"image/gif":          ".gif",
	"image/webp":         ".webp",
	"image/svg+xml":      ".svg",
	"audio/mpeg":         ".mp3",
	"video/mp4":          ".mp4",
}

// generatedName builds a filename such as "file_2026-01-01_12-00-00.txt" (UTC timestamp).
func generatedName(ctype string) string {
	ext := ".bin"
	if mt, _, err := mime.ParseMediaType(ctype); err == nil {
		if e, ok := extensions[mt]; ok {
			ext = e
		}
	}
	return "file_" + time.Now().UTC().Format("2006-01-02_15-04-05") + ext
}

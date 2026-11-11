package api

import (
	"errors"
	"net/http"

	"filedrop/internal/slug"
	"filedrop/internal/store"
)

// handleDelete handles DELETE /{slug}/{filename}[?version=N]: removes the
// SQLite rows, then the files on disk. Without version, every version goes.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
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

	deleted, err := s.store.Delete(r.Context(), sl, name, version)
	if errors.Is(err, store.ErrNotFound) {
		s.httpError(w, r, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		s.internalError(w, r, "delete metadata", err)
		return
	}

	// The rows are gone: a blob that fails to be removed is only an orphan on disk.
	for _, f := range deleted {
		if err := s.blobs.Remove(f.ID); err != nil {
			s.log.Error("remove blob", "err", err, "id", f.ID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

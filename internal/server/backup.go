package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// getExport downloads the current CV as a zip.
func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var b bytes.Buffer
	unlock := s.lock(user)
	err := s.store.WriteBackup(&b, user)
	unlock()
	if err != nil {
		httpError(w, err)
		return
	}
	name := fmt.Sprintf("cv-%s-%s.zip", user, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b.Bytes())
}

// postImport replaces the current CV with a backup, after checking all of it.
func (s *Server) postImport(r *http.Request, user string) error {
	r.Body = http.MaxBytesReader(nil, r.Body, cv.MaxBackup+1<<20)
	file, _, err := r.FormFile("backup")
	if err != nil {
		return badRequest{fmt.Errorf("upload a backup zip of at most 50 MB: %w", err)}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, cv.MaxBackup+1))
	if err != nil {
		return err
	}
	stage, err := s.store.StageBackup(data, s.sharing)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	return s.edit(r.Context(), user, func() error { return s.store.RestoreBackup(user, stage) })
}

// getPing tells a second copy of the app that this one is running, and keeps
// a local app alive while an editor is open.
func (s *Server) getPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"app": "new-leaf", "local": s.local})
}

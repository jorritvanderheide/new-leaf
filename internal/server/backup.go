package server

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
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

// getResume downloads the current CV as a JSON Resume, in ?lang= or its
// main language.
func (s *Server) getResume(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	unlock := s.lock(user)
	profile, err := s.store.Profile(user)
	var items []cv.Item
	if err == nil {
		items, err = s.store.Items(user)
	}
	unlock()
	if err != nil {
		httpError(w, err)
		return
	}
	lang := cmp.Or(r.URL.Query().Get("lang"), profile.Langs[0])
	if !slices.Contains(profile.Langs, lang) {
		http.Error(w, "the CV isn't in that language", http.StatusBadRequest)
		return
	}
	data, err := json.MarshalIndent(cv.ExportResume(profile, items, lang), "", "  ")
	if err != nil {
		httpError(w, err)
		return
	}
	name := fmt.Sprintf("resume-%s-%s-%s.json", user, lang, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(append(data, '\n'))
}

// postResume replaces the current CV with a JSON Resume, keeping a backup of
// it as a restore does, and answers what the resume had that New Leaf has no
// place for.
func (s *Server) postResume(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxResume+1<<20)
	file, _, err := r.FormFile("resume")
	if err != nil {
		httpError(w, badRequest{fmt.Errorf("upload a JSON Resume of at most 5 MB: %w", err)})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxResume+1))
	if err != nil {
		httpError(w, err)
		return
	}
	if len(data) > maxResume {
		httpError(w, badRequest{errors.New("upload a JSON Resume of at most 5 MB")})
		return
	}
	var skipped []string
	err = s.edit(r.Context(), user, func() error {
		stage, skip, err := s.store.StageResume(user, data)
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		skipped = skip
		return s.store.RestoreBackup(user, stage)
	})
	if err != nil {
		httpError(w, err)
		return
	}
	if skipped == nil {
		skipped = []string{}
	}
	writeJSON(w, map[string]any{"skipped": skipped})
}

const maxResume = 5 << 20

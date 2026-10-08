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
	"path/filepath"
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
	if err := s.claimLinks(user, stage); err != nil {
		return err
	}
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

// claimLinks checks the share links in a staged backup. A link keeps its
// address if it is free: not another CV's, and not a folder of the webroot's
// own. Otherwise it gets a new one, so a backup restored into a second CV,
// or one made up, can't take over a page someone else shares.
func (s *Server) claimLinks(user, stage string) error {
	staged := &cv.Store{Root: filepath.Dir(stage)}
	name := filepath.Base(stage)
	links, err := staged.Links(name)
	if err != nil {
		return badRequest{err}
	}
	for _, l := range links {
		if err := l.Validate(); err != nil {
			return badRequest{fmt.Errorf("share link %s: %w", l.Slug, err)}
		}
		if !slices.Contains(publicAssets, l.Slug) && !s.linkElsewhere(user, l.Slug) {
			continue
		}
		if err := staged.DeleteLink(name, l.Slug); err != nil {
			return err
		}
		l.Slug = cv.NewSlug(l.Label)
		for s.linkElsewhere(user, l.Slug) || s.slugTaken(user, l.Slug) {
			l.Slug = cv.NewSlug(l.Label)
		}
		if err := staged.SaveLink(name, l); err != nil {
			return badRequest{fmt.Errorf("share link %s: %w", l.Slug, err)}
		}
	}
	return nil
}

// linkElsewhere reports whether a CV other than user has a link with slug.
func (s *Server) linkElsewhere(user, slug string) bool {
	users, _ := s.store.Users()
	for _, other := range users {
		if _, err := s.store.Link(other, slug); other != user && err == nil {
			return true
		}
	}
	return false
}

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A backup is a zip of one CV: its content files and versions,
//
//	content/_index.<lang>.md, content/photo.<ext>,
//	content/<section>/<id>.<lang>.md, content/links/<slug>.<lang>.md,
//	versions/<id>.json
//
// so it can move between computers and between local and hosted use.

const (
	maxBackup     = 50 << 20
	maxBackupFile = 12 << 20
)

// backupFile says whether a zip entry may be restored: only the files a CV
// consists of, no traversal, no unknown sections or languages. Share links
// are skipped where there are none, e.g. a server backup restored locally.
func (s *Server) backupFile(name string) (ok, skip bool) {
	if path.Clean(name) != name {
		return false, false
	}
	if name == "compose.json" { // backups from before versions
		return true, false
	}
	if file, found := strings.CutPrefix(name, "versions/"); found {
		id, found := strings.CutSuffix(file, ".json")
		return found && idRe.MatchString(id), false
	}
	rest, found := strings.CutPrefix(name, "content/")
	if !found {
		return false, false
	}
	dir, file := path.Split(rest)
	switch dir {
	case "":
		for _, ext := range photoExts {
			if file == "photo"+ext {
				return true, false
			}
		}
		for _, lang := range langCodes() {
			if file == "_index."+lang+".md" {
				return true, false
			}
		}
		return false, false
	case "links/":
		_, _, ok := splitLangFile(file)
		return ok, ok && !s.sharing
	default:
		_, _, ok := splitLangFile(file)
		return ok && sectionIndex(strings.TrimSuffix(dir, "/")) >= 0, false
	}
}

// getExport downloads the current CV as a zip.
func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var b bytes.Buffer
	unlock := s.lock(user)
	err := s.writeBackup(&b, user)
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

func (s *Server) writeBackup(w io.Writer, user string) error {
	zw := zip.NewWriter(w)
	root := filepath.Join(s.store.Root, user)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if ok, _ := s.backupFile(rel); !ok {
			return nil // backups/, temporary files
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := zw.Create(rel)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}

// postImport replaces the current CV with a backup. What it replaces is kept
// in the CV's backups/ folder first.
func (s *Server) postImport(r *http.Request, user string) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBackup+1<<20)
	file, _, err := r.FormFile("backup")
	if err != nil {
		return badRequest{fmt.Errorf("upload a backup zip of at most 50 MB: %w", err)}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBackup+1))
	if err != nil {
		return err
	}
	if len(data) > maxBackup {
		return badRequest{errors.New("backup is larger than 50 MB")}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return badRequest{errors.New("not a zip file")}
	}

	// Unpack into a fresh directory next to the CV and check it fully before
	// touching the CV itself.
	root := filepath.Join(s.store.Root, user)
	stage, err := os.MkdirTemp(s.store.Root, ".import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	found := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ok, skip := s.backupFile(f.Name)
		if skip {
			continue
		}
		if !ok {
			return badRequest{fmt.Errorf("unexpected file in backup: %q", f.Name)}
		}
		if f.UncompressedSize64 > maxBackupFile {
			return badRequest{fmt.Errorf("%s is too large", f.Name)}
		}
		rc, err := f.Open()
		if err != nil {
			return badRequest{err}
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxBackupFile+1))
		rc.Close()
		if err != nil {
			return badRequest{err}
		}
		dst := filepath.Join(stage, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(dst, content, 0o640); err != nil {
			return err
		}
		if filepath.Ext(dst) == ".md" {
			var fm map[string]any
			if _, err := readMarkdown(dst, &fm); err != nil {
				return badRequest{fmt.Errorf("%s: %v", f.Name, err)}
			}
		}
		found = found || slices.ContainsFunc(langCodes(), func(lang string) bool { return f.Name == "content/_index."+lang+".md" })
	}
	if !found {
		return badRequest{errors.New("this is not a CV backup: it has no profile")}
	}
	check := &Store{Root: filepath.Dir(stage)}
	if _, err := check.Items(filepath.Base(stage)); err != nil {
		return badRequest{err}
	}

	return s.edit(r.Context(), user, func() error {
		// Keep what is being replaced.
		var old bytes.Buffer
		if err := s.writeBackup(&old, user); err != nil {
			return err
		}
		backups := filepath.Join(root, "backups")
		if err := os.MkdirAll(backups, 0o750); err != nil {
			return err
		}
		keep := filepath.Join(backups, "before-import-"+time.Now().Format("2006-01-02-150405")+".zip")
		if err := os.WriteFile(keep, old.Bytes(), 0o640); err != nil {
			return err
		}
		// Swap in the new content and versions. A backup from before
		// versions has compose.json instead, which EnsureVersions takes over.
		for _, name := range []string{"content", "versions", "compose.json"} {
			cur, next := filepath.Join(root, name), filepath.Join(stage, name)
			if err := os.RemoveAll(cur); err != nil {
				return err
			}
			if _, err := os.Stat(next); err == nil {
				if err := os.Rename(next, cur); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// getPing tells a second copy of the app that this one is running, and keeps
// a local app alive while an editor is open.
func (s *Server) getPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"app": "new-leaf", "local": s.local})
}

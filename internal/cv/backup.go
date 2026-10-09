package cv

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A backup is a zip of one CV: its content files and versions,
//
//	content/_index.md, content/_index.<lang>.md, content/photo.<ext>,
//	content/<section>/<id>.<lang>.md, content/links/<slug>.md,
//	versions/<id>.json
//
// so it can move between computers and between local and hosted use.

const (
	MaxBackup      = 50 << 20  // the zip
	maxBackupFile  = 12 << 20  // one file in it, unpacked
	maxBackupTotal = 100 << 20 // all of them, unpacked
	maxBackupFiles = 5000
)

// backupFile says whether a zip entry may be restored: only the files a CV
// consists of, no traversal, no unknown sections or languages. Share links
// are skipped where there are none (links false), e.g. a server backup
// restored locally.
func backupFile(name string, links bool) (ok, skip bool) {
	if path.Clean(name) != name {
		return false, false
	}
	if name == "compose.json" { // backups from before versions
		return true, false
	}
	if file, found := strings.CutPrefix(name, "versions/"); found {
		id, found := strings.CutSuffix(file, ".json")
		return found && IDRe.MatchString(id), false
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
		return isProfileFile(file), false
	case "links/":
		slug, found := strings.CutSuffix(file, ".md")
		ok := found && IDRe.MatchString(slug)
		if !ok {
			_, _, ok = splitLangFile(file) // from before format 2
		}
		return ok, ok && !links
	default:
		_, _, ok := splitLangFile(file)
		return ok && SectionIndex(strings.TrimSuffix(dir, "/")) >= 0, false
	}
}

// WriteBackup writes a CV as a backup zip.
func (s *Store) WriteBackup(w io.Writer, user string) error {
	zw := zip.NewWriter(w)
	root := filepath.Join(s.Root, user)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if ok, _ := backupFile(rel, true); !ok {
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

// StageBackup unpacks a backup zip into a fresh folder next to the CVs and
// checks it fully, without touching any CV. RestoreBackup puts it in place;
// the caller removes the folder either way. Share links are left out unless
// links.
func (s *Store) StageBackup(data []byte, links bool) (stage string, err error) {
	if len(data) > MaxBackup {
		return "", Invalid{errors.New("backup is larger than 50 MB")}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", Invalid{errors.New("not a zip file")}
	}
	stage, err = os.MkdirTemp(s.Root, ".import-")
	if err != nil {
		return "", err
	}
	if err := s.unpack(stage, zr, links); err != nil {
		os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}

func (s *Store) unpack(stage string, zr *zip.Reader, links bool) error {
	// The sizes a zip declares are what its reader will give: check them all
	// before writing anything, so a small zip can't fill the disk.
	if len(zr.File) > maxBackupFiles {
		return Invalid{fmt.Errorf("a backup has at most %d files", maxBackupFiles)}
	}
	var total uint64
	for _, f := range zr.File {
		if total += f.UncompressedSize64; total > maxBackupTotal {
			return Invalid{errors.New("backup is larger than 100 MB unpacked")}
		}
	}
	found := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ok, skip := backupFile(f.Name, links)
		if skip {
			continue
		}
		if !ok {
			return Invalid{fmt.Errorf("unexpected file in backup: %q", f.Name)}
		}
		if f.UncompressedSize64 > maxBackupFile {
			return Invalid{fmt.Errorf("%s is too large", f.Name)}
		}
		rc, err := f.Open()
		if err != nil {
			return Invalid{err}
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxBackupFile+1))
		rc.Close()
		if err != nil {
			return Invalid{err}
		}
		dst := filepath.Join(stage, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(dst, content, 0o640); err != nil {
			return err
		}
		if strings.HasPrefix(f.Name, "content/photo.") {
			if err := checkPhoto(bytes.NewReader(content)); err != nil {
				return err
			}
		}
		if filepath.Ext(dst) == ".md" {
			var fm map[string]any
			if _, err := readMarkdown(dst, &fm); err != nil {
				return Invalid{fmt.Errorf("%s: %v", f.Name, err)}
			}
		}
		found = found || isProfileFile(strings.TrimPrefix(f.Name, "content/"))
	}
	if !found {
		return Invalid{errors.New("this is not a CV backup: it has no profile")}
	}
	// The staged CV is read as the editor would, and brought up to date as
	// opening it would, before the CV it replaces is touched: a backup that
	// fails here would otherwise restore, and then never open.
	check := &Store{Root: filepath.Dir(stage)}
	name := filepath.Base(stage)
	if err := check.Migrate(name); err != nil {
		return Invalid{err}
	}
	if _, err := check.Items(name); err != nil {
		return Invalid{err}
	}
	if err := check.EnsureVersions(name); err != nil {
		return Invalid{err}
	}
	versions, err := check.Versions(name)
	if err != nil {
		return Invalid{err}
	}
	for _, v := range versions {
		if err := v.Validate(); err != nil {
			return Invalid{fmt.Errorf("version %s: %w", v.ID, err)}
		}
	}
	if _, err := check.Links(name); err != nil {
		return Invalid{err}
	}
	return nil
}

// RestoreBackup replaces a CV with a staged backup. What it replaces is kept
// in the CV's backups/ folder first.
func (s *Store) RestoreBackup(user, stage string) error {
	root := filepath.Join(s.Root, user)
	var old bytes.Buffer
	if err := s.WriteBackup(&old, user); err != nil {
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
	// Swap in the new content and versions, by moving the old aside and the
	// new in: there is never a moment without either. (The stage has been
	// migrated and has its versions; see unpack.)
	for _, name := range []string{"content", "versions", "compose.json"} {
		cur, next, old := filepath.Join(root, name), filepath.Join(stage, name), filepath.Join(stage, name+".old")
		if err := os.Rename(cur, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if _, err := os.Stat(next); err == nil {
			if err := os.Rename(next, cur); err != nil {
				return err
			}
		}
	}
	return s.Migrate(user)
}

// isProfileFile reports whether a file in content/ is part of the profile:
// _index.md (from format 1) or _index.<lang>.md.
func isProfileFile(name string) bool {
	return name == "_index.md" || slices.ContainsFunc(langCodes(), func(lang string) bool { return name == "_index."+lang+".md" })
}

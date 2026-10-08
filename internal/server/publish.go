package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// Share links are published as static files into a webroot that a public
// web server serves:
//
//	<public>/<slug>/index.html, cv.pdf          the page, in every language; the PDF in the link's
//	<public>/<slug>/<lang>/cv.pdf, index.html   the PDF in another language, and a page that
//	                                            sends visitors on to /<slug>/#<lang>
//	<public>/css/, fonts/                       shared by every share page
//
// Nothing else is allowed in the webroot: reconcile removes whatever is not
// an active link, which is also how links expire.

const publicMarker = ".new-leaf-public"

var publicAssets = []string{"css", "fonts"}

// claimPublicDir refuses to manage a directory that wasn't created for us,
// since reconcile deletes everything it doesn't recognise.
func claimPublicDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, publicMarker)); err == nil {
		return nil
	}
	// A webroot from when New Leaf was called cv-app.
	if err := os.Rename(filepath.Join(dir, ".cv-app-public"), filepath.Join(dir, publicMarker)); err == nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty and has no %s marker; refusing to manage it", dir, publicMarker)
	}
	return os.WriteFile(filepath.Join(dir, publicMarker), nil, 0o644)
}

// publishLink writes a link's page and its PDFs. PDFs are only rendered
// again when the page changed, since it carries everything they show.
// Publishes of one user run one at a time, so the newest content always
// lands last.
func (s *Server) publishLink(ctx context.Context, user string, l cv.Link) error {
	st := s.state(user)
	st.publish.Lock()
	defer st.publish.Unlock()

	langs, err := s.store.Langs(user)
	if err != nil {
		return err
	}
	page, err := s.renderShare(user, l, langs)
	if err != nil {
		return fmt.Errorf("link %s: %w", l.Slug, err)
	}
	files := map[string][]byte{"index.html": page}
	for _, lang := range langs {
		if lang != l.Lang {
			if files[lang+"/index.html"], err = s.renderMoved(l.Label, lang); err != nil {
				return err
			}
		}
	}
	if err := s.syncAssets(); err != nil {
		return err
	}
	if err := s.syncTheme(l.Theme); err != nil {
		return err
	}
	if s.isPublished(l, langs, files) {
		return nil
	}

	stage, err := os.MkdirTemp(s.publicDir, ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, lang := range langs {
		opt := l.Print()
		opt.Lang = lang
		pdf, err := s.renderPDF(ctx, user, opt)
		if err != nil {
			return err
		}
		files[sharePDF(l, lang)] = pdf
	}
	for name, data := range files {
		dst := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}

	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	// A link that expired or was deleted while we rendered must not come back.
	if cur, err := s.store.Link(user, l.Slug); err != nil || cur.Expired(time.Now()) {
		return nil
	}
	dst := filepath.Join(s.publicDir, l.Slug)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(stage, dst)
}

// isPublished reports whether the webroot already has these files, with a
// PDF in every language, and no other languages: a language the CV no longer
// has makes it publish again.
func (s *Server) isPublished(l cv.Link, langs []string, files map[string][]byte) bool {
	dir := filepath.Join(s.publicDir, l.Slug)
	subdirs, _ := os.ReadDir(dir)
	for _, e := range subdirs {
		if e.IsDir() && !slices.Contains(langs, e.Name()) {
			return false
		}
	}
	for name, data := range files {
		if cur, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); err != nil || !bytes.Equal(cur, data) {
			return false
		}
	}
	for _, lang := range langs {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(sharePDF(l, lang)))); err != nil {
			return false
		}
	}
	return true
}

// syncAssets writes the share pages' stylesheet and fonts. Additive only:
// pages published by an older version keep the stylesheet they reference.
func (s *Server) syncAssets() error {
	files := map[string]string{s.shareCSS(): "share/share.css"}
	fonts, err := fs.ReadDir(s.assets.fs, "share/fonts")
	if err != nil {
		return err
	}
	for _, f := range fonts {
		files[path.Join("fonts", f.Name())] = path.Join("share/fonts", f.Name())
	}
	for dst, src := range files {
		data, err := s.assets.ReadFile(src)
		if err != nil {
			return err
		}
		dst = filepath.Join(s.publicDir, filepath.FromSlash(dst))
		if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := cv.WriteAtomic(dst, data); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// syncTheme writes a theme's stylesheet, once: its name changes with it.
func (s *Server) syncTheme(t cv.Theme) error {
	name, data := t.CSS()
	dst := filepath.Join(s.publicDir, filepath.FromSlash(name))
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := cv.WriteAtomic(dst, data); err != nil {
		return err
	}
	return os.Chmod(dst, 0o644)
}

// reconcile makes the webroot contain exactly the active links of all users.
func (s *Server) reconcile() error {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()

	keep := map[string]bool{publicMarker: true}
	for _, a := range publicAssets {
		keep[a] = true
	}
	users, err := s.store.Users()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, u := range users {
		// Under the CV's lock, so a restore isn't caught halfway.
		unlock := s.lock(u)
		links, err := s.store.Links(u)
		unlock()
		if err != nil {
			return fmt.Errorf("%s: %w", u, err) // don't delete anything on a read error
		}
		for _, l := range links {
			if !l.Expired(now) {
				keep[l.Slug] = true
			}
		}
	}
	entries, err := os.ReadDir(s.publicDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if keep[e.Name()] || strings.HasPrefix(e.Name(), ".stage-") {
			continue // stage dirs belong to an in-flight publishLink
		}
		log.Printf("unpublishing %s", e.Name())
		if err := os.RemoveAll(filepath.Join(s.publicDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// publishUser brings a user's links up to date, e.g. at startup.
func (s *Server) publishUser(ctx context.Context, user string) error {
	unlock := s.lock(user)
	_, err := s.store.UpgradeLinks(user)
	unlock()
	if err != nil {
		return err
	}
	return s.publishLinks(ctx, user)
}

func (s *Server) publishAll(ctx context.Context) {
	// Also without links, so the webroot always has something to monitor.
	if err := s.syncAssets(); err != nil {
		log.Printf("syncing assets: %v", err)
	}
	users, err := s.store.Users()
	if err != nil {
		log.Printf("listing users: %v", err)
	}
	for _, u := range users {
		if err := s.publishUser(ctx, u); err != nil {
			log.Printf("publishing %s: %v", u, err)
		}
	}
	if err := s.reconcile(); err != nil {
		log.Printf("reconcile: %v", err)
	}
}

// schedulePublish republishes in the background after an edit, coalescing
// edits that arrive while a run is in progress into one follow-up run.
func (s *Server) schedulePublish(user string) {
	if !s.sharing {
		return
	}
	st := s.state(user)
	st.mu.Lock()
	if st.publishing {
		st.pending = true
		st.mu.Unlock()
		return
	}
	st.publishing = true
	st.mu.Unlock()

	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			if err := s.publishLinks(ctx, user); err != nil {
				log.Printf("republishing %s: %v", user, err)
			}
			cancel()
			st.mu.Lock()
			if !st.pending {
				st.publishing = false
				st.mu.Unlock()
				return
			}
			st.pending = false
			st.mu.Unlock()
		}
	}()
}

// publishLinks republishes a user's active links.
func (s *Server) publishLinks(ctx context.Context, user string) error {
	links, err := s.store.Links(user)
	if err != nil {
		return err
	}
	var errs []error
	for _, l := range links {
		if !l.Expired(time.Now()) {
			errs = append(errs, s.publishLink(ctx, user, l))
		}
	}
	return errors.Join(errs...)
}

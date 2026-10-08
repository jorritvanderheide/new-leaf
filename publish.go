package main

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
	"regexp"
	"strings"
	"time"
)

// Share links are published as static files into a webroot that a public
// web server serves:
//
//	<public>/<slug>/index.html, cv.pdf          the link's own language
//	<public>/<slug>/<lang>/index.html, cv.pdf   the other languages
//	<public>/css/, fonts/                       shared by every share page
//
// Nothing else is allowed in the webroot: reconcile removes whatever is not
// an active link, which is also how links expire.

const publicMarker = ".cv-app-public"

var publicAssets = []string{"css", "fonts"}

var pageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

// PageCount counts page objects; Typst writes them uncompressed.
func PageCount(pdf []byte) int { return len(pageRe.FindAll(pdf, -1)) }

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(bytes.TrimSpace(b))
}

// claimPublicDir refuses to manage a directory that wasn't created for us,
// since reconcile deletes everything it doesn't recognise.
func claimPublicDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, publicMarker)); err == nil {
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

// publishLink writes a link's pages and PDFs, one of each per language. PDFs
// are only re-rendered when a page changed, since the pages carry everything
// the PDFs show. Publishes of one user run one at a time, so the newest
// content always lands last.
func (s *Server) publishLink(ctx context.Context, user string, l Link) error {
	st := s.state(user)
	st.publish.Lock()
	defer st.publish.Unlock()

	pages := map[string][]byte{}
	for _, lang := range Langs {
		page, err := s.renderShare(user, l, lang)
		if err != nil {
			return fmt.Errorf("link %s (%s): %w", l.Slug, lang, err)
		}
		pages[lang] = page
	}
	if err := s.syncAssets(); err != nil {
		return err
	}
	if err := s.syncTheme(l.Theme); err != nil {
		return err
	}
	if s.isPublished(l, pages) {
		return nil
	}

	stage, err := os.MkdirTemp(s.publicDir, ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, lang := range Langs {
		rel, _ := filepath.Rel(l.Slug, l.LinkDir(lang)) // "." or the language
		dir := filepath.Join(stage, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		opt := l.Print()
		opt.Lang = lang
		pdf, err := s.renderPDF(ctx, user, opt)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), pages[lang], 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "cv.pdf"), pdf, 0o644); err != nil {
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

// isPublished reports whether the webroot already has these pages, with PDFs.
func (s *Server) isPublished(l Link, pages map[string][]byte) bool {
	for lang, page := range pages {
		dir := filepath.Join(s.publicDir, l.LinkDir(lang))
		if cur, err := os.ReadFile(filepath.Join(dir, "index.html")); err != nil || !bytes.Equal(cur, page) {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, "cv.pdf")); err != nil {
			return false
		}
	}
	return true
}

// syncAssets writes the share pages' stylesheet and fonts. Additive only:
// pages published by an older version keep the stylesheet they reference.
func (s *Server) syncAssets() error {
	files := map[string]string{s.shareCSS(): "web/share/share.css"}
	fonts, err := fs.ReadDir(s.assets.fs, "web/share/fonts")
	if err != nil {
		return err
	}
	for _, f := range fonts {
		files[path.Join("fonts", f.Name())] = path.Join("web/share/fonts", f.Name())
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
		if err := writeAtomic(dst, data); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// syncTheme writes a theme's stylesheet, once: its name changes with it.
func (s *Server) syncTheme(t Theme) error {
	name, data := t.css()
	dst := filepath.Join(s.publicDir, filepath.FromSlash(name))
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(dst, data); err != nil {
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
		links, err := s.store.Links(u)
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

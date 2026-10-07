package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Renderer runs Hugo and headless Chromium for a user. The Hugo site is
// read-only (the Nix store in production); everything it writes goes to a
// per-user work directory.
type Renderer struct {
	Hugo, Chromium string
	Site           string
	Work           string

	chromeSlots chan struct{}
}

func (r *Renderer) init() { r.chromeSlots = make(chan struct{}, 2) }

func (r *Renderer) work(user string) string { return filepath.Join(r.Work, user) }

// BuildDir is a symlink to the latest complete build, swapped atomically.
func (r *Renderer) BuildDir(user string) string { return filepath.Join(r.work(user), "build") }

func (r *Renderer) Build(ctx context.Context, user, contentDir string) error {
	w := r.work(user)
	if err := os.MkdirAll(w, 0o750); err != nil {
		return err
	}
	dest, err := os.MkdirTemp(w, "build-")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Hugo, "build",
		"--source", r.Site,
		"--contentDir", contentDir,
		"--destination", dest,
		"--cacheDir", filepath.Join(w, "cache"),
		"--noBuildLock")
	cmd.Env = append(os.Environ(), "HUGO_RESOURCEDIR="+filepath.Join(w, "resources"), "HOME="+w)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("hugo: %v: %s", err, bytes.TrimSpace(out))
	}

	link := filepath.Join(w, "build.new")
	os.Remove(link)
	if err := os.Symlink(filepath.Base(dest), link); err != nil {
		return err
	}
	if err := os.Rename(link, r.BuildDir(user)); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(w, "build-*"))
	for _, dir := range old {
		if dir != dest {
			os.RemoveAll(dir)
		}
	}
	return nil
}

func (r *Renderer) HasBuild(user string) bool {
	_, err := os.Stat(filepath.Join(r.BuildDir(user), "en", "index.html"))
	return err == nil
}

// PDF prints the user's print page trimmed to the given items.
func (r *Renderer) PDF(ctx context.Context, user string, opt PrintOptions) ([]byte, error) {
	page, err := filepath.Abs(filepath.Join(r.BuildDir(user), opt.Lang, "index.html"))
	if err != nil {
		return nil, err
	}
	q := url.Values{"sel": {strings.Join(opt.Entries, ",")}}
	if opt.Photo {
		q.Set("photo", "1")
	}
	if opt.Spacing != 0 {
		q.Set("space", strconv.FormatFloat(opt.Spacing, 'f', 2, 64))
	}
	target := (&url.URL{Scheme: "file", Path: page, RawQuery: q.Encode()}).String()

	select {
	case r.chromeSlots <- struct{}{}:
		defer func() { <-r.chromeSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	tmp, err := os.MkdirTemp("", "cv-app-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "cv.pdf")

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// No Chromium sandbox: it needs user namespaces that a hardened systemd
	// service doesn't grant. It only ever loads our own Hugo output, in
	// which Markdown can't carry raw HTML or scripts.
	cmd := exec.CommandContext(ctx, r.Chromium,
		"--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--no-first-run", "--disable-extensions", "--disable-background-networking",
		"--allow-file-access-from-files", // web fonts load from file://
		"--user-data-dir="+filepath.Join(tmp, "profile"),
		"--no-pdf-header-footer",
		"--print-to-pdf="+out,
		"--virtual-time-budget=5000",
		target)
	cmd.Env = append(os.Environ(), "HOME="+tmp)
	log, runErr := cmd.CombinedOutput()
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("chromium produced no PDF (%v): %s", runErr, tail(log, 800))
	}
	return data, nil
}

var pageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

// PageCount counts page objects; Chromium writes them uncompressed.
func PageCount(pdf []byte) int { return len(pageRe.FindAll(pdf, -1)) }

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(bytes.TrimSpace(b))
}

// --- publishing into the public webroot
//
//	<public>/<slug>/index.html   share page
//	<public>/<slug>/cv.pdf       its PDF
//	<public>/css/, fonts/        assets shared by every share page
//
// Nothing else is allowed in the webroot: reconcile removes whatever is not
// an active link, which is also how links expire.

const publicMarker = ".cv-app-public"

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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty and has no %s marker; refusing to manage it", dir, publicMarker)
	}
	return os.WriteFile(filepath.Join(dir, publicMarker), nil, 0o644)
}

// publishLink writes one link's page and PDF. The PDF is only re-rendered
// when the page changed, since the page carries everything the PDF shows.
func (s *Server) publishLink(ctx context.Context, user string, l Link) error {
	build := s.renderer.BuildDir(user)
	page, err := os.ReadFile(filepath.Join(build, l.Slug, "index.html"))
	if err != nil {
		return fmt.Errorf("link %s missing from build: %w", l.Slug, err)
	}
	if err := s.syncAssets(build); err != nil {
		return err
	}
	dst := filepath.Join(s.publicDir, l.Slug)
	if cur, err := os.ReadFile(filepath.Join(dst, "index.html")); err == nil && bytes.Equal(cur, page) {
		if _, err := os.Stat(filepath.Join(dst, "cv.pdf")); err == nil {
			return nil
		}
	}
	pdf, err := s.renderer.PDF(ctx, user, l.Print())
	if err != nil {
		return err
	}

	stage, err := os.MkdirTemp(s.publicDir, ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, "index.html"), page, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "cv.pdf"), pdf, 0o644); err != nil {
		return err
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}

	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	// A link that expired or was deleted while we rendered must not come
	// back, and a render that a newer build overtook must not replace it.
	if cur, err := s.store.Link(user, l.Slug); err != nil || cur.Expired(time.Now()) {
		return nil
	}
	if latest, err := os.ReadFile(filepath.Join(build, l.Slug, "index.html")); err != nil || !bytes.Equal(latest, page) {
		return nil
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(stage, dst)
}

// syncAssets copies the shared CSS and fonts. Additive only: share pages
// rendered by an older version keep the stylesheet they reference.
func (s *Server) syncAssets(build string) error {
	for _, dir := range publicAssets {
		err := filepath.WalkDir(filepath.Join(build, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(build, path)
			dst := filepath.Join(s.publicDir, rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := writeAtomic(dst, data); err != nil {
				return err
			}
			return os.Chmod(dst, 0o644)
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
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

// publishUser rebuilds a user's CV and republishes their active links.
func (s *Server) publishUser(ctx context.Context, user string) error {
	unlock := s.lock(user)
	err := s.renderer.Build(ctx, user, s.store.ContentDir(user))
	unlock()
	if err != nil {
		return err
	}
	return s.publishLinks(ctx, user)
}

func (s *Server) publishAll(ctx context.Context) {
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

// publishLinks republishes active links from the current build.
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

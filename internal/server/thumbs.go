package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// Thumbnails: page 1 of each version as a small PNG, plus its page count,
// for the overview. They are rendered in the background into the work
// directory, named <version>.<key>.<pages>.png, where the key says what the
// PDF would look like; so a thumbnail is never stale, only missing for a
// moment after a change.

const thumbPPI = 36 // an A4 page 298 px wide

func (s *Server) thumbDir(user string) string { return filepath.Join(s.work, "thumbs", user) }

// thumbKey identifies a document as typst would render it.
func (s *Server) thumbKey(doc cv.Document, photo string) string {
	h := sha256.New()
	json.NewEncoder(h).Encode(doc)
	io.WriteString(h, s.assets.Version("typst/cv.typ"))
	if fi, err := os.Stat(photo); err == nil && doc.Photo != "" {
		fmt.Fprint(h, fi.Size(), fi.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)[:6])
}

// thumb finds a version's thumbnail for a key, and the page count in it.
func (s *Server) thumb(user, id, key string) (path string, pages int, ok bool) {
	matches, _ := filepath.Glob(filepath.Join(s.thumbDir(user), id+"."+key+".*.png"))
	if len(matches) == 0 {
		return "", 0, false
	}
	parts := strings.Split(filepath.Base(matches[0]), ".")
	pages, _ = strconv.Atoi(parts[len(parts)-2])
	return matches[0], pages, true
}

// versionThumbs adds the current thumbnails to a CV's versions, and has the
// missing ones made.
func (s *Server) versionThumbs(user string, profile cv.Profile, items []cv.Item, versions []versionView) {
	if s.work == "" {
		return
	}
	photo := s.store.PhotoPath(user)
	missing := false
	for i, v := range versions {
		key := s.thumbKey(cv.BuildDocument(profile, items, v.PrintOptions, filepath.Base(photo)), photo)
		if _, pages, ok := s.thumb(user, v.ID, key); ok {
			versions[i].Thumb, versions[i].Pages = key, pages
		} else {
			missing = true
		}
	}
	if missing {
		s.scheduleThumbs(user)
	}
}

// scheduleThumbs renders a CV's missing thumbnails in the background, a
// moment after the last change, one run at a time.
func (s *Server) scheduleThumbs(user string) {
	st := s.state(user)
	st.mu.Lock()
	if st.thumbing {
		st.thumbsPending = true
		st.mu.Unlock()
		return
	}
	st.thumbing = true
	st.mu.Unlock()

	go func() {
		for {
			time.Sleep(time.Second) // let a burst of edits settle
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := s.renderThumbs(ctx, user); err != nil {
				log.Printf("thumbnails for %s: %v", user, err)
			}
			cancel()
			st.mu.Lock()
			if !st.thumbsPending {
				st.thumbing = false
				st.mu.Unlock()
				return
			}
			st.thumbsPending = false
			st.mu.Unlock()
		}
	}()
}

func (s *Server) renderThumbs(ctx context.Context, user string) error {
	profile, err := s.store.Profile(user)
	if err != nil {
		return err
	}
	items, err := s.store.Items(user)
	if err != nil {
		return err
	}
	versions, err := s.store.Versions(user)
	if err != nil {
		return err
	}
	dir := s.thumbDir(user)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	photo := s.store.PhotoPath(user)
	keep := map[string]bool{}
	for _, v := range versions {
		doc := cv.BuildDocument(profile, items, v.PrintOptions, filepath.Base(photo))
		key := s.thumbKey(doc, photo)
		if path, _, ok := s.thumb(user, v.ID, key); ok {
			keep[path] = true
			continue
		}
		png, pages, err := s.typst.PNG(ctx, doc, photo, thumbPPI)
		if err != nil {
			return fmt.Errorf("%s: %w", v.ID, err)
		}
		path := filepath.Join(dir, fmt.Sprintf("%s.%s.%d.png", v.ID, key, pages))
		if err := cv.WriteAtomic(path, png); err != nil {
			return err
		}
		keep[path] = true
	}
	// Older thumbnails, and those of deleted versions.
	all, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	for _, path := range all {
		if !keep[path] {
			os.Remove(path)
		}
	}
	return nil
}

// getThumb serves a version's thumbnail. The URL carries its key, so it can
// be cached for good.
func (s *Server) getThumb(w http.ResponseWriter, r *http.Request) {
	id, key := r.PathValue("id"), r.URL.Query().Get("k")
	if !cv.IDRe.MatchString(id) || !thumbKeyRe.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	path, _, ok := s.thumb(userOf(r), id, key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

var thumbKeyRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

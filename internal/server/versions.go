package server

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

type versionRequest struct {
	cv.Version
	From string `json:"from"` // duplicate this version
}

// postVersion makes a version: a copy of another one, everything, or (with
// an ID, to undo a delete) exactly the one given.
func (s *Server) postVersion(r *http.Request, user string) error {
	var req versionRequest
	if err := readJSON(r, &req); err != nil {
		return err
	}
	now := time.Now()
	return s.change(user, func() error {
		v := req.Version
		switch {
		case v.ID != "":
			if _, err := s.store.Version(user, v.ID); !errors.Is(err, cv.ErrNotFound) {
				return badRequest{fmt.Errorf("version %q exists", v.ID)}
			}
		case req.From != "":
			from, err := s.store.Version(user, req.From)
			if err != nil {
				return err
			}
			v = from
			v.Name = cmp.Or(cv.Clean(req.Name), "Copy of "+from.Name)
		default:
			profile, err := s.store.Profile(user)
			if err != nil {
				return err
			}
			items, err := s.store.Items(user)
			if err != nil {
				return err
			}
			v.PrintOptions = cv.PrintOptions{Lang: cmp.Or(v.Lang, profile.Langs[0]), Photo: profile.Photo, Spacing: cmp.Or(profile.Spacing, 1), Order: profile.Order, Theme: profile.Theme.ForNewVersion(), Entries: []string{}}
			for _, it := range items {
				v.Entries = append(v.Entries, it.Section+"/"+it.ID)
			}
		}
		if v.ID == "" {
			v.ID = s.store.NewVersionID(user, v.Name)
		}
		v.Created = cmp.Or(v.Created, now.In(cv.LinkZone).Format("2006-01-02"))
		v.Updated = now.UTC().Format(time.RFC3339)
		if err := s.store.SaveVersion(user, v); err != nil {
			return badRequest{err}
		}
		return nil
	})
}

// putVersion saves a version as it is edited. A shared version's link
// follows it.
func (s *Server) putVersion(r *http.Request, user string) error {
	var v cv.Version
	if err := readJSON(r, &v); err != nil {
		return err
	}
	shared := false
	err := s.change(user, func() error {
		cur, err := s.store.Version(user, r.PathValue("id"))
		if err != nil {
			return err
		}
		v.ID, v.Created, v.Updated = cur.ID, cur.Created, cur.Updated
		if !sameSettings(v, cur) { // not just the preview's page count
			v.Updated = time.Now().UTC().Format(time.RFC3339)
		}
		shared, err = s.storeVersion(user, v)
		return err
	})
	if err == nil && shared {
		s.schedulePublish(user)
	}
	return err
}

// storeVersion saves a version and makes its link, if any, follow it. The
// caller holds the user's lock and republishes.
func (s *Server) storeVersion(user string, v cv.Version) (shared bool, err error) {
	link, err := s.store.VersionLink(user, v.ID)
	if err != nil {
		return false, err
	}
	if link != nil && len(v.Entries) == 0 {
		return true, badRequest{errors.New("a shared version needs at least one item; stop sharing it first")}
	}
	if err := s.store.SaveVersion(user, v); err != nil {
		return link != nil, badRequest{err}
	}
	if link == nil {
		return false, nil
	}
	follow(link, v)
	return true, s.store.SaveLink(user, *link)
}

func sameSettings(a, b cv.Version) bool {
	a.Pages, b.Pages = 0, 0
	a.Order, b.Order = cv.SectionOrder(a.Order), cv.SectionOrder(b.Order)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// follow makes a link show a version. The language it opens in is the
// link's own, chosen when sharing.
func follow(l *cv.Link, v cv.Version) {
	l.Version, l.Label = v.ID, v.Name
	l.Entries, l.Photo, l.Spacing, l.Order, l.Theme = v.Entries, v.Photo, v.Spacing, v.Order, v.Theme
}

// deleteVersion deletes a version and takes its link offline.
func (s *Server) deleteVersion(r *http.Request, user string) error {
	id := r.PathValue("id")
	shared := false
	err := s.change(user, func() error {
		link, err := s.store.VersionLink(user, id)
		if err != nil {
			return err
		}
		if err := s.store.DeleteVersion(user, id); err != nil {
			return err
		}
		if link == nil {
			return nil
		}
		shared = true
		return s.store.DeleteLink(user, link.Slug)
	})
	if err != nil || !shared {
		return err
	}
	return s.reconcile()
}

// putShare shares a version until a date, or changes that date, and the
// language the page opens in (by default the version's). The link is
// published before the answer, so it works as soon as the editor shows it.
func (s *Server) putShare(r *http.Request, user string) error {
	var req struct {
		Expires string `json:"expires"`
		Lang    string `json:"lang"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	v, err := s.store.Version(user, r.PathValue("id"))
	if err != nil {
		return err
	}
	link, err := s.store.VersionLink(user, v.ID)
	if err != nil {
		return err
	}
	if link == nil {
		link = &cv.Link{Created: time.Now().In(cv.LinkZone).Format("2006-01-02")}
		for link.Slug == "" || s.slugTaken(user, link.Slug) {
			link.Slug = cv.NewSlug()
		}
	}
	follow(link, v)
	link.Lang = cmp.Or(req.Lang, link.Lang, v.Lang)
	if langs, err := s.store.Langs(user); err != nil {
		return err
	} else if !slices.Contains(langs, link.Lang) {
		return badRequest{fmt.Errorf("the CV has no %s", cv.LanguageOf(link.Lang).English)}
	}
	link.Expires = req.Expires
	return s.saveLink(r.Context(), user, *link)
}

func (s *Server) deleteShare(r *http.Request, user string) error {
	err := s.change(user, func() error {
		link, err := s.store.VersionLink(user, r.PathValue("id"))
		if err != nil {
			return err
		}
		if link == nil {
			return cv.ErrNotFound
		}
		return s.store.DeleteLink(user, link.Slug)
	})
	if err != nil {
		return err
	}
	return s.reconcile()
}

// slugTaken reports whether a slug is in use, by this user or in the webroot.
func (s *Server) slugTaken(user, slug string) bool {
	if _, err := os.Stat(filepath.Join(s.publicDir, slug)); !errors.Is(err, fs.ErrNotExist) {
		return true
	}
	_, err := s.store.Link(user, slug)
	return !errors.Is(err, cv.ErrNotFound)
}

// saveLink writes the link and publishes it before answering.
func (s *Server) saveLink(ctx context.Context, user string, l cv.Link) error {
	if err := l.Validate(); err != nil {
		return badRequest{err}
	}
	if l.Expired(time.Now()) {
		return badRequest{errors.New("expiry date must be in the future")}
	}
	if err := s.change(user, func() error { return s.store.SaveLink(user, l) }); err != nil {
		return err
	}
	return s.publishLink(ctx, user, l)
}

package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

func (s *Server) putProfile(r *http.Request, user string) error {
	var p cv.Profile
	if err := readJSON(r, &p); err != nil {
		return err
	}
	if p.Langs == nil { // an editor from before languages could be chosen
		langs, err := s.store.Langs(user)
		if err != nil {
			return err
		}
		p.Langs = langs
	}
	if err := p.Validate(); err != nil {
		return badRequest{err}
	}
	return s.edit(r.Context(), user, func() error {
		versions, err := s.store.Versions(user)
		if err != nil {
			return err
		}
		// Versions in a language the CV loses move to its main one, unless
		// they are shared: the link would change language under its readers.
		var moved []cv.Version
		for _, v := range versions {
			if slices.Contains(p.Langs, v.Lang) {
				continue
			}
			if link, err := s.store.VersionLink(user, v.ID); err != nil {
				return err
			} else if link != nil {
				return badRequest{fmt.Errorf("“%s” is shared in %s; stop sharing it or change its language first", v.Name, cv.LanguageOf(v.Lang).English)}
			}
			v.Lang = p.Langs[0]
			moved = append(moved, v)
		}
		if err := s.store.SaveProfile(user, p); err != nil {
			return err
		}
		for _, v := range moved {
			if err := s.store.SaveVersion(user, v); err != nil {
				return err
			}
		}
		_, err = s.store.UpgradeLinks(user) // links in the new languages
		return err
	})
}

var photoTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

func (s *Server) getPhoto(w http.ResponseWriter, r *http.Request) {
	path := s.store.PhotoPath(userOf(r))
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

func (s *Server) postPhoto(r *http.Request, user string) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 12<<20)
	file, _, err := r.FormFile("photo")
	if err != nil {
		return badRequest{fmt.Errorf("upload a photo of at most 10 MB: %w", err)}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 10<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 10<<20 {
		return badRequest{errors.New("photo is larger than 10 MB")}
	}
	ext, ok := photoTypes[http.DetectContentType(data)]
	if !ok {
		return badRequest{errors.New("photo must be a JPEG, PNG or WebP image")}
	}
	return s.edit(r.Context(), user, func() error { return s.store.SavePhoto(user, ext, data) })
}

func (s *Server) deletePhoto(r *http.Request, user string) error {
	return s.edit(r.Context(), user, func() error { return s.store.DeletePhoto(user) })
}

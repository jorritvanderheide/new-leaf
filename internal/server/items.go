package server

import (
	"fmt"
	"net/http"
	"slices"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

func (s *Server) postItem(r *http.Request, user string) error {
	var it cv.Item
	if err := readJSON(r, &it); err != nil {
		return err
	}
	if cv.SectionIndex(it.Section) < 0 {
		return badRequest{fmt.Errorf("unknown section %q", it.Section)}
	}
	return s.edit(r.Context(), user, func() error {
		// An ID is only given when undoing a delete: restore under the old
		// ID, so share links that select the item keep it.
		if it.ID == "" {
			it.ID = s.store.NewItemID(user, it)
		} else if !cv.IDRe.MatchString(it.ID) || s.store.ItemExists(user, it.Section, it.ID) {
			return badRequest{fmt.Errorf("item id %q is invalid or taken", it.ID)}
		}
		if err := it.Validate(); err != nil {
			return badRequest{err}
		}
		before, err := s.store.Items(user)
		if err != nil {
			return err
		}
		if err := s.store.SaveItem(user, it); err != nil {
			return err
		}
		return s.addToCompleteVersions(user, before, it.Section+"/"+it.ID)
	})
}

// addToCompleteVersions adds a new item to the versions that hold every
// other item, such as "Full CV", so they stay complete. A shared one's link
// follows; s.edit republishes it.
func (s *Server) addToCompleteVersions(user string, before []cv.Item, key string) error {
	versions, err := s.store.Versions(user)
	if err != nil {
		return err
	}
	for _, v := range versions {
		complete := !slices.Contains(v.Entries, key)
		for _, it := range before {
			complete = complete && slices.Contains(v.Entries, it.Section+"/"+it.ID)
		}
		if !complete {
			continue
		}
		v.Entries = append(v.Entries, key)
		if _, err := s.storeVersion(user, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) putItem(r *http.Request, user string) error {
	var it cv.Item
	if err := readJSON(r, &it); err != nil {
		return err
	}
	it.Section, it.ID = r.PathValue("section"), r.PathValue("id")
	return s.edit(r.Context(), user, func() error {
		if cv.SectionIndex(it.Section) < 0 || !cv.IDRe.MatchString(it.ID) || !s.store.ItemExists(user, it.Section, it.ID) {
			return cv.ErrNotFound
		}
		if err := it.Validate(); err != nil {
			return badRequest{err}
		}
		return s.store.SaveItem(user, it)
	})
}

func (s *Server) deleteItem(r *http.Request, user string) error {
	return s.edit(r.Context(), user, func() error {
		return s.store.DeleteItem(user, r.PathValue("section"), r.PathValue("id"))
	})
}

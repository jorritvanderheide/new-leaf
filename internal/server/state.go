package server

import (
	"context"
	"net/http"
	"slices"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

type linkView struct {
	cv.Link
	URL     string `json:"url"`
	Expired bool   `json:"expired"`
}

type editorState struct {
	User       string        `json:"user"`
	Langs      []string      `json:"langs"`
	Languages  []cv.Language `json:"languages"` // every language a CV can have
	Sections   []string      `json:"sections"`
	Point      []string      `json:"pointSections"`
	Profile    cv.Profile    `json:"profile"`
	Items      []cv.Item     `json:"items"`
	Versions   []versionView `json:"versions"` // most recently edited first
	Today      string        `json:"today"`
	CVs        []cv.CVInfo   `json:"cvs"`        // the CVs the visitor may edit, for the switcher
	Manage     bool          `json:"manage"`     // CVs can be created, renamed and deleted
	OwnersOnly bool          `json:"ownersOnly"` // CVs are for their owners only
	Login      string        `json:"login"`      // the visitor's tailnet login; none in dev and local mode
	Sharing    bool          `json:"sharing"`    // share links are available
	Local      bool          `json:"local"`      // running on the user's own computer
}

func (s *Server) editorState(ctx context.Context, user string) (editorState, error) {
	st := editorState{
		User: user, Manage: s.auth.CVs.Manage, OwnersOnly: s.auth.CVs.OwnersOnly, Login: loginOf(ctx),
		Languages: cv.Languages, Sections: cv.Sections, Point: cv.PointSections,
		Sharing: s.sharing, Local: s.local,
		Today: time.Now().In(cv.LinkZone).Format("2006-01-02"),
	}
	st.CVs = slices.DeleteFunc(s.auth.CVs.List(), func(c cv.CVInfo) bool { return !s.auth.MayEdit(c.ID, st.Login) })
	var err error
	if st.Profile, err = s.store.Profile(user); err != nil {
		return st, err
	}
	st.Langs = st.Profile.Langs
	if st.Items, err = s.store.Items(user); err != nil {
		return st, err
	}
	if st.Items == nil {
		st.Items = []cv.Item{}
	}
	versions, err := s.store.Versions(user)
	if err != nil {
		return st, err
	}
	links, err := s.store.Links(user)
	if err != nil {
		return st, err
	}
	now := time.Now()
	st.Versions = []versionView{}
	for _, v := range versions {
		vv := versionView{Version: v}
		for _, l := range links {
			if l.Version == v.ID {
				vv.Link = &linkView{l, s.publicURL + "/" + l.Slug + "/", l.Expired(now)}
			}
		}
		st.Versions = append(st.Versions, vv)
	}
	s.versionThumbs(user, st.Profile, st.Items, st.Versions)
	return st, nil
}

// versionView is a version with its share link, if it has one.
type versionView struct {
	cv.Version
	Link  *linkView `json:"link"`
	Thumb string    `json:"thumb"` // key of the thumbnail; empty while it is being made
}

func (s *Server) getState(r *http.Request, user string) error { return nil }

// change applies a change to a user's content, one at a time.
func (s *Server) change(user string, fn func() error) error {
	unlock := s.lock(user)
	defer unlock()
	return fn()
}

// edit is change for CV content, which share links show too: their pages
// and PDFs are refreshed in the background.
func (s *Server) edit(ctx context.Context, user string, fn func() error) error {
	if err := s.change(user, fn); err != nil {
		return err
	}
	s.schedulePublish(user)
	return nil
}

package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	store     *Store
	typst     *Typst
	assets    *Assets
	auth      *Auth
	publicDir string
	publicURL string
	work      string // regenerable files, such as thumbnails
	sharing   bool   // share links (server); off when running locally
	local     bool   // running on someone's own computer
	quit      func() // local mode: stop the app

	mu    sync.Mutex
	users map[string]*userState
	pubMu sync.Mutex // guards renames into, and deletions from, the webroot
}

type userState struct {
	content sync.Mutex // serialises content writes
	publish sync.Mutex // one publish at a time, so the newest lands last

	mu                      sync.Mutex
	publishing, pending     bool
	thumbing, thumbsPending bool
}

func (s *Server) state(user string) *userState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users == nil {
		s.users = map[string]*userState{}
	}
	if s.users[user] == nil {
		s.users[user] = &userState{}
	}
	return s.users[user]
}

func (s *Server) lock(user string) (unlock func()) {
	st := s.state(user)
	st.content.Lock()
	return st.content.Unlock
}

type ctxKey struct{}

func userOf(r *http.Request) string { return r.Context().Value(ctxKey{}).(string) }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handle(s.getState))
	mux.HandleFunc("PUT /api/profile", s.handle(s.putProfile))
	mux.HandleFunc("GET /api/photo", s.getPhoto)
	mux.HandleFunc("POST /api/photo", s.handle(s.postPhoto))
	mux.HandleFunc("DELETE /api/photo", s.handle(s.deletePhoto))
	mux.HandleFunc("POST /api/items", s.handle(s.postItem))
	mux.HandleFunc("PUT /api/items/{section}/{id}", s.handle(s.putItem))
	mux.HandleFunc("DELETE /api/items/{section}/{id}", s.handle(s.deleteItem))
	mux.HandleFunc("POST /api/pdf", s.postPDF)
	mux.HandleFunc("POST /api/fit", s.postFit)
	mux.HandleFunc("POST /api/layout", s.postLayout)
	mux.HandleFunc("POST /api/versions", s.handle(s.postVersion))
	mux.HandleFunc("PUT /api/versions/{id}", s.handle(s.putVersion))
	mux.HandleFunc("DELETE /api/versions/{id}", s.handle(s.deleteVersion))
	mux.HandleFunc("GET /api/versions/{id}/thumb", s.getThumb)
	mux.HandleFunc("GET /api/export", s.getExport)
	mux.HandleFunc("POST /api/import", s.handle(s.postImport))
	mux.HandleFunc("GET /api/ping", s.getPing)
	if s.auth.CVs.manage {
		mux.HandleFunc("POST /api/cvs", s.postCV)
		mux.HandleFunc("PUT /api/cvs/{id}", s.handle(s.putCV))
		mux.HandleFunc("DELETE /api/cvs/{id}", s.deleteCV)
	}
	if s.sharing {
		mux.HandleFunc("PUT /api/versions/{id}/share", s.handle(s.putShare))
		mux.HandleFunc("DELETE /api/versions/{id}/share", s.handle(s.deleteShare))
	}
	if s.quit != nil {
		mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
			go s.quit()
		})
	}
	mux.Handle("/", s.uiHandler())
	return s.authenticate(mux)
}

// authenticate resolves the tailnet user for every request, including the
// UI itself, and blocks cross-site writes: a custom header can't be sent
// cross-origin without a CORS preflight, which this server never approves.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; img-src 'self' blob: data:; frame-src 'none'; object-src 'none'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")

		user, err := s.auth.User(r)
		if err != nil {
			if !errors.Is(err, errForbidden) {
				log.Printf("auth: %v", err)
			}
			http.Error(w, "This editor is only available to approved users on the tailnet.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-New-Leaf") != "1" {
			http.Error(w, "missing X-New-Leaf header", http.StatusForbidden)
			return
		}
		if err := s.change(user, func() error {
			if err := s.store.Init(user); err != nil {
				return err
			}
			return s.store.EnsureVersions(user)
		}); err != nil {
			httpError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

// handle runs a mutation (or read) and answers with the full editor state,
// which keeps the UI a plain re-render of whatever the server returns.
func (s *Server) handle(fn func(r *http.Request, user string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userOf(r)
		if err := fn(r, user); err != nil {
			httpError(w, err)
			return
		}
		st, err := s.editorState(r.Context(), user)
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, st)
	}
}

type badRequest struct{ error }

func httpError(w http.ResponseWriter, err error) {
	var bad badRequest
	switch {
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), http.StatusBadRequest)
	case errors.Is(err, errNotFound), errors.Is(err, fs.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		log.Printf("error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return badRequest{fmt.Errorf("invalid JSON: %w", err)}
	}
	return nil
}

// --- state

type linkView struct {
	Link
	URL     string `json:"url"`
	Expired bool   `json:"expired"`
}

type editorState struct {
	User      string        `json:"user"`
	Langs     []string      `json:"langs"`
	Languages []Language    `json:"languages"` // every language a CV can have
	Sections  []string      `json:"sections"`
	Point     []string      `json:"pointSections"`
	Profile   Profile       `json:"profile"`
	Items     []Item        `json:"items"`
	Versions  []versionView `json:"versions"` // most recently edited first
	Today     string        `json:"today"`
	CVs       []CVInfo      `json:"cvs"`     // all CVs, for the switcher
	Manage    bool          `json:"manage"`  // CVs can be created, renamed and deleted
	Sharing   bool          `json:"sharing"` // share links are available
	Local     bool          `json:"local"`   // running on the user's own computer
}

func (s *Server) editorState(ctx context.Context, user string) (editorState, error) {
	st := editorState{
		User: user, CVs: s.auth.CVs.List(), Manage: s.auth.CVs.manage, Languages: languages, Sections: Sections, Point: PointSections,
		Sharing: s.sharing, Local: s.local,
		Today: time.Now().In(linkZone).Format("2006-01-02"),
	}
	var err error
	if st.Profile, err = s.store.Profile(user); err != nil {
		return st, err
	}
	st.Langs = st.Profile.Langs
	if st.Items, err = s.store.Items(user); err != nil {
		return st, err
	}
	if st.Items == nil {
		st.Items = []Item{}
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
	Version
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

// --- profile

func (s *Server) putProfile(r *http.Request, user string) error {
	var p Profile
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
		var moved []Version
		for _, v := range versions {
			if slices.Contains(p.Langs, v.Lang) {
				continue
			}
			if link, err := s.store.VersionLink(user, v.ID); err != nil {
				return err
			} else if link != nil {
				return badRequest{fmt.Errorf("“%s” is shared in %s; stop sharing it or change its language first", v.Name, language(v.Lang).English)}
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

// --- items

func (s *Server) postItem(r *http.Request, user string) error {
	var it Item
	if err := readJSON(r, &it); err != nil {
		return err
	}
	if sectionIndex(it.Section) < 0 {
		return badRequest{fmt.Errorf("unknown section %q", it.Section)}
	}
	return s.edit(r.Context(), user, func() error {
		// An ID is only given when undoing a delete: restore under the old
		// ID, so share links that select the item keep it.
		if it.ID == "" {
			it.ID = s.store.NewItemID(user, it)
		} else if !idRe.MatchString(it.ID) || s.store.itemExists(user, it.Section, it.ID) {
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
func (s *Server) addToCompleteVersions(user string, before []Item, key string) error {
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
	var it Item
	if err := readJSON(r, &it); err != nil {
		return err
	}
	it.Section, it.ID = r.PathValue("section"), r.PathValue("id")
	return s.edit(r.Context(), user, func() error {
		if sectionIndex(it.Section) < 0 || !idRe.MatchString(it.ID) || !s.store.itemExists(user, it.Section, it.ID) {
			return errNotFound
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

// --- PDF

// postPDF renders a selection. The editor shows it as the preview and
// reads the page count from X-Page-Count.
func (s *Server) postPDF(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req PrintOptions
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	pdf, err := s.renderPDF(r.Context(), user, req)
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Page-Count", strconv.Itoa(PageCount(pdf)))
	w.Write(pdf)
}

type fitRequest struct {
	PrintOptions
	Pages int `json:"pages"` // the page count to fit on
}

type fitResult struct {
	Spacing float64 `json:"spacing"`
	Pages   int     `json:"pages"` // pages at that spacing
	Fits    bool    `json:"fits"`  // false: even the tightest spacing needs more pages
}

// postFit finds the most generous spacing at which a selection still fits
// on the requested number of pages. More spacing never means fewer pages,
// so a binary search over the slider's steps needs about five renders.
func (s *Server) postFit(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req fitRequest
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	if req.Pages < 1 || req.Pages > 10 {
		httpError(w, badRequest{errors.New("pages must be between 1 and 10")})
		return
	}

	const step = 0.05
	steps := int(math.Round((MaxSpacing - MinSpacing) / step))
	spacing := func(i int) float64 { return math.Round((MinSpacing+float64(i)*step)*100) / 100 }
	pagesAt := map[int]int{}
	count := func(i int) (int, error) {
		if n, ok := pagesAt[i]; ok {
			return n, nil
		}
		opt := req.PrintOptions
		opt.Spacing = spacing(i)
		pdf, err := s.renderPDF(r.Context(), user, opt)
		if err != nil {
			return 0, err
		}
		pagesAt[i] = PageCount(pdf)
		return pagesAt[i], nil
	}

	best := -1 // largest step that fits
	for lo, hi := 0, steps; lo <= hi; {
		mid := (lo + hi) / 2
		n, err := count(mid)
		if err != nil {
			httpError(w, err)
			return
		}
		if n <= req.Pages {
			best, lo = mid, mid+1
		} else {
			hi = mid - 1
		}
	}
	res := fitResult{Fits: best >= 0}
	if !res.Fits {
		best = 0 // as tight as it gets
	}
	res.Spacing = spacing(best)
	n, err := count(best)
	if err != nil {
		httpError(w, err)
		return
	}
	res.Pages = n
	writeJSON(w, res)
}

// postLayout tells where each item of a selection lands in its PDF, so the
// preview can make items clickable.
func (s *Server) postLayout(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req PrintOptions
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	doc, photo, err := s.document(user, req)
	if err != nil {
		httpError(w, err)
		return
	}
	marks, err := s.typst.Layout(r.Context(), doc, photo)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, marks)
}

// --- versions

type versionRequest struct {
	Version
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
			if _, err := s.store.Version(user, v.ID); !errors.Is(err, errNotFound) {
				return badRequest{fmt.Errorf("version %q exists", v.ID)}
			}
		case req.From != "":
			from, err := s.store.Version(user, req.From)
			if err != nil {
				return err
			}
			v = from
			v.Name = cmp.Or(clean(req.Name), "Copy of "+from.Name)
		default:
			profile, err := s.store.Profile(user)
			if err != nil {
				return err
			}
			items, err := s.store.Items(user)
			if err != nil {
				return err
			}
			v.PrintOptions = PrintOptions{Lang: cmp.Or(v.Lang, profile.Langs[0]), Photo: profile.Photo, Spacing: 1, Order: profile.Order, Theme: profile.Theme.forNewVersion(), Entries: []string{}}
			for _, it := range items {
				v.Entries = append(v.Entries, it.Section+"/"+it.ID)
			}
		}
		if v.ID == "" {
			v.ID = s.store.NewVersionID(user, v.Name)
		}
		v.Created = cmp.Or(v.Created, now.In(linkZone).Format("2006-01-02"))
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
	var v Version
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
func (s *Server) storeVersion(user string, v Version) (shared bool, err error) {
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

func sameSettings(a, b Version) bool {
	a.Pages, b.Pages = 0, 0
	a.Order, b.Order = SectionOrder(a.Order), SectionOrder(b.Order)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// follow makes a link show a version.
func follow(l *Link, v Version) {
	l.Version, l.Label = v.ID, v.Name
	l.Lang, l.Entries, l.Photo, l.Spacing, l.Order, l.Theme = v.Lang, v.Entries, v.Photo, v.Spacing, v.Order, v.Theme
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

// putShare shares a version until a date, or changes that date. The link is
// published before the answer, so it works as soon as the editor shows it.
func (s *Server) putShare(r *http.Request, user string) error {
	var req struct {
		Expires string `json:"expires"`
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
		link = &Link{Created: time.Now().In(linkZone).Format("2006-01-02")}
		for link.Slug == "" || s.slugTaken(user, link.Slug) {
			link.Slug = NewSlug(v.Name)
		}
	}
	follow(link, v)
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
			return errNotFound
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
	return !errors.Is(err, errNotFound)
}

// saveLink writes the link and publishes it before answering.
func (s *Server) saveLink(ctx context.Context, user string, l Link) error {
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

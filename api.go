package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Server struct {
	store     *Store
	renderer  *Renderer
	auth      *Auth
	uiDir     string
	publicDir string
	publicURL string

	mu    sync.Mutex
	users map[string]*userState
	pubMu sync.Mutex // guards renames into, and deletions from, the webroot
}

type userState struct {
	content sync.Mutex // serialises content writes and builds

	mu                  sync.Mutex
	publishing, pending bool
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
	mux.HandleFunc("PUT /api/compose", s.putCompose)
	mux.HandleFunc("POST /api/links", s.handle(s.postLink))
	mux.HandleFunc("PUT /api/links/{slug}", s.handle(s.putLink))
	mux.HandleFunc("DELETE /api/links/{slug}", s.handle(s.deleteLink))
	mux.Handle("/", http.FileServer(http.Dir(s.uiDir)))
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
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-CV-App") != "1" {
			http.Error(w, "missing X-CV-App header", http.StatusForbidden)
			return
		}
		if err := s.store.Init(user); err != nil {
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
	User     string        `json:"user"`
	Langs    []string      `json:"langs"`
	Sections []string      `json:"sections"`
	Point    []string      `json:"pointSections"`
	Profile  Profile       `json:"profile"`
	Items    []Item        `json:"items"`
	Links    []linkView    `json:"links"`
	Today    string        `json:"today"`
	Users    []string      `json:"users"`   // all CVs, for the switcher
	Compose  *PrintOptions `json:"compose"` // the composer's last settings for this CV, if any
}

func (s *Server) editorState(ctx context.Context, user string) (editorState, error) {
	st := editorState{
		User: user, Users: s.auth.Names(), Langs: Langs, Sections: Sections, Point: PointSections,
		Today: time.Now().In(linkZone).Format("2006-01-02"),
	}
	var err error
	if st.Profile, err = s.store.Profile(user); err != nil {
		return st, err
	}
	if st.Compose, err = s.store.Compose(user); err != nil {
		return st, err
	}
	if st.Items, err = s.store.Items(user); err != nil {
		return st, err
	}
	links, err := s.store.Links(user)
	if err != nil {
		return st, err
	}
	now := time.Now()
	st.Links = []linkView{}
	for _, l := range links {
		st.Links = append(st.Links, linkView{l, s.publicURL + "/" + l.Slug + "/", l.Expired(now)})
	}
	if st.Items == nil {
		st.Items = []Item{}
	}
	return st, nil
}

func (s *Server) getState(r *http.Request, user string) error {
	if s.renderer.HasBuild(user) {
		return nil
	}
	return s.rebuild(r.Context(), user, func() error { return nil })
}

// rebuild applies a content change and re-renders the user's CV.
func (s *Server) rebuild(ctx context.Context, user string, change func() error) error {
	unlock := s.lock(user)
	defer unlock()
	if err := change(); err != nil {
		return err
	}
	return s.renderer.Build(ctx, user, s.store.ContentDir(user))
}

// edit is rebuild for CV content, which share links show too: their pages
// and PDFs are refreshed in the background.
func (s *Server) edit(ctx context.Context, user string, change func() error) error {
	if err := s.rebuild(ctx, user, change); err != nil {
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
	if err := p.Validate(); err != nil {
		return badRequest{err}
	}
	return s.edit(r.Context(), user, func() error { return s.store.SaveProfile(user, p) })
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
		it.ID = s.store.NewItemID(user, it)
		if err := it.Validate(); err != nil {
			return badRequest{err}
		}
		return s.store.SaveItem(user, it)
	})
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

// putCompose saves the composer's settings. It answers 204 rather than the
// full state: it is called on every change and nothing else depends on it.
func (s *Server) putCompose(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var o PrintOptions
	if err := readJSON(r, &o); err != nil {
		httpError(w, err)
		return
	}
	if err := o.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	unlock := s.lock(user)
	err := s.store.SaveCompose(user, o)
	unlock()
	if err != nil {
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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
	if !s.renderer.HasBuild(user) {
		if err := s.rebuild(r.Context(), user, func() error { return nil }); err != nil {
			httpError(w, err)
			return
		}
	}
	pdf, err := s.renderer.PDF(r.Context(), user, req)
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Page-Count", strconv.Itoa(PageCount(pdf)))
	w.Write(pdf)
}

// --- links

func (s *Server) postLink(r *http.Request, user string) error {
	var l Link
	if err := readJSON(r, &l); err != nil {
		return err
	}
	l.Created = time.Now().In(linkZone).Format("2006-01-02")
	for {
		l.Slug = NewSlug(l.Label)
		if _, err := os.Stat(filepath.Join(s.publicDir, l.Slug)); errors.Is(err, fs.ErrNotExist) {
			if _, err := s.store.Link(user, l.Slug); errors.Is(err, errNotFound) {
				break
			}
		}
	}
	return s.saveLink(r.Context(), user, l)
}

func (s *Server) putLink(r *http.Request, user string) error {
	cur, err := s.store.Link(user, r.PathValue("slug"))
	if err != nil {
		return err
	}
	var l Link
	if err := readJSON(r, &l); err != nil {
		return err
	}
	l.Slug, l.Created = cur.Slug, cur.Created
	return s.saveLink(r.Context(), user, l)
}

// saveLink writes the link and publishes it before answering, so the URL
// works as soon as the editor shows it.
func (s *Server) saveLink(ctx context.Context, user string, l Link) error {
	if err := l.Validate(); err != nil {
		return badRequest{err}
	}
	if l.Expired(time.Now()) {
		return badRequest{errors.New("expiry date must be in the future")}
	}
	if err := s.rebuild(ctx, user, func() error { return s.store.SaveLink(user, l) }); err != nil {
		return err
	}
	return s.publishLink(ctx, user, l)
}

func (s *Server) deleteLink(r *http.Request, user string) error {
	err := s.rebuild(r.Context(), user, func() error { return s.store.DeleteLink(user, r.PathValue("slug")) })
	if err != nil {
		return err
	}
	return s.reconcile()
}

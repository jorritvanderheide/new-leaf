// Package server is the editor: its API and pages, sign-in over the
// tailnet, share pages and publishing them, thumbnails, and New Leaf on
// your own computer. Serve and Local run it.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/render"
)

type Server struct {
	store     *cv.Store
	typst     *render.Typst
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
	mux.HandleFunc("GET /api/resume", s.getResume)
	mux.HandleFunc("POST /api/resume", s.postResume)
	mux.HandleFunc("GET /api/ping", s.getPing)
	if s.auth.CVs.Manage {
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
			if err := s.store.Migrate(user); err != nil {
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
	var inv cv.Invalid
	switch {
	case errors.As(err, &bad):
		http.Error(w, bad.Error(), http.StatusBadRequest)
	case errors.As(err, &inv):
		http.Error(w, inv.Error(), http.StatusBadRequest)
	case errors.Is(err, cv.ErrNotFound), errors.Is(err, fs.ErrNotExist):
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

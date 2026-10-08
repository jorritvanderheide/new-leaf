package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// getHealth tells monitoring whether New Leaf can do its work: write CVs,
// make PDFs and, on a server, publish share links. It needs no sign-in, so
// it says what is wrong and nothing else.
func (s *Server) getHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if problem := s.health(r.Context()); problem != "" {
		http.Error(w, problem, http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

func (s *Server) health(ctx context.Context) string {
	// A server without CVs yet has no users folder; making it is harmless.
	if os.MkdirAll(s.store.Root, 0o750) != nil || !writable(s.store.Root) {
		return "the data folder can't be written"
	}
	if s.sharing && !writable(s.publicDir) {
		return "the share links folder can't be written"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, s.typst.Bin, "--version").Run(); err != nil {
		return "typst doesn't run"
	}
	return ""
}

// writable reports whether a file can be made in dir, by making one.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".health-*")
	if err != nil {
		return false
	}
	f.Close()
	return os.Remove(f.Name()) == nil
}

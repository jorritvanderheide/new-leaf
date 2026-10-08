package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicHandler(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "uva-abc"), 0o755)
	os.WriteFile(filepath.Join(dir, "uva-abc", "index.html"), []byte("<p>cv</p>"), 0o644)
	os.WriteFile(filepath.Join(dir, ".new-leaf-public"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, ".stage-1"), 0o755)
	h := publicHandler(dir)
	for path, want := range map[string]int{
		"/uva-abc/":                    http.StatusOK,
		"/":                            http.StatusNotFound, // no listing
		"/.new-leaf-public":            http.StatusNotFound,
		"/.stage-1/":                   http.StatusNotFound,
		"/uva-abc/../.new-leaf-public": http.StatusNotFound,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s: %d, want %d", path, w.Code, want)
		}
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Robots-Tag") == "" {
			t.Errorf("%s: security headers missing", path)
		}
	}
}

// A share links folder from when New Leaf was called cv-app is taken over.
func TestClaimOldPublicDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".cv-app-public"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, "uva-abc"), 0o755)
	if err := claimPublicDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, publicMarker)); err != nil {
		t.Errorf("marker not renamed: %v", err)
	}
}

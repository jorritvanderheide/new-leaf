package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFlagsFromEnvironment(t *testing.T) {
	t.Setenv("NEW_LEAF_PUBLIC_URL", "https://cv.test")
	t.Setenv("NEW_LEAF_DATA", "/from/env")
	fl := flag.NewFlagSet("test", flag.ContinueOnError)
	publicURL := fl.String("public-url", "", "")
	data := fl.String("data", "", "")
	parseFlags(fl, []string{"-data", "/from/args"})
	if *publicURL != "https://cv.test" || *data != "/from/args" {
		t.Errorf("public-url %q, data %q: the environment fills in, the command line wins", *publicURL, *data)
	}
}

func TestFindTypst(t *testing.T) {
	if got := findTypst("/opt/typst"); got != "/opt/typst" {
		t.Errorf("an explicit binary is used as given: %q", got)
	}
	exe, _ := os.Executable()
	next := filepath.Join(filepath.Dir(exe), "typst")
	if err := os.WriteFile(next, nil, 0o755); err != nil {
		t.Skip(err)
	}
	defer os.Remove(next)
	if got := findTypst("typst"); got != next {
		t.Errorf("typst next to new-leaf: %q, want %q", got, next)
	}
}

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

// Data from when New Leaf was called cv-app moves along.
func TestMoveOldData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	old := platformDataDir("cv-app")
	os.MkdirAll(filepath.Join(old, "users", "me"), 0o755)
	dir := defaultDataDir()
	moveOldData(dir)
	if _, err := os.Stat(filepath.Join(dir, "users", "me")); err != nil {
		t.Errorf("not moved: %v", err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("old directory still there")
	}
	os.MkdirAll(filepath.Join(old, "users", "other"), 0o755)
	moveOldData(dir)
	if _, err := os.Stat(filepath.Join(dir, "users", "other")); err == nil {
		t.Error("moved over existing data")
	}
}

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

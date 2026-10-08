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
	t.Setenv("CV_APP_PUBLIC_URL", "https://cv.test")
	t.Setenv("CV_APP_DATA", "/from/env")
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
		t.Errorf("typst next to cv-app: %q, want %q", got, next)
	}
}

func TestPublicHandler(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "uva-abc"), 0o755)
	os.WriteFile(filepath.Join(dir, "uva-abc", "index.html"), []byte("<p>cv</p>"), 0o644)
	os.WriteFile(filepath.Join(dir, ".cv-app-public"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, ".stage-1"), 0o755)
	h := publicHandler(dir)
	for path, want := range map[string]int{
		"/uva-abc/":                  http.StatusOK,
		"/":                          http.StatusNotFound, // no listing
		"/.cv-app-public":            http.StatusNotFound,
		"/.stage-1/":                 http.StatusNotFound,
		"/uva-abc/../.cv-app-public": http.StatusNotFound,
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

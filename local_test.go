package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func newLocalServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	assets := NewAssets("")
	typst, err := NewTypst("typst", t.TempDir(), assets)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{Root: filepath.Join(t.TempDir(), "users")}
	s := &Server{
		store:  store,
		typst:  typst,
		assets: assets,
		auth:   &Auth{DevUser: "me", CVs: NewRegistry(store, nil, true)},
		local:  true,
	}
	var seen atomic.Int64
	return s, localOnly(&seen, s.routes())
}

func TestLocalOnlyAnswersLocalhost(t *testing.T) {
	_, h := newLocalServer(t)
	for host, want := range map[string]int{
		"127.0.0.1:8484":        http.StatusOK,
		"localhost:8484":        http.StatusOK,
		"[::1]:8484":            http.StatusOK,
		"evil.example.com:8484": http.StatusForbidden, // DNS rebinding
		"evil.example.com":      http.StatusForbidden,
	} {
		r := httptest.NewRequest("GET", "/api/ping", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %s: %d, want %d", host, w.Code, want)
		}
	}
}

func TestLocalHasNoSharing(t *testing.T) {
	_, h := newLocalServer(t)
	r := httptest.NewRequest("PUT", "/api/versions/full-cv/share", strings.NewReader(`{}`))
	r.Host = "localhost:8484"
	r.Header.Set("X-New-Leaf", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("sharing locally: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/links/", nil)
	r.Host = "localhost:8484"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /links/ locally: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "localhost:8484"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var st editorState
	json.Unmarshal(w.Body.Bytes(), &st)
	if st.Sharing || !st.Local || st.User != "me" {
		t.Errorf("state: sharing=%v local=%v user=%q", st.Sharing, st.Local, st.User)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	s, h := newLocalServer(t)
	s.store.Init("me")
	s.store.SaveProfile("me", Profile{Langs: []string{"en"}, Name: "Me", Text: map[string]ProfileText{"en": {Summary: "Hello"}}})
	s.store.SaveItem("me", Item{Section: "experience", ID: "job", Start: "2020-01", Text: map[string]ItemText{"en": {Title: "Job"}}})
	os.WriteFile(filepath.Join(s.store.dir("me"), "photo.jpg"), []byte("jpeg"), 0o600)

	w := do(h, "GET", "/api/export", nil)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	backup := w.Body.Bytes()

	// Change the CV, then restore the backup.
	s.store.DeleteItem("me", "experience", "job")
	if w := do(h, "POST", "/api/import", uploadOf(t, backup)); w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	items, _ := s.store.Items("me")
	if len(items) != 1 || items[0].Text["en"].Title != "Job" {
		t.Errorf("restored items = %+v", items)
	}
	if s.store.PhotoPath("me") == "" {
		t.Error("photo not restored")
	}
	kept, _ := filepath.Glob(filepath.Join(s.store.Root, "me", "backups", "before-import-*.zip"))
	if len(kept) != 1 {
		t.Errorf("the replaced CV was not kept: %v", kept)
	}
}

func TestImportRejects(t *testing.T) {
	_, h := newLocalServer(t)
	profile := "---\nname: X\n---\n"
	for name, files := range map[string]map[string]string{
		"traversal":     {"content/_index.en.md": profile, "content/../../evil.md": "x"},
		"unknown file":  {"content/_index.en.md": profile, "content/run.sh": "x"},
		"no profile":    {"content/experience/job.en.md": "---\ntitle: x\nstart: \"2020\"\n---\n"},
		"bad section":   {"content/_index.en.md": profile, "content/hobbies/x.en.md": profile},
		"broken yaml":   {"content/_index.en.md": "---\nname: [\n---\n"},
		"absolute path": {"content/_index.en.md": profile, "/etc/passwd": "x"},
	} {
		if w := do(h, "POST", "/api/import", uploadOf(t, zipOf(t, files))); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	// Share links in a server backup are skipped locally, not refused.
	ok := zipOf(t, map[string]string{"content/_index.en.md": profile, "content/links/x-abcdefgh.en.md": "---\ntitle: x\n---\n"})
	if w := do(h, "POST", "/api/import", uploadOf(t, ok)); w.Code != http.StatusOK {
		t.Errorf("server backup locally: %d %s", w.Code, w.Body)
	}
}

// upload is a multipart form with a backup zip, as the Profile page sends it.
type upload struct {
	body        *bytes.Buffer
	contentType string
}

func uploadOf(t *testing.T, data []byte) *upload {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	f, _ := mw.CreateFormFile("backup", "cv.zip")
	f.Write(data)
	mw.Close()
	return &upload{&b, mw.FormDataContentType()}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	zw.Close()
	return b.Bytes()
}

func do(h http.Handler, method, path string, u *upload) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if u != nil {
		r = httptest.NewRequest(method, path, u.body)
		r.Header.Set("Content-Type", u.contentType)
	}
	r.Host = "localhost:8484"
	r.Header.Set("X-New-Leaf", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestEmptyCVMakesPDF(t *testing.T) {
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst not in PATH")
	}
	s, h := newLocalServer(t)
	s.store.Init("me")
	r := httptest.NewRequest("POST", "/api/pdf", strings.NewReader(`{"lang":"en","entries":[]}`))
	r.Host = "localhost:8484"
	r.Header.Set("X-New-Leaf", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Page-Count") != "1" {
		t.Errorf("empty CV: %d %s", w.Code, w.Body.String()[:min(300, w.Body.Len())])
	}
}

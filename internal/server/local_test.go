package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/render"
)

func newLocalServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	assets := NewAssets("")
	typst, err := render.NewTypst("typst", t.TempDir(), assets.fs)
	if err != nil {
		t.Fatal(err)
	}
	store := &cv.Store{Root: filepath.Join(t.TempDir(), "users")}
	s := &Server{
		store:  store,
		typst:  typst,
		assets: assets,
		auth:   &Auth{DevUser: "me", CVs: cv.NewRegistry(store, nil, true)},
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
		r.RemoteAddr = "127.0.0.1:50000"
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %s: %d, want %d", host, w.Code, want)
		}
	}

	// Someone else on the network, with -listen on an address they reach.
	r := httptest.NewRequest("GET", "/api/ping", nil)
	r.RemoteAddr = "192.168.1.20:50000"
	r.Host = "localhost:8484"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("from the network: %d", w.Code)
	}
}

func TestLocalHasNoSharing(t *testing.T) {
	_, h := newLocalServer(t)
	r := httptest.NewRequest("PUT", "/api/versions/full-cv/share", strings.NewReader(`{}`))
	r.Host = "localhost:8484"
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-New-Leaf", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("sharing locally: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/links/", nil)
	r.Host = "localhost:8484"
	r.RemoteAddr = "127.0.0.1:50000"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /links/ locally: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "localhost:8484"
	r.RemoteAddr = "127.0.0.1:50000"
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
	s.store.SaveProfile("me", cv.Profile{Langs: []string{"en"}, Name: "Me", Text: map[string]cv.ProfileText{"en": {Summary: "Hello"}}})
	s.store.SaveItem("me", cv.Item{Section: "experience", ID: "job", Start: "2020-01", Text: map[string]cv.ItemText{"en": {Title: "Job"}}})
	var photo bytes.Buffer
	png.Encode(&photo, image.NewGray(image.Rect(0, 0, 4, 4)))
	os.WriteFile(filepath.Join(s.store.ContentDir("me"), "photo.png"), photo.Bytes(), 0o600)

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
		"traversal":                          {"content/_index.en.md": profile, "content/../../evil.md": "x"},
		"unknown file":                       {"content/_index.en.md": profile, "content/run.sh": "x"},
		"no profile":                         {"content/experience/job.en.md": "---\ntitle: x\nstart: \"2020\"\n---\n"},
		"bad section":                        {"content/_index.en.md": profile, "content/hobbies/x.en.md": profile},
		"broken yaml":                        {"content/_index.en.md": "---\nname: [\n---\n"},
		"compose in a language the CV lacks": {"content/_index.en.md": profile, "compose.json": `{"lang": "de", "entries": []}`},
		"broken version":                     {"content/_index.en.md": profile, "versions/x.json": "{"},
		"version without a name":             {"content/_index.en.md": profile, "versions/x.json": `{"name": "", "lang": "en", "entries": []}`},
		"absolute path":                      {"content/_index.en.md": profile, "/etc/passwd": "x"},
		"newer format":                       {"content/_index.md": "---\nformat: 99\n---\n", "content/_index.en.md": profile},
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

// A backup from before format 1 restores, and is migrated as it lands.
func TestRestoreOldBackup(t *testing.T) {
	s, h := newLocalServer(t)
	old := zipOf(t, map[string]string{"content/_index.en.md": "---\nname: Old Example\nlanguages: [en]\nheadline: Designer\n---\nHello.\n"})
	if w := do(h, "POST", "/api/import", uploadOf(t, old)); w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(s.store.ContentDir("me"), "_index.md")); err != nil {
		t.Errorf("not migrated: %v", err)
	}
	if p, _ := s.store.Profile("me"); p.Name != "Old Example" || p.Text["en"].Headline != "Designer" || p.Text["en"].Summary != "Hello." {
		t.Errorf("profile = %+v", p)
	}
}

// upload is a multipart form with a backup zip, as the Profile page sends it.
type upload struct {
	body        *bytes.Buffer
	contentType string
}

func uploadOf(t *testing.T, data []byte) *upload {
	t.Helper()
	return formOf(t, "backup", data)
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
	r.RemoteAddr = "127.0.0.1:50000"
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
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-New-Leaf", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Page-Count") != "1" {
		t.Errorf("empty CV: %d %s", w.Code, w.Body.String()[:min(300, w.Body.Len())])
	}
}

// A CV goes out as a JSON Resume and comes back in, replacing the CV.
func TestResumeExportImport(t *testing.T) {
	s, h := newLocalServer(t)
	s.store.Init("me")
	s.store.SaveProfile("me", cv.Profile{Langs: []string{"en"}, Name: "Me Example", Text: map[string]cv.ProfileText{"en": {Headline: "Designer"}}})
	s.store.SaveItem("me", cv.Item{Section: "experience", ID: "job", Start: "2020-01", Text: map[string]cv.ItemText{"en": {Title: "Job", Org: "Acme"}}})

	w := do(h, "GET", "/api/resume", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "resume-me-en-") {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	var r cv.Resume
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || r.Basics.Name != "Me Example" || len(r.Work) != 1 || r.Work[0].Name != "Acme" {
		t.Fatalf("resume = %+v, %v", r, err)
	}
	if w := do(h, "GET", "/api/resume?lang=nl", nil); w.Code != http.StatusBadRequest {
		t.Errorf("a language the CV doesn't have: %d", w.Code)
	}

	r.Basics.Name = "Me Again"
	r.Skills = []json.RawMessage{[]byte(`{"name": "Go"}`)}
	data, _ := json.Marshal(r)
	w = do(h, "POST", "/api/resume", formOf(t, "resume", data))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "skills (1)") {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	if p, _ := s.store.Profile("me"); p.Name != "Me Again" {
		t.Errorf("profile = %+v", p)
	}
	if items, _ := s.store.Items("me"); len(items) != 1 || items[0].Text["en"].Org != "Acme" {
		t.Errorf("items = %+v", items)
	}

	if w := do(h, "POST", "/api/resume", formOf(t, "resume", []byte("not json"))); w.Code != http.StatusBadRequest {
		t.Errorf("not JSON: %d %s", w.Code, w.Body)
	}
}

func formOf(t *testing.T, field string, data []byte) *upload {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	f, _ := mw.CreateFormFile(field, "file")
	f.Write(data)
	mw.Close()
	return &upload{&b, mw.FormDataContentType()}
}

// A restored share link keeps its address only if it's free: one another CV
// shares, or the webroot's own css, gets a new one.
func TestRestoredLinksDontTakeOver(t *testing.T) {
	store := &cv.Store{Root: filepath.Join(t.TempDir(), "users")}
	s := &Server{store: store, sharing: true, publicDir: t.TempDir()}
	store.Init("bob")
	store.SaveItem("bob", cv.Item{Section: "experience", ID: "job", Start: "2020", Text: map[string]cv.ItemText{"en": {Title: "Job"}}})
	if err := store.SaveLink("bob", cv.Link{Slug: "job-abcdefgh", Label: "Job", Lang: "en", Entries: []string{"experience/job"}, Created: "2026-01-01"}); err != nil {
		t.Fatal(err)
	}
	link := func(slug string) string {
		return "---\ntitle: Job\nurl: /" + slug + "/\nentries: [experience/job]\ncreated: \"2026-01-01\"\n---\n"
	}
	backup := zipOf(t, map[string]string{
		"content/_index.en.md":               "---\nname: Alice\n---\n",
		"content/experience/job.en.md":       "---\ntitle: Job\nstart: \"2020\"\n---\n",
		"content/links/job-abcdefgh.en.md":   link("job-abcdefgh"), // bob's
		"content/links/css.en.md":            link("css"),
		"content/links/alice-mine1234.en.md": link("alice-mine1234"),
	})
	stage, err := store.StageBackup(backup, true)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if err := s.claimLinks("alice", stage); err != nil {
		t.Fatal(err)
	}
	staged := &cv.Store{Root: filepath.Dir(stage)}
	links, _ := staged.Links(filepath.Base(stage))
	var slugs []string
	for _, l := range links {
		slugs = append(slugs, l.Slug)
	}
	if len(slugs) != 3 || slices.Contains(slugs, "job-abcdefgh") || slices.Contains(slugs, "css") || !slices.Contains(slugs, "alice-mine1234") {
		t.Errorf("restored links: %v", slugs)
	}

	bad := zipOf(t, map[string]string{"content/_index.en.md": "---\nname: Alice\n---\n", "content/links/x-abcdefgh.en.md": "---\ntitle: X\nurl: /x-abcdefgh/\n---\n"})
	if stage, err := store.StageBackup(bad, true); err == nil {
		os.RemoveAll(stage)
		t.Error("a link without items was restored")
	}
}

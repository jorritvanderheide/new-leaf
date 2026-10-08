package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEndToEnd drives the API with real Typst. It checks the
// guarantees that matter: unselected items never reach the webroot, PDFs
// report their page count, edits propagate to share links, and expired or
// deleted links disappear.
func TestEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst not in PATH")
	}

	data := t.TempDir()
	public := filepath.Join(t.TempDir(), "public")
	if err := claimPublicDir(public); err != nil {
		t.Fatal(err)
	}
	assets := NewAssets("")
	typst, err := NewTypst("typst", filepath.Join(data, "work"), assets)
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{Root: filepath.Join(data, "users")}
	s := &Server{
		store:     store,
		typst:     typst,
		assets:    assets,
		auth:      &Auth{DevUser: "alice", CVs: NewRegistry(store, []string{"alice"}, true)},
		sharing:   true,
		publicDir: public,
		publicURL: "https://cv.test",
	}
	h := s.routes()

	call := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("X-CV-App", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w
	}

	call("PUT", "/api/profile", Profile{Name: "Alice Example", Email: "alice@example.com"})
	call("POST", "/api/items", Item{Section: "experience", Start: "2020-01", Text: map[string]ItemText{
		"en": {Title: "Public role", Org: "Visible Org", Body: "Shared detail."},
		"nl": {Title: "Publieke rol", Org: "Visible Org"},
	}})
	call("POST", "/api/items", Item{Section: "publications", Start: "2021-03", Link: "https://doi.org/10.1000/xyz", Text: map[string]ItemText{
		"en": {Title: "On Testing", Org: "Journal of Tests"},
	}})
	call("POST", "/api/items", Item{Section: "publications", Start: "", Link: "https://doi.org/10.1/ref", Text: map[string]ItemText{
		"en": {Title: "Short name", Org: "Refs", Body: "Doe, J. (2024). A long title. *Journal of Refs*, *1*(2), 3-4. https://doi.org/10.1/ref"},
	}})
	call("POST", "/api/items", Item{Section: "publications", Start: "", Text: map[string]ItemText{
		"en": {Title: "Pending", Org: "Pending", Body: "Roe, R. A pending paper. *Journal X*. Manuscript under review."},
	}})
	call("POST", "/api/items", Item{Section: "experience", Start: "2018-01", End: "2019-12", Text: map[string]ItemText{
		"en": {Title: "Secret role", Org: "Hidden Org", Body: "Confidential detail."},
	}})

	// Preview PDF with page count.
	w := call("POST", "/api/pdf", PrintOptions{Lang: "en", Entries: []string{"experience/visible-org"}})
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF")) || w.Header().Get("X-Page-Count") != "1" {
		t.Fatalf("pdf: pages=%q, starts %q", w.Header().Get("X-Page-Count"), w.Body.Bytes()[:min(8, w.Body.Len())])
	}

	// Spacing changes the layout, never makes it longer when tighter, and
	// must stay within range.
	tight := call("POST", "/api/pdf", PrintOptions{Lang: "en", Entries: []string{"experience/visible-org", "experience/hidden-org"}, Spacing: 0.4})
	airy := call("POST", "/api/pdf", PrintOptions{Lang: "en", Entries: []string{"experience/visible-org", "experience/hidden-org"}, Spacing: 1.4})
	if bytes.Equal(tight.Body.Bytes(), airy.Body.Bytes()) {
		t.Error("spacing did not change the PDF")
	}
	if tight.Header().Get("X-Page-Count") > airy.Header().Get("X-Page-Count") {
		t.Error("tighter spacing produced more pages")
	}
	r0 := httptest.NewRequest("POST", "/api/pdf", strings.NewReader(`{"lang":"en","entries":[],"spacing":3}`))
	r0.Header.Set("X-CV-App", "1")
	w0 := httptest.NewRecorder()
	h.ServeHTTP(w0, r0)
	if w0.Code != http.StatusBadRequest {
		t.Errorf("out-of-range spacing: %d", w0.Code)
	}

	// Composer settings are kept on the server, per CV.
	putCompose := func(body string) int {
		r := httptest.NewRequest("PUT", "/api/compose", strings.NewReader(body))
		r.Header.Set("X-CV-App", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := putCompose(`{"lang":"nl","entries":["experience/visible-org"],"photo":true,"spacing":0.75}`); code != http.StatusNoContent {
		t.Fatalf("PUT /api/compose: %d", code)
	}
	var withCompose editorState
	json.Unmarshal(call("GET", "/api/state", nil).Body.Bytes(), &withCompose)
	if c := withCompose.Compose; c == nil || c.Lang != "nl" || c.Spacing != 0.75 || len(c.Entries) != 1 {
		t.Errorf("compose in state = %+v", c)
	}
	if code := putCompose(`{"lang":"de"}`); code != http.StatusBadRequest {
		t.Errorf("invalid compose settings: %d", code)
	}

	// Share link.
	expires := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	var st editorState
	json.Unmarshal(call("POST", "/api/links", Link{Label: "Test", Lang: "en", Entries: []string{"experience/visible-org", "publications/journal-of-tests", "publications/refs", "publications/pending"}, Spacing: 0.8, Expires: expires}).Body.Bytes(), &st)
	if len(st.Links) != 1 || !strings.HasPrefix(st.Links[0].Slug, "test-") || st.Links[0].URL != "https://cv.test/"+st.Links[0].Slug+"/" {
		t.Fatalf("links = %+v", st.Links)
	}
	slug := st.Links[0].Slug
	if st.Links[0].Spacing != 0.8 {
		t.Errorf("link spacing = %v", st.Links[0].Spacing)
	}
	page := readFile(t, filepath.Join(public, slug, "index.html"))

	// Every language is published; the toggle links them both ways.
	nlPage := readFile(t, filepath.Join(public, slug, "nl", "index.html"))
	if !strings.Contains(nlPage, "Publieke rol") || !strings.Contains(nlPage, `lang="nl"`) {
		t.Error("Dutch share page lacks its Dutch content")
	}
	if !strings.Contains(page, `href="nl/"`) || !strings.Contains(nlPage, `href="../"`) {
		re := regexp.MustCompile(`href="[^"]*" hreflang="[^"]*"`)
		t.Errorf("language toggle does not link the pages to each other: %q / %q", re.FindAllString(page, -1), re.FindAllString(nlPage, -1))
	}
	for _, leak := range []string{"Hidden Org", "Confidential"} {
		if strings.Contains(nlPage, leak) {
			t.Errorf("Dutch share page leaks unselected %q", leak)
		}
	}
	if pdf := readFile(t, filepath.Join(public, slug, "nl", "cv.pdf")); PageCount([]byte(pdf)) < 1 {
		t.Error("Dutch PDF missing")
	}
	for _, want := range []string{"Visible Org", "Shared detail.", "Alice Example", `href="https://doi.org/10.1000/xyz"`, ">Mar 2021<"} {
		if !strings.Contains(page, want) {
			t.Errorf("share page lacks %q", want)
		}
	}
	// A publication with a reference is shown as that reference.
	for _, want := range []string{"<em>Journal of Refs</em>", `<a href="https://doi.org/10.1/ref"`} {
		if !strings.Contains(page, want) {
			t.Errorf("share page lacks %q", want)
		}
	}
	if pub, pending := strings.Index(page, "A long title"), strings.Index(page, "A pending paper"); pub < 0 || pending < pub {
		t.Errorf("undated publication should come after published ones (%d, %d)", pub, pending)
	}
	if strings.Contains(page, "Short name") {
		t.Error("reference-style publication still shows its short title")
	}
	for _, leak := range []string{"Hidden Org", "Secret role", "Confidential"} {
		if strings.Contains(page, leak) {
			t.Errorf("share page leaks unselected %q", leak)
		}
	}
	if pdf := readFile(t, filepath.Join(public, slug, "cv.pdf")); PageCount([]byte(pdf)) != 1 {
		t.Errorf("share PDF pages = %d", PageCount([]byte(pdf)))
	}
	css := regexp.MustCompile(`href="?([^" >]+\.css)`).FindStringSubmatch(page)
	if css == nil {
		t.Fatal("share page has no stylesheet")
	}
	if _, err := os.Stat(filepath.Join(public, slug, css[1])); err != nil {
		t.Errorf("stylesheet %s not published: %v", css[1], err)
	}

	// Editing a shared item republishes the link in the background.
	call("PUT", "/api/items/experience/visible-org", Item{Start: "2020-01", Text: map[string]ItemText{
		"en": {Title: "Renamed role", Org: "Visible Org"},
	}})
	deadline := time.Now().Add(60 * time.Second)
	for !strings.Contains(readFile(t, filepath.Join(public, slug, "index.html")), "Renamed role") {
		if time.Now().After(deadline) {
			t.Fatal("share page was not republished after an edit")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Writes without the anti-CSRF header are refused.
	r := httptest.NewRequest("DELETE", "/api/links/"+slug, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("write without X-CV-App: %d", rec.Code)
	}

	// Expiry: reconcile unpublishes expired links and anything unknown,
	// and keeps the shared assets.
	expired := linkFile{Title: "Old", URL: "/old-aaaaaaaa/", Entries: []string{"experience/visible-org"}, ExpiryDate: "2020-01-01", Created: "2019-12-01"}
	writeMarkdown(filepath.Join(s.store.ContentDir("alice"), "links", "old-aaaaaaaa.en.md"), expired, "")
	os.MkdirAll(filepath.Join(public, "old-aaaaaaaa"), 0o755)
	os.MkdirAll(filepath.Join(public, "stray"), 0o755)
	if err := s.reconcile(); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"old-aaaaaaaa", "stray"} {
		if _, err := os.Stat(filepath.Join(public, gone)); err == nil {
			t.Errorf("%s still published", gone)
		}
	}
	for _, kept := range []string{slug, "css", "fonts", publicMarker} {
		if _, err := os.Stat(filepath.Join(public, kept)); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}

	// Deleting a link unpublishes it immediately.
	call("DELETE", "/api/links/"+slug, nil)
	if _, err := os.Stat(filepath.Join(public, slug)); err == nil {
		t.Error("deleted link still published")
	}

	// Undo: a deleted link comes back at its old URL, an item under its old ID.
	restored := Link{Slug: slug, Label: "Test", Lang: "en", Entries: []string{"experience/visible-org"}, Expires: expires, Created: "2026-01-02"}
	json.Unmarshal(call("POST", "/api/links", restored).Body.Bytes(), &st)
	if i := slices.IndexFunc(st.Links, func(l linkView) bool { return l.Slug == slug }); i < 0 || st.Links[i].Created != "2026-01-02" {
		t.Errorf("restored links = %+v", st.Links)
	}
	if _, err := os.Stat(filepath.Join(public, slug, "index.html")); err != nil {
		t.Errorf("restored link not published: %v", err)
	}
	if code := post(h, "/api/links", restored); code != http.StatusBadRequest {
		t.Errorf("restoring onto a taken slug: %d", code)
	}
	call("DELETE", "/api/items/experience/visible-org", nil)
	call("POST", "/api/items", Item{Section: "experience", ID: "visible-org", Start: "2020-01", Text: map[string]ItemText{"en": {Title: "Back"}}})
	if code := post(h, "/api/items", Item{Section: "experience", ID: "visible-org", Start: "2020-01"}); code != http.StatusBadRequest {
		t.Errorf("restoring onto a taken item id: %d", code)
	}

	// Fit to pages: a short CV fits on one page at the most generous spacing.
	var fit fitResult
	json.Unmarshal(call("POST", "/api/fit", fitRequest{PrintOptions: PrintOptions{Lang: "en", Entries: []string{"experience/visible-org"}}, Pages: 1}).Body.Bytes(), &fit)
	if !fit.Fits || fit.Pages != 1 || fit.Spacing != MaxSpacing {
		t.Errorf("fit = %+v", fit)
	}
}

func post(h http.Handler, path string, body any) int {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, bytes.NewReader(b))
	r.Header.Set("X-CV-App", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestClaimPublicDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "precious.txt"), nil, 0o644)
	if err := claimPublicDir(dir); err == nil {
		t.Error("claimed a non-empty directory without marker")
	}
	empty := filepath.Join(t.TempDir(), "public")
	if err := claimPublicDir(empty); err != nil {
		t.Fatal(err)
	}
	if err := claimPublicDir(empty); err != nil {
		t.Errorf("reclaiming own directory: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

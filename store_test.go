package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := &Store{Root: t.TempDir()}
	if err := s.Init("alice"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestItemRoundTrip(t *testing.T) {
	s := newTestStore(t)
	in := Item{
		Section: "experience", ID: "acme", Start: "2023-09",
		Text: map[string]ItemText{
			"en": {Title: "Engineer", Org: "Acme", Body: "Built *things*.\n\n- one\n- two"},
			"nl": {Title: "Ingenieur", Org: "Acme"},
		},
	}
	if err := s.SaveItem("alice", in); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(s.dir("alice"), "experience", "acme.en.md"))
	if !strings.Contains(string(raw), `start: "2023-09"`) {
		t.Errorf("start must stay a quoted string:\n%s", raw)
	}
	if strings.Contains(string(raw), "end:") {
		t.Errorf("ongoing item should omit end:\n%s", raw)
	}

	items, err := s.Items("alice")
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %v, %v", items, err)
	}
	got := items[0]
	if got.Start != "2023-09" || got.End != "" || got.Text["en"] != in.Text["en"] || got.Text["nl"].Title != "Ingenieur" {
		t.Errorf("round trip mismatch: %+v", got)
	}

	if err := s.DeleteItem("alice", "experience", "acme"); err != nil {
		t.Fatal(err)
	}
	if items, _ := s.Items("alice"); len(items) != 0 {
		t.Errorf("item not deleted: %v", items)
	}
}

func TestItemValidation(t *testing.T) {
	s := newTestStore(t)
	bad := []Item{
		{Section: "hobbies", ID: "x", Start: "2020-01"},
		{Section: "education", ID: "../x", Start: "2020-01"},
		{Section: "education", ID: "x", Start: "2020-13"},
		{Section: "education", ID: "x", Start: ""}, // only point sections may omit the date
		{Section: "education", ID: "x", Start: "2020-05", End: "2020-01"},
	}
	for _, it := range bad {
		if err := s.SaveItem("alice", it); err == nil {
			t.Errorf("expected %+v to be rejected", it)
		}
	}
}

func TestNewItemID(t *testing.T) {
	s := newTestStore(t)
	it := Item{Section: "education", Start: "2020-01", Text: map[string]ItemText{"en": {Title: "MSc", Org: "Universität Zürich & Co"}}}
	it.ID = s.NewItemID("alice", it)
	if it.ID != "universitat-zurich-and-co" {
		t.Errorf("id = %q", it.ID)
	}
	if err := s.SaveItem("alice", it); err != nil {
		t.Fatal(err)
	}
	if id := s.NewItemID("alice", it); id != "universitat-zurich-and-co-2" {
		t.Errorf("duplicate id = %q", id)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	s := newTestStore(t)
	in := Profile{
		Name: "Alice  Example", Email: "a@example.com",
		Text: map[string]ProfileText{"en": {Headline: "Designer", Summary: "Hello."}, "nl": {Headline: "Ontwerper"}},
	}
	if err := s.SaveProfile("alice", in); err != nil {
		t.Fatal(err)
	}
	got, err := s.Profile("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Alice Example" || got.Email != "a@example.com" || got.Text["en"].Summary != "Hello." || got.Text["nl"].Headline != "Ontwerper" || got.Photo {
		t.Errorf("profile = %+v", got)
	}
}

func TestReadsHandWrittenFiles(t *testing.T) {
	s := newTestStore(t)
	dir := filepath.Join(s.dir("alice"), "volunteering")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "cafe.en.md"), []byte("---\r\ntitle: Host\r\nstart: 2017-05\r\n---"), 0o644)
	items, err := s.Items("alice")
	if err != nil || len(items) != 1 || items[0].Start != "2017-05" || items[0].Text["en"].Title != "Host" {
		t.Fatalf("items = %+v, %v", items, err)
	}
}

func TestLinks(t *testing.T) {
	s := newTestStore(t)
	l := Link{Slug: NewSlug("UvA application"), Label: "UvA", Lang: "nl", Entries: []string{"experience/acme"}, Expires: "2030-01-01", Created: "2026-10-07"}
	if err := s.SaveLink("alice", l); err != nil {
		t.Fatal(err)
	}
	got, err := s.Link("alice", l.Slug)
	if err != nil || got.Lang != "nl" || got.Expires != "2030-01-01" || len(got.Entries) != 1 {
		t.Fatalf("link = %+v, %v", got, err)
	}

	// Switching language replaces the file rather than adding a second one.
	l.Lang = "en"
	if err := s.SaveLink("alice", l); err != nil {
		t.Fatal(err)
	}
	if links, _ := s.Links("alice"); len(links) != 1 || links[0].Lang != "en" {
		t.Errorf("links after language change = %+v", links)
	}

	for _, bad := range []Link{
		{Slug: l.Slug, Lang: "de", Entries: l.Entries, Expires: "2030-01-01"},
		{Slug: l.Slug, Lang: "en", Entries: nil, Expires: "2030-01-01"},
		{Slug: l.Slug, Lang: "en", Entries: []string{"../../etc"}, Expires: "2030-01-01"},
		{Slug: "Bad Slug", Lang: "en", Entries: l.Entries, Expires: "2030-01-01"},
	} {
		if err := s.SaveLink("alice", bad); err == nil {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}

	if err := s.DeleteLink("alice", l.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link("alice", l.Slug); err != errNotFound {
		t.Errorf("link not deleted: %v", err)
	}
}

func TestLinkExpiry(t *testing.T) {
	l := Link{Expires: "2026-12-01"}
	before := time.Date(2026, 11, 30, 23, 59, 0, 0, linkZone)
	at := time.Date(2026, 12, 1, 0, 0, 0, 0, linkZone)
	if l.Expired(before) || !l.Expired(at) {
		t.Errorf("link should expire at the start of its expiry day in Amsterdam")
	}
	if !(Link{Expires: "garbage"}).Expired(before) {
		t.Errorf("unparsable expiry must count as expired")
	}
}

func TestNewSlug(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		slug := NewSlug("UvA – PhD Application 2026!")
		if !strings.HasPrefix(slug, "uva-phd-application-2026-") || !idRe.MatchString(slug) {
			t.Fatalf("slug %q", slug)
		}
		if seen[slug] {
			t.Fatalf("duplicate slug %q", slug)
		}
		seen[slug] = true
	}
	if slug := NewSlug(""); len(slug) != 8 || !idRe.MatchString(slug) {
		t.Errorf("slug without label = %q", slug)
	}
}

func TestSplitLangFile(t *testing.T) {
	for name, want := range map[string][2]string{
		"alltrons.en.md":      {"alltrons", "en"},
		"tue-msc.nl.md":       {"tue-msc", "nl"},
		"alltrons.md":         {},
		"alltrons.de.md":      {},
		".tmp-123":            {},
		"../evil.en.md":       {},
		"Upper.en.md":         {},
		"a.b.en.md":           {},
		"photo.jpg":           {},
		"x.en.md.backup.md":   {},
		"spaces here.en.md":   {},
		"trailing-.en.md":     {"trailing-", "en"},
		"-leading-dash.en.md": {},
	} {
		id, lang, ok := splitLangFile(name)
		if ok != (want[0] != "") || id != want[0] && ok || lang != want[1] && ok {
			t.Errorf("splitLangFile(%q) = %q, %q, %v", name, id, lang, ok)
		}
	}
}

func TestPointSectionsAndLinks(t *testing.T) {
	s := newTestStore(t)
	pub := Item{Section: "publications", ID: "paper", Start: "2024-03", Link: " https://doi.org/10.1000/x ",
		Text: map[string]ItemText{"en": {Title: "A paper", Org: "Journal"}}}
	if err := s.SaveItem("alice", pub); err != nil {
		t.Fatal(err)
	}
	items, _ := s.Items("alice")
	if len(items) != 1 || items[0].Link != "https://doi.org/10.1000/x" {
		t.Fatalf("items = %+v", items)
	}

	withEnd := pub
	withEnd.End = "2024-05"
	if err := s.SaveItem("alice", withEnd); err == nil {
		t.Error("publication with an end date was accepted")
	}
	badLink := pub
	badLink.Link = "javascript:alert(1)"
	if err := s.SaveItem("alice", badLink); err == nil {
		t.Error("non-http link was accepted")
	}
}

func TestSectionOrder(t *testing.T) {
	got := SectionOrder([]string{"education", "bogus", "education", "volunteering"})
	if len(got) != len(Sections) || got[0] != "education" || got[1] != "volunteering" || got[2] != "experience" {
		t.Errorf("SectionOrder = %v", got)
	}

	s := newTestStore(t)
	if err := s.SaveProfile("alice", Profile{Name: "Alice", Order: []string{"publications"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Profile("alice")
	if p.Order[0] != "publications" || len(p.Order) != len(Sections) {
		t.Errorf("profile order = %v", p.Order)
	}
}

func TestFlexibleDates(t *testing.T) {
	s := newTestStore(t)
	ok := []Item{
		{Section: "teaching", ID: "coach", Start: "2024"},              // year only, ongoing
		{Section: "teaching", ID: "ta", Start: "2019-08", End: "2020"}, // mixed precision
		{Section: "publications", ID: "under-review", Start: ""},       // undated point item
		{Section: "output", ID: "whitepaper", Start: "2025"},           // year-only point item
	}
	for _, it := range ok {
		it.Text = map[string]ItemText{"en": {Title: "x"}}
		if err := s.SaveItem("alice", it); err != nil {
			t.Errorf("%+v rejected: %v", it, err)
		}
	}
}

func TestProfileLinks(t *testing.T) {
	s := newTestStore(t)
	in := Profile{Name: "Alice", Links: []ProfileLink{
		{Label: " Google  Scholar ", URL: "https://scholar.google.com/citations?user=x"},
		{Label: "empty", URL: " "}, // dropped
	}}
	if err := s.SaveProfile("alice", in); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Profile("alice")
	if len(p.Links) != 1 || p.Links[0].Label != "Google Scholar" {
		t.Errorf("links = %+v", p.Links)
	}
	if err := s.SaveProfile("alice", Profile{Links: []ProfileLink{{Label: "x", URL: "javascript:alert(1)"}}}); err == nil {
		t.Error("non-http profile link accepted")
	}
}

func TestProfileLocationPerLanguage(t *testing.T) {
	s := newTestStore(t)
	in := Profile{Name: "Alice", Text: map[string]ProfileText{
		"en": {Location: "Eindhoven, the Netherlands"},
		"nl": {Location: "Eindhoven, Nederland"},
	}}
	if err := s.SaveProfile("alice", in); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Profile("alice")
	if p.Text["en"].Location != "Eindhoven, the Netherlands" || p.Text["nl"].Location != "Eindhoven, Nederland" {
		t.Errorf("locations = %q, %q", p.Text["en"].Location, p.Text["nl"].Location)
	}
}

func TestCompose(t *testing.T) {
	s := newTestStore(t)
	if o, err := s.Compose("alice"); o != nil || err != nil {
		t.Fatalf("no settings yet: %v, %v", o, err)
	}
	in := PrintOptions{Lang: "nl", Entries: []string{"experience/acme"}, Photo: true, Spacing: 0.7}
	if err := s.SaveCompose("alice", in); err != nil {
		t.Fatal(err)
	}
	got, err := s.Compose("alice")
	if err != nil || got.Lang != "nl" || got.Spacing != 0.7 || !got.Photo || len(got.Entries) != 1 {
		t.Errorf("compose = %+v, %v", got, err)
	}
	if err := s.SaveCompose("alice", PrintOptions{Lang: "nl", Spacing: 9}); err == nil {
		t.Error("out-of-range spacing saved")
	}
}

func TestLinksInEveryLanguage(t *testing.T) {
	s := newTestStore(t)
	l := Link{Slug: "uva-abcdefgh", Label: "UvA", Lang: "nl", Entries: []string{"experience/acme"}, Expires: "2030-01-01", Created: "2026-10-07"}
	if err := s.SaveLink("alice", l); err != nil {
		t.Fatal(err)
	}
	for lang, url := range map[string]string{"nl": "/uva-abcdefgh/", "en": "/uva-abcdefgh/en/"} {
		raw, err := os.ReadFile(filepath.Join(s.dir("alice"), "links", "uva-abcdefgh."+lang+".md"))
		if err != nil || !strings.Contains(string(raw), "url: "+url) {
			t.Errorf("%s file: %v\n%s", lang, err, raw)
		}
	}
	if links, _ := s.Links("alice"); len(links) != 1 || links[0].Lang != "nl" {
		t.Errorf("links = %+v", links)
	}

	// A link from before the toggle: a single file. Upgrading adds the rest.
	old := linkFile{Title: "Old", URL: "/old-abcdefgh/", Entries: []string{"experience/acme"}, ExpiryDate: "2030-01-01", Created: "2026-10-01"}
	writeMarkdown(filepath.Join(s.dir("alice"), "links", "old-abcdefgh.en.md"), old, "")
	if changed, err := s.UpgradeLinks("alice"); !changed || err != nil {
		t.Fatalf("UpgradeLinks = %v, %v", changed, err)
	}
	if got, _ := s.Link("alice", "old-abcdefgh"); got.Lang != "en" || got.files != len(Langs) {
		t.Errorf("upgraded link = %+v", got)
	}
	if changed, _ := s.UpgradeLinks("alice"); changed {
		t.Error("second upgrade changed something")
	}

	if err := s.DeleteLink("alice", "uva-abcdefgh"); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(s.dir("alice"), "links", "uva-abcdefgh.*")); len(m) != 0 {
		t.Errorf("files left after delete: %v", m)
	}
}

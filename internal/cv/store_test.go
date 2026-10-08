package cv

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the Nix build has no zoneinfo
)

// newTestStore has one CV, alice's, in English and Dutch.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := &Store{Root: t.TempDir()}
	if err := s.Init("alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProfile("alice", Profile{Langs: []string{"en", "nl"}}); err != nil {
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

	// A long name makes an id that is still one an item can have.
	it = Item{Section: "experience", Start: "2020", Text: map[string]ItemText{"en": {Org: "Netherlands Organisation for Applied Scientific Research and Innovation Programme"}}}
	it.ID = s.NewItemID("alice", it)
	if err := s.SaveItem("alice", it); err != nil || len(it.ID) > 40 {
		t.Errorf("long name: id %q, %v", it.ID, err)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	s := newTestStore(t)
	in := Profile{Langs: []string{"en", "nl"},
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
	if _, err := s.Link("alice", l.Slug); err != ErrNotFound {
		t.Errorf("link not deleted: %v", err)
	}
}

func TestLinkExpiry(t *testing.T) {
	zone, _ := time.LoadLocation("America/New_York")
	defer func(old *time.Location) { LinkZone = old }(LinkZone)
	LinkZone = zone
	l := Link{Expires: "2026-12-01"}
	before := time.Date(2026, 11, 30, 23, 59, 0, 0, LinkZone)
	at := time.Date(2026, 12, 1, 0, 0, 0, 0, LinkZone)
	if l.Expired(before) || !l.Expired(at) {
		t.Errorf("link should expire at the start of its expiry day, in the server's time zone")
	}
	if !(Link{Expires: "garbage"}).Expired(before) {
		t.Errorf("unparsable expiry must count as expired")
	}
}

func TestNewSlug(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		slug := NewSlug("UvA – PhD Application 2026!")
		if !strings.HasPrefix(slug, "uva-phd-application-2026-") || !IDRe.MatchString(slug) {
			t.Fatalf("slug %q", slug)
		}
		if seen[slug] {
			t.Fatalf("duplicate slug %q", slug)
		}
		seen[slug] = true
	}
	if slug := NewSlug(""); len(slug) != 8 || !IDRe.MatchString(slug) {
		t.Errorf("slug without label = %q", slug)
	}
}

func TestSplitLangFile(t *testing.T) {
	for name, want := range map[string][2]string{
		"alltrons.en.md":      {"alltrons", "en"},
		"tue-msc.nl.md":       {"tue-msc", "nl"},
		"alltrons.md":         {},
		"alltrons.xx.md":      {},
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
	if err := s.SaveProfile("alice", Profile{Langs: []string{"en", "nl"}, Name: "Alice", Order: []string{"publications"}}); err != nil {
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
	in := Profile{Langs: []string{"en", "nl"}, Name: "Alice", Links: []ProfileLink{
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
	if err := s.SaveProfile("alice", Profile{Langs: []string{"en"}, Links: []ProfileLink{{Label: "x", URL: "javascript:alert(1)"}}}); err == nil {
		t.Error("non-http profile link accepted")
	}
}

func TestProfileLocationPerLanguage(t *testing.T) {
	s := newTestStore(t)
	in := Profile{Langs: []string{"en", "nl"}, Name: "Alice", Text: map[string]ProfileText{
		"en": {Location: "Exampletown, the Netherlands"},
		"nl": {Location: "Exampletown, Nederland"},
	}}
	if err := s.SaveProfile("alice", in); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Profile("alice")
	if p.Text["en"].Location != "Exampletown, the Netherlands" || p.Text["nl"].Location != "Exampletown, Nederland" {
		t.Errorf("locations = %q, %q", p.Text["en"].Location, p.Text["nl"].Location)
	}
}

// A share link is one file. One from before format 2, a file per language,
// is read as it was, and Migrate makes it one file, with the teal it had.
func TestLinkFile(t *testing.T) {
	s := newTestStore(t)
	l := Link{Slug: "uva-abcdefgh", Label: "UvA", Lang: "nl", Entries: []string{"experience/acme"}, Theme: Theme{Accent: "#1d4ed8"}, Expires: "2030-01-01", Created: "2026-10-07"}
	if err := s.SaveLink("alice", l); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(s.dir("alice"), "links", "uva-abcdefgh.md"))
	for _, want := range []string{"name: UvA", "lang: nl", `expires: "2030-01-01"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("file lacks %q:\n%s", want, raw)
		}
	}
	if got, _ := s.Link("alice", "uva-abcdefgh"); !reflect.DeepEqual(got, l) {
		t.Errorf("link = %+v", got)
	}

	// From before format 2: Dutch at /old-abcdefgh/, English next to it.
	for lang, url := range map[string]string{"nl": "/old-abcdefgh/", "en": "/old-abcdefgh/en/"} {
		writeMarkdown(filepath.Join(s.dir("alice"), "links", "old-abcdefgh."+lang+".md"), legacyLinkFile{
			Title: "Old", URL: url, Entries: []string{"experience/acme"}, ExpiryDate: "2030-01-01", Created: "2026-10-01",
		}, "")
	}
	if got, _ := s.Link("alice", "old-abcdefgh"); got.Lang != "nl" || got.Label != "Old" || got.Expires != "2030-01-01" {
		t.Errorf("old link = %+v", got)
	}
	os.WriteFile(s.sharedProfilePath("alice"), []byte("---\nformat: 1\nlanguages: [en, nl]\n---\n"), 0o640)
	if err := s.Migrate("alice"); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(s.dir("alice"), "links", "old-abcdefgh.*")); len(m) != 1 {
		t.Errorf("files after migrating: %v", m)
	}
	if got, _ := s.Link("alice", "old-abcdefgh"); got.Lang != "nl" || got.Theme.Accent != oldAccent {
		t.Errorf("migrated link = %+v", got)
	}

	// A CV that loses the link's language: it opens in the main one.
	p, _ := s.Profile("alice")
	p.Langs = []string{"en"}
	s.SaveProfile("alice", p)
	if changed, err := s.UpgradeLinks("alice"); !changed || err != nil {
		t.Fatalf("UpgradeLinks = %v, %v", changed, err)
	}
	if got, _ := s.Link("alice", "old-abcdefgh"); got.Lang != "en" {
		t.Errorf("link = %+v", got)
	}

	if err := s.DeleteLink("alice", "old-abcdefgh"); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(s.dir("alice"), "links", "old-abcdefgh*")); len(m) != 0 {
		t.Errorf("files left after delete: %v", m)
	}
}

// A CV from before format 1 has the shared profile fields in every
// language's file. Migrate moves them to _index.md, also out of a language
// the CV no longer has, and changes nothing the editor shows.
func TestMigrate(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	dir := s.dir("old")
	os.MkdirAll(dir, 0o750)
	shared := "name: Old Example\nlanguages: [en, nl]\nemail: old@example.com\nlinks:\n  - label: Site\n    url: https://example.com\n"
	os.WriteFile(filepath.Join(dir, "_index.en.md"), []byte("---\n"+shared+"headline: Designer\n---\nHello.\n"), 0o640)
	os.WriteFile(filepath.Join(dir, "_index.nl.md"), []byte("---\n"+shared+"headline: Ontwerper\n---\nHallo.\n"), 0o640)
	os.WriteFile(filepath.Join(dir, "_index.de.md"), []byte("---\n"+shared+"headline: Gestalterin\n---\n"), 0o640) // hidden
	before, err := s.Profile("old")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Migrate("old"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Profile("old")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || after.Name != "Old Example" || after.Text["de"].Headline != "Gestalterin" {
		t.Errorf("profile changed:\n%+v\n%+v", before, after)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "_index.md"))
	if !strings.Contains(string(data), fmt.Sprintf("format: %d", Format)) || !strings.Contains(string(data), "name: Old Example") {
		t.Errorf("_index.md = %s", data)
	}
	for _, lang := range []string{"en", "nl", "de"} {
		data, _ := os.ReadFile(filepath.Join(dir, "_index."+lang+".md"))
		if strings.Contains(string(data), "name:") || strings.Contains(string(data), "languages:") || !strings.Contains(string(data), "headline:") {
			t.Errorf("_index.%s.md = %s", lang, data)
		}
	}

	// Saving no longer touches a language the CV doesn't have.
	after.Name = "New Example"
	if err := s.SaveProfile("old", after); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "_index.de.md")); string(data) != "---\nheadline: Gestalterin\n---\n" {
		t.Errorf("_index.de.md = %q", data)
	}
	if err := s.Migrate("old"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Profile("old"); p.Name != "New Example" {
		t.Errorf("a second Migrate changed the profile: %+v", p)
	}
}

// A CV from a newer New Leaf is refused, not read half and written back.
func TestNewerFormat(t *testing.T) {
	s := newTestStore(t)
	os.WriteFile(filepath.Join(s.dir("alice"), "_index.md"), []byte("---\nformat: 3\nname: Alice\n---\n"), 0o640)
	if _, err := s.Profile("alice"); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("Profile = %v", err)
	}
	if err := s.Migrate("alice"); err != nil {
		t.Errorf("Migrate = %v", err)
	}
}

// A small zip that unpacks to a lot is refused before anything is written.
func TestBackupBomb(t *testing.T) {
	s := newTestStore(t)
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create("content/_index.en.md")
	w.Write([]byte("---\nname: X\n---\n"))
	zeros := make([]byte, 1<<20)
	for i := range 11 {
		w, _ := zw.Create(fmt.Sprintf("content/experience/x%d.en.md", i))
		w.Write([]byte("---\ntitle: x\nstart: \"2020\"\n---\n"))
		for range 10 {
			w.Write(zeros)
		}
	}
	zw.Close()
	if b.Len() > 1<<20 {
		t.Fatalf("the zip is %d bytes", b.Len())
	}
	if stage, err := s.StageBackup(b.Bytes(), false); err == nil || !strings.Contains(err.Error(), "100 MB") {
		os.RemoveAll(stage)
		t.Errorf("StageBackup = %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(s.Root, ".import-*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

// tinyPNG is a real photo, 4 by 4 pixels.
func tinyPNG() []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 4)))
	return b.Bytes()
}

// hugePNG is a small file that declares an image of width by height.
func hugePNG(width, height uint32) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, width)
	ihdr = binary.BigEndian.AppendUint32(ihdr, height)
	ihdr = append(ihdr, 8, 0, 0, 0, 0) // 8-bit grey
	chunk := append([]byte("IHDR"), ihdr...)
	b := []byte("\x89PNG\r\n\x1a\n")
	b = binary.BigEndian.AppendUint32(b, uint32(len(ihdr)))
	b = append(b, chunk...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(chunk))
}

// A photo that would take gigabytes to decode is refused, and one already
// there is left out.
func TestHugePhoto(t *testing.T) {
	s := newTestStore(t)
	if err := s.SavePhoto("alice", ".png", hugePNG(100_000, 100_000)); err == nil {
		t.Error("saved a 10-gigapixel photo")
	}
	if err := s.SavePhoto("alice", ".png", tinyPNG()); err != nil || s.PhotoPath("alice") == "" {
		t.Fatalf("a small photo: %v", err)
	}
	os.WriteFile(filepath.Join(s.dir("alice"), "photo.png"), hugePNG(100_000, 100_000), 0o640)
	if s.PhotoPath("alice") != "" {
		t.Error("a huge photo on disk is used")
	}
}

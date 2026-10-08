package cv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Every language has every word a CV needs.
func TestLanguagesComplete(t *testing.T) {
	day := time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC)
	for _, l := range Languages {
		if l.Code == "" || l.Name == "" || l.English == "" || l.present == "" || l.DownloadLabel == "" || !strings.Contains(l.ExpiryNote, "%s") {
			t.Errorf("%s: missing words: %+v", l.Code, l)
		}
		for _, s := range Sections {
			if l.sections[s] == "" {
				t.Errorf("%s: no title for %s", l.Code, s)
			}
		}
		if slices.Contains(l.months[:], "") || slices.Contains(l.longMonths[:], "") {
			t.Errorf("%s: missing month names", l.Code)
		}
		if d := l.LongDate(day); !strings.Contains(d, "9") || !strings.Contains(d, "2026") || !strings.Contains(d, l.longMonths[2]) || strings.Contains(d, "%") {
			t.Errorf("%s: date = %q", l.Code, d)
		}
	}
}

func TestProfileLangs(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	s.Init("new")
	if langs, _ := s.Langs("new"); !slices.Equal(langs, []string{"en"}) {
		t.Errorf("a new CV's languages = %v", langs)
	}

	// From before languages could be chosen: English and Dutch files.
	os.MkdirAll(s.dir("old"), 0o750)
	for _, lang := range []string{"en", "nl"} {
		writeMarkdown(filepath.Join(s.dir("old"), "_index."+lang+".md"), profileFile{Name: "Old"}, "")
	}
	if langs, _ := s.Langs("old"); !slices.Equal(langs, []string{"en", "nl"}) {
		t.Errorf("an old CV's languages = %v", langs)
	}

	for _, bad := range [][]string{nil, {"en", "nl", "de"}, {"en", "en"}, {"xx"}} {
		if err := s.SaveProfile("new", Profile{Langs: bad}); err == nil {
			t.Errorf("saved languages %v", bad)
		}
	}
}

// A language the CV loses keeps its text, hidden, and gets it back with
// the language; its files keep up with the dates meanwhile.
func TestRemovedLanguageKeepsText(t *testing.T) {
	s := newTestStore(t)
	it := Item{Section: "experience", ID: "acme", Start: "2020", Text: map[string]ItemText{"en": {Title: "Engineer"}, "nl": {Title: "Ingenieur"}}}
	s.SaveItem("alice", it)
	s.SaveProfile("alice", Profile{Langs: []string{"en", "nl"}, Text: map[string]ProfileText{"nl": {Headline: "Ontwerper"}}})

	if err := s.SaveProfile("alice", Profile{Langs: []string{"de", "en"}, Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	it.Start, it.Text = "2021", map[string]ItemText{"en": {Title: "Engineer"}, "de": {Title: "Ingenieurin"}}
	if err := s.SaveItem("alice", it); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLink("alice", Link{Slug: "x-abcdefgh", Label: "X", Lang: "de", Entries: []string{"experience/acme"}, Created: "2026-01-02"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir("alice"), "links", "x-abcdefgh.nl.md")); err == nil {
		t.Error("a link was written in a language the CV doesn't have")
	}

	// As the editor does: the profile as read, which has the hidden text.
	p, _ := s.Profile("alice")
	p.Langs = []string{"en", "nl"}
	s.SaveProfile("alice", p)
	items, _ := s.Items("alice")
	if got := items[0]; got.Text["nl"].Title != "Ingenieur" || got.Text["de"].Title != "Ingenieurin" || got.Start != "2021" {
		t.Errorf("item = %+v", got)
	}
	if p, _ := s.Profile("alice"); p.Text["nl"].Headline != "Ontwerper" || p.Name != "Alice" {
		t.Errorf("profile = %+v", p)
	}
	if err := s.SaveVersion("alice", Version{ID: "v", Name: "V", PrintOptions: PrintOptions{Lang: "de"}}); err == nil {
		t.Error("saved a version in a language the CV doesn't have")
	}
}

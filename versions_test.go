package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestVersions(t *testing.T) {
	s := newTestStore(t)
	v := Version{ID: "uva", Name: " UvA   PhD ", PrintOptions: PrintOptions{Lang: "nl", Entries: []string{"experience/acme"}, Order: []string{"education"}}}
	if err := s.SaveVersion("alice", v); err != nil {
		t.Fatal(err)
	}
	got, err := s.Version("alice", "uva")
	if err != nil || got.Name != "UvA PhD" || got.Lang != "nl" || got.Order[0] != "education" || len(got.Order) != len(Sections) {
		t.Fatalf("version = %+v, %v", got, err)
	}
	if id := s.NewVersionID("alice", "UvA"); id != "uva-2" {
		t.Errorf("NewVersionID = %q", id)
	}
	for _, bad := range []Version{
		{ID: "x", Name: "", PrintOptions: PrintOptions{Lang: "en"}},
		{ID: "x", Name: "X", PrintOptions: PrintOptions{Lang: "de"}},
		{ID: "../x", Name: "X", PrintOptions: PrintOptions{Lang: "en"}},
		{ID: "x", Name: "X", PrintOptions: PrintOptions{Lang: "en", Order: []string{"hobbies"}}},
	} {
		if err := s.SaveVersion("alice", bad); err == nil {
			t.Errorf("saved %+v", bad)
		}
	}
	if err := s.DeleteVersion("alice", "uva"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Version("alice", "uva"); err != errNotFound {
		t.Errorf("deleted version: %v", err)
	}
}

// A CV from before versions: the composer's settings become "Full CV", and
// each share link a version it keeps showing.
func TestEnsureVersions(t *testing.T) {
	s := newTestStore(t)
	s.SaveItem("alice", Item{Section: "experience", ID: "acme", Start: "2020", Text: map[string]ItemText{"en": {Title: "x"}}})
	s.SaveItem("alice", Item{Section: "education", ID: "uni", Start: "2015", End: "2019", Text: map[string]ItemText{"en": {Title: "y"}}})
	os.WriteFile(s.composePath("alice"), []byte(`{"lang":"nl","entries":["experience/acme"],"photo":false,"spacing":0.7}`), 0o644)
	link := Link{Slug: "uva-abcdefgh", Label: "UvA", Lang: "en", Entries: []string{"education/uni"}, Expires: "2030-01-01", Created: "2026-01-02"}
	if err := s.SaveLink("alice", link); err != nil {
		t.Fatal(err)
	}

	if err := s.EnsureVersions("alice"); err != nil {
		t.Fatal(err)
	}
	versions, _ := s.Versions("alice")
	if len(versions) != 2 || versions[0].ID != "full-cv" || versions[0].Lang != "nl" || versions[0].Spacing != 0.7 || !slices.Equal(versions[0].Entries, []string{"experience/acme"}) {
		t.Fatalf("versions = %+v", versions)
	}
	if v := versions[1]; v.ID != "uva" || v.Name != "UvA" || !slices.Equal(v.Entries, link.Entries) || v.Created != "2026-01-02" {
		t.Errorf("link's version = %+v", v)
	}
	if l, _ := s.VersionLink("alice", "uva"); l == nil || l.Slug != link.Slug {
		t.Errorf("link not attached: %+v", l)
	}
	if _, err := os.Stat(s.composePath("alice")); err == nil {
		t.Error("compose.json still there")
	}

	// Only once: a deleted Full CV stays deleted.
	s.DeleteVersion("alice", "full-cv")
	s.EnsureVersions("alice")
	if versions, _ := s.Versions("alice"); len(versions) != 1 {
		t.Errorf("versions came back: %+v", versions)
	}

	// A new CV gets a Full CV with everything.
	fresh := &Store{Root: filepath.Join(t.TempDir(), "users")}
	fresh.Init("bob")
	fresh.SaveItem("bob", Item{Section: "experience", ID: "acme", Start: "2020", Text: map[string]ItemText{"en": {Title: "x"}}})
	fresh.EnsureVersions("bob")
	if v, err := fresh.Version("bob", "full-cv"); err != nil || len(v.Entries) != 1 {
		t.Errorf("fresh full CV = %+v, %v", v, err)
	}
}

func TestDocumentUsesVersionOrder(t *testing.T) {
	p := Profile{Order: []string{"experience", "education"}}
	items := []Item{
		{Section: "experience", ID: "a", Start: "2020", Text: map[string]ItemText{"en": {Title: "Job"}}},
		{Section: "education", ID: "b", Start: "2015", End: "2019", Text: map[string]ItemText{"en": {Title: "Uni"}}},
	}
	doc := BuildDocument(p, items, PrintOptions{Lang: "en", Order: []string{"education"}}, "")
	if doc.Sections[0].Title != "Education" {
		t.Errorf("sections = %+v", doc.Sections)
	}
	doc = BuildDocument(p, items, PrintOptions{Lang: "en"}, "")
	if doc.Sections[0].Title != "Work experience" {
		t.Errorf("without a version order: %+v", doc.Sections)
	}
}

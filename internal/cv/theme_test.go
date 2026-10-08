package cv

import (
	"strings"
	"testing"
)

func TestTheme(t *testing.T) {
	for _, bad := range []Theme{{Accent: "red"}, {Accent: "#12345"}, {Font: "comic"}, {Photo: "star"}} {
		if bad.Validate() == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if err := (PrintOptions{Lang: "en", Theme: Theme{Font: "mono"}}).Validate(); err == nil {
		t.Error("print options with a bad theme accepted")
	}

	if r := (Theme{}).Resolved(); r != (Theme{Accent: defaultAccent, Font: "sans", Photo: "rounded"}) {
		t.Errorf("defaults = %+v", r)
	}
	doc := BuildDocument(Profile{}, nil, PrintOptions{Lang: "en", Theme: Theme{Accent: "#B91C1C", Font: "serif", Photo: "circle"}}, "")
	if doc.Theme != (DocTheme{Accent: "#b91c1c", Font: "Source Serif 4", Photo: "circle"}) {
		t.Errorf("document theme = %+v", doc.Theme)
	}

	// Share pages: one stylesheet per look, named after its content.
	name, css := Theme{Accent: "#b91c1c", Font: "serif", Photo: "circle"}.CSS()
	same, _ := Theme{Accent: "#B91C1C", Font: "serif", Photo: "circle"}.CSS()
	other, _ := Theme{}.CSS()
	if name != same || name == other || !strings.HasPrefix(name, "css/theme.") {
		t.Errorf("names %q %q %q", name, same, other)
	}
	for _, want := range []string{"--color-accent:#b91c1c", `"Source Serif 4"`, "--photo-radius:9999px"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("css lacks %q: %s", want, css)
		}
	}
}

func TestThemeRoundTrip(t *testing.T) {
	s := newTestStore(t)
	look := Theme{Accent: "#1d4ed8", Font: "serif"}
	if err := s.SaveProfile("alice", Profile{Langs: []string{"en"}, Name: "Alice", Theme: look}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Profile("alice"); p.Theme != look {
		t.Errorf("profile theme = %+v", p.Theme)
	}
	l := Link{Slug: "uva-abcdefgh", Lang: "en", Entries: []string{"experience/acme"}, Expires: "2030-01-01", Theme: look}
	if err := s.SaveLink("alice", l); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Link("alice", l.Slug); got.Theme != look || got.Print().Theme != look {
		t.Errorf("link theme = %+v", got.Theme)
	}
}

package cv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// As JSON Resume's own example has it, cut down.
const sampleResume = `{
  "basics": {
    "name": "Richard Example", "label": "Programmer", "image": "https://example.com/me.jpg",
    "email": "richard@example.com", "phone": "(912) 555-4321", "url": "https://example.com",
    "summary": "A summary of Richard.",
    "location": {"address": "2712 Broadway St", "city": "San Francisco", "countryCode": "US", "region": "California"},
    "profiles": [{"network": "Twitter", "username": "rich", "url": "https://example.com/rich"}, {"network": "Nowhere", "username": "x"}]
  },
  "work": [
    {"name": "Company", "position": "President", "url": "https://company.example.com", "startDate": "2013-01-01", "endDate": "2014-01-01",
     "summary": "Description.", "highlights": ["Started the company"]},
    {"name": "No dates", "position": "Ghost"}
  ],
  "volunteer": [{"organization": "Organization", "position": "Volunteer", "startDate": "2012-01-01", "summary": "Helped."}],
  "education": [{"institution": "University", "area": "Software Development", "studyType": "Bachelor", "startDate": "2011-01-01", "endDate": "2013-01-01", "score": "4.0", "courses": ["DB1101 - Basic SQL"]}],
  "certificates": [{"name": "Certificate", "date": "2021-11-07", "issuer": "Company"}],
  "awards": [{"title": "Award", "date": "2014-11-01", "awarder": "Company", "summary": "There is no spoon."}],
  "publications": [{"name": "Publication", "publisher": "Company", "releaseDate": "2014-10-01", "url": "https://example.com/pub", "summary": "Description."}],
  "projects": [{"name": "Project", "startDate": "2019-01-01", "endDate": "2021-01-01", "description": "Description.", "highlights": ["Won award at AIHacks 2016"], "url": "https://example.com/project"}],
  "skills": [{"name": "Web Development", "level": "Master"}],
  "languages": [{"language": "English", "fluency": "Native speaker"}],
  "interests": [{"name": "Wildlife"}],
  "references": [{"name": "Jane Doe", "reference": "Reference."}]
}`

func TestParseResume(t *testing.T) {
	p, items, skipped, err := ParseResume([]byte(sampleResume), "en")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Richard Example" || p.Website != "https://example.com" || p.Text["en"].Headline != "Programmer" ||
		p.Text["en"].Location != "San Francisco, California" || len(p.Links) != 1 || p.Links[0].Label != "Twitter" {
		t.Errorf("profile = %+v", p)
	}
	got := map[string]Item{}
	for _, it := range items {
		got[it.Section+"/"+it.Text["en"].Title] = it
	}
	for key, want := range map[string]struct{ start, end, org, body string }{
		"experience/President":                    {"2013-01", "2014-01", "Company", "Description.\n\n- Started the company"},
		"volunteering/Volunteer":                  {"2012-01", "", "Organization", "Helped."},
		"education/Bachelor Software Development": {"2011-01", "2013-01", "University", "- DB1101 - Basic SQL"},
		"education/Certificate":                   {"2021-11", "2021-11", "Company", ""},
		"awards/Award":                            {"2014-11", "", "Company", "There is no spoon."},
		"publications/Publication":                {"2014-10", "", "Company", "Description."},
		"output/Project":                          {"2021-01", "", "", "Description.\n\n- Won award at AIHacks 2016"},
	} {
		it, ok := got[key]
		if !ok {
			t.Errorf("%s missing", key)
			continue
		}
		if it.Start != want.start || it.End != want.end || it.Text["en"].Org != want.org || it.Text["en"].Body != want.body {
			t.Errorf("%s = %+v", key, it)
		}
	}
	if len(items) != 7 {
		t.Errorf("%d items", len(items))
	}
	all := strings.Join(skipped, "\n")
	for _, want := range []string{"photo", `"Ghost"`, "score", "skills (1)", "languages (1)", "interests (1)", "references (1)"} {
		if !strings.Contains(all, want) {
			t.Errorf("skipped doesn't mention %q:\n%s", want, all)
		}
	}

	for _, bad := range []string{"", "[]", `{"basics": {}}`, `{"name": "x"}`} {
		if _, _, _, err := ParseResume([]byte(bad), "en"); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// A CV that goes out as JSON Resume and comes back in has the same items in
// the same sections, and keeps its photo and look.
func TestResumeRoundTrip(t *testing.T) {
	s := newTestStore(t)
	s.SaveProfile("alice", Profile{
		Langs: []string{"nl", "en"}, Name: "Alice Example", Email: "alice@example.com", Website: "https://example.com",
		Links: []ProfileLink{{Label: "LinkedIn", URL: "https://example.com/in/alice"}},
		Theme: Theme{Accent: "#1d4ed8", Font: "serif"}, Spacing: 0.8,
		Text: map[string]ProfileText{"nl": {Headline: "Ontwerper", Location: "Utrecht", Summary: "Hallo **daar**."}, "en": {Headline: "Designer"}},
	})
	os.WriteFile(filepath.Join(s.dir("alice"), "photo.png"), tinyPNG(), 0o600)
	periods := map[string]bool{}
	for _, section := range Sections {
		it := Item{Section: section, ID: section, Start: "2020-01", End: "2021-06", Link: "https://example.com/" + section,
			Text: map[string]ItemText{"nl": {Title: "Titel " + section, Org: "Org " + section, Body: "- een\n- twee"}, "en": {Title: "Title"}}}
		if IsPointSection(section) {
			it.End = ""
		} else {
			periods[section] = true
		}
		if err := s.SaveItem("alice", it); err != nil {
			t.Fatal(err)
		}
	}
	profile, _ := s.Profile("alice")
	items, _ := s.Items("alice")
	data, err := json.Marshal(ExportResume(profile, items, "nl"))
	if err != nil {
		t.Fatal(err)
	}

	stage, skipped, err := s.StageResume("alice", data)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if len(skipped) != 0 {
		t.Errorf("skipped = %v", skipped)
	}
	if err := s.RestoreBackup("alice", stage); err != nil {
		t.Fatal(err)
	}

	p, _ := s.Profile("alice")
	if p.Name != "Alice Example" || p.Text["nl"].Summary != "Hallo **daar**." || p.Text["nl"].Location != "Utrecht" ||
		!slices.Equal(p.Langs, []string{"nl"}) || p.Theme.Accent != "#1d4ed8" || p.Spacing != 0.8 || !p.Photo || len(p.Links) != 1 {
		t.Errorf("profile = %+v", p)
	}
	back, _ := s.Items("alice")
	if len(back) != len(Sections) {
		t.Fatalf("%d items, want %d", len(back), len(Sections))
	}
	for _, it := range back {
		x := it.Text["nl"]
		want := "2021-06"
		if IsPointSection(it.Section) {
			want = ""
		}
		if x.Title != "Titel "+it.Section || x.Org != "Org "+it.Section || x.Body != "- een\n- twee" || it.Start != "2020-01" || it.End != want {
			t.Errorf("%s = %+v", it.Section, it)
		}
		if it.Section != "awards" && it.Link != "https://example.com/"+it.Section {
			t.Errorf("%s link = %q", it.Section, it.Link)
		}
	}
	versions, _ := s.Versions("alice")
	if len(versions) != 1 || len(versions[0].Entries) != len(Sections) {
		t.Errorf("versions = %+v", versions)
	}
	kept, _ := filepath.Glob(filepath.Join(s.Root, "alice", "backups", "before-import-*.zip"))
	if len(kept) != 1 {
		t.Errorf("the replaced CV was not kept: %v", kept)
	}
}

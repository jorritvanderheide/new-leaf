package cv

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JSON Resume (https://jsonresume.org/schema) is how other CV tools import
// and export a CV: one language, no photo file, no look. New Leaf reads and
// writes the parts it has a place for. Its sections without one in JSON
// Resume (other output, presentations, teaching, extracurricular) go to
// projects, with the section as the project's type, so they come back where
// they were.

type Resume struct {
	Schema       string              `json:"$schema,omitempty"`
	Basics       ResumeBasics        `json:"basics"`
	Work         []ResumeWork        `json:"work,omitempty"`
	Volunteer    []ResumeWork        `json:"volunteer,omitempty"`
	Education    []ResumeEducation   `json:"education,omitempty"`
	Awards       []ResumeAward       `json:"awards,omitempty"`
	Certificates []ResumeCertificate `json:"certificates,omitempty"`
	Publications []ResumePublication `json:"publications,omitempty"`
	Projects     []ResumeProject     `json:"projects,omitempty"`
	Skills       []json.RawMessage   `json:"skills,omitempty"`
	Languages    []json.RawMessage   `json:"languages,omitempty"`
	Interests    []json.RawMessage   `json:"interests,omitempty"`
	References   []json.RawMessage   `json:"references,omitempty"`
}

type ResumeBasics struct {
	Name     string          `json:"name,omitempty"`
	Label    string          `json:"label,omitempty"`
	Image    string          `json:"image,omitempty"`
	Email    string          `json:"email,omitempty"`
	Phone    string          `json:"phone,omitempty"`
	URL      string          `json:"url,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Location *ResumeLocation `json:"location,omitempty"`
	Profiles []ResumeProfile `json:"profiles,omitempty"`
}

type ResumeLocation struct {
	Address     string `json:"address,omitempty"`
	City        string `json:"city,omitempty"`
	Region      string `json:"region,omitempty"`
	CountryCode string `json:"countryCode,omitempty"`
}

type ResumeProfile struct {
	Network  string `json:"network,omitempty"`
	Username string `json:"username,omitempty"`
	URL      string `json:"url,omitempty"`
}

// ResumeWork is a job (work) or volunteering (volunteer), which name the
// organisation differently.
type ResumeWork struct {
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Position     string   `json:"position,omitempty"`
	Location     string   `json:"location,omitempty"`
	URL          string   `json:"url,omitempty"`
	StartDate    string   `json:"startDate,omitempty"`
	EndDate      string   `json:"endDate,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Highlights   []string `json:"highlights,omitempty"`
}

type ResumeEducation struct {
	Institution string   `json:"institution,omitempty"`
	URL         string   `json:"url,omitempty"`
	Area        string   `json:"area,omitempty"`
	StudyType   string   `json:"studyType,omitempty"`
	StartDate   string   `json:"startDate,omitempty"`
	EndDate     string   `json:"endDate,omitempty"`
	Score       string   `json:"score,omitempty"`
	Courses     []string `json:"courses,omitempty"`
	// Not in the schema, which has no description for education, but
	// allowed by it: the description of a New Leaf item.
	Summary string `json:"summary,omitempty"`
}

type ResumeAward struct {
	Title   string `json:"title,omitempty"`
	Date    string `json:"date,omitempty"`
	Awarder string `json:"awarder,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type ResumeCertificate struct {
	Name   string `json:"name,omitempty"`
	Date   string `json:"date,omitempty"`
	Issuer string `json:"issuer,omitempty"`
	URL    string `json:"url,omitempty"`
}

type ResumePublication struct {
	Name        string `json:"name,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	URL         string `json:"url,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

type ResumeProject struct {
	Name        string   `json:"name,omitempty"`
	Entity      string   `json:"entity,omitempty"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Highlights  []string `json:"highlights,omitempty"`
	URL         string   `json:"url,omitempty"`
	StartDate   string   `json:"startDate,omitempty"`
	EndDate     string   `json:"endDate,omitempty"`
}

const resumeSchema = "https://raw.githubusercontent.com/jsonresume/resume-schema/v1.0.0/schema.json"

// projectSections are the sections that go to JSON Resume's projects, by
// the project type they get. Output has none.
var projectSections = map[string]string{"presentation": "presentations", "teaching": "teaching", "extracurricular": "extracurricular"}

// ExportResume writes a CV's text in one language as a JSON Resume.
func ExportResume(p Profile, items []Item, lang string) Resume {
	t := p.Text[lang]
	r := Resume{Schema: resumeSchema, Basics: ResumeBasics{
		Name: p.Name, Label: t.Headline, Email: p.Email, Phone: p.Phone, URL: p.Website, Summary: t.Summary,
	}}
	if t.Location != "" {
		r.Basics.Location = &ResumeLocation{City: t.Location}
	}
	for _, l := range p.Links {
		r.Basics.Profiles = append(r.Basics.Profiles, ResumeProfile{Network: l.Label, URL: l.URL})
	}
	// Newest first in each section, as on the CV.
	var sorted []Item
	for _, section := range Sections {
		var in []Item
		for _, it := range items {
			if it.Section == section {
				in = append(in, it)
			}
		}
		sortItems(in, section, lang)
		sorted = append(sorted, in...)
	}
	for _, it := range sorted {
		x := it.Text[lang]
		switch it.Section {
		case "experience":
			r.Work = append(r.Work, ResumeWork{Name: x.Org, Position: x.Title, Location: x.Location, URL: it.Link, StartDate: it.Start, EndDate: it.End, Summary: x.Body})
		case "volunteering":
			r.Volunteer = append(r.Volunteer, ResumeWork{Organization: x.Org, Position: x.Title, Location: x.Location, URL: it.Link, StartDate: it.Start, EndDate: it.End, Summary: x.Body})
		case "education":
			r.Education = append(r.Education, ResumeEducation{Institution: x.Org, StudyType: x.Title, URL: it.Link, StartDate: it.Start, EndDate: it.End, Summary: x.Body})
		case "awards":
			r.Awards = append(r.Awards, ResumeAward{Title: x.Title, Awarder: x.Org, Date: it.Start, Summary: x.Body})
		case "publications":
			r.Publications = append(r.Publications, ResumePublication{Name: x.Title, Publisher: x.Org, ReleaseDate: it.Start, URL: it.Link, Summary: x.Body})
		default: // output, presentations, teaching, extracurricular
			typ := ""
			for k, s := range projectSections {
				if s == it.Section {
					typ = k
				}
			}
			r.Projects = append(r.Projects, ResumeProject{Name: x.Title, Entity: x.Org, Type: typ, URL: it.Link, StartDate: it.Start, EndDate: it.End, Description: x.Body})
		}
	}
	return r
}

// ParseResume reads a JSON Resume as a CV in one language: its profile and
// items, and what it had that New Leaf has no place for.
func ParseResume(data []byte, lang string) (p Profile, items []Item, skipped []string, err error) {
	var r Resume
	if err := json.Unmarshal(data, &r); err != nil {
		return p, nil, nil, Invalid{fmt.Errorf("not a JSON Resume: %v", err)}
	}
	b := r.Basics
	if b.Name == "" && len(r.Work)+len(r.Education) == 0 {
		return p, nil, nil, Invalid{errors.New("not a JSON Resume: it has no name, work or education")}
	}
	p = Profile{Langs: []string{lang}, Name: b.Name, Email: b.Email, Phone: b.Phone, Website: webURL(b.URL), Text: map[string]ProfileText{lang: {
		Headline: b.Label, Summary: b.Summary, Location: b.Location.text(),
	}}}
	for _, pr := range b.Profiles {
		if u := webURL(pr.URL); u != "" {
			p.Links = append(p.Links, ProfileLink{Label: cmp.Or(pr.Network, pr.Username), URL: u})
		}
	}
	if b.Image != "" {
		skipped = append(skipped, "the photo (upload it on the Profile page)")
	}

	add := func(what string, it Item, title, org, location, body string) {
		it.Start, it.End = resumeDate(it.Start), resumeDate(it.End)
		if IsPointSection(it.Section) {
			it.Start, it.End = cmp.Or(it.End, it.Start), ""
		} else if it.Start == "" {
			it.Start = it.End // ended then; no end would mean it still goes on
		}
		it.ID, it.Link = "item", webURL(it.Link) // the ID is made when it is saved
		it.Text = map[string]ItemText{lang: {Title: title, Org: org, Location: location, Body: strings.TrimSpace(body)}}
		if err := it.Validate(); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s %q: %v", what, cmp.Or(title, org), err))
			return
		}
		items = append(items, it)
	}
	for _, w := range r.Work {
		add("work", Item{Section: "experience", Start: w.StartDate, End: w.EndDate, Link: w.URL}, w.Position, w.Name, w.Location, withList(w.Summary, w.Highlights))
	}
	for _, w := range r.Volunteer {
		add("volunteering", Item{Section: "volunteering", Start: w.StartDate, End: w.EndDate, Link: w.URL}, w.Position, w.Organization, w.Location, withList(w.Summary, w.Highlights))
	}
	for _, e := range r.Education {
		add("education", Item{Section: "education", Start: e.StartDate, End: e.EndDate, Link: e.URL}, strings.TrimSpace(e.StudyType+" "+e.Area), e.Institution, "", withList(e.Summary, e.Courses))
		if e.Score != "" {
			skipped = append(skipped, fmt.Sprintf("the score of education %q", cmp.Or(strings.TrimSpace(e.StudyType+" "+e.Area), e.Institution)))
		}
	}
	for _, c := range r.Certificates {
		add("certificate", Item{Section: "education", Start: c.Date, End: c.Date, Link: c.URL}, c.Name, c.Issuer, "", "")
	}
	for _, a := range r.Awards {
		add("award", Item{Section: "awards", Start: a.Date}, a.Title, a.Awarder, "", a.Summary)
	}
	for _, pub := range r.Publications {
		add("publication", Item{Section: "publications", Start: pub.ReleaseDate, Link: pub.URL}, pub.Name, pub.Publisher, "", pub.Summary)
	}
	for _, pr := range r.Projects {
		add("project", Item{Section: cmp.Or(projectSections[pr.Type], "output"), Start: pr.StartDate, End: pr.EndDate, Link: pr.URL}, pr.Name, pr.Entity, "", withList(pr.Description, pr.Highlights))
	}
	for _, l := range []struct {
		what string
		n    int
	}{{"skills", len(r.Skills)}, {"languages", len(r.Languages)}, {"interests", len(r.Interests)}, {"references", len(r.References)}} {
		if l.n > 0 {
			skipped = append(skipped, fmt.Sprintf("%s (%d), which New Leaf has no section for", l.what, l.n))
		}
	}
	return p, items, skipped, nil
}

// StageResume makes a CV out of a JSON Resume in a fresh folder next to the
// CVs, as StageBackup does with a backup, for RestoreBackup to put in place.
// It is in the CV's main language, and keeps the CV's photo and look, which
// JSON Resume has no place for. The caller removes the folder either way.
func (s *Store) StageResume(user string, data []byte) (stage string, skipped []string, err error) {
	cur, err := s.Profile(user)
	if err != nil {
		return "", nil, err
	}
	p, items, skipped, err := ParseResume(data, cur.Langs[0])
	if err != nil {
		return "", nil, err
	}
	p.Theme, p.Spacing, p.Order = cur.Theme, cur.Spacing, cur.Order
	if err := p.Validate(); err != nil {
		return "", nil, Invalid{err}
	}
	stage, err = os.MkdirTemp(s.Root, ".import-")
	if err != nil {
		return "", nil, err
	}
	staged := &Store{Root: filepath.Dir(stage)}
	name := filepath.Base(stage)
	err = func() error {
		if err := staged.SaveProfile(name, p); err != nil {
			return err
		}
		if photo := s.PhotoPath(user); photo != "" {
			data, err := os.ReadFile(photo)
			if err != nil {
				return err
			}
			if err := staged.SavePhoto(name, filepath.Ext(photo), data); err != nil {
				return err
			}
		}
		for _, it := range items {
			it.ID = staged.NewItemID(name, it)
			if err := staged.SaveItem(name, it); err != nil {
				return err
			}
		}
		return staged.EnsureVersions(name)
	}()
	if err != nil {
		os.RemoveAll(stage)
		return "", nil, err
	}
	return stage, skipped, nil
}

func (l *ResumeLocation) text() string {
	if l == nil {
		return ""
	}
	var parts []string
	for _, s := range []string{l.City, cmp.Or(l.Region, l.CountryCode)} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// resumeDate turns a JSON Resume date (YYYY-MM-DD, YYYY-MM or YYYY) into
// one of New Leaf's (YYYY-MM or YYYY); anything else is left out.
func resumeDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 7 {
		s = s[:7]
	}
	if !dateRe.MatchString(s) {
		return ""
	}
	return s
}

// webURL keeps a URL if New Leaf can link to it.
func webURL(s string) string {
	if s = strings.TrimSpace(s); urlRe.MatchString(s) {
		return s
	}
	return ""
}

// withList is a description followed by a Markdown list, such as a job's
// highlights.
func withList(text string, list []string) string {
	text = strings.TrimSpace(text)
	var b strings.Builder
	for _, l := range list {
		if l = strings.TrimSpace(l); l != "" {
			b.WriteString("- " + l + "\n")
		}
	}
	if b.Len() > 0 && text != "" {
		text += "\n\n"
	}
	return text + b.String()
}

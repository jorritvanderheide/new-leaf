package cv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type ProfileText struct {
	Headline string `json:"headline" yaml:"headline,omitempty"`
	Location string `json:"location" yaml:"location,omitempty"` // e.g. "Utrecht, the Netherlands"
	Summary  string `json:"summary"  yaml:"-"`
}

type Profile struct {
	Name    string                 `json:"name"`
	Email   string                 `json:"email"`
	Phone   string                 `json:"phone"`
	Website string                 `json:"website"`
	Links   []ProfileLink          `json:"links"` // e.g. LinkedIn, Google Scholar
	Photo   bool                   `json:"photo"`
	Order   []string               `json:"order"`   // default section order for new versions
	Langs   []string               `json:"langs"`   // the CV's languages, the main one first
	Theme   Theme                  `json:"theme"`   // default look for new versions
	Spacing float64                `json:"spacing"` // default spacing for new versions; 0 means 1
	Text    map[string]ProfileText `json:"text"`
}

// Format is the layout of the CV's files that this New Leaf writes, kept as
// `format` in content/_index.md. A CV without that file is from before it
// (format 0), and Migrate moves it over. To change the layout again, raise
// Format and teach Migrate to move a CV up from the one before.
const Format = 1

// profileShared is content/_index.md: what the profile has once, whatever
// the language.
type profileShared struct {
	Format    int           `yaml:"format"`
	Languages []string      `yaml:"languages,omitempty"`
	Name      string        `yaml:"name,omitempty"`
	Email     string        `yaml:"email,omitempty"`
	Phone     string        `yaml:"phone,omitempty"`
	Website   string        `yaml:"website,omitempty"`
	Links     []ProfileLink `yaml:"links,omitempty"`
	Order     []string      `yaml:"order,omitempty"`
	Theme     Theme         `yaml:"theme,omitempty"`
	Spacing   float64       `yaml:"spacing,omitempty"`
}

// legacyProfileFile is content/_index.<lang>.md from before format 1, which
// had the shared fields too, written to every language.
type legacyProfileFile struct {
	profileShared `yaml:",inline"`
	ProfileText   `yaml:",inline"`
}

type ProfileLink struct {
	Label string `json:"label" yaml:"label"`
	URL   string `json:"url"   yaml:"url"`
}

// ErrNewerFormat is a CV written by a newer New Leaf than this one.
var ErrNewerFormat = errors.New("this CV was saved by a newer New Leaf: update New Leaf to open it")

// Profile reads the profile, with the text of every language it has files
// for, also those the CV no longer has (they come back if it gets them
// again).
func (s *Store) Profile(user string) (Profile, error) {
	p := Profile{Text: map[string]ProfileText{}}
	var shared profileShared
	_, err := readMarkdown(s.sharedProfilePath(user), &shared)
	found := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return p, err
	}
	if shared.Format > Format {
		return p, fmt.Errorf("%w (format %d; this one reads up to %d)", ErrNewerFormat, shared.Format, Format)
	}
	for _, lang := range langCodes() {
		var f legacyProfileFile
		body, err := readMarkdown(s.profilePath(user, lang), &f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return p, err
		}
		// Before format 1: take the shared fields from the first file found.
		if !found {
			found, shared = true, f.profileShared
		}
		f.ProfileText.Summary = body
		p.Text[lang] = f.ProfileText
	}
	p.Name, p.Email, p.Phone, p.Website = shared.Name, shared.Email, shared.Phone, shared.Website
	p.Links, p.Order, p.Theme, p.Spacing, p.Langs = shared.Links, shared.Order, shared.Theme, shared.Spacing, shared.Languages
	// CVs from before languages could be chosen were in English and Dutch,
	// with files for both.
	if len(p.Langs) == 0 {
		for _, lang := range []string{"en", "nl"} {
			if _, ok := p.Text[lang]; ok {
				p.Langs = append(p.Langs, lang)
			}
		}
	}
	if len(p.Langs) == 0 {
		p.Langs = []string{"en"}
	}
	p.Order = SectionOrder(p.Order)
	if p.Links == nil {
		p.Links = []ProfileLink{}
	}
	p.Photo = s.PhotoPath(user) != ""
	return p, nil
}

func (s *Store) sharedProfilePath(user string) string { return filepath.Join(s.dir(user), "_index.md") }

func (s *Store) profilePath(user, lang string) string {
	return filepath.Join(s.dir(user), "_index."+lang+".md")
}

// Migrate moves a CV from before format 1 over: its profile's shared fields
// go to content/_index.md, and out of every language's file, also those of
// languages it no longer has. The shared file is written first, so a
// migration that is cut off loses nothing: the language files then keep
// fields that are no longer read.
func (s *Store) Migrate(user string) error {
	if _, err := os.Stat(s.sharedProfilePath(user)); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	p, err := s.Profile(user)
	if err != nil {
		return err
	}
	if err := s.writeSharedProfile(user, p); err != nil {
		return err
	}
	for lang, t := range p.Text {
		if err := writeMarkdown(s.profilePath(user, lang), t, t.Summary); err != nil {
			return err
		}
	}
	return nil
}

func (p Profile) Validate() error {
	if err := validLangs(p.Langs); err != nil {
		return err
	}
	if err := p.Theme.Validate(); err != nil {
		return err
	}
	if p.Spacing != 0 && (p.Spacing < MinSpacing || p.Spacing > MaxSpacing) {
		return fmt.Errorf("spacing must be between %g and %g", MinSpacing, MaxSpacing)
	}
	for _, u := range append([]string{p.Website}, linkURLs(p.Links)...) {
		if u = strings.TrimSpace(u); u != "" && !urlRe.MatchString(u) {
			return fmt.Errorf("%q is not a URL starting with http:// or https://", u)
		}
	}
	return nil
}

func linkURLs(links []ProfileLink) []string {
	var urls []string
	for _, l := range links {
		urls = append(urls, l.URL)
	}
	return urls
}

// SaveProfile writes the shared fields and the text of the CV's languages.
// A language the CV no longer has keeps its file, and with it its text.
func (s *Store) SaveProfile(user string, p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := s.writeSharedProfile(user, p); err != nil {
		return err
	}
	for _, lang := range p.Langs {
		t := p.Text[lang]
		if err := writeMarkdown(s.profilePath(user, lang), ProfileText{Headline: Clean(t.Headline), Location: Clean(t.Location)}, t.Summary); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) writeSharedProfile(user string, p Profile) error {
	var links []ProfileLink
	for _, l := range p.Links {
		if l.URL = strings.TrimSpace(l.URL); l.URL != "" {
			links = append(links, ProfileLink{Label: Clean(l.Label), URL: l.URL})
		}
	}
	return writeMarkdown(s.sharedProfilePath(user), profileShared{
		Format: Format, Languages: p.Langs,
		Name: Clean(p.Name), Email: Clean(p.Email), Phone: Clean(p.Phone), Website: Clean(p.Website),
		Links:   links,
		Order:   SectionOrder(p.Order),
		Theme:   p.Theme,
		Spacing: p.Spacing,
	}, "")
}

// Langs are the CV's languages, the main one first.
func (s *Store) Langs(user string) ([]string, error) {
	p, err := s.Profile(user)
	return p.Langs, err
}

func (s *Store) PhotoPath(user string) string {
	for _, ext := range photoExts {
		p := filepath.Join(s.dir(user), "photo"+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (s *Store) SavePhoto(user, ext string, data []byte) error {
	if err := s.DeletePhoto(user); err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(s.dir(user), "photo"+ext), data)
}

func (s *Store) DeletePhoto(user string) error {
	for _, ext := range photoExts {
		if err := os.Remove(filepath.Join(s.dir(user), "photo"+ext)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

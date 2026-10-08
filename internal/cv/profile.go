package cv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

type profileFile struct {
	Name        string   `yaml:"name,omitempty"`
	Languages   []string `yaml:"languages,omitempty"`
	ProfileText `yaml:",inline"`
	Email       string        `yaml:"email,omitempty"`
	Phone       string        `yaml:"phone,omitempty"`
	Website     string        `yaml:"website,omitempty"`
	Links       []ProfileLink `yaml:"links,omitempty"`
	Order       []string      `yaml:"order,omitempty"`
	Theme       Theme         `yaml:"theme,omitempty"`
	Spacing     float64       `yaml:"spacing,omitempty"`
}

type ProfileLink struct {
	Label string `json:"label" yaml:"label"`
	URL   string `json:"url"   yaml:"url"`
}

// Profile reads the profile, with the text of every language it has files
// for, also those the CV no longer has (they come back if it gets them
// again).
func (s *Store) Profile(user string) (Profile, error) {
	p := Profile{Text: map[string]ProfileText{}}
	shared := false
	for _, lang := range langCodes() {
		var f profileFile
		body, err := readMarkdown(filepath.Join(s.dir(user), "_index."+lang+".md"), &f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return p, err
		}
		// Shared fields are written to every language; take the first found.
		if !shared {
			shared = true
			p.Name, p.Email, p.Phone, p.Website = f.Name, f.Email, f.Phone, f.Website
			p.Links, p.Order, p.Theme, p.Spacing, p.Langs = f.Links, f.Order, f.Theme, f.Spacing, f.Languages
		}
		f.ProfileText.Summary = body
		p.Text[lang] = f.ProfileText
	}
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

func (s *Store) SaveProfile(user string, p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	var links []ProfileLink
	for _, l := range p.Links {
		if l.URL = strings.TrimSpace(l.URL); l.URL != "" {
			links = append(links, ProfileLink{Label: Clean(l.Label), URL: l.URL})
		}
	}
	for _, lang := range langCodes() {
		path := filepath.Join(s.dir(user), "_index."+lang+".md")
		t := p.Text[lang]
		if !slices.Contains(p.Langs, lang) {
			// A language the CV no longer has keeps its text, and gets the
			// shared fields, so its file never disagrees with the others.
			var old profileFile
			body, err := readMarkdown(path, &old)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			t = old.ProfileText
			t.Summary = body
		}
		f := profileFile{
			Name: Clean(p.Name), Languages: p.Langs, Email: Clean(p.Email), Phone: Clean(p.Phone),
			Website: Clean(p.Website), Links: links,
			Order:       SectionOrder(p.Order),
			Theme:       p.Theme,
			Spacing:     p.Spacing,
			ProfileText: ProfileText{Headline: Clean(t.Headline), Location: Clean(t.Location)},
		}
		if err := writeMarkdown(path, f, t.Summary); err != nil {
			return err
		}
	}
	return nil
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

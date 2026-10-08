package cv

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// A share link is published in every language of the CV: one file per
// language, all with the same fields. The link's own language (that of its
// version) opens at /<slug>/, the others at /<slug>/<lang>/.

type Link struct {
	Slug    string   `json:"slug"`
	Label   string   `json:"label"`
	Lang    string   `json:"lang"` // opens at /<slug>/; the other languages at /<slug>/<lang>/
	Entries []string `json:"entries"`
	Photo   bool     `json:"photo"`
	Spacing float64  `json:"spacing"` // as in PrintOptions
	Order   []string `json:"order"`
	Version string   `json:"version"` // the version it shows
	Theme   Theme    `json:"theme"`
	Expires string   `json:"expires"` // YYYY-MM-DD, the link stops working at the start of this day; empty: never
	Created string   `json:"created"`

	langs []string // the languages it has files for
}

func (l Link) Print() PrintOptions {
	return PrintOptions{Lang: l.Lang, Entries: l.Entries, Photo: l.Photo, Spacing: l.Spacing, Order: l.Order, Theme: l.Theme}
}

type linkFile struct {
	Title      string   `yaml:"title"`
	URL        string   `yaml:"url"`
	Entries    []string `yaml:"entries"`
	Photo      bool     `yaml:"photo"`
	Spacing    float64  `yaml:"spacing,omitempty"`
	Order      []string `yaml:"order,omitempty"`
	Version    string   `yaml:"version,omitempty"`
	Theme      Theme    `yaml:"theme,omitempty"`
	ExpiryDate quoted   `yaml:"expiryDate,omitempty"` // none: the link does not expire
	Created    quoted   `yaml:"created"`
}

func (l Link) ExpiresAt() time.Time {
	t, err := time.ParseInLocation("2006-01-02", l.Expires, LinkZone)
	if err != nil {
		return time.Time{} // unparsable: treat as already expired
	}
	return t
}

func (l Link) Expired(now time.Time) bool { return l.Expires != "" && !now.Before(l.ExpiresAt()) }

// quoted forces YAML double quotes, so "2023-09" and "2026-12-01" stay
// LinkDir is where a link's page in lang lives, relative to the webroot.
func (l Link) LinkDir(lang string) string {
	if lang == l.Lang {
		return l.Slug
	}
	return filepath.Join(l.Slug, lang)
}

func (s *Store) Links(user string) ([]Link, error) {
	files, _ := filepath.Glob(filepath.Join(s.dir(user), "links", "*.md"))
	bySlug := map[string]*Link{}
	for _, path := range files {
		slug, lang, ok := splitLangFile(filepath.Base(path))
		if !ok {
			continue
		}
		var f linkFile
		if _, err := readMarkdown(path, &f); err != nil {
			return nil, err
		}
		l := bySlug[slug]
		if l == nil {
			l = &Link{Slug: slug}
			bySlug[slug] = l
		}
		l.langs = append(l.langs, lang)
		// The files agree on everything but the URL; the one at /<slug>/
		// is the link's own language. Links from before the toggle have a
		// single file, which is then that language.
		if l.Lang == "" || f.URL == "/"+slug+"/" {
			*l = Link{
				Slug: slug, Label: f.Title, Lang: lang, Entries: f.Entries, Photo: f.Photo, Spacing: f.Spacing, Order: f.Order, Version: f.Version, Theme: f.Theme.fromFile(),
				Expires: string(f.ExpiryDate), Created: string(f.Created), langs: l.langs,
			}
		}
	}
	links := []Link{}
	for _, l := range bySlug {
		links = append(links, *l)
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Created != links[j].Created {
			return links[i].Created > links[j].Created
		}
		return links[i].Slug < links[j].Slug
	})
	return links, nil
}

func (s *Store) Link(user, slug string) (Link, error) {
	links, err := s.Links(user)
	if err != nil {
		return Link{}, err
	}
	for _, l := range links {
		if l.Slug == slug {
			return l, nil
		}
	}
	return Link{}, ErrNotFound
}

func (l Link) Validate() error {
	if !IDRe.MatchString(l.Slug) {
		return fmt.Errorf("invalid slug %q", l.Slug)
	}
	if l.Expires != "" && !dayRe.MatchString(l.Expires) {
		return errors.New("expiry must be a date (YYYY-MM-DD)")
	}
	if len(l.Entries) == 0 {
		return errors.New("select at least one item")
	}
	return l.Print().Validate()
}

// SaveLink writes a link in each of the CV's languages, and removes the
// files of languages it no longer has.
func (s *Store) SaveLink(user string, l Link) error {
	if err := l.Validate(); err != nil {
		return err
	}
	langs, err := s.Langs(user)
	if err != nil {
		return err
	}
	if !slices.Contains(langs, l.Lang) {
		return fmt.Errorf("the CV has no %s", LanguageOf(l.Lang).English)
	}
	for _, lang := range langCodes() {
		path := filepath.Join(s.dir(user), "links", l.Slug+"."+lang+".md")
		if !slices.Contains(langs, lang) {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		f := linkFile{
			Title: Clean(l.Label), URL: "/" + filepath.ToSlash(l.LinkDir(lang)) + "/",
			Entries: l.Entries, Photo: l.Photo, Spacing: l.Spacing, Order: l.Order, Version: l.Version, Theme: l.Theme,
			ExpiryDate: quoted(l.Expires), Created: quoted(l.Created),
		}
		if err := writeMarkdown(path, f, ""); err != nil {
			return err
		}
	}
	return nil
}

// UpgradeLinks brings links in line with the CV's languages: links from
// before the language toggle, and links of a CV that gained or lost a
// language. It reports whether anything changed.
func (s *Store) UpgradeLinks(user string) (bool, error) {
	links, err := s.Links(user)
	if err != nil {
		return false, err
	}
	langs, err := s.Langs(user)
	if err != nil {
		return false, err
	}
	changed := false
	for _, l := range links {
		if !sameSet(l.langs, langs) {
			if err := s.SaveLink(user, l); err != nil {
				return changed, fmt.Errorf("link %s: %w", l.Slug, err)
			}
			changed = true
		}
	}
	return changed, nil
}

func (s *Store) DeleteLink(user, slug string) error {
	if _, err := s.Link(user, slug); err != nil {
		return err
	}
	for _, lang := range langCodes() {
		if err := os.Remove(filepath.Join(s.dir(user), "links", slug+"."+lang+".md")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// NewSlug is a readable label plus a random suffix, so share links can't be
// guessed: 8 base32 characters, 2^40 ≈ 1.1e12 possibilities per label.
func NewSlug(label string) string {
	suffix := strings.ToLower(rand.Text()[:8])
	base := slugify(label)
	if len(base) > 24 {
		base = strings.Trim(base[:24], "-")
	}
	if base == "" {
		return suffix
	}
	return base + "-" + suffix
}

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

// A share link is one file, links/<slug>.md, published as one page at
// /<slug>/ with every language of the CV in it. The page opens in the
// link's language; the others are a click away, at /<slug>/#<lang>.

type Link struct {
	Slug    string   `json:"slug"`
	Label   string   `json:"label"` // the version's name
	Lang    string   `json:"lang"`  // the language the page opens in
	Entries []string `json:"entries"`
	Photo   bool     `json:"photo"`
	Spacing float64  `json:"spacing"` // as in PrintOptions
	Order   []string `json:"order"`
	Version string   `json:"version"` // the version it shows
	Theme   Theme    `json:"theme"`
	Expires string   `json:"expires"` // YYYY-MM-DD, the link stops working at the start of this day; empty: never
	Created string   `json:"created"`
}

func (l Link) Print() PrintOptions {
	return PrintOptions{Lang: l.Lang, Entries: l.Entries, Photo: l.Photo, Spacing: l.Spacing, Order: l.Order, Theme: l.Theme}
}

// linkFile is links/<slug>.md.
type linkFile struct {
	Name    string   `yaml:"name"`
	Lang    string   `yaml:"lang"`
	Version string   `yaml:"version,omitempty"`
	Entries []string `yaml:"entries"`
	Photo   bool     `yaml:"photo"`
	Spacing float64  `yaml:"spacing,omitempty"`
	Order   []string `yaml:"order,omitempty"`
	Theme   Theme    `yaml:"theme,omitempty"`
	Expires quoted   `yaml:"expires,omitempty"` // none: the link does not expire
	Created quoted   `yaml:"created"`
}

// legacyLinkFile is links/<slug>.<lang>.md, from before format 2: one per
// language, alike but for the url, which is /<slug>/ in the link's own
// language.
type legacyLinkFile struct {
	Title      string   `yaml:"title"`
	URL        string   `yaml:"url"`
	Entries    []string `yaml:"entries"`
	Photo      bool     `yaml:"photo"`
	Spacing    float64  `yaml:"spacing,omitempty"`
	Order      []string `yaml:"order,omitempty"`
	Version    string   `yaml:"version,omitempty"`
	Theme      Theme    `yaml:"theme,omitempty"`
	ExpiryDate quoted   `yaml:"expiryDate,omitempty"`
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

func (s *Store) linkPath(user, slug string) string {
	return filepath.Join(s.dir(user), "links", slug+".md")
}

// Links reads a CV's share links, also those from before format 2 that
// Migrate hasn't moved over yet: a link must never seem gone, or its page
// would be taken offline.
func (s *Store) Links(user string) ([]Link, error) {
	files, _ := filepath.Glob(filepath.Join(s.dir(user), "links", "*.md"))
	bySlug := map[string]Link{}
	legacy := map[string]bool{}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		if IDRe.MatchString(name) {
			var f linkFile
			if _, err := readMarkdown(path, &f); err != nil {
				return nil, err
			}
			bySlug[name] = Link{
				Slug: name, Label: f.Name, Lang: f.Lang, Entries: f.Entries, Photo: f.Photo, Spacing: f.Spacing, Order: f.Order,
				Version: f.Version, Theme: f.Theme, Expires: string(f.Expires), Created: string(f.Created),
			}
			delete(legacy, name)
			continue
		}
		slug, lang, ok := splitLangFile(filepath.Base(path))
		if _, done := bySlug[slug]; !ok || done && !legacy[slug] {
			continue
		}
		var f legacyLinkFile
		if _, err := readMarkdown(path, &f); err != nil {
			return nil, err
		}
		// The file at /<slug>/ is the link's own language. Links from
		// before languages could be chosen have a single file.
		if _, seen := bySlug[slug]; !seen || f.URL == "/"+slug+"/" {
			bySlug[slug] = Link{
				Slug: slug, Label: f.Title, Lang: lang, Entries: f.Entries, Photo: f.Photo, Spacing: f.Spacing, Order: f.Order,
				Version: f.Version, Theme: f.Theme, Expires: string(f.ExpiryDate), Created: string(f.Created),
			}
			legacy[slug] = true
		}
	}
	links := []Link{}
	for _, l := range bySlug {
		if l.Entries == nil {
			l.Entries = []string{}
		}
		links = append(links, l)
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

// SaveLink writes a link, in place of its files from before format 2.
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
	return s.writeLink(user, l)
}

func (s *Store) writeLink(user string, l Link) error {
	f := linkFile{
		Name: Clean(l.Label), Lang: l.Lang, Version: l.Version,
		Entries: l.Entries, Photo: l.Photo, Spacing: l.Spacing, Order: l.Order, Theme: l.Theme,
		Expires: quoted(l.Expires), Created: quoted(l.Created),
	}
	if err := writeMarkdown(s.linkPath(user, l.Slug), f, ""); err != nil {
		return err
	}
	return s.removeLegacyLink(user, l.Slug)
}

func (s *Store) removeLegacyLink(user, slug string) error {
	for _, lang := range langCodes() {
		if err := os.Remove(filepath.Join(s.dir(user), "links", slug+"."+lang+".md")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// UpgradeLinks makes links of a CV that lost their language open in its
// main language instead. It reports whether anything changed.
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
		if !slices.Contains(langs, l.Lang) {
			l.Lang = langs[0]
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
	if err := os.Remove(s.linkPath(user, slug)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return s.removeLegacyLink(user, slug)
}

// NewSlug is random, so share links can't be guessed: 12 base32 characters,
// 2^60 ≈ 1.2e18 possibilities. It has no name in it, which would tell whoever
// gets one link what the version was made for.
func NewSlug() string {
	return strings.ToLower(rand.Text()[:12])
}

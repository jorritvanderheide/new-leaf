package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Content layout per user:
//
//	_index.<lang>.md                  profile; body is the summary
//	photo.{jpg,png,webp}              optional profile photo
//	<section>/<id>.<lang>.md          one CV item; body is the description
//	links/<slug>.<lang>.md            one share link, rendered in <lang>

var (
	// Sections in their default order on a CV. Their titles are in
	// languages.go and web/editor/app.js (SECTION_NAMES).
	Sections = []string{"experience", "education", "publications", "output", "presentations", "teaching", "awards", "extracurricular", "volunteering"}
	// Sections whose items happen at one moment: a date, not a period. The
	// date may be left out (e.g. a manuscript under review).
	PointSections = []string{"publications", "output", "presentations", "awards"}

	urlRe     = regexp.MustCompile(`^https?://[^\s]+$`)
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	dateRe    = regexp.MustCompile(`^\d{4}(-(0[1-9]|1[0-2]))?$`) // YYYY-MM or YYYY
	dayRe     = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$`)
	nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

	errNotFound = errors.New("not found")
	photoExts   = []string{".jpg", ".png", ".webp"}
)

// Links expire at midnight in this zone.
var linkZone = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		panic(err)
	}
	return loc
}()

type Store struct{ Root string }

type ItemText struct {
	Title    string `json:"title"    yaml:"title"`
	Org      string `json:"org"      yaml:"org,omitempty"`
	Location string `json:"location" yaml:"location,omitempty"`
	Body     string `json:"body"     yaml:"-"`
}

type Item struct {
	Section string              `json:"section"`
	ID      string              `json:"id"`
	Start   string              `json:"start"`
	End     string              `json:"end"`  // empty: ongoing, or a single date in a point section
	Link    string              `json:"link"` // optional URL, e.g. a DOI
	Text    map[string]ItemText `json:"text"`
}

type itemFile struct {
	ItemText `yaml:",inline"`
	Start    quoted `yaml:"start"`
	End      quoted `yaml:"end,omitempty"`
	Link     string `yaml:"link,omitempty"`
}

type ProfileText struct {
	Headline string `json:"headline" yaml:"headline,omitempty"`
	Location string `json:"location" yaml:"location,omitempty"` // e.g. "Eindhoven, the Netherlands"
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

// PrintOptions select what a PDF shows and how it is laid out.
type PrintOptions struct {
	Lang    string   `json:"lang"` // opens at /<slug>/; the other languages at /<slug>/<lang>/
	Entries []string `json:"entries"`
	Photo   bool     `json:"photo"`
	Spacing float64  `json:"spacing"`         // whitespace scale, MinSpacing..MaxSpacing; 0 means 1
	Order   []string `json:"order,omitempty"` // section order; empty: the profile's
	Theme   Theme    `json:"theme"`
}

const MinSpacing, MaxSpacing = 0.4, 1.4

func (o PrintOptions) Validate() error {
	if !knownLang(o.Lang) {
		return fmt.Errorf("unknown language %q", o.Lang)
	}
	for _, e := range o.Entries {
		if !validEntry(e) {
			return fmt.Errorf("invalid item %q", e)
		}
	}
	for _, s := range o.Order {
		if sectionIndex(s) < 0 {
			return fmt.Errorf("unknown section %q", s)
		}
	}
	if err := o.Theme.Validate(); err != nil {
		return err
	}
	if o.Spacing != 0 && (o.Spacing < MinSpacing || o.Spacing > MaxSpacing) {
		return fmt.Errorf("spacing must be between %g and %g", MinSpacing, MaxSpacing)
	}
	return nil
}

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
	t, err := time.ParseInLocation("2006-01-02", l.Expires, linkZone)
	if err != nil {
		return time.Time{} // unparsable: treat as already expired
	}
	return t
}

func (l Link) Expired(now time.Time) bool { return l.Expires != "" && !now.Before(l.ExpiresAt()) }

// quoted forces YAML double quotes, so "2023-09" and "2026-12-01" stay
// strings for every YAML parser instead of becoming dates.
type quoted string

func (q quoted) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: string(q)}, nil
}

func (s *Store) dir(user string) string { return filepath.Join(s.Root, user, "content") }

// Init creates an empty CV for a new user.
func (s *Store) Init(user string) error {
	dir := s.dir(user)
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return s.SaveProfile(user, Profile{Langs: []string{"en"}})
}

// Users lists everyone with a content directory.
func (s *Store) Users() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var users []string
	for _, e := range entries {
		if e.IsDir() && userNameRe.MatchString(e.Name()) {
			users = append(users, e.Name())
		}
	}
	return users, err
}

// ContentDir is where a user's content files live.
func (s *Store) ContentDir(user string) string { return s.dir(user) }

// --- profile

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
			links = append(links, ProfileLink{Label: clean(l.Label), URL: l.URL})
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
			Name: clean(p.Name), Languages: p.Langs, Email: clean(p.Email), Phone: clean(p.Phone),
			Website: clean(p.Website), Links: links,
			Order:       SectionOrder(p.Order),
			Theme:       p.Theme,
			Spacing:     p.Spacing,
			ProfileText: ProfileText{Headline: clean(t.Headline), Location: clean(t.Location)},
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

// --- compose settings: from before versions, the composer's last selection,
// read once to make the first version (see EnsureVersions).

func (s *Store) composePath(user string) string { return filepath.Join(s.Root, user, "compose.json") }

// Compose returns the saved composer settings, or nil if there are none.
func (s *Store) Compose(user string) (*PrintOptions, error) {
	data, err := os.ReadFile(s.composePath(user))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var o PrintOptions
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("%s: %w", s.composePath(user), err)
	}
	return &o, nil
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
	return writeAtomic(filepath.Join(s.dir(user), "photo"+ext), data)
}

func (s *Store) DeletePhoto(user string) error {
	for _, ext := range photoExts {
		if err := os.Remove(filepath.Join(s.dir(user), "photo"+ext)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// --- items

func (s *Store) Items(user string) ([]Item, error) {
	var items []Item
	for _, section := range Sections {
		byID := map[string]*Item{}
		files, _ := filepath.Glob(filepath.Join(s.dir(user), section, "*.md"))
		for _, path := range files {
			id, lang, ok := splitLangFile(filepath.Base(path))
			if !ok {
				continue
			}
			var f itemFile
			body, err := readMarkdown(path, &f)
			if err != nil {
				return nil, err
			}
			it := byID[id]
			if it == nil {
				it = &Item{Section: section, ID: id, Text: map[string]ItemText{}}
				byID[id] = it
			}
			it.Start, it.End, it.Link = string(f.Start), string(f.End), f.Link
			f.ItemText.Body = body
			it.Text[lang] = f.ItemText
		}
		for _, it := range byID {
			items = append(items, *it)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Section != items[j].Section {
			return sectionIndex(items[i].Section) < sectionIndex(items[j].Section)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (it Item) Validate() error {
	if sectionIndex(it.Section) < 0 {
		return fmt.Errorf("unknown section %q", it.Section)
	}
	if !idRe.MatchString(it.ID) {
		return fmt.Errorf("invalid id %q", it.ID)
	}
	if IsPointSection(it.Section) {
		if it.Start != "" && !dateRe.MatchString(it.Start) {
			return errors.New("date must be YYYY-MM, YYYY or empty")
		}
		if it.End != "" {
			return fmt.Errorf("%s items have a single date, not an end", it.Section)
		}
	} else if !dateRe.MatchString(it.Start) {
		return errors.New("start must be YYYY-MM or YYYY")
	}
	if it.End != "" && !dateRe.MatchString(it.End) {
		return errors.New("end must be YYYY-MM, YYYY or empty")
	}
	if link := strings.TrimSpace(it.Link); link != "" && !urlRe.MatchString(link) {
		return errors.New("link must be a URL starting with http:// or https://")
	}
	if it.End != "" && it.End < it.Start {
		return errors.New("end is before start")
	}
	return nil
}

// NewItemID derives a readable, unused id from the item's organisation or title.
func (s *Store) NewItemID(user string, it Item) string {
	base := ""
	for _, lang := range langCodes() {
		if base = slugify(it.Text[lang].Org); base == "" {
			base = slugify(it.Text[lang].Title)
		}
		if base != "" {
			break
		}
	}
	if base == "" {
		base = "item"
	}
	id := base
	for n := 2; s.itemExists(user, it.Section, id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func (s *Store) itemExists(user, section, id string) bool {
	m, _ := filepath.Glob(filepath.Join(s.dir(user), section, id+".*.md"))
	return len(m) > 0
}

// SaveItem writes an item in the CV's languages. A language the CV no
// longer has keeps its text, but gets the new dates and link.
func (s *Store) SaveItem(user string, it Item) error {
	if err := it.Validate(); err != nil {
		return err
	}
	langs, err := s.Langs(user)
	if err != nil {
		return err
	}
	for _, lang := range langCodes() {
		path := filepath.Join(s.dir(user), it.Section, it.ID+"."+lang+".md")
		t := it.Text[lang]
		if !slices.Contains(langs, lang) {
			var old itemFile
			body, err := readMarkdown(path, &old)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			t = old.ItemText
			t.Body = body
		}
		f := itemFile{
			ItemText: ItemText{Title: clean(t.Title), Org: clean(t.Org), Location: clean(t.Location)},
			Start:    quoted(it.Start),
			End:      quoted(it.End),
			Link:     strings.TrimSpace(it.Link),
		}
		if err := writeMarkdown(path, f, t.Body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteItem(user, section, id string) error {
	if sectionIndex(section) < 0 || !idRe.MatchString(id) {
		return errNotFound
	}
	if !s.itemExists(user, section, id) {
		return errNotFound
	}
	for _, lang := range langCodes() {
		if err := os.Remove(filepath.Join(s.dir(user), section, id+"."+lang+".md")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// --- links
//
// A link is published in every language: one file per language, all with
// the same fields. The link's own language (chosen when it was made) opens
// at /<slug>/, the others at /<slug>/<lang>/.

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
	return Link{}, errNotFound
}

func (l Link) Validate() error {
	if !idRe.MatchString(l.Slug) {
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
		return fmt.Errorf("the CV has no %s", language(l.Lang).English)
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
			Title: clean(l.Label), URL: "/" + filepath.ToSlash(l.LinkDir(lang)) + "/",
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

// --- helpers

func validEntry(e string) bool {
	section, id, ok := strings.Cut(e, "/")
	return ok && sectionIndex(section) >= 0 && idRe.MatchString(id)
}

func sectionIndex(s string) int { return indexOf(Sections, s) }

// IsPointSection reports whether a section's items have one date (start) instead of a period.
func IsPointSection(s string) bool { return indexOf(PointSections, s) >= 0 }

// SectionOrder cleans a user's section order: unknown and duplicate names
// are dropped and missing sections are appended in their default order.
func SectionOrder(order []string) []string {
	var out []string
	for _, s := range append(append([]string{}, order...), Sections...) {
		if sectionIndex(s) >= 0 && indexOf(out, s) < 0 {
			out = append(out, s)
		}
	}
	return out
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// splitLangFile splits "alltrons.en.md" into ("alltrons", "en").
func splitLangFile(name string) (id, lang string, ok bool) {
	base, found := strings.CutSuffix(name, ".md")
	if !found {
		return "", "", false
	}
	dot := strings.LastIndexByte(base, '.')
	if dot < 0 {
		return "", "", false
	}
	id, lang = base[:dot], base[dot+1:]
	return id, lang, idRe.MatchString(id) && knownLang(lang)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("é", "e", "è", "e", "ë", "e", "á", "a", "à", "a", "ä", "a", "ö", "o", "ó", "o",
		"ü", "u", "ú", "u", "ï", "i", "í", "i", "ç", "c", "ñ", "n", "ß", "ss", "&", "and").Replace(s)
	return strings.Trim(nonSlugRe.ReplaceAllString(s, "-"), "-")
}

// clean keeps single-line fields single-line.
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func readMarkdown(path string, frontMatter any) (body string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return "", fmt.Errorf("%s: missing front matter", path)
	}
	if after, empty := bytes.CutPrefix(rest, []byte("---\n")); empty {
		return strings.TrimSpace(string(after)), nil
	}
	fm, body2, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		// Front matter that runs to EOF without a trailing newline.
		if fm, ok = bytes.CutSuffix(rest, []byte("\n---")); !ok {
			return "", fmt.Errorf("%s: unterminated front matter", path)
		}
	}
	if err := yaml.Unmarshal(fm, frontMatter); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return strings.TrimSpace(string(body2)), nil
}

func writeMarkdown(path string, frontMatter any, body string) error {
	fm, err := yaml.Marshal(frontMatter)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	if body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return writeAtomic(path, b.Bytes())
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// sameSet reports whether two lists hold the same strings, in any order.
func sameSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(s string) bool { return !slices.Contains(b, s) })
}

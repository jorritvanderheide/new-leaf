package cv

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A version is one tailored selection of a CV, e.g. for an application:
// its own items, section order, language and layout, and optionally a share
// link that always shows it. Items and the profile are shared by all
// versions. Stored as users/<cv>/versions/<id>.json.
type Version struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	PrintOptions
	Pages   int    `json:"pages"`   // page count at the last preview, for the overview
	Fit     int    `json:"fit"`     // the page count to fit on, 1 to 4; 0 means 2
	Created string `json:"created"` // YYYY-MM-DD
	Updated string `json:"updated"` // RFC 3339
}

type versionFile struct {
	Name string `json:"name"`
	PrintOptions
	Pages   int    `json:"pages,omitempty"`
	Fit     int    `json:"fit,omitempty"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

func (s *Store) versionsDir(user string) string { return filepath.Join(s.Root, user, "versions") }

func (s *Store) versionPath(user, id string) string {
	return filepath.Join(s.versionsDir(user), id+".json")
}

func (v Version) Validate() error {
	if !IDRe.MatchString(v.ID) {
		return fmt.Errorf("invalid version id %q", v.ID)
	}
	if Clean(v.Name) == "" {
		return errors.New("give the version a name")
	}
	if v.Fit < 0 || v.Fit > 4 {
		return errors.New("fit on 1 to 4 pages")
	}
	if v.Pages < 0 || v.Pages > 99 {
		return errors.New("invalid page count")
	}
	return v.PrintOptions.Validate()
}

// Versions lists a CV's versions, the most recently edited first.
func (s *Store) Versions(user string) ([]Version, error) {
	files, err := filepath.Glob(filepath.Join(s.versionsDir(user), "*.json"))
	if err != nil {
		return nil, err
	}
	out := []Version{}
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		if !IDRe.MatchString(id) {
			continue
		}
		v, err := s.readVersion(path)
		if err != nil {
			return nil, err
		}
		v.ID = id
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Version) int {
		return cmp.Or(strings.Compare(b.Updated, a.Updated), strings.Compare(a.ID, b.ID))
	})
	return out, nil
}

func (s *Store) readVersion(path string) (Version, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Version{}, err
	}
	var f versionFile
	if err := json.Unmarshal(data, &f); err != nil {
		return Version{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.Entries == nil {
		f.Entries = []string{}
	}
	return Version{Name: f.Name, PrintOptions: f.PrintOptions, Pages: f.Pages, Fit: f.Fit, Created: f.Created, Updated: f.Updated}, nil
}

func (s *Store) Version(user, id string) (Version, error) {
	if !IDRe.MatchString(id) {
		return Version{}, ErrNotFound
	}
	v, err := s.readVersion(s.versionPath(user, id))
	if errors.Is(err, fs.ErrNotExist) {
		return Version{}, ErrNotFound
	}
	v.ID = id
	return v, err
}

func (s *Store) SaveVersion(user string, v Version) error {
	v.Name = Clean(v.Name)
	if err := v.Validate(); err != nil {
		return err
	}
	if langs, err := s.Langs(user); err != nil {
		return err
	} else if !slices.Contains(langs, v.Lang) {
		return fmt.Errorf("the CV has no %s", LanguageOf(v.Lang).English)
	}
	if v.Entries == nil {
		v.Entries = []string{}
	}
	v.Order = SectionOrder(v.Order)
	return s.writeVersion(user, v)
}

func (s *Store) writeVersion(user string, v Version) error {
	data, err := json.MarshalIndent(versionFile{
		Name: v.Name, PrintOptions: v.PrintOptions, Pages: v.Pages, Fit: v.Fit, Created: v.Created, Updated: v.Updated,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.versionsDir(user), 0o750); err != nil {
		return err
	}
	return WriteAtomic(s.versionPath(user, v.ID), append(data, '\n'))
}

func (s *Store) DeleteVersion(user, id string) error {
	if _, err := s.Version(user, id); err != nil {
		return err
	}
	return os.Remove(s.versionPath(user, id))
}

// NewVersionID derives a free ID from a name.
func (s *Store) NewVersionID(user, name string) string {
	base := slugify(name)
	if len(base) > 40 {
		base = strings.Trim(base[:40], "-")
	}
	if base == "" {
		base = "version"
	}
	id := base
	for n := 2; ; n++ {
		if _, err := os.Stat(s.versionPath(user, id)); errors.Is(err, fs.ErrNotExist) {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

// VersionLink is the share link that shows a version, if any.
func (s *Store) VersionLink(user, id string) (*Link, error) {
	links, err := s.Links(user)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if l.Version == id {
			return &l, nil
		}
	}
	return nil, nil
}

// EnsureVersions gives a CV from before versions its first ones: the
// composer's last settings become "Full CV", and every share link a version
// of its own that it keeps showing. Runs once, when there is no versions
// directory yet.
func (s *Store) EnsureVersions(user string) error {
	if _, err := os.Stat(s.versionsDir(user)); err == nil {
		return nil
	}
	profile, err := s.Profile(user)
	if err != nil {
		return err
	}
	items, err := s.Items(user)
	if err != nil {
		return err
	}
	now := time.Now()
	today, stamp := now.In(LinkZone).Format("2006-01-02"), now.UTC().Format(time.RFC3339)

	full := Version{
		ID: "full-cv", Name: "Full CV", Created: today, Updated: stamp,
		PrintOptions: PrintOptions{Lang: profile.Langs[0], Photo: profile.Photo, Spacing: cmp.Or(profile.Spacing, 1), Order: profile.Order, Theme: profile.Theme.ForNewVersion(), Entries: []string{}},
	}
	for _, it := range items {
		full.Entries = append(full.Entries, it.Section+"/"+it.ID)
	}
	compose, err := s.Compose(user)
	if err != nil {
		return err
	}
	if compose != nil {
		full.Lang, full.Photo, full.Spacing = compose.Lang, compose.Photo, cmp.Or(compose.Spacing, 1)
		full.Entries = compose.Entries
	}
	if err := s.SaveVersion(user, full); err != nil {
		return err
	}

	links, err := s.Links(user)
	if err != nil {
		return err
	}
	for i, l := range links {
		if l.Version != "" {
			continue
		}
		v := Version{
			ID: s.NewVersionID(user, cmp.Or(l.Label, l.Slug)), Name: cmp.Or(Clean(l.Label), l.Slug),
			PrintOptions: l.Print(), Created: cmp.Or(l.Created, today),
			// Older links first, so the list keeps their order.
			Updated: now.Add(-time.Duration(i+1) * time.Second).UTC().Format(time.RFC3339),
		}
		v.Order = profile.Order
		if err := s.SaveVersion(user, v); err != nil {
			return fmt.Errorf("link %s: %w", l.Slug, err)
		}
		l.Version, l.Order = v.ID, v.Order
		if err := s.SaveLink(user, l); err != nil {
			return fmt.Errorf("link %s: %w", l.Slug, err)
		}
	}
	if err := os.Remove(s.composePath(user)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// PrintOptions select what a PDF shows and how it is laid out.
type PrintOptions struct {
	Lang    string   `json:"lang"` // the language the PDF is in
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
		if SectionIndex(s) < 0 {
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

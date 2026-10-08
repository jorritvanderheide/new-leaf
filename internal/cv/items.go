package cv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

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
			return SectionIndex(items[i].Section) < SectionIndex(items[j].Section)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (it Item) Validate() error {
	if SectionIndex(it.Section) < 0 {
		return fmt.Errorf("unknown section %q", it.Section)
	}
	if !IDRe.MatchString(it.ID) {
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
	if len(base) > 40 {
		base = strings.Trim(base[:40], "-")
	}
	if base == "" {
		base = "item"
	}
	id := base
	for n := 2; s.ItemExists(user, it.Section, id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func (s *Store) ItemExists(user, section, id string) bool {
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
			ItemText: ItemText{Title: Clean(t.Title), Org: Clean(t.Org), Location: Clean(t.Location)},
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
	if SectionIndex(section) < 0 || !IDRe.MatchString(id) {
		return ErrNotFound
	}
	if !s.ItemExists(user, section, id) {
		return ErrNotFound
	}
	for _, lang := range langCodes() {
		if err := os.Remove(filepath.Join(s.dir(user), section, id+"."+lang+".md")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

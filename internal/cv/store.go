// Package cv is a CV and its rules: the files it is kept in (profile,
// items, versions, share links, backups), the languages and looks it can
// have, and the Document a version lays out to. It knows nothing of HTTP or
// Typst.
package cv

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Content layout per user:
//
//	_index.md                         profile: what it has once, and the format
//	_index.<lang>.md                  profile text; body is the summary
//	photo.{jpg,png,webp}              optional profile photo
//	<section>/<id>.<lang>.md          one CV item; body is the description
//	links/<slug>.md                   one share link, with every language

var (
	// Sections in their default order on a CV. Their titles are in
	// languages.go and web/editor/app.js (SECTION_NAMES).
	Sections = []string{"experience", "education", "publications", "output", "presentations", "teaching", "awards", "extracurricular", "volunteering"}
	// Sections whose items happen at one moment: a date, not a period. The
	// date may be left out (e.g. a manuscript under review).
	PointSections = []string{"publications", "output", "presentations", "awards"}

	urlRe     = regexp.MustCompile(`^https?://[^\s]+$`)
	IDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	dateRe    = regexp.MustCompile(`^\d{4}(-(0[1-9]|1[0-2]))?$`) // YYYY-MM or YYYY
	dayRe     = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$`)
	nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

	ErrNotFound = errors.New("not found")
	photoExts   = []string{".jpg", ".png", ".webp"}
)

// LinkZone is the time zone of dates: links expire at midnight in it, and
// it says which day it is. The system's, unless the server is given one
// (-timezone).
var LinkZone = time.Local

type Store struct{ Root string }

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

func validEntry(e string) bool {
	section, id, ok := strings.Cut(e, "/")
	return ok && SectionIndex(section) >= 0 && IDRe.MatchString(id)
}

func SectionIndex(s string) int { return indexOf(Sections, s) }

// IsPointSection reports whether a section's items have one date (start) instead of a period.
func IsPointSection(s string) bool { return indexOf(PointSections, s) >= 0 }

// SectionOrder cleans a user's section order: unknown and duplicate names
// are dropped and missing sections are appended in their default order.
func SectionOrder(order []string) []string {
	var out []string
	for _, s := range append(append([]string{}, order...), Sections...) {
		if SectionIndex(s) >= 0 && indexOf(out, s) < 0 {
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
	return id, lang, IDRe.MatchString(id) && knownLang(lang)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("é", "e", "è", "e", "ë", "e", "á", "a", "à", "a", "ä", "a", "ö", "o", "ó", "o",
		"ü", "u", "ú", "u", "ï", "i", "í", "i", "ç", "c", "ñ", "n", "ß", "ss", "&", "and").Replace(s)
	return strings.Trim(nonSlugRe.ReplaceAllString(s, "-"), "-")
}

// clean keeps single-line fields single-line.
func Clean(s string) string { return strings.Join(strings.Fields(s), " ") }

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
	return WriteAtomic(path, b.Bytes())
}

func WriteAtomic(path string, data []byte) error {
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

// invalid is a change a CV can't take, with the reason for the person who
// asked for it.
type Invalid struct{ error }

// userNameRe is what a CV's name (its folder) looks like.
var userNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidName reports whether s can name a CV.
func ValidName(s string) bool { return userNameRe.MatchString(s) }

var nonName = regexp.MustCompile(`[^a-z0-9-]+`)

// NameFor makes a CV name out of a login: lowercase letters, digits and
// dashes, at most 32. Empty if nothing is left.
func NameFor(login string) string {
	name := strings.Trim(nonName.ReplaceAllString(strings.ToLower(login), "-"), "-")
	if len(name) > 32 {
		name = strings.Trim(name[:32], "-")
	}
	if !ValidName(name) {
		return ""
	}
	return name
}

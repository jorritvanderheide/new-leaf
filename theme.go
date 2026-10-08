package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Theme is how a version looks. Empty fields mean the defaults.
type Theme struct {
	Accent string `json:"accent,omitempty" yaml:"accent,omitempty"` // #rrggbb
	Font   string `json:"font,omitempty"   yaml:"font,omitempty"`   // a key of themeFonts
	Photo  string `json:"photo,omitempty"  yaml:"photo,omitempty"`  // rounded, circle or square
}

const defaultAccent = "#00696a"

// themeFonts are the bundled fonts: the family for Typst (web/typst/fonts)
// and the CSS font stack for share pages (web/share/fonts). The first is
// the default.
var themeFonts = []struct{ Key, Typst, CSS string }{
	{"sans", "Inter", `"Inter", ui-sans-serif, system-ui, sans-serif`},
	{"serif", "Source Serif 4", `"Source Serif 4", ui-serif, Georgia, serif`},
}

// photoRadius is each photo shape's corner radius on share pages; the PDF
// has its own in cv.typ.
var photoRadius = map[string]string{"rounded": "1rem", "circle": "9999px", "square": "0"}

var accentRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (t Theme) Validate() error {
	if t.Accent != "" && !accentRe.MatchString(t.Accent) {
		return fmt.Errorf("accent must be a colour like #00696a, not %q", t.Accent)
	}
	if t.Font != "" && t.font().Key != t.Font {
		return fmt.Errorf("unknown font %q", t.Font)
	}
	if _, ok := photoRadius[t.Photo]; t.Photo != "" && !ok {
		return fmt.Errorf("unknown photo shape %q", t.Photo)
	}
	return nil
}

// Resolved fills in the defaults.
func (t Theme) Resolved() Theme {
	if t.Accent == "" {
		t.Accent = defaultAccent
	}
	t.Accent = strings.ToLower(t.Accent)
	t.Font = t.font().Key
	if _, ok := photoRadius[t.Photo]; !ok {
		t.Photo = "rounded"
	}
	return t
}

func (t Theme) font() struct{ Key, Typst, CSS string } {
	for _, f := range themeFonts {
		if f.Key == t.Font {
			return f
		}
	}
	return themeFonts[0]
}

// css is a stylesheet that gives a share page the theme, over share.css,
// and its name in the webroot: the same theme, the same file.
func (t Theme) css() (name string, data []byte) {
	t = t.Resolved()
	data = fmt.Appendf(nil, ":root{--color-accent:%s;--color-accent-dark:color-mix(in oklab,%s 82%%,black);--font-sans:%s;--photo-radius:%s}\n",
		t.Accent, t.Accent, t.font().CSS, photoRadius[t.Photo])
	sum := sha256.Sum256(data)
	return "css/theme." + hex.EncodeToString(sum[:4]) + ".css", data
}

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Share pages are rendered from the same Document as the PDF, so what a
// visitor sees online and in the PDF always match.

var shareLabels = map[string]map[string]string{
	"en": {"download": "Download PDF", "expires": "This link expires on %s.", "language": "English"},
	"nl": {"download": "Download pdf", "expires": "Deze link verloopt op %s.", "language": "Nederlands"},
}

var longMonths = map[string][]string{
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	"nl": {"januari", "februari", "maart", "april", "mei", "juni", "juli", "augustus", "september", "oktober", "november", "december"},
}

func longDate(t time.Time, lang string) string {
	m := longMonths[lang][t.Month()-1]
	if lang == "en" {
		return fmt.Sprintf("%s %d, %d", m, t.Day(), t.Year())
	}
	return fmt.Sprintf("%d %s %d", t.Day(), m, t.Year())
}

type sharePage struct {
	Doc      Document
	CSS      string // relative URL of the stylesheet
	Langs    []shareLang
	Download string
	Expires  string
	Photo    template.URL // inlined thumbnail, so the page is one file
}

type shareLang struct {
	Lang, Code, Label, URL string
	Current                bool
}

// shareCSS is the stylesheet's name in the webroot; the hash in it lets
// visitors cache it forever.
func (s *Server) shareCSS() string {
	return "css/share." + s.assets.Version("web/share/share.css") + ".css"
}

// renderShare renders a link's page in one language. The link's own
// language lives at /<slug>/, the others at /<slug>/<lang>/.
func (s *Server) renderShare(user string, l Link, lang string) ([]byte, error) {
	profile, err := s.store.Profile(user)
	if err != nil {
		return nil, err
	}
	items, err := s.store.Items(user)
	if err != nil {
		return nil, err
	}
	opt := l.Print()
	opt.Lang = lang
	page := sharePage{
		Doc:      BuildDocument(profile, items, opt, ""),
		Download: shareLabels[lang]["download"],
		Expires:  fmt.Sprintf(shareLabels[lang]["expires"], longDate(l.ExpiresAt(), lang)),
	}
	if l.Photo {
		if page.Photo, err = thumbnail(s.store.PhotoPath(user)); err != nil {
			return nil, err
		}
	}
	root := "../"
	if lang != l.Lang {
		root = "../../"
	}
	page.CSS = root + s.shareCSS()
	for _, target := range Langs {
		sl := shareLang{Lang: target, Code: strings.ToUpper(target), Label: shareLabels[target]["language"], Current: target == lang}
		switch {
		case target == l.Lang && lang == l.Lang:
			sl.URL = "./"
		case target == l.Lang:
			sl.URL = "../"
		case lang == l.Lang:
			sl.URL = target + "/"
		default:
			sl.URL = "../" + target + "/"
		}
		page.Langs = append(page.Langs, sl)
	}

	t, err := s.assets.Template("web/share/page.html")
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, page); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// --- photo

var thumbs sync.Map // path, size and mtime -> data URL

// thumbnail is the profile photo cropped to a square of 320 px, as a JPEG
// data URL. Empty when there is no photo.
func thumbnail(path string) (template.URL, error) {
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s %d %d", path, info.Size(), info.ModTime().UnixNano())
	if u, ok := thumbs.Load(key); ok {
		return u.(template.URL), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("photo: %w", err)
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(0, 0, side, side).Add(image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2))
	const size = 320
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	u := template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()))
	thumbs.Store(key, u)
	return u, nil
}

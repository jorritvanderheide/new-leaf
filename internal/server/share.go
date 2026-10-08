package server

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	"image/jpeg"

	"codeberg.org/BW20/new-leaf/internal/cv"

	_ "image/png"
	"os"
	"strings"
	"sync"

	"golang.org/x/image/draw"

	_ "golang.org/x/image/webp"
)

// Share pages are rendered from the same Document as the PDF, so what a
// visitor sees online and in the PDF always match.

type sharePage struct {
	Doc      cv.Document
	CSS      string // relative URL of the stylesheet
	ThemeCSS string // and of the theme's, which overrides colour, font and photo shape
	Langs    []shareLang
	Download string
	Expires  string       // when the link stops working; empty if it does not
	Photo    template.URL // inlined thumbnail, so the page is one file
}

type shareLang struct {
	Lang, Code, Label, URL string
	Current                bool
}

// shareCSS is the stylesheet's name in the webroot; the hash in it lets
// visitors cache it forever.
func (s *Server) shareCSS() string {
	return "css/share." + s.assets.Version("share/share.css") + ".css"
}

// renderShare renders a link's page in one language. The link's own
// language lives at /<slug>/, the others at /<slug>/<lang>/.
func (s *Server) renderShare(user string, l cv.Link, lang string) ([]byte, error) {
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
		Doc:      cv.BuildDocument(profile, items, opt, ""),
		Download: cv.LanguageOf(lang).DownloadLabel,
	}
	if l.Expires != "" {
		page.Expires = fmt.Sprintf(cv.LanguageOf(lang).ExpiryNote, cv.LanguageOf(lang).LongDate(l.ExpiresAt()))
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
	themeCSS, _ := l.Theme.CSS()
	page.ThemeCSS = root + themeCSS
	for _, target := range profile.Langs {
		sl := shareLang{Lang: target, Code: strings.ToUpper(target), Label: cv.LanguageOf(target).Name, Current: target == lang}
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

	t, err := s.assets.Template("share/page.html")
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

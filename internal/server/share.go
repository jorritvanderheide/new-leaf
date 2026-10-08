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
	"slices"
	"strings"
	"sync"

	"golang.org/x/image/draw"

	_ "golang.org/x/image/webp"
)

// Share pages are rendered from the same Document as the PDF, so what a
// visitor sees online and in the PDF always match. A link is one page with
// every language of the CV in it: the link's own shows, and the others are
// at #<lang> on the same page, shown by the stylesheet alone (share.css).

type sharePage struct {
	Lang     string // the language the page opens in
	Name     string
	CSS      string // relative URL of the stylesheet
	ThemeCSS string // and of the theme's, which overrides colour, font and photo shape
	Langs    []shareLang
	Blocks   []shareBlock // one per language, the page's own first
	Photo    template.URL // inlined thumbnail, so the page is one file
}

// shareBlock is the CV in one language, with what goes around it.
type shareBlock struct {
	Lang     string
	Doc      cv.Document
	PDF      string // its URL, relative to the page
	Download string
	Expires  string // when the link stops working; empty if it does not
}

type shareLang struct {
	Lang, Code, Label string
}

// shareCSS is the stylesheet's name in the webroot; the hash in it lets
// visitors cache it forever.
func (s *Server) shareCSS() string {
	return "css/share." + s.assets.Version("share/share.css") + ".css"
}

// sharePDF is where a link's PDF in lang is, relative to its page: next to
// it in the link's own language, in <lang>/ for the others.
func sharePDF(l cv.Link, lang string) string {
	if lang == l.Lang {
		return "cv.pdf"
	}
	return lang + "/cv.pdf"
}

// renderShare renders a link's page, at /<slug>/, in the CV's languages.
func (s *Server) renderShare(user string, l cv.Link, langs []string) ([]byte, error) {
	profile, err := s.store.Profile(user)
	if err != nil {
		return nil, err
	}
	items, err := s.store.Items(user)
	if err != nil {
		return nil, err
	}
	page := sharePage{Lang: l.Lang, Name: profile.Name, CSS: "../" + s.shareCSS()}
	themeCSS, _ := l.Theme.CSS()
	page.ThemeCSS = "../" + themeCSS
	if l.Photo {
		if page.Photo, err = thumbnail(s.store.PhotoPath(user)); err != nil {
			return nil, err
		}
	}
	order := append([]string{l.Lang}, slices.DeleteFunc(slices.Clone(langs), func(lang string) bool { return lang == l.Lang })...)
	for _, lang := range langs {
		page.Langs = append(page.Langs, shareLang{Lang: lang, Code: strings.ToUpper(lang), Label: cv.LanguageOf(lang).Name})
	}
	for _, lang := range order {
		opt := l.Print()
		opt.Lang = lang
		b := shareBlock{Lang: lang, Doc: cv.BuildDocument(profile, items, opt, ""), PDF: sharePDF(l, lang), Download: cv.LanguageOf(lang).DownloadLabel}
		if l.Expires != "" {
			b.Expires = fmt.Sprintf(cv.LanguageOf(lang).ExpiryNote, cv.LanguageOf(lang).LongDate(l.ExpiresAt()))
		}
		page.Blocks = append(page.Blocks, b)
	}
	return s.renderTemplate("share/page.html", page)
}

// renderMoved is the page at /<slug>/<lang>/, where a link's other
// languages were before they joined its page: it sends visitors on.
func (s *Server) renderMoved(name, lang string) ([]byte, error) {
	return s.renderTemplate("share/moved.html", struct{ Name, Lang string }{name, lang})
}

func (s *Server) renderTemplate(name string, data any) ([]byte, error) {
	t, err := s.assets.Template(name)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Typst makes PDFs with the typst CLI from web/typst/cv.typ and a Document
// as JSON. It needs nothing else: the template and fonts come from Assets.
type Typst struct {
	Bin    string
	assets *Assets
	fonts  string // the embedded fonts, extracted once
	slots  chan struct{}
}

func NewTypst(bin, work string, assets *Assets) (*Typst, error) {
	fonts := filepath.Join(work, "typst-fonts")
	sub, _ := fs.Sub(assets.fs, "web/typst/fonts")
	if err := os.MkdirAll(fonts, 0o750); err != nil {
		return nil, err
	}
	if err := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(fonts, path), data)
	}); err != nil {
		return nil, err
	}
	return &Typst{Bin: bin, assets: assets, fonts: fonts, slots: make(chan struct{}, 4)}, nil
}

// PDF renders a document; photo is the path of the profile photo, if any.
func (t *Typst) PDF(ctx context.Context, doc Document, photo string) ([]byte, error) {
	var pdf []byte
	err := t.run(ctx, doc, photo, func(dir string) []string {
		return []string{"compile", filepath.Join(dir, "cv.typ"), filepath.Join(dir, "cv.pdf")}
	}, func(dir string, _ []byte) (err error) {
		pdf, err = os.ReadFile(filepath.Join(dir, "cv.pdf"))
		return err
	})
	return pdf, err
}

// Mark is where an item ended up in a PDF: its page (from 1), and its top
// and height in pt.
type Mark struct {
	ID     string  `json:"id"` // section/id
	Page   int     `json:"page"`
	Top    float64 `json:"top"`
	Height float64 `json:"height"`
}

// Layout tells where each item of a document lands, as PDF would lay it out.
func (t *Typst) Layout(ctx context.Context, doc Document, photo string) ([]Mark, error) {
	marks := []Mark{}
	err := t.run(ctx, doc, photo, func(dir string) []string {
		return []string{"eval", "query(<cv-item>).map(it => it.value)", "--in", filepath.Join(dir, "cv.typ"), "--format", "json"}
	}, func(_ string, out []byte) error {
		return json.Unmarshal(out, &marks)
	})
	return marks, err
}

// run writes the template, the document and the photo into a fresh
// directory, runs typst there with args, and hands its output to read.
func (t *Typst) run(ctx context.Context, doc Document, photo string, args func(dir string) []string, read func(dir string, stdout []byte) error) error {
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}

	tmp, err := os.MkdirTemp("", "new-leaf-typst-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if doc.Photo != "" && photo != "" {
		data, err := os.ReadFile(photo)
		if err != nil {
			return err
		}
		doc.Photo = "photo" + filepath.Ext(photo)
		if err := os.WriteFile(filepath.Join(tmp, doc.Photo), data, 0o600); err != nil {
			return err
		}
	} else {
		doc.Photo = ""
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	tpl, err := t.assets.ReadFile("web/typst/cv.typ")
	if err != nil {
		return err
	}
	for name, content := range map[string][]byte{"data.json": data, "cv.typ": tpl} {
		if err := os.WriteFile(filepath.Join(tmp, name), content, 0o600); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.Bin, append(args(tmp),
		"--root", tmp,
		"--font-path", t.fonts,
		"--ignore-system-fonts")...)
	cmd.Env = append(os.Environ(), "HOME="+tmp, "XDG_CACHE_HOME="+tmp)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			//lint:ignore ST1005 shown to the user as it is
			return errors.New(typstHelp)
		}
		return fmt.Errorf("typst: %v: %s", err, tail(stderr.Bytes(), 800))
	}
	return read(tmp, out)
}

// document lays out a user's CV for print options; photo is the path of the
// profile photo.
func (s *Server) document(user string, opt PrintOptions) (doc Document, photo string, err error) {
	profile, err := s.store.Profile(user)
	if err != nil {
		return Document{}, "", err
	}
	items, err := s.store.Items(user)
	if err != nil {
		return Document{}, "", err
	}
	photo = s.store.PhotoPath(user)
	return BuildDocument(profile, items, opt, filepath.Base(photo)), photo, nil
}

// renderPDF makes a PDF of a user's CV.
func (s *Server) renderPDF(ctx context.Context, user string, opt PrintOptions) ([]byte, error) {
	doc, photo, err := s.document(user, opt)
	if err != nil {
		return nil, err
	}
	return s.typst.PDF(ctx, doc, photo)
}

// PNG renders a document's first page as an image, and counts its pages.
func (t *Typst) PNG(ctx context.Context, doc Document, photo string, ppi int) (png []byte, pages int, err error) {
	err = t.run(ctx, doc, photo, func(dir string) []string {
		return []string{"compile", "--format", "png", "--ppi", strconv.Itoa(ppi), filepath.Join(dir, "cv.typ"), filepath.Join(dir, "page-{p}.png")}
	}, func(dir string, _ []byte) error {
		all, _ := filepath.Glob(filepath.Join(dir, "page-*.png"))
		pages = len(all)
		png, err = os.ReadFile(filepath.Join(dir, "page-1.png"))
		return err
	})
	return png, pages, err
}

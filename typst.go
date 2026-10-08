package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
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
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	tmp, err := os.MkdirTemp("", "cv-app-typst-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	if doc.Photo != "" && photo != "" {
		data, err := os.ReadFile(photo)
		if err != nil {
			return nil, err
		}
		doc.Photo = "photo" + filepath.Ext(photo)
		if err := os.WriteFile(filepath.Join(tmp, doc.Photo), data, 0o600); err != nil {
			return nil, err
		}
	} else {
		doc.Photo = ""
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	tpl, err := t.assets.ReadFile("web/typst/cv.typ")
	if err != nil {
		return nil, err
	}
	for name, content := range map[string][]byte{"data.json": data, "cv.typ": tpl} {
		if err := os.WriteFile(filepath.Join(tmp, name), content, 0o600); err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out := filepath.Join(tmp, "cv.pdf")
	cmd := exec.CommandContext(ctx, t.Bin, "compile",
		"--root", tmp,
		"--font-path", t.fonts,
		"--ignore-system-fonts",
		filepath.Join(tmp, "cv.typ"), out)
	cmd.Env = append(os.Environ(), "HOME="+tmp, "XDG_CACHE_HOME="+tmp)
	if log, err := cmd.CombinedOutput(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New(typstHelp)
		}
		return nil, fmt.Errorf("typst: %v: %s", err, tail(log, 800))
	}
	return os.ReadFile(out)
}

// renderPDF makes a PDF of a user's CV.
func (s *Server) renderPDF(ctx context.Context, user string, opt PrintOptions) ([]byte, error) {
	profile, err := s.store.Profile(user)
	if err != nil {
		return nil, err
	}
	items, err := s.store.Items(user)
	if err != nil {
		return nil, err
	}
	photo := s.store.PhotoPath(user)
	return s.typst.PDF(ctx, BuildDocument(profile, items, opt, filepath.Base(photo)), photo)
}

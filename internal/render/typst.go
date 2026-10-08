// Package render runs Typst: a cv.Document to a PDF, to an image of its
// first page, or to where each item lands (marks, for the preview).
package render

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
	"regexp"
	"strconv"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// typstHelp is shown when there is no typst.
const TypstHelp = `Typst makes the PDFs and was not found; everything else works without it.
Install it from https://github.com/typst/typst#installation, for example
with "nix profile install nixpkgs#typst", "brew install typst", "cargo install
typst-cli", or a binary from the releases page.`

// Typst makes PDFs with the typst CLI from web/typst/cv.typ and a Document
// as JSON. It needs nothing else: the template and fonts come from Assets.
type Typst struct {
	Bin   string
	files fs.FS  // web/: the template and the fonts
	fonts string // the embedded fonts, extracted once
	slots chan struct{}
}

func NewTypst(bin, work string, files fs.FS) (*Typst, error) {
	fonts := filepath.Join(work, "typst-fonts")
	sub, _ := fs.Sub(files, "typst/fonts")
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
		return cv.WriteAtomic(filepath.Join(fonts, path), data)
	}); err != nil {
		return nil, err
	}
	return &Typst{Bin: bin, files: files, fonts: fonts, slots: make(chan struct{}, 4)}, nil
}

// PDF renders a document; photo is the path of the profile photo, if any.
func (t *Typst) PDF(ctx context.Context, doc cv.Document, photo string) ([]byte, error) {
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
func (t *Typst) Layout(ctx context.Context, doc cv.Document, photo string) ([]Mark, error) {
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
func (t *Typst) run(ctx context.Context, doc cv.Document, photo string, args func(dir string) []string, read func(dir string, stdout []byte) error) error {
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
	tpl, err := fs.ReadFile(t.files, "typst/cv.typ")
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
			return errors.New(TypstHelp)
		}
		return fmt.Errorf("typst: %v: %s", err, tail(stderr.Bytes(), 800))
	}
	return read(tmp, out)
}

// PNG renders a document's first page as an image, and counts its pages.
func (t *Typst) PNG(ctx context.Context, doc cv.Document, photo string, ppi int) (png []byte, pages int, err error) {
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

var pageRe = regexp.MustCompile(`/Type\s*/Page[^s]`)

// PageCount counts page objects; Typst writes them uncompressed.
func PageCount(pdf []byte) int { return len(pageRe.FindAll(pdf, -1)) }

func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(bytes.TrimSpace(b))
}

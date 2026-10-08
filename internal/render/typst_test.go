package render

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/web"
)

// testDocument has a bit of everything the template lays out: a photo, a
// linked title, an item without a title, a publication shown as its
// reference, and lists.
func testDocument(t *testing.T) (cv.Document, string) {
	t.Helper()
	photo := filepath.Join(t.TempDir(), "photo.png")
	var b bytes.Buffer
	png.Encode(&b, image.NewGray(image.Rect(0, 0, 8, 8)))
	os.WriteFile(photo, b.Bytes(), 0o600)
	p := func(s string) cv.Rich { return cv.Rich{{Kind: "p", Runs: []cv.Run{{Text: s}}}} }
	return cv.Document{
		Lang: "en", Spacing: 1, Name: "Alex Example", Headline: "Data engineer",
		Contacts: []cv.Contact{{Text: "alex@example.com", URL: "mailto:alex@example.com"}, {Text: "LinkedIn", URL: "https://example.com/alex", Underline: true}},
		Summary:  p("Builds offline-first data pipelines."),
		Photo:    "photo.png",
		Sections: []cv.DocSection{
			{Title: "Work experience", Items: []cv.DocItem{
				{ID: "experience/acme", Date: "Sep 2023 – Present", Title: "Senior data engineer", Sub: "Acme · Utrecht",
					Body: cv.Rich{{Kind: "ul", Items: [][]cv.Run{{{Text: "Led the warehouse migration."}}, {{Text: "Mentored four engineers."}}}}}},
				{ID: "experience/globex", Date: "Jan 2020 – Aug 2023", Title: "Data engineer", Link: "https://example.com", Sub: "Globex", Body: p("Kafka and Python.")},
				{ID: "experience/untitled", Date: "2019", Sub: "Only an organisation", Body: cv.Rich{}},
			}},
			{Title: "Publications", Items: []cv.DocItem{
				{ID: "publications/paper", Date: "2021", Reference: true, Body: p("Example, A. (2021). A paper. Journal, 1(2).")},
			}},
		},
		Theme: cv.DocTheme{Accent: "#15803d", Font: "Inter", Photo: "rounded"},
	}, photo
}

func newTestTypst(t *testing.T) *Typst {
	t.Helper()
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst not in PATH")
	}
	typst, err := NewTypst("typst", t.TempDir(), web.Files)
	if err != nil {
		t.Fatal(err)
	}
	return typst
}

// The PDF is accessible as PDF/UA-1 defines it: tagged, with headings and
// alt text. Typst checks that when asked for the standard.
func TestPDFIsAccessible(t *testing.T) {
	typst := newTestTypst(t)
	doc, photo := testDocument(t)
	err := typst.run(context.Background(), doc, photo, func(dir string) []string {
		return []string{"compile", "--pdf-standard", "ua-1", filepath.Join(dir, "cv.typ"), filepath.Join(dir, "cv.pdf")}
	}, func(string, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}

// Applicant tracking systems read a PDF's text: it comes out in reading
// order, with every item's date, title and organisation together.
func TestPDFText(t *testing.T) {
	typst := newTestTypst(t)
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext (poppler) not in PATH")
	}
	doc, photo := testDocument(t)
	pdf, err := typst.PDF(context.Background(), doc, photo)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("pdftotext", "-", "-")
	cmd.Stdin = bytes.NewReader(pdf)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(out)), " ")
	at := 0
	for _, want := range []string{
		"Alex Example", "Data engineer", "alex@example.com", "LinkedIn", "Builds offline-first data pipelines.",
		"WORK EXPERIENCE",
		"Sep 2023 – Present Senior data engineer Acme · Utrecht", "• Led the warehouse migration.", "• Mentored four engineers.",
		"Jan 2020 – Aug 2023 Data engineer Globex Kafka and Python.",
		"2019 Only an organisation",
		"PUBLICATIONS", "2021 Example, A. (2021). A paper. Journal, 1(2).",
	} {
		i := strings.Index(text[at:], want)
		if i < 0 {
			t.Fatalf("%q not found in order in:\n%s", want, text)
		}
		at += i + len(want)
	}
}

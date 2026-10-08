package cv

import (
	"bytes"
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Document is one CV laid out for a selection and language, independent of
// the output format: everything a template needs, already sorted, filtered
// and localised. Text from the user arrives as Rich, never as markup, so a
// template never has to evaluate user input.
type Document struct {
	Lang     string       `json:"lang"`
	Spacing  float64      `json:"spacing"`
	Name     string       `json:"name"`
	Headline string       `json:"headline"`
	Contacts []Contact    `json:"contacts"`
	Summary  Rich         `json:"summary"`
	Photo    string       `json:"photo,omitempty"` // file name next to the document
	Sections []DocSection `json:"sections"`
	Theme    DocTheme     `json:"theme"`
}

// DocTheme is a Theme as a template uses it, defaults filled in.
type DocTheme struct {
	Accent string `json:"accent"` // #rrggbb
	Font   string `json:"font"`   // font family
	Photo  string `json:"photo"`  // rounded, circle or square
}

type Contact struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
	// Labelled links (LinkedIn, Google Scholar) are underlined, since their
	// text is not an address; email, phone and website are not.
	Underline bool `json:"underline,omitempty"`
}

type DocSection struct {
	Title string    `json:"title"`
	Items []DocItem `json:"items"`
}

type DocItem struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Link      string `json:"link,omitempty"`
	Sub       string `json:"sub,omitempty"` // organisation · location
	Body      Rich   `json:"body"`
	Reference bool   `json:"reference,omitempty"` // a publication shown as its reference
}

// Rich is Markdown reduced to what a CV uses: paragraphs and lists of runs.
type Rich []RichBlock

type RichBlock struct {
	Kind  string  `json:"kind"` // "p", "ul" or "ol"
	Runs  []Run   `json:"runs,omitempty"`
	Items [][]Run `json:"items,omitempty"`
}

type Run struct {
	Text   string `json:"text,omitempty"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
	Link   string `json:"link,omitempty"`
	Break  bool   `json:"break,omitempty"`
}

var (
	refYearRe = regexp.MustCompile(`\((\d{4})[a-z]?\)`)
	schemeRe  = regexp.MustCompile(`^https?://`)
)

// BuildDocument lays out a CV for print options. A nil selection means all
// items.
func BuildDocument(p Profile, items []Item, opt PrintOptions, photo string) Document {
	lang := opt.Lang
	pt := p.Text[lang]
	theme := opt.Theme.Resolved()
	doc := Document{
		Lang: lang, Spacing: cmp.Or(opt.Spacing, 1), Name: p.Name, Headline: pt.Headline,
		Summary: markdownRich(pt.Summary),
		// Empty lists, not null: templates iterate over them.
		Contacts: []Contact{}, Sections: []DocSection{},
		Theme: DocTheme{Accent: theme.Accent, Font: theme.font().Typst, Photo: theme.Photo},
	}
	if opt.Photo {
		doc.Photo = photo
	}
	if p.Email != "" {
		doc.Contacts = append(doc.Contacts, Contact{Text: p.Email, URL: "mailto:" + p.Email})
	}
	if p.Phone != "" {
		doc.Contacts = append(doc.Contacts, Contact{Text: p.Phone, URL: "tel:" + strings.ReplaceAll(p.Phone, " ", "")})
	}
	if p.Website != "" {
		doc.Contacts = append(doc.Contacts, Contact{Text: schemeRe.ReplaceAllString(p.Website, ""), URL: p.Website})
	}
	for _, l := range p.Links {
		doc.Contacts = append(doc.Contacts, Contact{Text: cmp.Or(l.Label, l.URL), URL: l.URL, Underline: true})
	}
	if pt.Location != "" {
		doc.Contacts = append(doc.Contacts, Contact{Text: pt.Location})
	}

	selected := map[string]bool{}
	for _, e := range opt.Entries {
		selected[e] = true
	}
	order := p.Order
	if len(opt.Order) > 0 {
		order = opt.Order
	}
	for _, section := range SectionOrder(order) {
		var in []Item
		for _, it := range items {
			if it.Section == section && (opt.Entries == nil || selected[section+"/"+it.ID]) {
				in = append(in, it)
			}
		}
		if len(in) == 0 {
			continue
		}
		sortItems(in, section, lang)
		ds := DocSection{Title: LanguageOf(lang).sections[section]}
		for _, it := range in {
			ds.Items = append(ds.Items, docItem(it, section, lang))
		}
		doc.Sections = append(doc.Sections, ds)
	}
	return doc
}

func docItem(it Item, section, lang string) DocItem {
	t := it.Text[lang]
	d := DocItem{ID: section + "/" + it.ID, Title: t.Title, Link: it.Link, Body: markdownRich(t.Body)}
	if IsPointSection(section) {
		d.Date = formatDate(it.Start, lang)
	} else {
		d.Date = formatDate(it.Start, lang) + " – " + cmp.Or(formatDate(it.End, lang), LanguageOf(lang).present)
	}
	d.Sub = strings.Join(slices.DeleteFunc([]string{t.Org, t.Location}, func(s string) bool { return s == "" }), " · ")
	// A publication with a description is shown as that reference, as
	// written; a link not already in it is added underneath.
	if section == "publications" && strings.TrimSpace(t.Body) != "" {
		d.Reference = true
		if it.Link != "" && !strings.Contains(t.Body, it.Link) {
			d.Body = append(d.Body, RichBlock{Kind: "p", Runs: []Run{{Text: it.Link, Link: it.Link}}})
		}
	}
	return d
}

// sortItems puts the most recent first: ongoing items, then by end date; on
// equal end dates the longer item first, so a programme precedes its thesis.
// Single-date items sort by their date, undated ones last; an undated
// publication by the year in its reference, e.g. "(2026)". Same order as the
// editor's list.
func sortItems(items []Item, section, lang string) {
	point := IsPointSection(section)
	start := func(it Item) string {
		if it.Start == "" && section == "publications" {
			for _, l := range append([]string{lang}, langCodes()...) {
				if m := refYearRe.FindStringSubmatch(it.Text[l].Body); m != nil {
					return m[1]
				}
			}
		}
		return it.Start
	}
	end := func(it Item) string {
		if point {
			return start(it)
		}
		return cmp.Or(it.End, "9999-12")
	}
	slices.SortStableFunc(items, func(a, b Item) int {
		return cmp.Or(strings.Compare(end(b), end(a)), strings.Compare(a.Start, b.Start), strings.Compare(a.ID, b.ID))
	})
}

// formatDate turns "2023-09" into "Sep 2023" ("sep 2023" in Dutch) and keeps
// a bare year as is.
func formatDate(d, lang string) string {
	y, m, ok := strings.Cut(d, "-")
	if !ok || len(m) != 2 {
		return d
	}
	i := int(m[0]-'0')*10 + int(m[1]-'0')
	if i < 1 || i > 12 {
		return d
	}
	return LanguageOf(lang).months[i-1] + " " + y
}

// --- Markdown

var markdown = goldmark.New(goldmark.WithExtensions(extension.Linkify))

// markdownRich converts Markdown to Rich. A single newline is a line break,
// since descriptions are typed in a textarea. Anything a CV doesn't need
// (headings, code, raw HTML) is kept as plain text or dropped.
func markdownRich(src string) Rich {
	source := []byte(strings.TrimSpace(strings.ReplaceAll(src, "\r\n", "\n")))
	if len(source) == 0 {
		return Rich{}
	}
	doc := markdown.Parser().Parse(text.NewReader(source))
	out := Rich{}
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch n := n.(type) {
		case *ast.List:
			b := RichBlock{Kind: "ul"}
			if n.IsOrdered() {
				b.Kind = "ol"
			}
			for li := n.FirstChild(); li != nil; li = li.NextSibling() {
				var runs []Run
				for c := li.FirstChild(); c != nil; c = c.NextSibling() {
					if len(runs) > 0 {
						runs = append(runs, Run{Break: true})
					}
					runs = append(runs, inlineRuns(c, source, Run{})...)
				}
				b.Items = append(b.Items, mergeRuns(runs))
			}
			out = append(out, b)
		default:
			if runs := mergeRuns(inlineRuns(n, source, Run{})); len(runs) > 0 {
				out = append(out, RichBlock{Kind: "p", Runs: runs})
			}
		}
	}
	return out
}

// inlineRuns flattens inline Markdown into runs carrying their formatting.
func inlineRuns(n ast.Node, source []byte, style Run) []Run {
	var runs []Run
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			r := style
			r.Text = string(c.Segment.Value(source))
			runs = append(runs, r)
			if c.SoftLineBreak() || c.HardLineBreak() {
				runs = append(runs, Run{Break: true})
			}
		case *ast.String:
			r := style
			r.Text = string(c.Value)
			runs = append(runs, r)
		case *ast.CodeSpan:
			r := style
			r.Text = string(nodeText(c, source))
			runs = append(runs, r)
		case *ast.Emphasis:
			s := style
			if c.Level >= 2 {
				s.Bold = true
			} else {
				s.Italic = true
			}
			runs = append(runs, inlineRuns(c, source, s)...)
		case *ast.Link:
			s := style
			s.Link = safeURL(string(c.Destination))
			runs = append(runs, inlineRuns(c, source, s)...)
		case *ast.AutoLink:
			r := style
			r.Text = string(c.Label(source))
			r.Link = safeURL(string(c.URL(source)))
			if r.Link != "" && c.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(r.Link, "mailto:") {
				r.Link = "mailto:" + r.Link
			}
			runs = append(runs, r)
		case *ast.RawHTML:
			// dropped: descriptions are text, not HTML
		default:
			runs = append(runs, inlineRuns(c, source, style)...)
		}
	}
	return runs
}

func nodeText(n ast.Node, source []byte) []byte {
	var b bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(source))
		}
	}
	return b.Bytes()
}

// safeURL keeps links to the web, mail and phone only.
func safeURL(u string) string {
	l := strings.ToLower(u)
	for _, p := range []string{"https://", "http://", "mailto:", "tel:"} {
		if strings.HasPrefix(l, p) {
			return u
		}
	}
	if strings.Contains(u, "@") && !strings.Contains(u, "/") {
		return u // bare email from linkify; the caller adds mailto:
	}
	if strings.HasPrefix(l, "www.") {
		return "https://" + u
	}
	return ""
}

// mergeRuns joins neighbouring runs with the same formatting; the parser
// splits text at arbitrary points.
func mergeRuns(runs []Run) []Run {
	var out []Run
	for _, r := range runs {
		if n := len(out); n > 0 && !r.Break && !out[n-1].Break && out[n-1].Bold == r.Bold && out[n-1].Italic == r.Italic && out[n-1].Link == r.Link {
			out[n-1].Text += r.Text
			continue
		}
		out = append(out, r)
	}
	return out
}

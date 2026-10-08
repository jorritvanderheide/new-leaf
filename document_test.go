package main

import (
	"encoding/json"
	"testing"
)

func TestMarkdownRich(t *testing.T) {
	got := markdownRich("Line one\nline **two** and *three*.\n\nSee [site](https://example.com), https://doi.org/10.1/x or <b>x</b> [bad](javascript:alert(1)).\n\n- a\n- b")
	want := Rich{
		{Kind: "p", Runs: []Run{
			{Text: "Line one"}, {Break: true}, {Text: "line "}, {Text: "two", Bold: true}, {Text: " and "}, {Text: "three", Italic: true}, {Text: "."},
		}},
		{Kind: "p", Runs: []Run{
			{Text: "See "}, {Text: "site", Link: "https://example.com"}, {Text: ", "}, {Text: "https://doi.org/10.1/x", Link: "https://doi.org/10.1/x"},
			{Text: " or x bad."}, // raw HTML and the javascript: link are reduced to text
		}},
		{Kind: "ul", Items: [][]Run{{{Text: "a"}}, {{Text: "b"}}}},
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("markdownRich:\n got %s\nwant %s", g, w)
	}
	if len(markdownRich("  \n ")) != 0 {
		t.Error("empty Markdown should give no blocks")
	}
}

func TestFormatDate(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"2023-09", "en"}: "Sep 2023",
		{"2022-03", "nl"}: "mrt 2022",
		{"2024", "en"}:    "2024",
		{"", "en"}:        "",
	} {
		if got := formatDate(in[0], in[1]); got != want {
			t.Errorf("formatDate(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestBuildDocument(t *testing.T) {
	text := func(title, body string) map[string]ItemText {
		return map[string]ItemText{"en": {Title: title, Body: body}, "nl": {Title: title + " (nl)"}}
	}
	items := []Item{
		{Section: "education", ID: "ma", Start: "2020-09", End: "2022-08", Text: text("MA", "")},
		{Section: "education", ID: "thesis", Start: "2022-03", End: "2022-08", Text: text("Thesis", "")},
		{Section: "experience", ID: "now", Start: "2023-04", Text: text("Now", "")},
		{Section: "experience", ID: "old", Start: "2018-01", End: "2019-01", Text: text("Old", "")},
		{Section: "publications", ID: "review", Text: text("Review", "Doe, J. Under review.")},
		{Section: "publications", ID: "paper", Link: "https://doi.org/10.1/p", Text: text("Paper", "Doe, J. (2026). Paper.")},
		{Section: "volunteering", ID: "skip", Start: "2010-01", End: "2011-01", Text: text("Skip", "")},
	}
	p := Profile{Name: "Alice", Email: "a@example.com", Links: []ProfileLink{{Label: "Scholar", URL: "https://scholar.example"}},
		Order: []string{"education", "publications", "experience"}, Text: map[string]ProfileText{"en": {Location: "Exampletown"}}}
	sel := []string{"education/ma", "education/thesis", "experience/now", "experience/old", "publications/review", "publications/paper"}

	doc := BuildDocument(p, items, PrintOptions{Lang: "en", Entries: sel}, "photo.jpg")
	var order []string
	for _, s := range doc.Sections {
		for _, it := range s.Items {
			order = append(order, it.ID)
		}
	}
	want := []string{
		"education/ma", "education/thesis", // same end: the longer item first
		"publications/paper", "publications/review", // year from the reference; undated last
		"experience/now", "experience/old", // ongoing first; then the default order's remaining sections
	}
	if g, w := toJSON(order), toJSON(want); g != w {
		t.Errorf("order = %s, want %s", g, w)
	}
	if doc.Sections[0].Title != "Education" || doc.Photo != "" {
		t.Errorf("sections/photo: %q, %q", doc.Sections[0].Title, doc.Photo)
	}
	paper := doc.Sections[1].Items[0]
	if !paper.Reference || len(paper.Body) != 2 || paper.Body[1].Runs[0].Link != "https://doi.org/10.1/p" {
		t.Errorf("reference with its link added = %+v", paper)
	}
	if doc.Sections[2].Items[0].Date != "Apr 2023 – Present" {
		t.Errorf("date = %q", doc.Sections[2].Items[0].Date)
	}
	if g := toJSON(doc.Contacts); g != `[{"text":"a@example.com","url":"mailto:a@example.com"},{"text":"Scholar","url":"https://scholar.example","underline":true},{"text":"Exampletown"}]` {
		t.Errorf("contacts = %s", g)
	}

	nl := BuildDocument(p, items, PrintOptions{Lang: "nl", Entries: []string{"experience/now"}, Photo: true}, "photo.jpg")
	if len(nl.Sections) != 1 || nl.Sections[0].Title != "Werkervaring" || nl.Sections[0].Items[0].Date != "apr 2023 – heden" || nl.Photo != "photo.jpg" {
		t.Errorf("nl = %+v", nl)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

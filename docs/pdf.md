# PDF

Every PDF, preview and thumbnail New Leaf makes comes from one Typst template,
`web/typst/cv.typ`, and one JSON file with the CV in it. The share pages are
rendered from the same JSON by a Go template, so the page and the PDF always
show the same thing.

## How a PDF is made

1. `BuildDocument` in `internal/cv/document.go` turns a CV and a version into a
   `Document`: only the chosen items, in the version's language, sorted
   newest first, sections in the version's order, empty sections left out,
   dates written out (`Sep 2023`, `sep 2023` in Dutch) and Markdown parsed.
2. `internal/render/typst.go` writes `cv.typ`, the `Document` as `data.json` and the photo into
   a fresh temporary folder, and runs `typst compile` there with `--root` set
   to that folder, the bundled fonts only (`--ignore-system-fonts`), and at
   most four at a time.
3. The PDF is read back and the folder removed.

A PDF takes tens of milliseconds, which is why the preview can follow every
change.

## The contract

`data.json` is the `Document` type in `internal/cv/document.go`; `cv.typ` reads nothing
else.

| Key | Value |
| --- | --- |
| `lang` | The language code, for hyphenation |
| `spacing` | The whitespace scale, `0.4` to `1.4` |
| `name`, `headline` | Text |
| `contacts` | A list of `text`, an optional `url`, and `underline` for labelled links |
| `summary` | Rich text |
| `photo` | The photo's file name, or absent |
| `sections` | A list of `title` and `items` |
| `theme` | `accent` (`#rrggbb`), `font` (a font family) and `photo` (`rounded`, `circle` or `square`), defaults filled in |

An item has `id` (`<section>/<id>`), `date`, `title`, an optional `link`,
`sub` (organisation · location), `body` (rich text) and `reference`, set for a
publication shown as its full reference instead of a title.

**Rich text** is a list of blocks, each `p`, `ul` or `ol`, holding runs: `text`
with `bold`, `italic`, a `link`, or a line `break`. What you type never reaches
Typst as markup, so nothing in a CV can run as Typst code.

## Fonts and looks

The fonts are in `web/typst/fonts`, embedded in the binary and written to the
work folder on start: Inter for `sans` and Source Serif 4 for `serif`. The
share pages use the same fonts from `web/share/fonts`. `themeFonts` in
`internal/cv/theme.go` maps a look's font to both.

A look is the accent colour (the headline, the section headings), the font,
the photo shape and the spacing. The editor warns when an accent is too pale
to read on white (contrast under 4.5) and offers a darker shade.

Adding a font means adding its files to both font folders, its licence next
to them, and a line to `themeFonts`.

## Pages

A4, margins of 16 mm at the top, 17 mm at the sides and 15 mm at the bottom.
Items never break across pages, and a section's heading stays with its first
item. Page numbers are shown when there is more than one page.

**Fit on** finds the most spacing at which the version still fits on the
chosen number of pages: a binary search over the spacing slider's steps, so
about five PDFs (`postFit` in `internal/server/preview.go`).

## Marks and the preview

Every item in `cv.typ` is wrapped in `marked`, which records its id, page, top
and height as metadata labelled `<cv-item>` without changing the layout.
`Typst.Layout` reads them back with

```sh
typst eval 'query(<cv-item>).map(it => it.value)' --in cv.typ --format json
```

The editor draws the PDF with pdf.js and uses the marks to:

- make each item on the preview clickable, to edit or hide it;
- show where each page starts in the item list;
- say how many lines the version is over, or has room for, against the pages
  to fit on.

That estimate uses the page's text area and line height in `PAGE` in
`web/editor/app.js`. If you change the margins or the base size in `cv.typ`,
change `PAGE` with them.

## Thumbnails

The overview shows page 1 of each version, rendered with `typst compile
--format png` in the background (`internal/server/thumbs.go`). Each is named after a hash of
its `Document`, `cv.typ` and the photo, so a version that changes gets a new thumbnail and never shows
an old one.

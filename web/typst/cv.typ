// The CV as a PDF. All content comes from data.json (see Document in
// internal/cv/document.go): sorted, filtered and localised by the app, with user text as
// structured runs, so nothing typed by a user is ever evaluated as Typst.

#let data = json("data.json")
#let s = data.spacing // whitespace scale, 1 = default

#let theme = data.theme // accent colour, font, photo shape (see Theme in theme.go)
#let accent = rgb(theme.accent)
#let stone = (
  "200": rgb("#e7e5e4"), "300": rgb("#d6d3d1"), "400": rgb("#a6a09b"), "500": rgb("#79716b"),
  "600": rgb("#57534d"), "700": rgb("#44403b"), "800": rgb("#292524"), "900": rgb("#1c1917"),
)
#let base = 9.5pt
#let small = base * 0.875

#set document(title: "CV " + data.name, author: data.name)
#set page(
  paper: "a4",
  margin: (top: 16mm, x: 17mm, bottom: 15mm),
  footer: context {
    let total = counter(page).final().first()
    if total > 1 {
      align(right, text(size: 7pt, fill: stone.at("400"), [#counter(page).display() / #total]))
    }
  },
)
#set text(font: theme.font, size: base, fill: stone.at("800"), lang: data.lang, hyphenate: false, top-edge: "ascender", bottom-edge: "descender")
#set par(leading: (0.415 + 0.325 * (s - 1)) * 1em, spacing: 0.45em, justify: false)
// Links in text are underlined; a linked item title is not, as on the web.
#let underlined(it) = underline(stroke: 0.5pt + stone.at("300"), offset: 2pt, it)

// Rich text: paragraphs and lists of runs.
#let runs(rs) = for r in rs {
  if r.at("break", default: false) {
    linebreak()
  } else {
    let c = [#r.at("text", default: "")]
    if r.at("bold", default: false) { c = strong(delta: 200, c) }
    if r.at("italic", default: false) { c = emph(c) }
    if r.at("link", default: "") != "" { c = underlined(link(r.link, c)) }
    c
  }
}
#let rich(blocks) = for b in blocks {
  if b.kind == "ul" {
    list(indent: 0pt, body-indent: 0.5em, ..b.items.map(runs))
  } else if b.kind == "ol" {
    enum(indent: 0pt, body-indent: 0.5em, ..b.items.map(runs))
  } else {
    par(runs(b.runs))
  }
}

// The name, the sections and the items are headings (1, 2 and 3), so the PDF
// has them as such: for screen readers, applicant tracking systems, and
// bookmarks to the sections. They look exactly as they did as plain text.

// Header: name, headline, contact details, photo.
#show heading.where(level: 1): set block(spacing: 0.45 * base)
#show heading.where(level: 1): it => text(size: base * 2.5, weight: 600, fill: stone.at("900"), tracking: -0.02em, it.body)
#grid(
  columns: (1fr, auto),
  column-gutter: 8mm,
  {
    heading(level: 1, bookmarked: false, data.name)
    if data.headline != "" {
      v(2pt)
      text(size: base * 1.125, fill: accent, data.headline)
    }
    v(base)
    set text(size: small, fill: stone.at("600"))
    data
      .contacts
      .map(c => {
        if c.at("url", default: "") == "" { return c.text }
        let l = link(c.url, c.text)
        if c.at("underline", default: false) { underlined(l) } else { l }
      })
      .join(h(1.3em))
  },
  if data.at("photo", default: none) != none {
    let radius = (rounded: base, circle: 50%, square: 0pt).at(theme.photo)
    box(clip: true, radius: radius, image(data.photo, alt: data.name, width: base * 7, height: base * 7, fit: "cover"))
  },
)

#if data.summary.len() > 0 {
  v(base * 1.5 * s)
  set text(fill: stone.at("700"))
  rich(data.summary)
}

// Sections. Items never split across pages, and a heading stays with its
// first item.
#show heading.where(level: 2): set block(spacing: 0.45 * base)
#show heading.where(level: 2): set text(size: base, weight: "regular")
#show heading.where(level: 2): it => grid(
  columns: (auto, 1fr),
  column-gutter: 0.75em,
  align: horizon,
  text(size: base * 0.75, weight: 600, tracking: 0.08em, fill: accent, upper(it.body)),
  line(length: 100%, stroke: 0.5pt + stone.at("200")),
)

// Item titles. An item without a title keeps the line it would have had.
#let leading = (0.415 + 0.325 * (s - 1)) * base
#show heading.where(level: 3): set block(above: 0.45 * base, below: leading)
#show heading.where(level: 3): it => text(size: base, weight: 600, fill: stone.at("900"), it.body)

#let item(it) = grid(
  columns: (base * 9.5, 1fr),
  column-gutter: base * 1.5,
  text(size: small, fill: stone.at("500"), it.date),
  if it.at("reference", default: false) {
    set text(fill: stone.at("800"))
    rich(it.body)
  } else {
    let title = if it.at("link", default: "") != "" { link(it.link, it.title) } else { it.title }
    if it.title == "" {
      block(above: 0.45 * base, below: leading, text(size: base, sym.zws))
    } else {
      heading(level: 3, bookmarked: false, title)
    }
    if it.at("sub", default: "") != "" {
      // As far below the title as the next line of a paragraph.
      block(above: leading, below: 0.45 * base, text(size: small, fill: stone.at("600"), it.sub))
    }
    if it.body.len() > 0 {
      v(base * 0.375, weak: true)
      set text(fill: stone.at("700"))
      rich(it.body)
    }
  },
)

// An item that tells where it ended up, for the editor's preview: its page,
// top and height in pt (`typst eval 'query(<cv-item>)…'`). Leaves the layout
// as it is.
#let marked(id, body) = block(breakable: false, width: 100%, layout(size => {
  let height = measure(body, width: size.width).height
  place(top + left, context [#metadata((id: id, page: here().page(), top: here().position().y.pt(), height: height.pt())) <cv-item>])
  body
}))

// The first section keeps more room from the header above it.
#for (n, section) in data.sections.enumerate() {
  v(base * (if n == 0 { 3.25 } else { 2.25 }) * s, weak: true)
  block(breakable: false, {
    heading(level: 2, section.title)
    v(base * 1.25 * s)
    let first = section.items.first()
    marked(first.id, item(first))
  })
  for it in section.items.slice(1) {
    v(base * 1.25 * s, weak: true)
    marked(it.id, item(it))
  }
}

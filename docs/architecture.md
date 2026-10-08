# Architecture

New Leaf is one Go program with its web pages, styles, PDF template and fonts
embedded, plus Typst to make PDFs. The same program is the editor on your own
computer (`new-leaf`) and on a server (`new-leaf serve`); the server adds
sign-in, more CVs and share links.

## Where state lives

- **The files are the record.** A CV is a folder of Markdown and JSON (see
  [Data model](data-model.md)). The server reads them on every request and
  keeps no copy of its own, so editing a file by hand, or restoring a backup,
  takes effect at once.
- **Everything else can be made again.** PDFs and previews are rendered on
  request. Thumbnails and Typst's fonts are in the work folder. Share links
  are published from the files, and published again whenever the app starts.
- **The browser** keeps only light or dark mode, and which CV is open.

## Layers

```mermaid
flowchart LR
    B[Browser<br/>Alpine.js, pdf.js] -->|JSON| api[api.go]
    api --> auth[auth.go<br/>tailscale whois]
    api --> store[store.go, versions.go, cvs.go<br/>files]
    api --> doc[document.go<br/>Document]
    doc --> typst[typst.go<br/>typst CLI]
    doc --> share[share.go<br/>share pages]
    share --> publish[publish.go<br/>public folder]
    typst --> publish
    typst --> thumbs[thumbs.go]
```

| Path | What it holds |
| --- | --- |
| `main.go` | `new-leaf serve`: flags, the server, the public folder's own server, expiring links every five minutes |
| `local.go` | `new-leaf` on your own computer: data folder, a free port, opening the browser, stopping when idle, moving cv-app's data |
| `flags.go` | Flags from the environment (`NEW_LEAF_*`), `-version`, finding Typst |
| `api.go` | Every `/api/` route, the editor's security headers, and the state the editor gets after each change |
| `auth.go` | Who a visitor is (`tailscale whois`), and which CV opens for them |
| `cvs.go` | Which CVs exist; making, renaming and deleting them |
| `store.go` | Reading and writing the profile, items and links |
| `versions.go` | Versions, and the first ones for CVs from before them |
| `theme.go` | Looks: accent colour, font, photo shape |
| `document.go` | A CV for one version and language: sorted, localised, Markdown parsed |
| `typst.go` | Running Typst for PDFs, PNGs and marks |
| `thumbs.go` | The overview's thumbnails, rendered in the background |
| `share.go` | Share pages, from the same `Document` as the PDF |
| `publish.go` | Writing share links into the public folder, and removing what isn't a link |
| `backup.go` | Download backup and Restore |
| `assets.go` | The embedded `web/` files, the editor's pages and its icons |
| `web/editor/` | The editor: Go templates, `app.js` (Alpine.js), `mode.js` (light or dark before the page is drawn), `editor.css` |
| `web/share/` | The share page template, `share.css`, web fonts |
| `web/typst/` | `cv.typ` and its fonts |
| `web/css/` | Tailwind inputs for `editor.css` and `share.css`, which are generated and committed |
| `nix/` | The package, the NixOS module, its VM test, the container image |
| `tests/browser/` | Browser tests in headless Chromium, against a made-up CV |
| `scripts/` | Release archives with Typst, and Typst's pinned checksums |
| `deploy/` | The Docker Compose recipe |

## A change, from click to PDF

1. The editor sends the change as JSON, with an `X-New-Leaf` header. A
   browser only sends that header from the editor's own pages, so other
   websites can't make changes.
2. `authenticate` finds the visitor and their CV (on your own computer,
   `localOnly` refuses requests not addressed to `localhost`).
3. The handler validates and writes the files, one change per CV at a time,
   and answers with the CV's whole state, which the editor takes as it is.
4. If a version with a share link changed, its pages and PDFs are published
   again in the background. Changes made while that runs are published
   together, right after.
5. The editor asks for the PDF and the marks of the open version
   (`/api/pdf`, `/api/layout`) and draws them. See [PDF](pdf.md).

## Rules

**User text is never markup.** Typst gets runs of text, not Typst, and the
share pages go through `html/template`. A CV can't run code anywhere it is
shown.

**The share page and the PDF come from one `Document`.** Anything that changes
what a CV shows belongs in `document.go`, not in a template.

**Items that a version doesn't show never reach the public folder.** A share
link is published from the version's selection, not filtered on the page.

**The public folder holds only links.** `reconcile` removes anything else,
which is how expired links go offline. It only manages a folder that is empty
or carries its `.new-leaf-public` marker.

**No build step for the browser.** `app.js` and the vendored Alpine.js and
pdf.js are served as they are. Only the CSS is generated, by Tailwind, and
committed, so `go build` alone makes a working program.

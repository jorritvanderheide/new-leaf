// Package web holds the files New Leaf serves and renders with: the
// editor's pages, scripts and styles, the share page, and the PDF template,
// each with its fonts. They are embedded in the binary; web/css only holds
// the stylesheets' Tailwind sources.
package web

import "embed"

// Files are editor/, share/ and typst/.
//
//go:embed editor share typst
var Files embed.FS

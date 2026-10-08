package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
)

//go:embed web
var embedded embed.FS

// Assets are the templates and static files under web/: embedded in the
// binary, or read from disk (-dev-assets) so edits show without a restart.
type Assets struct {
	fs  fs.FS
	dev bool

	mu        sync.Mutex
	templates map[string]*template.Template
}

func NewAssets(dir string) *Assets {
	if dir != "" {
		return &Assets{fs: os.DirFS(dir), dev: true}
	}
	return &Assets{fs: embedded}
}

func (a *Assets) ReadFile(name string) ([]byte, error) { return fs.ReadFile(a.fs, name) }

// Template parses files (paths under web/), cached unless developing. The
// first file is the one that gets executed.
func (a *Assets) Template(files ...string) (*template.Template, error) {
	key := strings.Join(files, "\n")
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.templates[key]; t != nil && !a.dev {
		return t, nil
	}
	t, err := template.New(path.Base(files[0])).Funcs(a.funcs()).ParseFS(a.fs, files...)
	if err != nil {
		return nil, err
	}
	if a.templates == nil {
		a.templates = map[string]*template.Template{}
	}
	a.templates[key] = t
	return t, nil
}

// Version is a short hash of a file's content, for cache-busting URLs.
func (a *Assets) Version(name string) string {
	data, err := a.ReadFile(name)
	if err != nil {
		return "0"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:4])
}

func (a *Assets) funcs() template.FuncMap {
	return template.FuncMap{
		"icon": icon,
		"rich": richHTML,
		"url":  trustedURL,
		"cond": func(c bool, a, b any) any {
			if c {
				return a
			}
			return b
		},
		// asset links an editor file with its version, so it can be cached.
		"asset": func(name string) string {
			return "/" + name + "?v=" + a.Version("web/editor/"+name)
		},
	}
}

// --- editor UI

type editorPage struct {
	Path, File, Title, Description, Icon string
}

var editorPages = []editorPage{
	{"/", "versions.html", "Versions", "Versions of your CV, each with its own items and layout: one per application, or a full one.", "compose"},
	{"/items/", "items.html", "Items", "Everything that can go on your CV. Changes save automatically.", "items"},
	{"/profile/", "profile.html", "Profile", "Your name, contact details and summary. Changes save automatically.", "profile"},
}

// workspacePage edits one version, at /v/<id>/; it belongs under Versions.
var workspacePage = editorPage{"/v/", "workspace.html", "Version", "", "compose"}

type navItem struct {
	Name, URL, Icon string
	Active          bool
}

type pageData struct {
	Title, Description string
	Workspace          bool // full width, no title
	Nav                []navItem
}

// uiHandler serves the editor's pages and files.
func (s *Server) uiHandler() http.Handler {
	editor, _ := fs.Sub(s.assets.fs, "web/editor")
	files := http.FileServerFS(editor)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, workspacePage.Path); ok && strings.HasSuffix(id, "/") && !strings.Contains(strings.TrimSuffix(id, "/"), "/") {
			// A version that is gone (deleted, or of another CV): the overview.
			if _, err := s.store.Version(userOf(r), strings.TrimSuffix(id, "/")); err != nil {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			s.servePage(w, workspacePage)
			return
		}
		for _, p := range editorPages {
			if r.URL.Path == p.Path {
				s.servePage(w, p)
				return
			}
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "app.js" && name != "editor.css" && !strings.HasPrefix(name, "vendor/") && !strings.HasPrefix(name, "fonts/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Has("v") && !s.assets.dev {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) servePage(w http.ResponseWriter, p editorPage) {
	t, err := s.assets.Template("web/editor/base.html", "web/editor/parts.html", "web/editor/"+p.File)
	if err != nil {
		httpError(w, err)
		return
	}
	data := pageData{Title: p.Title, Description: p.Description, Workspace: p == workspacePage}
	for _, q := range editorPages {
		active := q.Path == p.Path || data.Workspace && q.Path == "/"
		data.Nav = append(data.Nav, navItem{Name: q.Title, URL: q.Path, Icon: q.Icon, Active: active})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if err := t.Execute(w, data); err != nil {
		httpError(w, err)
	}
}

// --- template functions

// Small line icons, 20×20, drawn with currentColor.
var icons = map[string]string{
	"compose": `<path d="M5.25 2.75h6l3.5 3.5v11h-9.5z"/><path d="M11 2.75v3.75h3.75M7.75 10.25h4.5M7.75 13.25h4.5"/>`,
	"items":   `<path d="M7.5 5.75h9M7.5 10h9M7.5 14.25h9"/><path d="M3.75 5.75h.01M3.75 10h.01M3.75 14.25h.01" stroke-width="2.2"/>`,
	"profile": `<circle cx="10" cy="7" r="3.25"/><path d="M3.75 17c.9-3.1 3.3-4.75 6.25-4.75s5.35 1.65 6.25 4.75"/>`,
	"links":   `<path d="M8.6 11.4a3.1 3.1 0 0 0 4.4 0l2.5-2.5a3.1 3.1 0 0 0-4.4-4.4l-.8.8"/><path d="M11.4 8.6a3.1 3.1 0 0 0-4.4 0l-2.5 2.5a3.1 3.1 0 0 0 4.4 4.4l.8-.8"/>`,
	"search":  `<circle cx="8.75" cy="8.75" r="5"/><path d="m12.5 12.5 4 4"/>`,
	"close":   `<path d="m5.5 5.5 9 9M14.5 5.5l-9 9"/>`,
	"warn":    `<path d="M10 3.5 17 16H3z"/><path d="M10 8.5v3M10 13.75h.01" stroke-width="2"/>`,
	"updown":  `<path d="m6.5 7.5 3.5-3.5 3.5 3.5M6.5 12.5l3.5 3.5 3.5-3.5"/>`,
	"match":   `<circle cx="10" cy="10" r="6.25"/><circle cx="10" cy="10" r="2.75"/><path d="M10 1.75v2.5M10 15.75v2.5M1.75 10h2.5M15.75 10h2.5"/>`,
	"plus":    `<path d="M10 4.5v11M4.5 10h11"/>`,
	"back":    `<path d="M11.5 5 6.5 10l5 5"/>`,
	"grip":    `<path d="M7.5 5h.01M12.5 5h.01M7.5 10h.01M12.5 10h.01M7.5 15h.01M12.5 15h.01" stroke-width="2.4"/>`,
	"undo":    `<path d="M7 5.5 3.75 8.75 7 12"/><path d="M4 8.75h7.5a4.25 4.25 0 0 1 0 8.5H9"/>`,
	"redo":    `<path d="m13 5.5 3.25 3.25L13 12"/><path d="M16 8.75H8.5a4.25 4.25 0 0 0 0 8.5H11"/>`,
	"trash":   `<path d="M4 5.75h12M8.25 5.75V4h3.5v1.75M5.75 5.75l.75 10.5h7l.75-10.5"/>`,
}

func icon(name, class string) template.HTML {
	if class == "" {
		class = "size-5"
	}
	return template.HTML(fmt.Sprintf(`<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="%s">%s</svg>`,
		html.EscapeString(class), icons[name]))
}

// trustedURL marks a URL as safe for an href. Only links the app itself made
// or checked get here (web, mail, phone, relative), so the html/template
// filter, which knows no tel:, can be skipped.
func trustedURL(u string) template.URL {
	l := strings.ToLower(u)
	for _, p := range []string{"https://", "http://", "mailto:", "tel:"} {
		if strings.HasPrefix(l, p) {
			return template.URL(u)
		}
	}
	if !strings.Contains(u, ":") {
		return template.URL(u) // relative
	}
	return "#"
}

// richHTML renders Rich as HTML; text is escaped, links were checked when
// the Markdown was read.
func richHTML(r Rich) template.HTML {
	var b strings.Builder
	runs := func(rs []Run) {
		for _, run := range rs {
			if run.Break {
				b.WriteString("<br>")
				continue
			}
			t := html.EscapeString(run.Text)
			if run.Bold {
				t = "<strong>" + t + "</strong>"
			}
			if run.Italic {
				t = "<em>" + t + "</em>"
			}
			if run.Link != "" {
				t = `<a href="` + html.EscapeString(string(trustedURL(run.Link))) + `">` + t + "</a>"
			}
			b.WriteString(t)
		}
	}
	for _, blk := range r {
		switch blk.Kind {
		case "ul", "ol":
			b.WriteString("<" + blk.Kind + ">")
			for _, item := range blk.Items {
				b.WriteString("<li>")
				runs(item)
				b.WriteString("</li>")
			}
			b.WriteString("</" + blk.Kind + ">")
		default:
			b.WriteString("<p>")
			runs(blk.Runs)
			b.WriteString("</p>")
		}
	}
	return template.HTML(b.String())
}

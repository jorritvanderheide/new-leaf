# cv-app

A CV editor. Each CV is Markdown (one file per item and language). You make
versions of it, say one per application: each picks its own items, section
order, language and spacing, with a live preview of the PDF and its page
count. On a server a version can also be shared as an expiring link such as
`https://cv.example.com/uva-k7f3q9ab/`.

It runs two ways, from one binary:

- `cv-app`: on your own computer. Opens the editor in your browser, keeps
  your CV in `~/.local/share/cv-app`, no sign-in, no share links. Stops with
  the Quit button, Ctrl+C, or a few minutes after the last editor closed.
- `cv-app serve`: the multi-user server, with tailnet sign-in and share links.

## Install

With Nix (the flake lives in this repository):

```
nix run git+https://codeberg.org/BW20/cv-app            # try it
nix profile install git+https://codeberg.org/BW20/cv-app # "CV" in your app launcher
```

On NixOS or with home-manager, add the flake as an input and install the
package (Typst comes with it):

```nix
inputs.cv-app.url = "git+https://codeberg.org/BW20/cv-app";

environment.systemPackages = [ inputs.cv-app.packages.${pkgs.system}.default ]; # or home.packages
# or: nixpkgs.overlays = [ inputs.cv-app.overlays.default ]; then use pkgs.cv-app
```

To host it, import the NixOS module and put a web server in front:

```nix
imports = [ inputs.cv-app.nixosModules.default ];
services.cv-app = {
  enable = true;
  publicURL = "https://cv.example.com";
  users = [ "alice" ];      # optional: CVs that always exist, named after a tailnet login
  # manageInEditor = false; # only the CVs in users; none made or deleted in the editor
};
services.nginx.virtualHosts = {
  # The editor: reachable over the tailnet only (e.g. listen on the
  # tailscale address). services.nginx.recommendedProxySettings sets X-Real-IP.
  "cv-editor.example.com".locations."/".proxyPass = "http://unix:${config.services.cv-app.socket}";
  # Share links: static files.
  "cv.example.com".root = config.services.cv-app.publicDir;
};
```

The editor listens on a Unix socket that only the web server's group
(`services.cv-app.proxyGroup`, default `nginx`) may open, so no other program
on the host can reach it and pretend to be a tailnet device.

Without Nix: `go install codeberg.org/BW20/cv-app@latest` (or `go build`), and
install [Typst](https://github.com/typst/typst#installation), which makes the
PDFs. `cv-app -h` and `cv-app serve -h` list the options. A backup of a CV
(Profile page) moves it between computers, and between local and hosted use.

## How it works

- **Editor**: changes save automatically; ⌘K searches items, ⌘S saves at
  once. Locally it only answers on `localhost`. On a server it is tailnet
  only, with no passwords: any (human) tailnet user can open and edit every
  CV via the switcher; `tailscale whois` only keeps out non-peers and
  tagged nodes, and picks your own CV by default.
- **CVs**: "Manage" next to the switcher creates, renames and deletes CVs.
  A CV opens by default for its owners (tailnet logins) or for the login it
  is named after. CVs listed with `-users` (the module's `users`) always
  exist and can't be deleted in the editor; with `-manage=false` those are
  the only ones. Deleted CVs move to `trash/` in the data directory. On a
  server without CVs, the first visitor gets one.
- **Rendering**: one Go binary with the templates, styles and fonts
  embedded. Typst makes the PDFs from `web/typst/cv.typ` (milliseconds per
  PDF); Go templates render the share pages from the same data, so the web
  page and the PDF always match. Languages: English and Dutch. The only
  runtime dependency is the `typst` binary.
- **Versions**: each version selects items and orders sections; items and
  the profile are shared by all of them. A new item joins the versions that
  hold every item, such as "Full CV". Typst reports where each item lands,
  so items in the preview can be clicked to edit or hide them, the item
  list shows where pages break, and the page count says how many lines it
  is over (or under) the pages to fit on. Each version has a look: accent
  colour (with a warning when it is too pale to read), sans or serif (Inter,
  Source Serif 4), photo shape and spacing, for the PDF and the share page
  alike. The overview shows page 1 of each version, rendered in the
  background into the work directory.
- **Vacancy matching**: paste a job ad into a version to see which items
  share its words and which of its words the CV lacks. It runs in the
  browser and is not saved.
- **Share links**: sharing a version publishes it as static files into a
  webroot that a public web server serves, in every language with a toggle
  (the chosen language at `/<slug>/`, others at `/<slug>/<lang>/`), each
  with its PDF. The link follows the version as it changes. Items that
  weren't selected never reach that webroot. Links get a random suffix and
  `noindex` headers. A link has an end date or none; with one, it
  disappears within five minutes of the start of that day
  (Europe/Amsterdam).

## Layout

```
main.go api.go auth.go store.go        server, API, tailnet identity, content files
cvs.go                                which CVs exist; creating, renaming, deleting
versions.go                           versions of a CV; the first ones made from older data
thumbs.go  theme.go                   overview thumbnails; looks (colour, font, photo shape)
document.go                           a CV for one selection and language (sorted, localised, Markdown parsed)
typst.go  share.go  publish.go        PDF, share pages, publishing into the webroot
local.go  backup.go                   local mode; backup and restore
assets.go                             embedded web/ files and the editor pages
web/editor/    editor pages (Go templates), app.js (Alpine.js), editor.css
web/share/     share page template, share.css, web fonts
web/typst/     the PDF template and its fonts
web/css/       Tailwind inputs for editor.css and share.css (generated, committed)
nix/           package, NixOS module, VM test
tests/browser/ browser tests (headless Chromium, made-up CV)
```

Per CV, under the data directory:

```
users/<cv>/cv.json                                   display name and owners
users/<cv>/versions/<id>.json                        a version: name, items, order, language, layout
users/<cv>/content/_index.{en,nl}.md                 profile (summary in the body, section order)
users/<cv>/content/photo.{jpg,png,webp}
users/<cv>/content/<section>/<id>.{en,nl}.md         items
users/<cv>/content/links/<slug>.<lang>.md            share links, one per shared version
```

Sections: work experience, education, publications, other output,
presentations, teaching, grants & awards, extracurricular activities,
volunteering. Publications, other output, presentations and awards have a
single, optional date (undated items go last; an undated publication sorts by
the year in its reference); the rest a period. Dates are YYYY-MM or YYYY. Each
version sets its own section order; empty sections are left out.

## Development

```
nix run .#dev        # editor on :8080 as user "jorrit", share links on :8081
CV_DEV_USER=alice nix run .#dev   # the same, opening another CV first
nix develop          # go, tailwindcss, typst
go test ./...        # includes an end-to-end test with Typst
nix flake check      # package, NixOS VM test, stylesheets up to date
nix run .#browser-tests            # the editor in headless Chromium; SHOTS=dir keeps screenshots
```

`nix run .#dev` reads templates from disk (`-dev-assets .`), so edits show on
reload, and rebuilds `editor.css`/`share.css` when templates change. Commit
the regenerated CSS with the templates; `nix flake check` fails otherwise.

Local data lives in `dev/` (gitignored). Nix flakes only see files tracked
by git, so until the first commit use `path:.` (e.g. `nix run path:.#dev`).

## License

[EUPL-1.2](LICENSE). Bundled: Alpine.js (MIT), pdf.js (Apache-2.0) and the
Inter and Source Serif 4 typefaces (SIL Open Font License 1.1); their
licenses are next to them under `web/`.

# cv-app

A CV editor for several people. Each CV is Markdown (one file per item and
language); per application you pick items, preview the PDF with its page
count, tune the spacing to fit, and publish expiring share links such as
`https://cv.bw20.nl/uva-k7f3q9ab/`.

- **Editor**: tailnet only, no passwords. Any (human) tailnet user can open
  and edit every CV via the switcher in the header; `tailscale whois` only
  keeps out non-peers and tagged nodes, and picks your own CV by default.
- **Rendering**: Hugo renders each user's CV (`web/cv`), headless Chromium
  prints it to PDF. Languages: English and Dutch.
- **Share links**: static files published into a webroot that a public web
  server serves. Items that weren't selected never reach that webroot. Links
  get a random suffix, `noindex` headers, and disappear when they expire.

## Layout

```
main.go auth.go store.go render.go api.go   server (Go, stdlib + YAML)
web/cv/        Hugo site for a CV: print page (PDF) and share pages
web/editor/    Hugo site for the editor UI (Alpine.js)
web/css/       Tailwind inputs for both sites
nix/           package, NixOS module, VM test
```

Per user, under the data directory:

```
users/<user>/content/_index.{en,nl}.md                 profile (summary in the body, section order)
users/<user>/content/photo.{jpg,png,webp}
users/<user>/content/<section>/<id>.{en,nl}.md         items
users/<user>/content/links/<slug>.<lang>.md            share links
```

Sections: work experience, education, publications, other output,
presentations, teaching, grants & awards, extracurricular activities,
volunteering. Publications, other output, presentations and awards have a
single, optional date (undated items go last; an undated publication sorts by
the year in its reference); the rest a period. Dates are YYYY-MM or YYYY. Each
user sets their own section order; empty sections are left out.

## Development

```
nix run .#dev        # editor on :8080 as user "jorrit", share links on :8081
CV_DEV_USER=jeltje nix run .#dev   # the same, opening another CV first
nix develop          # go, hugo, tailwindcss, chromium
go test ./...        # includes an end-to-end test with Hugo and Chromium
nix flake check      # package + NixOS VM test
```

Local data lives in `dev/` (gitignored). Nix flakes only see files tracked
by git, so until the first commit use `path:.` (e.g. `nix run path:.#dev`).

## Deployment

`nixosModules.default` provides `services.cv-app`; dapple's
`/etc/nixos/modules/features/services/cv-app.nix` wraps it with the nginx
vhosts. DNS needed:

- `cv.bw20.nl`: public, pointing at the home IP (port 443 is already forwarded for headscale).
- `cv-editor.bw20.nl`: the tailnet IP, like the other internal services.

Links stop working at the start of their expiry day (Europe/Amsterdam),
within five minutes.

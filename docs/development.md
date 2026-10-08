# Development

## Setup

```sh
nix develop     # go, tailwindcss, typst
nix run .#dev   # the server, against ./dev
```

`nix run .#dev` runs the editor on <http://localhost:8080> as you (your login
name, `$USER`), without Tailscale, and serves share links on
<http://localhost:8081>. It reads the templates and scripts from disk
(`-dev-assets .`), so edits show on reload, and rebuilds the stylesheets as
you go. Set `CV_DEV_USER` to open another CV first. Its data is in `dev/`,
which git ignores; keep real CVs there and nowhere else.

For local mode, `go run .` (or `go run . -data dev/local`, to keep your own
CV out of it).

Without Nix: Go (see `go.mod`), [Typst](https://typst.app) on the `PATH`,
and the [Tailwind CLI](https://tailwindcss.com/docs/installation/tailwind-cli)
v4 for the stylesheets.

## Checks

```sh
go test ./...              # includes an end-to-end test with Typst
nix flake check            # the package, the container image, the NixOS VM test, stylesheets up to date
nix run .#browser-tests    # the editor in headless Chromium
```

CI runs all three, plus `gofmt` and `go vet`, on every push to `main` and
every pull request (`.github/workflows/ci.yml`).

**The stylesheets are generated and committed.** `web/editor/editor.css` and
`web/share/share.css` are made by Tailwind from `web/css/` and the classes in
the templates. `nix run .#dev` keeps them up to date; otherwise run

```sh
tailwindcss --minify -i web/css/share.css -o web/share/share.css
tailwindcss --minify -i web/css/editor.css -o web/editor/editor.css
```

and commit them with the templates. `nix flake check` and CI fail when they
don't match.

Nix flakes only see files git tracks, so a new file needs `jj` or `git add`
to have seen it before `nix flake check` will.

## Tests

- **Go tests** are next to the code they test, `<file>_test.go`, in each
  package. `internal/cv` is tested with plain files;
  `internal/server/e2e_test.go` drives the whole API, with Typst, against a
  temporary data folder.
- **Browser tests** are in `tests/browser/`, one suite per file, run by
  `run.mjs` against a fresh server with a made-up CV (`fixture.mjs`), then
  against local mode. `nix run .#browser-tests -- versions share` runs only
  those suites. Set `SHOTS=<folder>` to keep screenshots.
- **The VM test** (`nix/test.nix`, `checks.vm`) runs the NixOS module in
  three machines: sign-in, that only the proxy's group can reach the socket,
  PDFs and thumbnails inside the sandbox, publishing, managing CVs and moving
  data from cv-app; nginx on its own domains (a visitor on a made-up tailnet
  address, large uploads, the share links' headers); and Tailscale serve, with
  a stand-in `tailscale` that notes what it is asked to serve.

Tests and screenshots use made-up people only. Anything that could lose
someone's CV wants a test before it wants a feature.

## Adding a language

A language is one entry in `languages` in `internal/cv/languages.go`: its code, its name
in itself and in English, and the words New Leaf adds to a CV (section titles,
"Present", short and long month names, how a full date is written, and the
share page's button and expiry note). `TestLanguagesComplete` checks that none
is missing. The editor offers it on the Profile page from then on. Ask a
native speaker to read the words over; they end up on people's CVs.

The code has to be one Typst knows, for hyphenation.

## Releasing

1. Set the version in `nix/package.nix`.
2. Push the commit and a tag `vX.Y.Z` for it to Codeberg. That is the only
   remote: GitHub mirrors it.
3. When the tag reaches GitHub, `.github/workflows/release.yml`:
   - builds the archives with `scripts/release.sh` (six platforms, Typst next
     to `new-leaf`) and `SHA256SUMS`, attests the archives, and creates a
     **draft** GitHub release with them;
   - checks the tag against the package version, builds the container image
     with Nix for amd64 and arm64, pushes it to
     `ghcr.io/jorritvanderheide/new-leaf` as `vX.Y.Z` and `latest`, and
     attests it.
4. Publish the draft.

`scripts/release.sh <version> <folder>` also runs locally.

Typst in the archives is downloaded from its releases by `scripts/typst.sh`
and checked against the checksums pinned there. Keep its version in step with
nixpkgs (`nix develop -c typst --version`), and update the checksums with it.

Before releasing anything that renames or removes a file, key or section, see
[Compatibility](data-model.md#compatibility).

## Updating an action

The workflows pin every action to a commit, with the version in a comment, so a
tag that is moved later can't change what builds a release. Nothing updates
them automatically, so look at them before a release. To move one to a newer
version, look up the commit the tag points to and replace both the hash and the
comment:

```sh
gh api repos/actions/checkout/commits/v6.1.0 --jq .sha
```

Both workflows also default to read-only. Only the release jobs ask for more:
writing the release, pushing the image, and signing the attestations.

CI downloads the Tailwind CLI with a pinned checksum, too. When nixpkgs moves
to a newer Tailwind, update the version and checksum in `ci.yml`.

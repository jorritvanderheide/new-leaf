# Compatibility

CVs written by New Leaf live on other people's computers and servers, and
servers are set up with its options. This page says what a release may and
may not change. Changes are listed in [CHANGELOG.md](../CHANGELOG.md).

From 1.0, New Leaf's version is `MAJOR.MINOR.PATCH`. A minor or patch release
breaks nothing below. A major release may, and the changelog then says what
changed and what to do. Before 1.0, any release may break things, but CVs are
always migrated.

## Data files

Every file in [Data model](data-model.md): the profile, items, versions, share
links' files and `cv.json`.

- **`format`** in `content/_index.md` is the layout of a CV's files. A release
  reads every older format and moves the CV over when it is opened, or when a
  backup of one is restored. A CV in a newer format than the release knows is
  refused, not read in part.
- **Renaming or removing** a front matter key, a JSON key, a section or a file
  name is breaking. It raises `format` and needs a migration in `Migrate`
  (`internal/cv/profile.go`), with a test that migrates a CV from before it.
- **Changing what a value means**, such as a default, is breaking too.
  That's why a version without an accent is still teal: green became the
  default later.
- **Adding** an optional key is not. An older New Leaf ignores it, and drops it
  when it saves that file.
- **Removing a language** from `internal/cv/languages.go` is breaking: CVs in it
  would lose their text.
- **Backups** from every earlier release restore.

Going back to an older release is not supported. A release from before its
format refuses a CV, but 0.5 and older don't know `format` at all, and show a
migrated CV without its name and contact details. Download a backup first.

## Options

- **Command-line options** of `new-leaf` and `new-leaf serve`, and the
  `NEW_LEAF_*` variables that set them.
- **NixOS options** under `services.new-leaf`.
- **The container image**: its tags, the `/data` volume, the user it runs as
  (1000), and the addresses it listens on.

Renaming or removing one, or changing its default so a setup behaves
differently, is breaking. A renamed NixOS option keeps working, with a
warning, until the next major release. Adding one is not breaking.

## Share links

A share link's address, `<public url>/<slug>/`, and its PDF, `cv.pdf` next to
the page, keep working for as long as the link is shared and hasn't expired,
also across major releases. The page has every language of the CV on it, at
`<slug>/#<lang>`; the PDF in another language is at `<slug>/<lang>/cv.pdf`,
and `<slug>/<lang>/`, where that language's page was before 0.7, sends
visitors on.

## Not covered

These can change in any release:

- How a PDF or share page looks, and their HTML, CSS and fonts. A PDF can get
  longer or shorter.
- The editor and its API (`/api/...`): it is only for the editor's own pages.
- The work folder: everything in it can be made again.
- The Go packages under `internal/`.

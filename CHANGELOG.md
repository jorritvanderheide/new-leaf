# Changelog

What changed in each release of New Leaf, newest first. What counts as
breaking is in [Compatibility](docs/compatibility.md). Releases before 0.6
are listed on the [releases page](https://github.com/jorritvanderheide/new-leaf/releases).

## Unreleased

- A backup is checked as fully as opening the CV would: one with versions
  that can't be read is refused, instead of restoring and then never
  opening.
- A backup may unpack to at most 100 MB and 5,000 files, so a small zip
  can't fill the disk.
- A photo may be at most 40 megapixels. A small file can claim to be a huge
  image, which would take the server gigabytes to open; one already there is
  left out of PDFs and share pages.
- An item named after a long organisation, over 64 characters, can be saved:
  its id is shortened. JSON Resume imports with one failed altogether.
- A JSON Resume job with only an end date is imported as ending then, not as
  going on until now.
- Two small races: deleting two CVs at once could remove the last one, and a
  restore could briefly take the CV's share pages offline.
- **One share page per link, with every language on it.** It opens in the
  language you choose when sharing (the version's, unless you pick another),
  and visitors switch with the language buttons, on the same page and without
  script. Addresses you already shared keep working: `/<slug>/<lang>/` sends
  visitors on. A link is one file now, `links/<slug>.md`, with `name`, `lang`
  and `expires`.
- **One default accent.** A version or link saved without an accent was teal,
  from before green was the default; the migration writes teal into those, so
  green is the default everywhere.
- The CV format is now 2; CVs and backups move over when opened or restored.
- The NixOS option `manageInEditor` is now `manage`, as the flag `-manage`. The
  old name keeps working, with a warning, until 2.0.

## 0.6.1

- **Security: behind `tailscale serve`, a tailnet user could sign in as
  another** by sending an `X-Real-IP` header with their address. That
  affects the NixOS module's `tailscaleServe` and the container; nginx was
  safe. Update if you use either.
- **Security: a restored backup could take over another CV's share page.** A
  restored share link now keeps its address only if no other CV uses it.
- **Security: on your own computer, New Leaf only answers this computer**,
  also when `-listen` is set to an address the network can reach. Before, a
  request from elsewhere that claimed to be for `localhost` got in.
- With `ownersOnly`, a `cv.json` that can't be read closes its CV instead of
  opening it to everyone.
- Configured CVs (`users`) can be given as a full tailnet login, for exactly
  that login, which matters with `ownersOnly` on a tailnet with people from
  other domains.

## 0.6.0

- **A format version for CVs.** The profile's shared fields (name, contact
  details, links, languages and the defaults for new versions) are now in
  `content/_index.md`, with `format: 1`, and `_index.<lang>.md` only holds the
  text. CVs and backups from before are moved over when they're opened or
  restored. A New Leaf from before this release shows a CV that has been moved
  over without its name and contact details, so download a backup before going
  back to one.
- **Share links end in the server's time zone**, instead of always in
  Europe/Amsterdam. Set it with `-timezone` (`NEW_LEAF_TIMEZONE`, or
  `services.new-leaf.timeZone`, which follows `time.timeZone`). The container
  image's own time zone is UTC.
- **PDFs that machines read well.** The name, sections and items are tagged
  as headings, the sections are bookmarks, and the photo has alt text, so
  screen readers and applicant tracking systems get the CV's structure. The
  PDFs look exactly as before.
- **JSON Resume, in and out.** Import one on the Profile page to start from a
  CV made elsewhere, or download yours in one of its languages. Sections that
  JSON Resume doesn't have go out as projects, and come back where they were.
- **CVs for their owners only**, as an option for a tailnet shared with people
  who shouldn't edit each other's CVs: `-owners-only`, or
  `services.new-leaf.ownersOnly`. You own the CVs you make, and a configured
  CV is owned by the person it's named after. A CV without owners stays open
  to everyone until someone adds themselves.
- **`/healthz`** on the editor, for monitoring: `ok`, or `503` and what's
  wrong. It needs no tailnet identity.
- **A stricter content security policy for the editor**, without
  `unsafe-eval`: Alpine is now its CSP build. A CV's text can't be run as
  script anyway, and now no expression can be evaluated from a string either.
- **Better with a screen reader.** In the preview, each item is a group
  named after it, with Edit and Hide buttons that show when they have focus,
  and the pages are named. In the outline, Alt+↑/↓ on a section's handle says
  where the section went, the All and None buttons say which section they're
  for, and a missing translation is read out.
- The title of an item being edited now also shows for a CV without English
  or Dutch.
- A new logo.

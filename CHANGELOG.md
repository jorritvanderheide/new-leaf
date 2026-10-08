# Changelog

What changed in each release of New Leaf, newest first. What counts as
breaking is in [Compatibility](docs/compatibility.md). Releases before 0.6
are listed on the [releases page](https://github.com/jorritvanderheide/new-leaf/releases).

## Unreleased

- **Security: behind `tailscale serve`, a tailnet user could sign in as
  another** by sending an `X-Real-IP` header with their address. That
  affects the NixOS module's `tailscaleServe` and the container; nginx was
  safe. Update if you use either.
- **Security: a restored backup could take over another CV's share page.** A
  restored share link now keeps its address only if no other CV uses it.
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

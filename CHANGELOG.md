# Changelog

What changed in each release of New Leaf, newest first. What counts as
breaking is in [Compatibility](docs/compatibility.md). Releases before 1.0
are listed on the [releases page](https://github.com/jorritvanderheide/new-leaf/releases).

## Unreleased

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
- A new logo.

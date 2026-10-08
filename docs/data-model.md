# Data model

New Leaf keeps a CV as a folder of plain files: Markdown with YAML front
matter for what you write, JSON for what the editor decides. Nothing is in a
database, so a CV can be read, backed up and moved with ordinary tools. This
page lists every file it reads and writes.

## The data folder

```
users/<cv>/cv.json                              display name and owners
users/<cv>/content/_index.md                    profile: what it has once, and the format
users/<cv>/content/_index.<lang>.md             profile: its text, per language
users/<cv>/content/photo.{jpg,png,webp}         profile photo
users/<cv>/content/<section>/<id>.<lang>.md     one item, per language
users/<cv>/content/links/<slug>.<lang>.md       share link of a version (server only)
users/<cv>/versions/<id>.json                   one version
users/<cv>/backups/before-import-<time>.zip     what a restore replaced
trash/<cv>-<time>/                              a deleted CV
```

On your own computer the data folder is `~/.local/share/new-leaf` on Linux,
`~/Library/Application Support/new-leaf` on macOS and `%AppData%\new-leaf` on
Windows, and there is one CV, named after your login so it can move to a
server later. On a server it is `-data` (`/var/lib/new-leaf`).

`<cv>`, `<id>` and `<slug>` are lowercase letters, digits and hyphens, at most
64 characters, starting with a letter or digit.
`<lang>` is a language code, such as `en` (see [Languages](#languages)).

## The profile

`_index.md` holds what the profile has once, in its front matter:

| Key | Value |
| --- | --- |
| `format` | The layout of the CV's files, `1` (see [Compatibility](compatibility.md#data-files)) |
| `languages` | The CV's languages, the main one first (see [Languages](#languages)) |
| `name` | Your name |
| `email`, `phone`, `website` | Contact details |
| `links` | A list of `label` and `url`, such as LinkedIn |
| `order` | The default section order, for new versions |
| `theme` | The default look, for new versions (see [Looks](#looks)) |
| `spacing` | The default spacing, for new versions, from `0.4` to `1.4`; none means `1` |

`_index.<lang>.md` holds the text, one file per language. The body is the
summary, in Markdown. The front matter:

| Key | Value |
| --- | --- |
| `headline` | One line under the name, such as a job title |
| `location` | Where you live, as it should read on the CV |

A CV from before `format` (New Leaf 0.5 and older) has no `_index.md`: the
shared keys were in every `_index.<lang>.md`. It is moved over the first time
it is opened, and when a backup of one is restored.

## Items

`<section>/<id>.<lang>.md`, one file per language of the CV. The
`<id>` is made from the organisation or the title when the item is created,
and never changes. The body is the description, in Markdown.

| Key | Per language | Value |
| --- | --- | --- |
| `title` | yes | The role, degree, title of the publication |
| `org` | yes | The organisation |
| `location` | yes | Where |
| `start` | no | `YYYY-MM` or `YYYY` |
| `end` | no | The same; none means ongoing |
| `link` | no | A URL, such as a DOI |

The sections, in their default order: `experience`, `education`,
`publications`, `output`, `presentations`, `teaching`, `awards`,
`extracurricular`, `volunteering`. Publications, other output, presentations
and awards happen at one moment: they have `start` only, and may leave it out
(a manuscript under review). Every other section has a period.

Items are always shown newest first: ongoing ones, then by end date, the
longer first when two end together. Undated items go last, except an undated
publication, which sorts by the year in its reference, such as `(2026)`.

The Markdown a CV uses is paragraphs, lists, bold, italic and links.
Anything else (headings, code, HTML) is shown as plain text or left out, and
only links to the web, mail and phone are kept.

## Languages

A CV is in one or two languages, `languages` in the profile, the main one
first. A new CV is in English. The languages New Leaf knows are in
`internal/cv/languages.go`: English (`en`), Dutch (`nl`), German (`de`), French (`fr`),
Spanish (`es`), Italian (`it`) and Portuguese (`pt`), each with the words it
adds to a PDF and share page.

- **New versions** start in the main language, and the editor names items by
  their title in it.
- **A language the CV loses** keeps its files. They are hidden, its items'
  files still get the shared keys (dates, links) when those change, and the
  text comes back with the language. Versions in it move to the main language; a shared
  version keeps it from being removed.
- **A share link** is published in each of the CV's languages, and its files
  follow them.

A CV from before languages could be chosen has no `languages` key. It is read
as English, plus Dutch if it has Dutch files, which is what it was.

## Versions

`versions/<id>.json`. Items and the profile belong to the CV; a version only
chooses from them.

| Key | Value |
| --- | --- |
| `name` | The name in the overview, such as the employer |
| `lang` | One of the CV's languages |
| `entries` | The items, as `<section>/<id>` |
| `photo` | Whether the PDF shows the photo |
| `spacing` | Whitespace, from `0.4` to `1.4`; none means `1` |
| `order` | The section order; none means the profile's |
| `theme` | The look (see [Looks](#looks)) |
| `fit` | The page count to fit on, 1 to 4; none means 2 |
| `pages` | The page count at the last preview, for the overview |
| `created`, `updated` | When |

A new item joins every version that held every item before it, such as the
Full CV, and no other. A CV with no `versions/` folder (from before versions)
gets one when it's opened, made from what it had.

## Looks

The `theme` of a version, and of the profile for new versions:

| Key | Value |
| --- | --- |
| `accent` | A colour, `#rrggbb`. None means `#15803d` (green) in the profile, and `#00696a` (teal) in a version or share link: those are from when teal was the default. New versions get the accent written out |
| `font` | `sans` (Inter) or `serif` (Source Serif 4); none means `sans` |
| `photo` | `rounded`, `circle` or `square`; none means `rounded` |

## Share links

On a server only. `links/<slug>.<lang>.md` is a link to a version, rendered in
`<lang>`. The slug is the version's name plus 8 random characters, so the
address can't be guessed. Sharing again makes a new one.

| Key | Value |
| --- | --- |
| `version` | The version it shows |
| `title`, `entries`, `photo`, `spacing`, `order`, `theme` | A copy of the version, kept up to date |
| `url` | Where it's published |
| `expiryDate` | `YYYY-MM-DD`: offline from the start of that day, in the server's time zone (`-timezone`). None: no end date |
| `created` | `YYYY-MM-DD` |

What a link publishes is described in
[Self-hosting](self-hosting.md#what-the-server-writes).

## cv.json

On a server, the CV's display name (`name`) and its owners (`owners`, tailnet
logins it opens for by default). Both are optional: without a name the CV is
shown by its folder's name.

## Backups

**Download backup** makes a zip of `content/` and `versions/`. **Restore**
takes one, refuses any file a CV doesn't consist of, and keeps the CV it replaces as
`backups/before-import-<time>.zip`. Backups from before versions, with a
`compose.json` instead of `versions/`, still restore.

## The work folder

Fonts for Typst and the overview's thumbnails, in your cache folder
(`~/.cache/new-leaf` on Linux) or `-work` on a server. Everything in it can be
made again, so it's safe to delete.

Thumbnails are `thumbs/<cv>/<version>.<key>.<pages>.png`, where the key is a
hash of everything the PDF shows. A changed version gets a new file instead
of an outdated one; the old file is removed.

## Compatibility

What a release may change in these files, and how a CV from an older one is
moved over, is in [Compatibility](compatibility.md).

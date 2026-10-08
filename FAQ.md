# Frequently asked questions

Can't find your answer here? Please
[open an issue](https://codeberg.org/BW20/new-leaf/issues/new/choose).

- [Starting New Leaf](#starting-new-leaf)
- [Your CV and versions](#your-cv-and-versions)
- [The PDF](#the-pdf)
- [Share links](#share-links)
- [Running it on a server](#running-it-on-a-server)

## Starting New Leaf

### macOS says it can't check `new-leaf` for malicious software

The programs in the release aren't signed with an Apple developer account. The
first time, right-click `new-leaf` in Finder and choose Open, then Open again.
Or run this in Terminal, in the folder you unpacked:

```sh
xattr -d com.apple.quarantine new-leaf typst
```

### Windows says it protected my PC

The same: the programs aren't signed. Choose *More info*, then *Run anyway*.

### "Typst makes the PDFs and was not found"

New Leaf uses [Typst](https://typst.app) to make PDFs. The release archives
have it next to `new-leaf`: keep the two files together. If you built New Leaf
yourself, install Typst so it's on your `PATH`, or point `-typst` at it.
Everything except the PDFs works without it.

### "New Leaf is already running"

It is, in another window or tab, so New Leaf opens that one instead of
starting a second time. It stops a few minutes after the last tab is closed,
or when you choose **Quit** in the menu at the bottom of the sidebar.

### Where is my CV?

In the data folder: `~/.local/share/new-leaf` on Linux,
`~/Library/Application Support/new-leaf` on macOS, `%AppData%\new-leaf` on
Windows. New Leaf prints it when it starts. It's a folder of Markdown files,
described in [Data model](docs/data-model.md).

### I used cv-app. Where did my CV go?

New Leaf was called cv-app. The first time it starts, it moves your CVs from
cv-app's folder (such as `~/.local/share/cv-app`) to its own, and says so.
Nothing is lost, and nothing changes in the files themselves.

## Your CV and versions

### I added an item, but it's not in my version

New items only join the versions that show every item, like the Full CV. A
version you tailored for an application stays as you made it: tick the new
item there if you want it.

### Can I change the order of items within a section?

No, items are always newest first, the way readers expect a CV. You can drag
whole sections into the order that suits each version.

### An item has a warning sign

It has no text in the language of this version yet, so the PDF would show an
empty title. Click it and fill in this language; **Start from** the other one
copies its text over for you to translate.

### Which languages can my CV be in?

One or two of English, Dutch, German, French, Spanish, Italian and
Portuguese, chosen under **Languages** on the Profile page. New Leaf adds a
few words of its own to the PDF and share page, such as the section titles and
the months, and those are translated for these languages. Missing yours, or a
title that reads wrong? Open an issue: a language is one table of words
(`internal/cv/languages.go`).

### I removed a language. Is its text gone?

No. It stays on disk, and in backups, and comes back when you add the language
again. Versions in that language move to your main one; a shared version keeps
the language from being removed until you stop sharing it or change its
language.

### How do I move my CV to another computer?

**Download backup** on the Profile page, and **Restore** it on the other
computer. A restore keeps what it replaces as a backup first. It works between
your computer and a server too.

### Can I bring my CV from another tool?

If it exports [JSON Resume](https://jsonresume.org): **Import JSON Resume** on
the Profile page. It replaces your current CV (kept as a backup first) with
the resume's text, in your CV's main language, and keeps your photo and look.
Skills, languages, interests and references have no section in New Leaf, so
it tells you what it left out. **Download JSON Resume** goes the other way.

### Can I undo?

Changes to a version (items, order, language, look) have undo and redo, ⌘Z and
⇧⌘Z. Deleting an item or a version shows an **Undo** button for a few
seconds. Text you type has your browser's own undo.

## The PDF

### Fit on 2 pages says it doesn't fit

Even at the tightest spacing, the version needs more pages. The item list
shows where each page starts, and the page count says how many lines you are
over. Untick an item, or shorten a description, and try again.

### Can I change the layout of the PDF itself?

The look of each version has a colour, a typeface, the photo shape and the
spacing. The layout is one design, in `web/typst/cv.typ`; see [PDF](docs/pdf.md).

### My photo isn't on the PDF

Open the version's **Look** and tick **Show** under Photo. If it says **Add a
photo on your Profile** instead, there's no photo yet: add one there.

## Share links

Share links need New Leaf running on a server, see
[Self-hosting](docs/self-hosting.md). On your own computer there is no
**Share** button.

### I changed my version. Does the link show the old one?

No, a link follows its version: the page is updated within seconds of a change.

### How do I take a link offline?

**Shared** → **Stop sharing**. The page is gone at once. Deleting the version
does the same. Sharing again gives a new address, so the old one never comes
back.

### When does a link expire?

At the start of the day you chose, in the server's time zone, within five
minutes. Or never, with **No end date**.

## Running it on a server

### "This editor is only available to approved users on the tailnet"

The editor only answers people on your Tailscale network, and asks Tailscale
who they are. You see this when the request didn't come through the reverse
proxy, came from a device that isn't on the tailnet, or from a tagged device
such as a server. See [Self-hosting](docs/self-hosting.md#how-sign-in-works).

### Someone else's CV opens instead of mine

A CV opens by default for its owners, or for the person it's named after. In
**Manage CVs**, add your tailnet login as an owner of your CV. Which CV you
picked last is remembered per browser.

### Can others on the tailnet edit my CV?

Yes, unless the server is set to owners only (`ownersOnly` in the NixOS
module, `-owners-only` otherwise). Then only a CV's owners see and change it.
A CV without owners stays open to everyone, so add yourself as its owner
under **Manage CVs**.

### I can't delete a CV

It's listed in the server's configuration (the `users` setting), so it would
only come back. Remove it there instead.

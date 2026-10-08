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
empty title. Click it and fill in the other language; **Start from English**
(or Dutch) copies the text over for you to translate.

### Why only English and Dutch?

Those are the languages it was made for, and the labels on the PDF and share
page exist in both. Adding a language means translating those labels; open an
issue if you'd like one.

### How do I move my CV to another computer?

**Download backup** on the Profile page, and **Restore** it on the other
computer. A restore keeps what it replaces as a backup first. It works between
your computer and a server too.

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

Tick **Photo** in the version's toolbar. If it says **Add a photo** instead,
there's no photo yet: add one on the Profile page.

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

At the start of the day you chose, in the Netherlands' time zone, within five
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

### I can't delete a CV

It's listed in the server's configuration (the `users` setting), so it would
only come back. Remove it there instead.

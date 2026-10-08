# Security policy

## Supported versions

Security fixes go into the latest release of New Leaf. Older releases don't
get separate fixes, so please update before you report.

## Reporting a problem

Please don't open a public issue for a security problem. Codeberg, where the
issues are, has no private reports, so report it privately through the GitHub
mirror instead:

https://github.com/jorritvanderheide/new-leaf/security/advisories/new

Include the New Leaf version (`new-leaf -version`), how you run it (on your own
computer, with the NixOS module, with Docker), your operating system, and the
steps to reproduce it. Reports are looked at before anything about them is made
public.

## What counts

On your own computer, New Leaf only answers requests for `localhost`, writes to
its data and cache folders, and runs Typst on your CV, as listed in
[section 10 of the README](README.md#10-network-and-file-disclosure). On a
server, the editor only answers people on the tailnet, and share links publish
only the items of their version; see [Self-hosting](docs/self-hosting.md).

So these are security problems:

- Reaching the editor from another computer, from a website, or, on a server,
  from outside the tailnet or without going through the reverse proxy.
- Changing a CV from another website, without the editor open.
- A share link that shows items its version doesn't, or that can be found
  without knowing its address.
- Reading or writing files outside the data, cache and public folders.
- Text in a CV, a backup or a profile photo that makes New Leaf or Typst run
  code, or that runs as script on a share page.

A bug that changes or loses text in your CV is serious too, but it isn't
secret: please report that as a normal issue, so others can see it.

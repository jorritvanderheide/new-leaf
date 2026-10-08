# Self-hosting

On a server, New Leaf is an editor for everyone on your
[Tailscale](https://tailscale.com) network, plus share links on the public web.
It is the same program as on your own computer, started as `new-leaf serve`.

- [How sign-in works](#how-sign-in-works)
- [CVs on a server](#cvs-on-a-server)
- [With NixOS](#with-nixos): [Tailscale only](#tailscale-only) or [your own domains](#your-own-domains)
- [With Docker](#with-docker)
- [Anything else](#anything-else)
- [Command-line options](#command-line-options)
- [What the server writes](#what-the-server-writes)

## How sign-in works

There are no passwords. The editor only answers people on your tailnet, and
asks Tailscale who each of them is:

1. A reverse proxy that is only reachable over the tailnet passes each request
   to New Leaf, with the visitor's tailnet address in `X-Real-IP` (or
   `X-Forwarded-For`, as `tailscale serve` sets it).
2. New Leaf only believes that header from the proxy itself: a connection on its
   Unix socket, which only the proxy may open, or from `127.0.0.1`. Anything
   else is taken at its own address. A proxy may pass the other header on as
   the visitor sent it, so when both are there they must name the same
   address (nginx sets both), or the request is turned away.
3. It runs `tailscale whois` on the address. Devices that aren't on the tailnet,
   and tagged devices such as servers, are turned away.
4. Everyone else may open and edit every CV, or with `-owners-only` only
   their own (see [CVs on a server](#cvs-on-a-server)). Their own CV opens
   first.

Writes must also carry an `X-New-Leaf` header, which a browser only sends from
the editor itself, so other websites can't change a CV on a visitor's behalf.

## CVs on a server

- **Configured CVs** (`-users`, or the module's `users`) always exist, named
  after a tailnet login. They can be renamed in the editor but not deleted.
- **More CVs** can be made, renamed and deleted in the editor, under **Manage
  CVs**, unless that's turned off (`-manage=false`, or `manageInEditor =
  false`). Deleted CVs are moved to `trash/` in the data folder.
- **Which CV opens:** the one picked last in this browser, or one the visitor
  owns (set under **Manage CVs**), or the one named after their login, or the
  first. On a server with no CVs at all, the first visitor gets one.
- **Owners only** (`-owners-only`, or `ownersOnly = true`), for a tailnet
  shared with people who shouldn't edit each other's CVs: a CV is then only
  listed for, and opened and changed by, its owners. You own the CVs you make,
  and only owners change who the owners are. A configured CV is owned by the
  person it's named after. A CV without owners stays open to everyone, with a
  warning under **Manage CVs**, until someone adds themselves. Someone who
  owns no CV gets a new one, named after them.

## With NixOS

The flake has a module. It runs New Leaf as its own user, in a systemd sandbox,
on a socket that only the proxy in front of it may open, so no other program on
the server can reach the editor and claim to be someone on the tailnet. It can
also set up that proxy, in one of two ways:

- [**Tailscale only**](#tailscale-only): no domain, web server, certificates or
  open ports. Tailscale serves the editor on your tailnet and the share links
  on the internet, through Funnel.
- [**Your own domains**](#your-own-domains): nginx serves both, for instance
  because you run headscale, which has no Funnel, or want the links on your
  own domain.

Either way, add the flake and import the module:

```nix
{
  inputs.new-leaf.url = "git+https://codeberg.org/BW20/new-leaf";

  # in your NixOS configuration:
  imports = [ inputs.new-leaf.nixosModules.default ];
}
```

### Tailscale only

```nix
services.tailscale.enable = true;

services.new-leaf = {
  enable = true;
  tailscaleServe.enable = true;
  publicURL = "https://myserver.my-tailnet.ts.net"; # this machine's name
  users = [ "alice" ]; # optional: CVs that always exist
};
```

The editor is then at `https://myserver.my-tailnet.ts.net:8443`, for the
tailnet only, and share links at `https://myserver.my-tailnet.ts.net/<link>/`.
In the Tailscale admin console, first:

1. Turn on HTTPS certificates (DNS → HTTPS Certificates).
2. Allow Funnel for this machine: the `funnel` node attribute in the tailnet
   policy.

A small service, `new-leaf-tailscale-serve`, tells Tailscale what to serve:
the editor straight from New Leaf's socket, and the share links from New
Leaf's own server for them on `127.0.0.1:8081`, with the headers they need.
Tailscale keeps those settings; after turning this off, `tailscale serve
reset` removes them.

### Your own domains

```nix
services.new-leaf = {
  enable = true;
  users = [ "alice" ];
  nginx.editor = {
    domain = "cv-editor.example.com";
    listenAddresses = [ "100.64.0.1" ]; # this machine's tailnet address
  };
  nginx.share = {
    domain = "cv.example.com"; # publicURL follows
    rootRedirect = "https://example.com/"; # optional; a 404 without
  };
};

# Certificates are yours to choose, on the same hosts:
security.acme.acceptTerms = true;
security.acme.defaults.email = "you@example.com";
services.nginx.virtualHosts = {
  "cv-editor.example.com" = { enableACME = true; forceSSL = true; };
  "cv.example.com" = { enableACME = true; forceSSL = true; };
};
networking.firewall.allowedTCPPorts = [ 80 443 ];
```

The module makes both nginx hosts:

- **The editor** is proxied to New Leaf's socket, with the visitor's address in
  `X-Real-IP`, room for backups up to 50 MB and time for fitting pages.
  Listening on the tailnet address only keeps it off the internet: New Leaf
  would turn strangers away anyway, but needn't be found.
- **The share links** are static files with the headers they need: kept out
  of search engines, a strict content security policy, no directory listings,
  and a 404 for anything starting with a dot.

The editor's certificate can't come from Let's Encrypt's HTTP check when its
host only listens on the tailnet: use a DNS check
(`security.acme.certs.<domain>.dnsProvider`) or a certificate of your own.

### All options

| Option | Default | What it does |
| --- | --- | --- |
| `enable` | `false` | Run New Leaf |
| `publicURL` | from `nginx.share.domain` | Where the share links are |
| `users` | `[ ]` | CVs that always exist, by tailnet login |
| `manageInEditor` | `true` | Make, rename and delete CVs in the editor |
| `ownersOnly` | `false` | Only a CV's owners can open and edit it |
| `timeZone` | `time.timeZone` | The time zone share links end in; `null` for the system's |
| `tailscaleServe.enable` | `false` | Serve with Tailscale: the editor on the tailnet, links through Funnel |
| `tailscaleServe.editorPort` | `8443` | The editor's port on the tailnet |
| `nginx.editor.domain` | `null` | Serve the editor with nginx on this domain |
| `nginx.editor.listenAddresses` | `[ ]` | Where the editor listens, such as the tailnet address. Empty: every address. |
| `nginx.share.domain` | `null` | Serve the share links with nginx on this domain |
| `nginx.share.rootRedirect` | `null` | Where the domain without a link goes; `null` for a 404 |
| `servePublic` | `null` | Also serve the share links from New Leaf itself, on this address |
| `proxyGroup` | nginx's group, or `"new-leaf"` | The only group that may open the editor's socket |
| `listen` | `null` | A TCP address instead of the socket. Any program on the server can then reach the editor, so prefer the socket. |
| `socket` | `/run/new-leaf/editor.sock` | Read-only: where the editor listens |
| `publicDir` | `/var/lib/new-leaf/public` | Read-only: the folder the share links are in |
| `tailscalePackage` | `services.tailscale.package` | The `tailscale` used for `whois`, and to serve |

Another reverse proxy works too: point it at `socket`, set `proxyGroup` to its
group, make it reachable from the tailnet only and have it set `X-Real-IP`.
Serve `publicDir` with the headers above, or put `servePublic` behind it.

The flake's `checks.vm` runs the module in virtual machines: sign-in, the
socket, PDFs inside the sandbox, publishing, CV management, moving data from
cv-app, and both ways of serving it.

## With Docker

[`deploy/compose.yaml`](../deploy/compose.yaml) runs the server image next to a
Tailscale container. Tailscale serves the editor on your tailnet and the share
links on the public web through
[Funnel](https://tailscale.com/kb/1223/funnel), both at
`https://cv.<your tailnet>.ts.net`, so you need no domain, certificate or web
server of your own.

1. In the Tailscale admin console, make an auth key, and allow Funnel for this
   machine (the `funnel` node attribute in the tailnet policy).
2. Next to `compose.yaml`, make a `.env` with:
   ```sh
   TS_AUTHKEY=tskey-auth-...
   NEW_LEAF_PUBLIC_URL=https://cv.<your tailnet>.ts.net
   NEW_LEAF_USERS=alice   # optional
   NEW_LEAF_TIMEZONE=Europe/Amsterdam   # optional; the container's is UTC
   ```
3. `docker compose up -d`

The editor is then at `https://cv.<your tailnet>.ts.net:8443`, for the tailnet
only, and share links at `https://cv.<your tailnet>.ts.net/<link>/`. Everything
New Leaf keeps is in the `new-leaf` volume.

The image is `ghcr.io/jorritvanderheide/new-leaf`, for amd64 and arm64. It's
built with Nix (`nix build .#docker`) and runs as an unprivileged user. Both of
its servers listen on `127.0.0.1` only, which is why it shares the Tailscale
container's network.

## Anything else

`new-leaf serve` runs anywhere Typst does. What it needs around it:

- **A reverse proxy for the editor** that only the tailnet can reach, and that
  sets `X-Real-IP` or `X-Forwarded-For`. Put New Leaf on a Unix socket
  (`-listen unix:/path`), or on `127.0.0.1` with the proxy on the same machine.
- **A web server for the share links** that serves the `-public` folder as
  static files, or `-serve-public` for New Leaf's own.
- **The `tailscale` command**, talking to the machine's Tailscale.

## Monitoring

`GET /healthz` on the editor's address answers `ok` when New Leaf can write
CVs and share links and run Typst, and `503` with what's wrong when it can't.
It needs no tailnet identity, so a monitor can reach it through the same proxy
as the editor. The share links are static files: check those with any file in
the public folder, such as `/fonts/OFL.txt`.

## Command-line options

`new-leaf serve -h` lists them. Every option can also be set in the environment,
as `NEW_LEAF_` and its name in capitals: `NEW_LEAF_PUBLIC_URL` for
`-public-url`. The command line wins.

| Option | Default | What it does |
| --- | --- | --- |
| `-listen` | `127.0.0.1:8080` | The editor's address: `host:port`, `unix:/path`, or `systemd` for a socket from systemd |
| `-data` | `/var/lib/new-leaf` | The CVs |
| `-work` | `<data>/work` | Files that can be made again: Typst's fonts, thumbnails |
| `-public` | `/var/lib/new-leaf/public` | The folder share links are published into |
| `-public-url` | `https://cv.example.com` | Where that folder is served |
| `-serve-public` | | Also serve that folder on this address, with the headers it needs |
| `-users` | | CVs that always exist, comma-separated tailnet logins |
| `-manage` | `true` | Make, rename and delete CVs in the editor |
| `-owners-only` | `false` | Only a CV's owners can open and edit it |
| `-timezone` | the system's, or `TZ` | The time zone share links end in, such as `Europe/Amsterdam` |
| `-tailscale` | `tailscale` | The Tailscale command, for `whois` |
| `-typst` | next to `new-leaf`, or on the `PATH` | The Typst command |
| `-version` | | Print the version |

## What the server writes

- **CVs:** in the data folder, as on your own computer. See
  [Data model](data-model.md).
- **Share links:** published into the public folder: a page and a PDF per
  language, and a stylesheet per look. Only the items of the version are in
  them. New Leaf only manages a folder that is empty or that it made itself (it
  leaves a `.new-leaf-public` file in it), and every five minutes it removes
  what no link needs anymore, such as expired links.
- **Thumbnails and fonts:** in the work folder.
- **Nothing else.** The server makes no connections of its own except to the
  local Tailscale, for `whois`.

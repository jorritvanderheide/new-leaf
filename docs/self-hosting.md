# Self-hosting

On a server, New Leaf is an editor for everyone on your
[Tailscale](https://tailscale.com) network, plus share links on the public web.
It is the same program as on your own computer, started as `new-leaf serve`.

- [How sign-in works](#how-sign-in-works)
- [CVs on a server](#cvs-on-a-server)
- [With NixOS](#with-nixos)
- [With Docker](#with-docker)
- [Anything else](#anything-else)
- [Options](#options)
- [What the server writes](#what-the-server-writes)

## How sign-in works

There are no passwords. The editor only answers people on your tailnet, and
asks Tailscale who each of them is:

1. A reverse proxy that is only reachable over the tailnet passes each request
   to New Leaf, with the visitor's tailnet address in `X-Real-IP` (or
   `X-Forwarded-For`, as `tailscale serve` sets it).
2. New Leaf only believes that header from the proxy itself: a connection on its
   Unix socket, which only the proxy may open, or from `127.0.0.1`. Anything
   else is taken at its own address.
3. It runs `tailscale whois` on the address. Devices that aren't on the tailnet,
   and tagged devices such as servers, are turned away.
4. Everyone else may open and edit every CV. Their own CV opens first.

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

## With NixOS

The flake has a module. It runs New Leaf as its own user, in a systemd sandbox,
on a socket that only your reverse proxy's group may open, so no other program
on the server can reach the editor and claim to be someone on the tailnet.

```nix
{
  inputs.new-leaf.url = "git+https://codeberg.org/BW20/new-leaf";

  # in your NixOS configuration:
  imports = [ inputs.new-leaf.nixosModules.default ];

  services.new-leaf = {
    enable = true;
    publicURL = "https://cv.example.com";
    users = [ "alice" ];       # optional: CVs that always exist
    # manageInEditor = false;  # only the CVs in users
    # proxyGroup = "nginx";    # the group that may open the socket
  };

  services.nginx = {
    recommendedProxySettings = true; # sets X-Real-IP
    virtualHosts = {
      # The editor: make it reachable over the tailnet only, for instance by
      # listening on the server's tailnet address.
      "cv-editor.example.com".locations."/".proxyPass =
        "http://unix:${config.services.new-leaf.socket}";
      # Share links: static files.
      "cv.example.com".root = config.services.new-leaf.publicDir;
    };
  };
}
```

For the share links, the web server should also send the headers New Leaf's
own public server sends (`-serve-public`, `publicHandler` in `main.go`):
`X-Robots-Tag: noindex, nofollow, noarchive`, `Cache-Control: no-cache`, a
strict `Content-Security-Policy` (`default-src 'none'; style-src 'self';
font-src 'self'; img-src data:; frame-ancestors 'none'; base-uri 'none';
form-action 'none'`), no directory listings, and a 404 for anything starting
with a dot.

| Option | Default | What it does |
| --- | --- | --- |
| `enable` | `false` | Run New Leaf |
| `publicURL` | | Where `publicDir` is served, for the links |
| `users` | `[ ]` | CVs that always exist, by tailnet login |
| `manageInEditor` | `true` | Make, rename and delete CVs in the editor |
| `proxyGroup` | `"nginx"` | The only group that may open the editor's socket |
| `listen` | `null` | A TCP address instead of the socket. Any program on the server can then reach the editor, so prefer the socket. |
| `socket` | `/run/new-leaf/editor.sock` | Read-only: where the editor listens |
| `publicDir` | `/var/lib/new-leaf/public` | Read-only: the folder to serve |
| `tailscalePackage` | `services.tailscale.package` | The `tailscale` used for `whois` |

The flake's `checks.vm` runs the module in a virtual machine: sign-in,
the socket, PDFs inside the sandbox, publishing, CV management and moving data
from cv-app.

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

## Options

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

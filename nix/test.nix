# Runs the hardened service in VMs with a stub `tailscale`: checks identity,
# PDF rendering under the systemd sandbox, publishing, that published files
# are readable by a web server while user content is not, and the two
# ready-made ways to serve it (nginx on your own domains, and Tailscale).
self:
{ pkgs, ... }:
let
  # Maps the tailnet IPs used below to users, like `tailscale whois --json`,
  # and notes what it was asked to serve.
  whois = pkgs.writeShellScriptBin "tailscale" ''
    case "$1" in
      serve|funnel) echo "$@" >> /run/tailscale-calls; exit 0 ;;
    esac
    case "$3" in
      100.64.0.5) echo '{"Node":{"Tags":[]},"UserProfile":{"LoginName":"alice@"}}' ;;
      100.64.0.6) echo '{"Node":{"Tags":[]},"UserProfile":{"LoginName":"mallory@"}}' ;;
      *) echo "no such peer" >&2; exit 1 ;;
    esac
  '';
in
pkgs.testers.runNixOSTest {
  name = "new-leaf";
  nodes.machine = {
    imports = [ self.nixosModules.default ];
    virtualisation.memorySize = 2048;
    # Stands in for nginx: the only one allowed on the editor's socket.
    users.groups.proxy = { };
    users.users.proxy = {
      isSystemUser = true;
      group = "proxy";
    };
    environment.systemPackages = [
      (pkgs.writeShellScriptBin "editor-curl" ''
        exec ${pkgs.util-linux}/bin/runuser -u proxy -- ${pkgs.curl}/bin/curl --unix-socket /run/new-leaf/editor.sock "$@"
      '')
    ];
    services.new-leaf = {
      enable = true;
      users = [
        "alice"
        "bob"
      ];
      publicURL = "https://cv.test";
      tailscalePackage = whois;
      proxyGroup = "proxy";
    };
  };

  # nginx on its own domains.
  nodes.web = {
    imports = [ self.nixosModules.default ];
    time.timeZone = "America/New_York";
    networking.hosts."127.0.0.1" = [
      "cv-editor.test"
      "cv.test"
    ];
    services.new-leaf = {
      enable = true;
      users = [ "alice" ];
      tailscalePackage = whois;
      nginx.editor.domain = "cv-editor.test";
      nginx.share = {
        domain = "cv.test";
        rootRedirect = "https://example.test/";
      };
    };
  };

  # Tailscale serve and Funnel.
  nodes.ts = {
    imports = [ self.nixosModules.default ];
    services.new-leaf = {
      enable = true;
      users = [ "alice" ];
      tailscalePackage = whois;
      publicURL = "https://ts.example.ts.net";
      tailscaleServe.enable = true;
    };
  };

  testScript = ''
    import json

    start_all()
    machine.wait_for_unit("new-leaf.service")
    machine.wait_for_unit("new-leaf.socket")

    def api(method, path, body=None, ip="100.64.0.5"):
        data = f"-d '{json.dumps(body)}'" if body is not None else ""
        return machine.succeed(
            f"editor-curl -sf -X {method} -H 'X-Real-IP: {ip}' -H 'X-New-Leaf: 1' {data} http://cv{path}"
        )

    with subtest("any tailnet user can open every CV"):
        assert json.loads(api("GET", "/api/state"))["user"] == "alice"  # own CV by default
        assert json.loads(api("GET", "/api/state", ip="100.64.0.6"))["user"] == "alice"  # first CV for others
        bob = json.loads(machine.succeed(
            "editor-curl -sf -H 'X-Real-IP: 100.64.0.6' -b cv-user=bob http://cv/api/state"
        ))
        assert bob["user"] == "bob" and [c["id"] for c in bob["cvs"]] == ["alice", "bob"], bob
        machine.fail("editor-curl -sf -H 'X-Real-IP: 100.64.0.7' http://cv/api/state")  # not a peer
        machine.fail("editor-curl -sf -b cv-user=bob http://cv/api/state")  # no identity at all

    with subtest("only the proxy can reach the editor"):
        machine.fail("curl -s --max-time 2 http://127.0.0.1:8090/api/state")  # no TCP port
        machine.fail(
            "runuser -u nobody -- curl -sf --unix-socket /run/new-leaf/editor.sock "
            + "-H 'X-Real-IP: 100.64.0.5' http://cv/api/state"
        )
        machine.succeed("stat -c '%U:%G %a' /run/new-leaf/editor.sock | grep -x 'new-leaf:proxy 660'")
        machine.wait_until_succeeds("test -s /var/lib/new-leaf/public/fonts/OFL.txt")  # synced at start, for monitoring
        machine.succeed("systemctl restart new-leaf.service")  # the socket stays
        api("GET", "/api/state")

    with subtest("PDF renders inside the sandbox"):
        api("PUT", "/api/profile", {"name": "Alice Example", "email": "alice@example.com"})
        api("POST", "/api/items", {"section": "experience", "start": "2020-01", "text": {"en": {"title": "Role", "org": "Acme"}}})
        api("POST", "/api/items", {"section": "education", "start": "2015-09", "end": "2019-07", "text": {"en": {"title": "Degree", "org": "Secret University"}}})
        headers = machine.succeed(
            "editor-curl -sf -D - -o /tmp/cv.pdf -X POST -H 'X-Real-IP: 100.64.0.5' -H 'X-New-Leaf: 1' "
            + """-d '{"lang":"en","entries":["experience/acme"]}' http://cv/api/pdf"""
        )
        assert "X-Page-Count: 1" in headers, headers
        machine.succeed("head -c4 /tmp/cv.pdf | grep -q %PDF")

    with subtest("a shared version publishes only its selection, readable by a web server"):
        api("POST", "/api/versions", {"name": "Job"})
        api("PUT", "/api/versions/job", {"name": "Job", "lang": "en", "entries": ["experience/acme"]})
        state = json.loads(api("PUT", "/api/versions/job/share", {"expires": "2099-01-01"}))
        slug = next(v for v in state["versions"] if v["id"] == "job")["link"]["slug"]
        page = machine.succeed(f"sudo -u nobody cat /var/lib/new-leaf/public/{slug}/index.html")
        assert "Acme" in page and "Secret University" not in page
        machine.succeed(f"sudo -u nobody test -s /var/lib/new-leaf/public/{slug}/cv.pdf")
        machine.succeed("sudo -u nobody ls /var/lib/new-leaf/public/css /var/lib/new-leaf/public/fonts")
        machine.fail("sudo -u nobody ls /var/lib/new-leaf/users/alice")

    with subtest("unsharing unpublishes it"):
        api("DELETE", "/api/versions/job/share")
        machine.fail(f"test -e /var/lib/new-leaf/public/{slug}")

    with subtest("thumbnails of versions render in the sandbox"):
        machine.wait_until_succeeds(
            "editor-curl -sf -H 'X-Real-IP: 100.64.0.5' http://cv/api/state"
            + " | grep -q '\"thumb\":\"[0-9a-f]\\{12\\}\"'",
            timeout=60,
        )
        state = json.loads(api("GET", "/api/state"))
        v = next(v for v in state["versions"] if v["thumb"])
        machine.succeed(
            f"editor-curl -sf -o /tmp/thumb.png -H 'X-Real-IP: 100.64.0.5' 'http://cv/api/versions/{v['id']}/thumb?k={v['thumb']}'"
            + " && head -c4 /tmp/thumb.png | grep -q PNG"
        )
        machine.succeed("ls /var/cache/new-leaf/thumbs/alice/*.png")

    with subtest("CVs can be managed in the editor, but configured ones not deleted"):
        new = json.loads(api("POST", "/api/cvs", {"name": "Carol Example", "owners": ["carol@"]}))["id"]
        assert new == "carol-example", new
        carol = lambda method, path, body: machine.succeed(
            f"editor-curl -sf -X {method} -H 'X-Real-IP: 100.64.0.5' -H 'X-New-Leaf: 1' -b cv-user={new} "
            + f"-d '{json.dumps(body)}' http://cv{path}"
        )
        carol("POST", "/api/items", {"section": "experience", "start": "2021-01", "text": {"en": {"title": "Job", "org": "Carol Corp"}}})
        state = json.loads(carol("PUT", "/api/versions/full-cv/share", {"expires": "2099-01-01"}))
        slug = state["versions"][0]["link"]["slug"]
        machine.succeed(f"test -s /var/lib/new-leaf/public/{slug}/index.html")
        machine.fail("editor-curl -sf -X DELETE -H 'X-Real-IP: 100.64.0.5' -H 'X-New-Leaf: 1' http://cv/api/cvs/bob")
        machine.succeed(f"editor-curl -sf -X DELETE -H 'X-Real-IP: 100.64.0.5' -H 'X-New-Leaf: 1' http://cv/api/cvs/{new}")
        machine.fail(f"test -e /var/lib/new-leaf/public/{slug}")
        machine.succeed(f"ls -d /var/lib/new-leaf/trash/{new}-*")
        assert [c["id"] for c in json.loads(api("GET", "/api/state"))["cvs"]] == ["alice", "bob"]

    with subtest("data from when New Leaf was cv-app moves over"):
        machine.succeed("systemctl stop new-leaf.service")
        machine.succeed(
            "mv /var/lib/new-leaf /var/lib/cv-app"
            + " && mv /var/lib/cv-app/public/.new-leaf-public /var/lib/cv-app/public/.cv-app-public"
        )
        machine.succeed("systemctl start new-leaf.service")
        machine.wait_for_unit("new-leaf.service")
        machine.succeed("test -d /var/lib/new-leaf/users/alice")
        machine.wait_until_succeeds("test -e /var/lib/new-leaf/public/.new-leaf-public")  # claimed as New Leaf starts
        machine.succeed("stat -c %U /var/lib/new-leaf/users/alice | grep -x new-leaf")
        assert json.loads(api("GET", "/api/state"))["profile"]["name"] == "Alice Example"

    with subtest("nginx: the editor through its domain, with the visitor's tailnet address"):
        web.wait_for_unit("nginx.service")
        web.wait_for_unit("new-leaf.socket")
        web.succeed("ip addr add 100.64.0.5/32 dev lo")  # the visitor, as if on the tailnet
        visitor = "curl -sf --interface 100.64.0.5 -H 'X-New-Leaf: 1'"
        assert json.loads(web.succeed(f"{visitor} http://cv-editor.test/api/state"))["user"] == "alice"
        web.fail("curl -sf http://cv-editor.test/api/state")  # 127.0.0.1 is no tailnet device
        web.succeed("stat -c '%G' /run/new-leaf/editor.sock | grep -x nginx")
        web.succeed("systemctl cat new-leaf.service | grep -q -- '-public-url https://cv.test'")
        web.succeed("systemctl cat new-leaf.service | grep -q -- '-timezone America/New_York'")

    with subtest("nginx: backups up to 50 MB get through to New Leaf"):
        web.succeed("head -c 20M /dev/urandom > /tmp/big")
        code = web.succeed(
            "curl -s -o /dev/null -w '%{http_code}' --interface 100.64.0.5 -H 'X-New-Leaf: 1' "
            + "-F backup=@/tmp/big http://cv-editor.test/api/import"
        )
        assert code == "400", code  # New Leaf's answer (not a zip), not nginx's 413

    with subtest("nginx: share links with their headers, and nothing else"):
        web.wait_until_succeeds("test -s /var/lib/new-leaf/public/fonts/OFL.txt")
        headers = web.succeed("curl -sfI http://cv.test/fonts/OFL.txt")
        assert "noindex" in headers and "default-src 'none'" in headers, headers
        web.succeed("curl -s -o /dev/null -w '%{http_code}' http://cv.test/.new-leaf-public | grep -x 404")
        web.succeed("curl -sI http://cv.test/ | grep -i '^location: https://example.test/'")

    with subtest("Tailscale: the editor on the tailnet, share links through Funnel"):
        ts.wait_for_unit("new-leaf-tailscale-serve.service")
        calls = ts.succeed("cat /run/tailscale-calls")
        assert "serve --bg --yes --https=8443 unix:/run/new-leaf/editor.sock" in calls, calls
        assert "funnel --bg --yes --https=443 http://127.0.0.1:8081" in calls, calls
        ts.succeed("stat -c '%G' /run/new-leaf/editor.sock | grep -x new-leaf")  # only root and New Leaf
        ts.wait_until_succeeds("curl -sfI http://127.0.0.1:8081/fonts/OFL.txt | grep -qi '^x-robots-tag: noindex'")
  '';
}

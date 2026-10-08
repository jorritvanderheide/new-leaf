# Runs the hardened service in a VM with a stub `tailscale whois`: checks
# identity, PDF rendering under the systemd sandbox, publishing, and that
# published files are readable by a web server while user content is not.
self:
{ pkgs, ... }:
let
  # Maps the tailnet IPs used below to users, like `tailscale whois --json`.
  whois = pkgs.writeShellScriptBin "tailscale" ''
    case "$3" in
      100.64.0.5) echo '{"Node":{"Tags":[]},"UserProfile":{"LoginName":"alice@"}}' ;;
      100.64.0.6) echo '{"Node":{"Tags":[]},"UserProfile":{"LoginName":"mallory@"}}' ;;
      *) echo "no such peer" >&2; exit 1 ;;
    esac
  '';
in
pkgs.testers.runNixOSTest {
  name = "cv-app";
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
        exec ${pkgs.util-linux}/bin/runuser -u proxy -- ${pkgs.curl}/bin/curl --unix-socket /run/cv-app/editor.sock "$@"
      '')
    ];
    services.cv-app = {
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

  testScript = ''
    import json

    machine.wait_for_unit("cv-app.service")
    machine.wait_for_unit("cv-app.socket")

    def api(method, path, body=None, ip="100.64.0.5"):
        data = f"-d '{json.dumps(body)}'" if body is not None else ""
        return machine.succeed(
            f"editor-curl -sf -X {method} -H 'X-Real-IP: {ip}' -H 'X-CV-App: 1' {data} http://cv{path}"
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
            "runuser -u nobody -- curl -sf --unix-socket /run/cv-app/editor.sock "
            + "-H 'X-Real-IP: 100.64.0.5' http://cv/api/state"
        )
        machine.succeed("stat -c '%U:%G %a' /run/cv-app/editor.sock | grep -x 'cv-app:proxy 660'")
        machine.wait_until_succeeds("test -s /var/lib/cv-app/public/fonts/OFL.txt")  # synced at start, for monitoring
        machine.succeed("systemctl restart cv-app.service")  # the socket stays
        api("GET", "/api/state")

    with subtest("PDF renders inside the sandbox"):
        api("PUT", "/api/profile", {"name": "Alice Example", "email": "alice@example.com"})
        api("POST", "/api/items", {"section": "experience", "start": "2020-01", "text": {"en": {"title": "Role", "org": "Acme"}}})
        api("POST", "/api/items", {"section": "education", "start": "2015-09", "end": "2019-07", "text": {"en": {"title": "Degree", "org": "Secret University"}}})
        headers = machine.succeed(
            "editor-curl -sf -D - -o /tmp/cv.pdf -X POST -H 'X-Real-IP: 100.64.0.5' -H 'X-CV-App: 1' "
            + """-d '{"lang":"en","entries":["experience/acme"]}' http://cv/api/pdf"""
        )
        assert "X-Page-Count: 1" in headers, headers
        machine.succeed("head -c4 /tmp/cv.pdf | grep -q %PDF")

    with subtest("a shared version publishes only its selection, readable by a web server"):
        api("POST", "/api/versions", {"name": "Job"})
        api("PUT", "/api/versions/job", {"name": "Job", "lang": "en", "entries": ["experience/acme"]})
        state = json.loads(api("PUT", "/api/versions/job/share", {"expires": "2099-01-01"}))
        slug = next(v for v in state["versions"] if v["id"] == "job")["link"]["slug"]
        page = machine.succeed(f"sudo -u nobody cat /var/lib/cv-app/public/{slug}/index.html")
        assert "Acme" in page and "Secret University" not in page
        machine.succeed(f"sudo -u nobody test -s /var/lib/cv-app/public/{slug}/cv.pdf")
        machine.succeed("sudo -u nobody ls /var/lib/cv-app/public/css /var/lib/cv-app/public/fonts")
        machine.fail("sudo -u nobody ls /var/lib/cv-app/users/alice")

    with subtest("unsharing unpublishes it"):
        api("DELETE", "/api/versions/job/share")
        machine.fail(f"test -e /var/lib/cv-app/public/{slug}")

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
        machine.succeed("ls /var/cache/cv-app/thumbs/alice/*.png")

    with subtest("CVs can be managed in the editor, but configured ones not deleted"):
        new = json.loads(api("POST", "/api/cvs", {"name": "Carol Example", "owners": ["carol@"]}))["id"]
        assert new == "carol-example", new
        carol = lambda method, path, body: machine.succeed(
            f"editor-curl -sf -X {method} -H 'X-Real-IP: 100.64.0.5' -H 'X-CV-App: 1' -b cv-user={new} "
            + f"-d '{json.dumps(body)}' http://cv{path}"
        )
        carol("POST", "/api/items", {"section": "experience", "start": "2021-01", "text": {"en": {"title": "Job", "org": "Carol Corp"}}})
        state = json.loads(carol("PUT", "/api/versions/full-cv/share", {"expires": "2099-01-01"}))
        slug = state["versions"][0]["link"]["slug"]
        machine.succeed(f"test -s /var/lib/cv-app/public/{slug}/index.html")
        machine.fail("editor-curl -sf -X DELETE -H 'X-Real-IP: 100.64.0.5' -H 'X-CV-App: 1' http://cv/api/cvs/bob")
        machine.succeed(f"editor-curl -sf -X DELETE -H 'X-Real-IP: 100.64.0.5' -H 'X-CV-App: 1' http://cv/api/cvs/{new}")
        machine.fail(f"test -e /var/lib/cv-app/public/{slug}")
        machine.succeed(f"ls -d /var/lib/cv-app/trash/{new}-*")
        assert [c["id"] for c in json.loads(api("GET", "/api/state"))["cvs"]] == ["alice", "bob"]
  '';
}

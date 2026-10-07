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
    services.cv-app = {
      enable = true;
      users = [
        "alice"
        "bob"
      ];
      publicURL = "https://cv.test";
      tailscalePackage = whois;
    };
  };

  testScript = ''
    import json

    machine.wait_for_unit("cv-app.service")
    machine.wait_for_open_port(8090)

    def api(method, path, body=None, ip="100.64.0.5"):
        data = f"-d '{json.dumps(body)}'" if body is not None else ""
        return machine.succeed(
            f"curl -sf -X {method} -H 'X-Real-IP: {ip}' -H 'X-CV-App: 1' {data} http://127.0.0.1:8090{path}"
        )

    with subtest("any tailnet user can open every CV"):
        assert json.loads(api("GET", "/api/state"))["user"] == "alice"  # own CV by default
        assert json.loads(api("GET", "/api/state", ip="100.64.0.6"))["user"] == "alice"  # first CV for others
        bob = json.loads(machine.succeed(
            "curl -sf -H 'X-Real-IP: 100.64.0.6' -b cv-user=bob http://127.0.0.1:8090/api/state"
        ))
        assert bob["user"] == "bob" and bob["users"] == ["alice", "bob"], bob
        machine.fail("curl -sf -H 'X-Real-IP: 100.64.0.7' http://127.0.0.1:8090/api/state")  # not a peer
        machine.fail("curl -sf -b cv-user=bob http://127.0.0.1:8090/api/state")  # no identity at all

    with subtest("PDF renders inside the sandbox"):
        api("PUT", "/api/profile", {"name": "Alice Example", "email": "alice@example.com"})
        api("POST", "/api/items", {"section": "experience", "start": "2020-01", "text": {"en": {"title": "Role", "org": "Acme"}}})
        api("POST", "/api/items", {"section": "education", "start": "2015-09", "end": "2019-07", "text": {"en": {"title": "Degree", "org": "Secret University"}}})
        headers = machine.succeed(
            "curl -sf -D - -o /tmp/cv.pdf -X POST -H 'X-Real-IP: 100.64.0.5' -H 'X-CV-App: 1' "
            + """-d '{"lang":"en","entries":["experience/acme"]}' http://127.0.0.1:8090/api/pdf"""
        )
        assert "X-Page-Count: 1" in headers, headers
        machine.succeed("head -c4 /tmp/cv.pdf | grep -q %PDF")

    with subtest("share links publish only the selection, readable by a web server"):
        state = json.loads(api("POST", "/api/links", {"label": "job", "lang": "en", "entries": ["experience/acme"], "expires": "2099-01-01"}))
        slug = state["links"][0]["slug"]
        page = machine.succeed(f"sudo -u nobody cat /var/lib/cv-app/public/{slug}/index.html")
        assert "Acme" in page and "Secret University" not in page
        machine.succeed(f"sudo -u nobody test -s /var/lib/cv-app/public/{slug}/cv.pdf")
        machine.succeed("sudo -u nobody ls /var/lib/cv-app/public/css /var/lib/cv-app/public/fonts")
        machine.fail("sudo -u nobody ls /var/lib/cv-app/users/alice")

    with subtest("deleting a link unpublishes it"):
        api("DELETE", f"/api/links/{slug}")
        machine.fail(f"test -e /var/lib/cv-app/public/{slug}")
  '';
}

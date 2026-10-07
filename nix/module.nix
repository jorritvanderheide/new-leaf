self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.cv-app;
  stateDir = "/var/lib/cv-app";
in
{
  options.services.cv-app = {
    enable = lib.mkEnableOption "the cv-app CV editor";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "cv-app package.";
    };

    listen = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8090";
      description = ''
        Editor listen address. Keep it on loopback behind a reverse proxy that
        only tailnet clients can reach and that sets X-Real-IP: the editor
        identifies users by looking up that address with `tailscale whois`.
      '';
    };

    users = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      example = [ "jorrit" ];
      description = ''
        The CVs, one per person, named after their owner's tailnet login (in
        full or the part before "@"). Every human tailnet user can open and
        edit all of them; their own CV opens by default.
      '';
    };

    publicURL = lib.mkOption {
      type = lib.types.str;
      example = "https://cv.example.com";
      description = "URL at which a web server serves {option}`services.cv-app.publicDir`.";
    };

    publicDir = lib.mkOption {
      type = lib.types.str;
      default = "${stateDir}/public";
      readOnly = true;
      description = "Webroot the share links are published into. Serve it statically.";
    };

    tailscalePackage = lib.mkOption {
      type = lib.types.package;
      default = config.services.tailscale.package;
      defaultText = lib.literalExpression "config.services.tailscale.package";
      description = "Tailscale CLI used for `tailscale whois`.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.cv-app = {
      isSystemUser = true;
      group = "cv-app";
      home = stateDir;
    };
    users.groups.cv-app = { };

    systemd.services.cv-app = {
      description = "cv-app CV editor";
      wantedBy = [ "multi-user.target" ];
      after = [
        "network.target"
        "tailscaled.service"
      ];

      # Fallback fonts for Chromium; CVs themselves use bundled web fonts.
      environment.FONTCONFIG_FILE = pkgs.makeFontsConf { fontDirectories = [ pkgs.dejavu_fonts ]; };

      serviceConfig = {
        ExecStart = lib.escapeShellArgs [
          (lib.getExe cfg.package)
          "-listen"
          cfg.listen
          "-data"
          stateDir
          "-work"
          "/var/cache/cv-app"
          "-public"
          cfg.publicDir
          "-public-url"
          cfg.publicURL
          "-users"
          (lib.concatStringsSep "," cfg.users)
          "-tailscale"
          (lib.getExe cfg.tailscalePackage)
        ];
        User = "cv-app";
        Group = "cv-app";
        StateDirectory = "cv-app";
        # World-readable so the web server can serve publicDir; user content
        # lives in users/, which cv-app creates 0750.
        StateDirectoryMode = "0755";
        CacheDirectory = "cv-app";
        UMask = "0022";
        Restart = "always";
        RestartSec = "5s";

        # Hardening. Chromium runs without its own sandbox (no user
        # namespaces here) and needs W^X off for V8, so this is the sandbox.
        CapabilityBoundingSet = "";
        LockPersonality = true;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProtectSystem = "strict";
        RestrictAddressFamilies = [
          "AF_UNIX"
          "AF_INET"
          "AF_INET6"
          "AF_NETLINK"
        ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
      };
    };
  };
}

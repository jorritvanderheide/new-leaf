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

    socket = lib.mkOption {
      type = lib.types.str;
      default = "/run/cv-app/editor.sock";
      readOnly = true;
      description = ''
        Unix socket the editor listens on (unless {option}`services.cv-app.listen`
        is set). Point a reverse proxy at it that only tailnet clients can
        reach and that sets X-Real-IP, e.g. nginx's
        `proxyPass = "http://unix:/run/cv-app/editor.sock";`.
      '';
    };

    proxyGroup = lib.mkOption {
      type = lib.types.str;
      default = "nginx";
      description = ''
        Group of the reverse proxy. Only it (and cv-app) may open the editor's
        socket, so no other local program can reach the editor and pass off
        a made-up visitor address as a tailnet device.
      '';
    };

    listen = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "127.0.0.1:8090";
      description = ''
        A TCP address for the editor instead of the socket. Any local program
        can then reach it and claim to be any tailnet device, so prefer the
        socket.
      '';
    };

    users = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "jorrit" ];
      description = ''
        CVs that always exist, named after their owner's tailnet login (in
        full or the part before "@"). Every human tailnet user can open and
        edit all CVs; their own opens by default. These can be renamed in the
        editor but not deleted.
      '';
    };

    manageInEditor = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Whether editor users can create, rename and delete CVs, next to
        {option}`services.cv-app.users`. Deleted CVs are moved to
        {file}`/var/lib/cv-app/trash`. When off, exactly the CVs in
        {option}`services.cv-app.users` exist.
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
    assertions = [
      {
        assertion = cfg.users != [ ] || cfg.manageInEditor;
        message = "services.cv-app: list the CVs in users, or enable manageInEditor.";
      }
    ];

    users.users.cv-app = {
      isSystemUser = true;
      group = "cv-app";
      home = stateDir;
    };
    users.groups.cv-app = { };

    # systemd owns the socket: cv-app needs no membership of the proxy's
    # group, and the proxy can connect while cv-app restarts.
    systemd.sockets.cv-app = lib.mkIf (cfg.listen == null) {
      description = "cv-app editor socket";
      wantedBy = [ "sockets.target" ];
      listenStreams = [ cfg.socket ];
      socketConfig = {
        SocketUser = "cv-app";
        SocketGroup = cfg.proxyGroup;
        SocketMode = "0660";
      };
    };

    systemd.services.cv-app = {
      description = "cv-app CV editor";
      wantedBy = [ "multi-user.target" ];
      requires = lib.optional (cfg.listen == null) "cv-app.socket";
      after = [
        "network.target"
        "tailscaled.service"
      ]
      ++ lib.optional (cfg.listen == null) "cv-app.socket";

      serviceConfig = {
        ExecStart = lib.escapeShellArgs [
          (lib.getExe cfg.package)
          "serve"
          "-listen"
          (if cfg.listen == null then "systemd" else cfg.listen)
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
          "-manage=${lib.boolToString cfg.manageInEditor}"
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

        # Hardening: cv-app and typst need little more than their files.
        CapabilityBoundingSet = "";
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
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
        ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
        ];
      };
    };
  };
}

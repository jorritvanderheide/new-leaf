self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.new-leaf;
  stateDir = "/var/lib/new-leaf";

  # New Leaf was called cv-app. Its data moves here on the first start
  # (copied: the old directory stays until you remove it).
  moveOldData = pkgs.writeShellScript "new-leaf-move-old-data" ''
    old=/var/lib/cv-app
    if [ -d "$old/users" ] && [ ! -e ${stateDir}/users ]; then
      echo "copying $old to ${stateDir}"
      cp -a "$old/." ${stateDir}/
      chown -R new-leaf:new-leaf ${stateDir}
    fi
  '';
in
{
  options.services.new-leaf = {
    enable = lib.mkEnableOption "New Leaf, the CV editor";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "New Leaf package.";
    };

    socket = lib.mkOption {
      type = lib.types.str;
      default = "/run/new-leaf/editor.sock";
      readOnly = true;
      description = ''
        Unix socket the editor listens on (unless
        {option}`services.new-leaf.listen` is set). Point a reverse proxy at it
        that only tailnet clients can reach and that sets X-Real-IP, e.g.
        nginx's `proxyPass = "http://unix:/run/new-leaf/editor.sock";`.
      '';
    };

    proxyGroup = lib.mkOption {
      type = lib.types.str;
      default = "nginx";
      description = ''
        Group of the reverse proxy. Only it (and new-leaf) may open the editor's
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
        {option}`services.new-leaf.users`. Deleted CVs are moved to
        {file}`/var/lib/new-leaf/trash`. When off, exactly the CVs in
        {option}`services.new-leaf.users` exist.
      '';
    };

    publicURL = lib.mkOption {
      type = lib.types.str;
      example = "https://cv.example.com";
      description = "URL at which a web server serves {option}`services.new-leaf.publicDir`.";
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
        message = "services.new-leaf: list the CVs in users, or enable manageInEditor.";
      }
    ];

    users.users.new-leaf = {
      isSystemUser = true;
      group = "new-leaf";
      home = stateDir;
    };
    users.groups.new-leaf = { };

    # systemd owns the socket: new-leaf needs no membership of the proxy's
    # group, and the proxy can connect while new-leaf restarts.
    systemd.sockets.new-leaf = lib.mkIf (cfg.listen == null) {
      description = "New Leaf editor socket";
      wantedBy = [ "sockets.target" ];
      listenStreams = [ cfg.socket ];
      socketConfig = {
        SocketUser = "new-leaf";
        SocketGroup = cfg.proxyGroup;
        SocketMode = "0660";
      };
    };

    systemd.services.new-leaf = {
      description = "New Leaf, the CV editor";
      wantedBy = [ "multi-user.target" ];
      requires = lib.optional (cfg.listen == null) "new-leaf.socket";
      after = [
        "network.target"
        "tailscaled.service"
      ]
      ++ lib.optional (cfg.listen == null) "new-leaf.socket";

      serviceConfig = {
        ExecStart = lib.escapeShellArgs [
          (lib.getExe cfg.package)
          "serve"
          "-listen"
          (if cfg.listen == null then "systemd" else cfg.listen)
          "-data"
          stateDir
          "-work"
          "/var/cache/new-leaf"
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
        User = "new-leaf";
        Group = "new-leaf";
        StateDirectory = "new-leaf";
        # World-readable so the web server can serve publicDir; user content
        # lives in users/, which new-leaf creates 0750.
        StateDirectoryMode = "0755";
        CacheDirectory = "new-leaf";
        ExecStartPre = "+${moveOldData}"; # as root, before the sandbox
        UMask = "0022";
        Restart = "always";
        RestartSec = "5s";

        # Hardening: new-leaf and typst need little more than their files.
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

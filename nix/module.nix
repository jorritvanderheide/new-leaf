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
        {option}`services.new-leaf.listen` is set).
        {option}`services.new-leaf.nginx.editor` and
        {option}`services.new-leaf.tailscaleServe` use it; another reverse
        proxy must be reachable from the tailnet only, and set X-Real-IP.
      '';
    };

    proxyGroup = lib.mkOption {
      type = lib.types.str;
      default = if config.services.nginx.enable then config.services.nginx.group else "new-leaf";
      defaultText = lib.literalExpression ''if config.services.nginx.enable then config.services.nginx.group else "new-leaf"'';
      description = ''
        Group of the reverse proxy. Only it (and new-leaf) may open the editor's
        socket, so no other local program can reach the editor and pass off
        a made-up visitor address as a tailnet device. Tailscale runs as root
        and needs no group.
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
      example = [ "alice" ];
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

    timeZone = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = config.time.timeZone;
      defaultText = lib.literalExpression "config.time.timeZone";
      example = "Europe/Amsterdam";
      description = ''
        Time zone of share links' end dates: a link goes offline at the start
        of its day in this zone. `null` for the system's.
      '';
    };

    publicURL = lib.mkOption {
      type = lib.types.str;
      example = "https://cv.example.com";
      description = "URL at which {option}`services.new-leaf.publicDir` is served: where the share links are.";
    };

    publicDir = lib.mkOption {
      type = lib.types.str;
      default = "${stateDir}/public";
      readOnly = true;
      description = "Webroot the share links are published into. Serve it statically.";
    };

    servePublic = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "127.0.0.1:8081";
      description = ''
        Also serve {option}`services.new-leaf.publicDir` on this address,
        with the headers share links need, for a proxy that serves no files
        itself. {option}`services.new-leaf.tailscaleServe` sets it.
      '';
    };

    tailscaleServe = {
      enable = lib.mkEnableOption ''
        serving New Leaf with Tailscale itself, without a domain, web server
        or open port: the editor on the tailnet at
        `https://<machine>.<tailnet>.ts.net:<editorPort>`, and the share links
        on the internet through Funnel at `https://<machine>.<tailnet>.ts.net/`.
        Set {option}`services.new-leaf.publicURL` to that address. Needs
        Tailscale (headscale has no Funnel) with HTTPS certificates on, and
        Funnel allowed for this machine in the tailnet policy'';

      editorPort = lib.mkOption {
        type = lib.types.port;
        default = 8443;
        description = "HTTPS port of the editor on the tailnet. Not 443: that one is the share links'.";
      };
    };

    nginx = {
      editor = {
        domain = lib.mkOption {
          type = lib.types.nullOr lib.types.str;
          default = null;
          example = "cv-editor.example.com";
          description = ''
            Serve the editor with nginx on this domain: proxied to the socket,
            with the visitor's address, room for photo and backup uploads,
            and time for fitting pages. TLS is yours to add to
            {option}`services.nginx.virtualHosts.<domain>`, e.g. `enableACME`
            and `forceSSL`.
          '';
        };
        listenAddresses = lib.mkOption {
          type = lib.types.listOf lib.types.str;
          default = [ ];
          example = [ "100.64.0.1" ];
          description = ''
            Addresses the editor listens on, such as this machine's tailnet
            address, so only the tailnet reaches it. Empty: nginx's default,
            every address; New Leaf still turns away anyone it can't find on
            the tailnet, but the editor is then known to the internet.
          '';
        };
      };

      share = {
        domain = lib.mkOption {
          type = lib.types.nullOr lib.types.str;
          default = null;
          example = "cv.example.com";
          description = ''
            Serve the share links with nginx on this domain, as static files
            with the headers they need. Sets
            {option}`services.new-leaf.publicURL` to `https://<domain>`. TLS
            is yours to add, as for the editor.
          '';
        };
        rootRedirect = lib.mkOption {
          type = lib.types.nullOr lib.types.str;
          default = null;
          example = "https://example.com/";
          description = "Where the domain without a share link redirects to; null for a 404.";
        };
      };
    };

    tailscalePackage = lib.mkOption {
      type = lib.types.package;
      default = config.services.tailscale.package;
      defaultText = lib.literalExpression "config.services.tailscale.package";
      description = "Tailscale CLI used for `tailscale whois`.";
    };
  };

  config = lib.mkIf cfg.enable (
    lib.mkMerge [
      {
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
            ExecStart = lib.escapeShellArgs (
              [
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
              ]
              ++ lib.optionals (cfg.servePublic != null) [
                "-serve-public"
                cfg.servePublic
              ]
              ++ lib.optionals (cfg.timeZone != null) [
                "-timezone"
                cfg.timeZone
              ]
            );
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
      }

      # Tailscale serves both: the editor on the tailnet straight from the
      # socket (tailscaled runs as root), the share links through Funnel from
      # New Leaf's own server for them. The settings stay in tailscaled until
      # `tailscale serve reset`.
      (lib.mkIf cfg.tailscaleServe.enable {
        services.new-leaf.servePublic = lib.mkDefault "127.0.0.1:8081";

        systemd.services.new-leaf-tailscale-serve = {
          description = "New Leaf on the tailnet, and its share links through Funnel";
          wantedBy = [ "multi-user.target" ];
          wants = [ "tailscaled.service" ];
          after = [
            "tailscaled.service"
            "new-leaf.service"
          ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = [
              "${lib.getExe cfg.tailscalePackage} serve --bg --yes --https=${toString cfg.tailscaleServe.editorPort} unix:${cfg.socket}"
              "${lib.getExe cfg.tailscalePackage} funnel --bg --yes --https=443 http://${cfg.servePublic}"
            ];
            Restart = "on-failure"; # tailscaled may not be logged in yet
            RestartSec = "30s";
          };
        };
      })

      (lib.mkIf (cfg.nginx.editor.domain != null || cfg.nginx.share.domain != null) {
        services.nginx.enable = true;
        services.new-leaf.publicURL = lib.mkIf (cfg.nginx.share.domain != null) (
          lib.mkDefault "https://${cfg.nginx.share.domain}"
        );

        services.nginx.virtualHosts =
          lib.optionalAttrs (cfg.nginx.editor.domain != null) {
            ${cfg.nginx.editor.domain} = {
              inherit (cfg.nginx.editor) listenAddresses;
              locations."/" = {
                proxyPass = "http://unix:${cfg.socket}";
                recommendedProxySettings = true; # X-Real-IP, which New Leaf asks Tailscale about
                extraConfig = ''
                  client_max_body_size 52m; # backups up to 50 MB
                  proxy_read_timeout 120s; # fitting pages renders the PDF a few times
                '';
              };
            };
          }
          // lib.optionalAttrs (cfg.nginx.share.domain != null) {
            # Static files only, with the headers New Leaf's own server for
            # them sends (publicHandler in internal/server/serve.go).
            ${cfg.nginx.share.domain} = {
              root = cfg.publicDir;
              extraConfig = ''
                autoindex off;
                add_header X-Robots-Tag "noindex, nofollow, noarchive" always;
                add_header X-Content-Type-Options "nosniff" always;
                add_header Referrer-Policy "no-referrer" always;
                add_header Cache-Control "no-cache" always;
                add_header Content-Security-Policy "default-src 'none'; style-src 'self'; font-src 'self'; img-src data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'" always;
              '';
              locations = {
                "= /".return =
                  if cfg.nginx.share.rootRedirect == null then "404" else "302 ${cfg.nginx.share.rootRedirect}";
                "~ /\\.".return = "404"; # New Leaf's marker, publishes in progress
                "/".tryFiles = "$uri $uri/ =404";
              };
            };
          };
      })
    ]
  );
}

# The self-hosting container: New Leaf (new-leaf serve), with typst and the
# tailscale CLI. It is meant to share the network of a tailscale container,
# which serves the editor on the tailnet and the share links publicly
# (funnel): see deploy/compose.yaml. Hence both listen on 127.0.0.1 only.
# Settings come from NEW_LEAF_* variables (see new-leaf serve -h).
{
  dockerTools,
  lib,
  new-leaf,
  tailscale,
}:
dockerTools.buildLayeredImage {
  name = "new-leaf";
  tag = "latest";
  contents = [
    new-leaf
    tailscale
  ];
  extraCommands = ''
    mkdir -p data tmp
    chmod 1777 tmp
  '';
  fakeRootCommands = ''
    chown 1000:1000 data
  '';
  config = {
    Entrypoint = [
      (lib.getExe new-leaf)
      "serve"
    ];
    Env = [
      "NEW_LEAF_LISTEN=127.0.0.1:8080"
      "NEW_LEAF_SERVE_PUBLIC=127.0.0.1:8081"
      "NEW_LEAF_DATA=/data"
      "NEW_LEAF_PUBLIC=/data/public"
      "NEW_LEAF_TAILSCALE=${lib.getExe tailscale}"
    ];
    Volumes."/data" = { };
    WorkingDir = "/data";
    User = "1000:1000";
    Labels = {
      "org.opencontainers.image.title" = "New Leaf";
      "org.opencontainers.image.source" = "https://codeberg.org/BW20/new-leaf";
      "org.opencontainers.image.licenses" = "EUPL-1.2";
      "org.opencontainers.image.version" = new-leaf.version;
    };
  };
}

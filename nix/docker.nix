# The self-hosting container: cv-app serve, with typst and the tailscale CLI.
# It is meant to share the network of a tailscale container, which serves
# the editor on the tailnet and the share links publicly (funnel): see
# deploy/compose.yaml. Hence both listen on 127.0.0.1 only. Settings come
# from CV_APP_* variables (see cv-app serve -h).
{
  dockerTools,
  lib,
  cv-app,
  tailscale,
}:
dockerTools.buildLayeredImage {
  name = "cv-app";
  tag = "latest";
  contents = [
    cv-app
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
      (lib.getExe cv-app)
      "serve"
    ];
    Env = [
      "CV_APP_LISTEN=127.0.0.1:8080"
      "CV_APP_SERVE_PUBLIC=127.0.0.1:8081"
      "CV_APP_DATA=/data"
      "CV_APP_PUBLIC=/data/public"
      "CV_APP_TAILSCALE=${lib.getExe tailscale}"
    ];
    Volumes."/data" = { };
    WorkingDir = "/data";
    User = "1000:1000";
    Labels = {
      "org.opencontainers.image.title" = "cv-app";
      "org.opencontainers.image.source" = "https://codeberg.org/BW20/cv-app";
      "org.opencontainers.image.licenses" = "EUPL-1.2";
      "org.opencontainers.image.version" = cv-app.version;
    };
  };
}

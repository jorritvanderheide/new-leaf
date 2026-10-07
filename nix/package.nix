{
  lib,
  buildGoModule,
  chromium,
  hugo,
  makeWrapper,
  tailwindcss_4,
}:
let
  root = ../.;
in
buildGoModule {
  pname = "cv-app";
  version = "0.1.0";

  src = lib.fileset.toSource {
    inherit root;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      (lib.fileset.fileFilter (f: f.hasExt "go") root)
      (lib.fileset.difference ../web (
        lib.fileset.unions [
          (lib.fileset.maybeMissing ../web/cv/assets/css/cv.css)
          (lib.fileset.maybeMissing ../web/editor/assets/css/editor.css)
          (lib.fileset.maybeMissing ../web/editor/public)
          (lib.fileset.maybeMissing ../web/editor/resources)
          (lib.fileset.maybeMissing ../web/cv/resources)
        ]
      ))
    ];
  };

  vendorHash = "sha256-vjGhS7nhjXlms1ZsRG6Qy/7ategUIgwiBwk943cz9Lk=";

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
  ];

  nativeBuildInputs = [
    hugo
    makeWrapper
    tailwindcss_4
  ];

  # Stylesheets for both Hugo sites, then the editor UI, which is static.
  # The CV site is rendered per user at runtime, so it ships as source.
  postBuild = ''
    tailwindcss --minify -i web/css/cv.css -o web/cv/assets/css/cv.css
    tailwindcss --minify -i web/css/editor.css -o web/editor/assets/css/editor.css
    hugo build --source web/editor --destination "$NIX_BUILD_TOP/ui" --noBuildLock --quiet
  '';

  # The end-to-end test skips itself here (no Chromium in the sandbox); run
  # it with `go test ./...` in the dev shell.

  postInstall = ''
    mkdir -p $out/share/cv-app
    cp -r web/cv $out/share/cv-app/site
    cp -r "$NIX_BUILD_TOP/ui" $out/share/cv-app/ui
    wrapProgram $out/bin/cv-app \
      --prefix PATH : ${
        lib.makeBinPath [
          chromium
          hugo
        ]
      } \
      --add-flags "-site $out/share/cv-app/site -ui $out/share/cv-app/ui"
  '';

  meta = {
    description = "Multi-user CV editor with PDF export and expiring share links";
    mainProgram = "cv-app";
    platforms = lib.platforms.linux;
  };
}

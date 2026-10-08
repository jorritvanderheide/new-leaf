{
  lib,
  buildGoModule,
  copyDesktopItems,
  makeDesktopItem,
  makeWrapper,
  typst,
}:
let
  root = ../.;
in
buildGoModule {
  pname = "cv-app";
  version = "0.3.0";

  # Templates, styles and fonts under web/ are embedded in the binary; the
  # stylesheets are generated (see checks.css) and committed.
  src = lib.fileset.toSource {
    inherit root;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ./cv-app.svg
      (lib.fileset.fileFilter (f: f.hasExt "go") root)
      ../web
    ];
  };

  vendorHash = "sha256-j7TKqYqjIh5lvu7Crnj+yB4J3+YJAsPwgoncKIQJF9A=";

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
  ];

  nativeBuildInputs = [
    copyDesktopItems
    makeWrapper
  ];

  # "cv-app" on its own is the local editor, so it gets a launcher entry.
  desktopItems = [
    (makeDesktopItem {
      name = "cv-app";
      desktopName = "CV";
      genericName = "CV editor";
      comment = "Edit your CV in English and Dutch and make PDFs";
      exec = "cv-app";
      icon = "cv-app";
      categories = [ "Office" ];
    })
  ];

  # The end-to-end test skips itself without typst; it runs in the dev shell.

  postInstall = ''
    install -Dm644 nix/cv-app.svg $out/share/icons/hicolor/scalable/apps/cv-app.svg
    wrapProgram $out/bin/cv-app --prefix PATH : ${lib.makeBinPath [ typst ]}
  '';

  meta = {
    description = "CV editor: pick items per application, make PDFs with Typst, share expiring links";
    mainProgram = "cv-app";
    homepage = "https://codeberg.org/BW20/cv-app";
    license = lib.licenses.eupl12;
    platforms = lib.platforms.linux;
  };
}

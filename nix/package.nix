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
  version = "0.6.0";
in
buildGoModule {
  pname = "new-leaf";
  inherit version;

  # Templates, styles and fonts under web/ are embedded in the binary; the
  # stylesheets are generated (see checks.css) and committed.
  src = lib.fileset.toSource {
    inherit root;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ./new-leaf.svg
      (lib.fileset.fileFilter (f: f.hasExt "go") root)
      ../web
    ];
  };

  vendorHash = "sha256-j7TKqYqjIh5lvu7Crnj+yB4J3+YJAsPwgoncKIQJF9A=";

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  nativeBuildInputs = [
    copyDesktopItems
    makeWrapper
  ];

  # "new-leaf" on its own is the local editor, so it gets a launcher entry.
  desktopItems = [
    (makeDesktopItem {
      name = "new-leaf";
      desktopName = "New Leaf";
      genericName = "CV editor";
      comment = "Edit your CV in English and Dutch and make PDFs";
      exec = "new-leaf";
      icon = "new-leaf";
      categories = [ "Office" ];
    })
  ];

  # The end-to-end test skips itself without typst; it runs in the dev shell.

  postInstall = ''
    install -Dm644 nix/new-leaf.svg $out/share/icons/hicolor/scalable/apps/new-leaf.svg
    wrapProgram $out/bin/new-leaf --prefix PATH : ${lib.makeBinPath [ typst ]}
  '';

  meta = {
    description = "New Leaf, a CV editor: a version per application, PDFs with Typst, share links";
    mainProgram = "new-leaf";
    homepage = "https://codeberg.org/BW20/new-leaf";
    license = lib.licenses.eupl12;
    platforms = lib.platforms.linux;
  };
}

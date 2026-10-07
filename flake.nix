{
  description = "Multi-user CV editor: Hugo-rendered CVs, PDF export and expiring share links";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.callPackage ./nix/package.nix { };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            chromium
            go
            gopls
            hugo
            tailwindcss_4
          ];
        };
      });

      apps = forAllSystems (pkgs: {
        # Local development: watches CSS and the editor UI, runs the server
        # as a fixed dev user against ./dev, and serves the public link
        # webroot on :8081 so share links can be clicked through.
        dev = {
          type = "app";
          program = pkgs.lib.getExe (
            pkgs.writeShellApplication {
              name = "cv-app-dev";
              runtimeInputs = with pkgs; [
                chromium
                git
                go
                hugo
                tailwindcss_4
              ];
              text = ''
                root=$(git rev-parse --show-toplevel)
                cd "$root"
                trap 'kill 0' EXIT
                export CGO_ENABLED=0
                for css in cv editor; do
                  tailwindcss -i "web/css/$css.css" -o "web/$css/assets/css/$css.css"
                  tailwindcss -i "web/css/$css.css" -o "web/$css/assets/css/$css.css" --watch=always &
                done
                hugo build --source web/editor --watch --quiet &
                go run . \
                  -dev-user "''${CV_DEV_USER:-jorrit}" \
                  -data dev/data \
                  -public dev/public \
                  -public-url http://localhost:8081 \
                  -serve-public 127.0.0.1:8081 \
                  -site web/cv \
                  -ui web/editor/public
              '';
            }
          );
        };
      });

      nixosModules.default = import ./nix/module.nix self;

      checks = forAllSystems (pkgs: {
        package = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        vm = import ./nix/test.nix self { inherit pkgs; };
      });
    };
}

{
  description = "New Leaf, a CV editor: one Go binary, PDFs with Typst, share links";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # The stylesheets are generated from web/css by Tailwind and committed,
      # so building the app needs no Node or Tailwind.
      buildCSS = ''
        tailwindcss --minify -i web/css/share.css -o web/share/share.css
        tailwindcss --minify -i web/css/editor.css -o web/editor/editor.css
      '';
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.callPackage ./nix/package.nix { };
        # The self-hosting container image (nix/docker.nix).
        docker = pkgs.callPackage ./nix/docker.nix {
          new-leaf = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            poppler-utils # pdftotext, for the test of a PDF's text
            tailwindcss_4
            typst
          ];
        };
      });

      # pkgs.new-leaf for other flakes and NixOS/home-manager configurations.
      overlays.default = final: _prev: {
        new-leaf = final.callPackage ./nix/package.nix { };
      };

      apps = forAllSystems (pkgs: {
        # `nix run <this flake>`: edit your CV on this computer.
        default = {
          type = "app";
          program = pkgs.lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };

        # Local development: rebuilds the stylesheets on change and runs the
        # server as you ($USER, or NEW_LEAF_DEV_USER) against ./dev, with
        # templates read from disk; serves the public webroot on :8081 so share
        # links can be clicked through.
        dev = {
          type = "app";
          program = pkgs.lib.getExe (
            pkgs.writeShellApplication {
              name = "new-leaf-dev";
              runtimeInputs = with pkgs; [
                git
                go
                tailwindcss_4
                typst
              ];
              text = ''
                root=$(git rev-parse --show-toplevel)
                cd "$root"
                trap 'kill 0' EXIT
                export CGO_ENABLED=0
                ${buildCSS}
                tailwindcss --minify -i web/css/share.css -o web/share/share.css --watch=always &
                tailwindcss --minify -i web/css/editor.css -o web/editor/editor.css --watch=always &
                go run . serve \
                  -dev-user "''${NEW_LEAF_DEV_USER:-$USER}" \
                  -dev-assets . \
                  -data dev/data \
                  -public dev/public \
                  -public-url http://localhost:8081 \
                  -serve-public 127.0.0.1:8081
              '';
            }
          );
        };

        # Drives the editor in headless Chromium against a made-up CV; see
        # tests/browser/run.mjs. Takes suite names to run only those.
        browser-tests = {
          type = "app";
          program = pkgs.lib.getExe (
            pkgs.writeShellApplication {
              name = "new-leaf-browser-tests";
              runtimeInputs = [
                self.packages.${pkgs.stdenv.hostPlatform.system}.default
                pkgs.chromium
                pkgs.nodejs
              ];
              text = ''exec node ${./tests/browser}/run.mjs "$@"'';
            }
          );
        };
      });

      nixosModules.default = import ./nix/module.nix self;

      checks = forAllSystems (pkgs: {
        package = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        docker = self.packages.${pkgs.stdenv.hostPlatform.system}.docker;
        vm = import ./nix/test.nix self { inherit pkgs; };

        # The committed stylesheets match the templates.
        css =
          pkgs.runCommand "css-up-to-date"
            {
              src = ./web;
              nativeBuildInputs = [ pkgs.tailwindcss_4 ];
            }
            ''
              cp -r $src web && chmod -R u+w web
              ${buildCSS}
              diff -q $src/share/share.css web/share/share.css
              diff -q $src/editor/editor.css web/editor/editor.css
              touch $out
            '';
      });
    };
}

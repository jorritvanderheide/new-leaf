#!/usr/bin/env bash
# Builds the release archives: new-leaf for each platform, with typst next to
# it, the licences and a short readme; and SHA256SUMS.
#   scripts/release.sh <version> <directory>
# Needs go, curl, sha256sum, tar, xz, zip and unzip.
set -euo pipefail

version=$1
out=$(realpath -m "$2")
root=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$out"

# GOOS GOARCH typst-target
targets=(
  "linux amd64 x86_64-unknown-linux-musl"
  "linux arm64 aarch64-unknown-linux-musl"
  "darwin amd64 x86_64-apple-darwin"
  "darwin arm64 aarch64-apple-darwin"
  "windows amd64 x86_64-pc-windows-msvc"
  "windows arm64 aarch64-pc-windows-msvc"
)

readme() {
  local os=$1 exe=$2
  cat <<EOF
New Leaf $version

A CV editor: keep your CV as items in English and Dutch, make versions of
it for applications, and download them as PDFs. Everything stays on this
computer.

Start it by running $exe. It opens the editor in your browser, and stops
with the Quit button, Ctrl+C, or a few minutes after the last editor is
closed. Run "$exe -h" for the options.
EOF
  case $os in
    darwin) cat <<'EOF'

macOS: these programs are not signed by Apple. The first time, right-click
new-leaf in Finder and choose Open, or run in Terminal:
  xattr -d com.apple.quarantine new-leaf typst
EOF
      ;;
    windows) cat <<'EOF'

Windows: these programs are not signed. If SmartScreen warns, choose
"More info", then "Run anyway".
EOF
      ;;
  esac
  cat <<'EOF'

typst (https://typst.app), which makes the PDFs, is included; its licence is
in LICENSE-typst. New Leaf is licensed under the EUPL-1.2 (LICENSE).
Source: https://codeberg.org/BW20/new-leaf
EOF
}

for t in "${targets[@]}"; do
  read -r goos goarch typst_target <<<"$t"
  name="new-leaf-$version-$goos-$goarch"
  dir="$out/$name"
  exe=new-leaf
  [[ $goos == windows ]] && exe=new-leaf.exe
  echo "building $name"
  rm -rf "$dir"
  mkdir -p "$dir"
  (cd "$root" && GOOS=$goos GOARCH=$goarch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$dir/$exe" .)
  "$root/scripts/typst.sh" "$typst_target" "$dir"
  cp "$root/LICENSE" "$dir/LICENSE"
  readme "$goos" "$exe" >"$dir/README.txt"
  if [[ $goos == windows ]]; then
    (cd "$out" && rm -f "$name.zip" && zip -qr "$name.zip" "$name")
  else
    tar -C "$out" -czf "$out/$name.tar.gz" "$name"
  fi
  rm -rf "$dir"
done

(cd "$out" && sha256sum new-leaf-"$version"-* >SHA256SUMS)
echo "done:"
ls -l "$out"

#!/usr/bin/env bash
# Downloads the official typst binary for a target and checks it against the
# pinned checksum: scripts/typst.sh <rust target> <directory>. Keep the version
# in step with nixpkgs (nix develop -c typst --version).
set -euo pipefail

version=0.15.1
declare -A sha256=(
  [x86_64-unknown-linux-musl]=a6d077d0a95eed5a2eba715b2dae06be954f624ccbf85758a03f389ded33118c
  [aarch64-unknown-linux-musl]=5aa8d74a3d906e60ea12a66ac2f37f8eef1b14cbad7182a745e393a10c23dcee
  [x86_64-apple-darwin]=7f9fdd9584866245de9a79e0add8f9236fae6f40a8a45e2c4771ccc14db4e0fa
  [aarch64-apple-darwin]=48f62ed034aa3a7978309579ac6ca00045e2ef0da73114e8af27cfd8e74dc05a
  [x86_64-pc-windows-msvc]=19ce3551153c2fe7ee9fa2f95208310c8f4d3209fedb699e0333faf8913f6736
  [aarch64-pc-windows-msvc]=4ab28e1b71ec3184d38d580ab797f499b6770d952b6b19167be5cea5c2662e14
)

target=$1
dest=$2
[[ -n ${sha256[$target]:-} ]] || { echo "no typst for $target" >&2; exit 1; }
ext=tar.xz
[[ $target == *windows* ]] && ext=zip
file="typst-$target.$ext"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$file" "https://github.com/typst/typst/releases/download/v$version/$file"
echo "${sha256[$target]}  $tmp/$file" | sha256sum -c --quiet -
mkdir -p "$dest"
if [[ $ext == zip ]]; then
  unzip -q -j "$tmp/$file" "typst-$target/typst.exe" "typst-$target/LICENSE" -d "$tmp/x"
else
  mkdir "$tmp/x"
  tar -xJf "$tmp/$file" -C "$tmp/x" --strip-components=1 "typst-$target/typst" "typst-$target/LICENSE"
fi
mv "$tmp"/x/typst* "$dest/"
mv "$tmp/x/LICENSE" "$dest/LICENSE-typst"

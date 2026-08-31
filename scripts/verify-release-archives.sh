#!/usr/bin/env bash
set -euo pipefail

dist="${1:-dist}"
expected=(
  herdlord_darwin_arm64.tar.gz
  herdlord_darwin_x64.tar.gz
  herdlord_linux_arm64.tar.gz
  herdlord_linux_x64.tar.gz
  checksums.txt
)

for f in "${expected[@]}"; do
  test -f "$dist/$f"
done

(cd "$dist" && sha256sum -c checksums.txt)

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for archive in "$dist"/herdlord_*.tar.gz; do
  find "$tmp" -mindepth 1 -delete
  tar -xzf "$archive" -C "$tmp"
  test -f "$tmp/LICENSE"
  test -f "$tmp/README.md"
  test -f "$tmp/CHANGELOG.md"
  test -x "$tmp/herdlord"
done

#!/usr/bin/env bash
set -euo pipefail

dist="${1:-dist}"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *)
    echo "unsupported release smoke OS: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64 | aarch64) arch="arm64" ;;
  x86_64 | amd64) arch="x64" ;;
  *)
    echo "unsupported release smoke architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

archive="$dist/herdlord_${os}_${arch}.tar.gz"

test -f "$archive"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

tar -xzf "$archive" -C "$tmp"

test -x "$tmp/herdlord"

"$tmp/herdlord" --version
"$tmp/herdlord" --help >/dev/null

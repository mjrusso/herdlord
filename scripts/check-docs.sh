#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

go run ./cmd/gen-docs "$tmp"
diff -ru docs/commands "$tmp"

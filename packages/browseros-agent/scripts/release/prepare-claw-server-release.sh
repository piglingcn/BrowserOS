#!/usr/bin/env bash
set -euo pipefail

# Resolve a BrowserClaw server GitHub Release. This mirrors the BrowserOS server
# release policy while using the BrowserClaw tag namespace and package version.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

exec "$script_dir/prepare-server-bundle-release.sh" \
  --release-name "BrowserClaw Server" \
  --component-name "claw server" \
  --tag-prefix "claw-server/v" \
  --legacy-prefix "claw-server-v" \
  --package-json "packages/browseros-agent/apps/claw-server/package.json" \
  "$@"

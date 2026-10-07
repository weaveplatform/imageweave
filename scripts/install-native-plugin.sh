#!/bin/bash
set -euo pipefail
packer="${PACKER:-packer}"
export PACKER_PLUGIN_PATH="${PACKER_PLUGIN_PATH:-$PWD/.packer.d/plugins}"
mkdir -p bin "$PACKER_PLUGIN_PATH"
plugin="$PWD/bin/packer-plugin-imageweave"
if [ "$(go env GOOS)" = windows ]; then plugin="$plugin.exe"; fi
go build -trimpath -o "$plugin" ./cmd/packer-plugin-imageweave
if [ "$(uname -s)" = Darwin ]; then
  codesign --force --sign "${IMAGEWEAVE_SIGN_IDENTITY:--}" --entitlements scripts/entitlements.plist "$plugin"
fi
"$packer" plugins install --path "$plugin" github.com/weaveplatform/imageweave

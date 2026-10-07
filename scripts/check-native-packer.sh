#!/bin/bash
set -euo pipefail
packer="${PACKER:-packer}"
export PACKER_PLUGIN_PATH="${PACKER_PLUGIN_PATH:-$PWD/.packer.d/plugins}"
bash scripts/install-native-plugin.sh
"$packer" fmt -check -recursive templates/macos
"$packer" fmt -check -recursive templates/windows
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
go run ./cmd/imageweave matrix > "$fixture/matrix.json"
jq -r '.[] | select(.status == "template" and (.family == "macos" or .family == "windows-11")) | [.family,.version,.arch] | @tsv' "$fixture/matrix.json" > "$fixture/native.tsv"
count=0
while IFS="$(printf '\t')" read -r family release arch; do
  template=macos
  args=(-var "release=$release")
  if [ "$family" = windows-11 ]; then
    template=windows
    args+=(-var edition=enterprise -var language=en-US)
  fi
  echo "Validating native $family/$release/$arch"
  "$packer" validate \
    -var "arch=$arch" \
    -var "source_path=$fixture/source.img" \
    -var "source_sha256=0000000000000000000000000000000000000000000000000000000000000000" \
    -var "source_build=validation-only" -var "output_directory=$fixture/candidate" \
    "${args[@]}" "templates/$template"
  count=$((count + 1))
done < "$fixture/native.tsv"
[ "$count" -eq 9 ] || { echo "Expected nine supported native combinations; got $count"; exit 1; }

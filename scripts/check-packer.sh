#!/bin/bash
set -euo pipefail
packer="${PACKER:-packer}"
"$packer" fmt -check -recursive templates
"$packer" init templates/qemu
# Validation files are synthetic, deliberately unsuitable for real image construction.
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
ssh-keygen -q -t ed25519 -N '' -f "$fixture/key"
printf 'validation-only\n' > "$fixture/code.fd"
printf 'validation-only\n' > "$fixture/vars.fd"
go run ./cmd/imageweave matrix > "$fixture/matrix.json"
jq -r '.[] | select(.family == "ubuntu" or .family == "fedora") | [.family, .version, .arch] | @tsv' "$fixture/matrix.json" > "$fixture/matrix.tsv"
while IFS="$(printf '\t')" read -r family release arch; do
  echo "Validating $family/$release/$arch"
  variables=( \
    -var "family=$family" -var "release=$release" -var "arch=$arch" \
    -var "source_url=https://example.invalid/disk.img" \
    -var "source_sha256=0000000000000000000000000000000000000000000000000000000000000000" \
    -var "source_build=validation-only" -var "workspace=$fixture/output" \
    -var "ssh_private_key_file=$fixture/key" -var "ssh_public_key_file=$fixture/key.pub" \
    -var "efi_firmware_code=$fixture/code.fd" -var "efi_firmware_vars=$fixture/vars.fd" \
    -var "firmware_code_sha256=0000000000000000000000000000000000000000000000000000000000000000" \
    -var "firmware_vars_sha256=0000000000000000000000000000000000000000000000000000000000000000" \
    -var "accelerator=tcg" )
  "$packer" validate "${variables[@]}" templates/qemu
  # Exercise HCL evaluation for every catalog row: validation alone cannot
  # detect a QEMU device that the selected machine does not support.
  case "$arch" in
    amd64) expected_cdrom=ide ;;
    arm64) expected_cdrom=virtio-scsi ;;
    *) exit 1 ;;
  esac
  actual_cdrom=$(printf 'local.cdrom_interface\n' | "$packer" console "${variables[@]}" templates/qemu)
  if [ "$actual_cdrom" != "$expected_cdrom" ]; then
    echo "Incompatible seed CD bus for $family/$release/$arch: $actual_cdrom" >&2
    exit 1
  fi
done < "$fixture/matrix.tsv"

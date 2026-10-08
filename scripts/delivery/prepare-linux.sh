#!/usr/bin/env bash
# Authenticate source media and freeze the runtime request before Packer starts.
set -euo pipefail
family=${1:?family} release=${2:?release} arch=${3:?architecture} work=${4:?workspace}
case "$arch" in amd64|arm64) ;; *) exit 2 ;; esac
mkdir -p "$work"
if [ "$family" = ubuntu ]; then
  case "$release" in 26.04) codename=resolute ;; 24.04) codename=noble ;; 22.04) codename=jammy ;; *) exit 2 ;; esac
  base="https://cloud-images.ubuntu.com/$codename"
  serial=${SOURCE_SERIAL:-}
  if [ -z "$serial" ]; then
    serial=$(curl --fail --show-error --silent "$base/current/unpacked/build-info.txt" | sed -n 's/^serial=//p')
  fi
  [[ "$serial" =~ ^[0-9]{8}(\.[0-9]+)?$ ]] || { echo 'Invalid Ubuntu serial' >&2; exit 2; }
  source_uri="$base/$serial/$codename-server-cloudimg-$arch.img"
  "$WEAVEOCI" source fetch "$source_uri" --checksums "$base/$serial/SHA256SUMS" \
    --signature "$base/$serial/SHA256SUMS.gpg" \
    --keyring images/linux/ubuntu-24.04-base/keys/ubuntu-cloud-image-signing.asc \
    --fingerprint D2EB44626FDDC30B513D5BB71A5D6C4C7DB87C81 \
    --kind cloud-image --out "$work/source.img" --record "$work/source.json"
  source_hash=$(jq -er '.digest | sub("^sha256:"; "")' "$work/source.json")
else
  [ "$family" = fedora ] || exit 2
  case "$release" in 42|43|44) ;; *) exit 2 ;; esac
  # Fedora's reviewed immutable pin is deliberately mandatory. Archived N-2
  # media remains selectable but must never silently acquire a current image.
  pin="delivery/sources/fedora-$release-$arch.json"
  git ls-files --error-unmatch "$pin" >/dev/null
  source_uri=$(jq -er '.sourceURL' "$pin")
  source_hash=$(jq -er '.sourceSHA256' "$pin")
  serial=$(jq -er '.sourceBuild' "$pin")
  [[ "$source_uri" =~ ^https://[^[:space:]]+$ && "$source_uri" != *'?'* && "$source_uri" != *'#'* && "$source_uri" != *'@'* ]] || exit 2
  [[ "$source_hash" =~ ^[a-f0-9]{64}$ && "$serial" =~ ^[a-zA-Z0-9_.-]+$ ]] || exit 2
  checksums=$(jq -er '.checksumsURL' "$pin")
  fingerprint=$(jq -er '.fingerprint' "$pin")
  keyring=$(jq -er '.keyring' "$pin")
  "$WEAVEOCI" source fetch "$source_uri" --checksums "$checksums" --clearsigned \
    --keyring "$keyring" --fingerprint "$fingerprint" --kind cloud-image \
    --out "$work/source.img" --record "$work/source.json"
  [ "$(jq -er .digest "$work/source.json")" = "sha256:$source_hash" ] || { echo 'Fedora upstream pin changed' >&2; exit 1; }
fi
# Authenticated APT packages supply firmware. Record exact installed versions
# and byte hashes before the plan is resolved. These are acquisition-time pins;
# a reproducible rebuild must reuse this request's firmware bytes.
if [ "$arch" = arm64 ]; then
  code=/usr/share/AAVMF/AAVMF_CODE.fd
  vars=/usr/share/AAVMF/AAVMF_VARS.fd
else
  code=/usr/share/OVMF/OVMF_CODE_4M.fd
  vars=/usr/share/OVMF/OVMF_VARS_4M.fd
fi
[ -s "$code" ] && [ -s "$vars" ]
dpkg-query -W -f='${Package} ${Version}\n' ovmf qemu-efi-aarch64 > "$work/firmware-packages.txt"
code_hash=$(sha256sum "$code" | cut -d' ' -f1)
vars_hash=$(sha256sum "$vars" | cut -d' ' -f1)
ssh-keygen -q -t ed25519 -N '' -C imageweave-build -f "$work/build-key"
accelerator=tcg
if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then accelerator=kvm; fi
jq -n --arg family "$family" --arg release "$release" --arg arch "$arch" \
  --arg source "$work/source.img" --arg digest "$source_hash" --arg serial "$serial" \
  --arg work "$work" --arg code "$code" --arg vars "$vars" --arg ch "$code_hash" --arg vh "$vars_hash" --arg accelerator "$accelerator" \
  '{schemaVersion:1,family:$family,release:$release,arch:$arch,purpose:"guest-base",target:"qemu",sourceURL:$source,sourceSHA256:$digest,sourceBuild:$serial,workspace:($work+"/unused"),sshPrivateKey:($work+"/build-key"),sshPublicKey:($work+"/build-key.pub"),firmwareCode:{path:$code,sha256:$ch},firmwareVars:{path:$vars,sha256:$vh},accelerator:$accelerator}' > "$work/request.json"
revision=${CANDIDATE_REVISION:-1}
[[ "$revision" =~ ^[1-9][0-9]*$ ]] || exit 2
jq -n --arg uri "$source_uri" --arg tag "$release-$serial-$arch-r$revision" \
  --arg repository "$family-$release-base" '{sourceURI:$uri,tag:$tag,repository:$repository}' > "$work/metadata.json"

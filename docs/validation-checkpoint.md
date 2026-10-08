# Foundation validation checkpoint

Initial Linux foundation checkpoint (before native builder implementation).\nRecorded 2026-10-07 on branch `feat/packer-image-foundation`, before commit.
This checkpoint records local evidence; it is not promotion authorization.

## Automated checks

- `make gate` passed: vet, lint, race/shuffled unit tests, coverage,
  vulnerability scan and six CLI cross-compiles (Linux/macOS/Windows,
  amd64/arm64).
- Go statement coverage: **97.5% total**. Packages: buildinfo 100%,
  cli 97.2%, catalog 100%, plan 96.7%. The gate requires at least 95%
  total and per package; thin command entrypoints follow the repository's
  existing coverage exclusion policy.
- `make packer-check` passed Packer formatting and validation for all
  twelve Ubuntu/Fedora release/architecture selections using Packer 1.16.0
  and hashicorp/qemu 1.1.7. These are configuration checks, not twelve boots.
- Shell syntax checks passed for preparation, sealing and validation scripts.
- Local tests ran on macOS arm64. The configured multi-OS GitHub test matrix
  still needs a CI run; cross-compilation does not replace executing tests.

## Actual Ubuntu 26.04 arm64 build

A Packer QEMU/HVF run successfully booted the vendor cloud disk, connected
using a disposable SSH key, checked Ubuntu 26.04 and aarch64, ran the sealing
script and shut down. Packer completed in approximately 90 seconds.

The source was the existing authenticated Ubuntu `20260927` cloud image:

- Source: <https://cloud-images.ubuntu.com/resolute/20260927/resolute-server-cloudimg-arm64.img>
- Source SHA256: `63a93bd5a8d76e33b15ceb5daa3657bd79be804748051ab178e643b0f5da22e7`
- Packer run: `e9433e10-fa77-1d8b-61c3-7e4e5e48d1bc`
- Recipe SHA256: `3d6a2bcb28286d7fdce08335179e76237b22bb56fb5808aa950cbc5fefe2466f`

The planner rechecked the cached source checksum. This run reused the
existing detached-signature verification record; it did not fetch and
authenticate a new vendor checksum manifest.

Build and validation artifacts were retained in the local validation workspace.
They are not publicly hosted evidence. Paths below are relative to that workspace.

The candidate is `candidate/disk.raw`; input firmware, resolved plan and
Packer manifest are alongside it. Build EFI variable state was excluded
from the OCI bundle; validation generated fresh state per clone.
Disposable build keys were removed after validation.

## OCI round trip and independent clones

The existing weaveplatform-oci `image validate-linux` command packed the
candidate, passed strict deep integrity checking, unpacked it, and booted
two fresh overlays. Both guests reported Ubuntu 26.04 and shut down.

- Index: `sha256:72f9a2ec4e76a7c194de6c958f3b15c9b1ea7d9761d86a69a8cbebadbbc46980`
- Platform manifest: `sha256:d11de6ad831e97071747fe26e8f78f0d883bc6def2feedc5c5104bb05771cd02`
- Clone 1 machine ID: `65f9ee16712d46b29f730850b5920b6e`
- Clone 2 machine ID: `c20ac4e694844d64a1bc8992c2c7663a`
- Clone boot/shutdown times: 15.7 and 15.5 seconds, HVF.
- Report: `oci-validation/acceptance.json`
- Full log: `validation.log`

The firmware emitted startup errors while probing, then both guests
booted successfully. Preserve those logs for firmware compatibility work.

This was a manually assembled development handoff through the existing
schema-1 Linux validator. Its successful result proves integrity and
independent initial machine identities for this exact candidate. It does
not implement the complete [acceptance contract](contracts.md): reboot
persistence, full removal of build access, SSH identity checks, agent
operations and signed promotion evidence remain separate work.
The Packer manifest deliberately remains `qualification=unverified`.

## Remaining implementation

1. Automate the build-result handoff and enforce the complete acceptance
   contract against the final artifact.
2. Qualify the remaining Ubuntu/Fedora releases on both architectures.
3. Integrate native macOS and Windows builders with compatible hosts/media.
   macOS 27 amd64 remains explicitly vendor-unsupported.
4. Add destination-specific AWS, Azure, GCP and private-cloud profiles;
   validate registered outputs where import changes their representation.
5. Add OCI publication and Hostweave pinning against accepted immutable
   outputs, followed by runtime consumption checks.

No image was published and no Hostweave image pin was created by this run.

# Native builder validation checkpoint

Branch: `feat/packer-image-foundation`. Date: 2026-10-07. Validation was performed locally before the first implementation commit.

## Completed checks

- `make gate`: passed vet, lint, race/shuffled tests, coverage, vulnerability scan
  and cross-compilation of both CLI and Packer plugin for six host platforms.
- Coverage: **97.4% (787/808 statements)**, every package at least 95%. Native
  builders 96.9%; Packer integration 96.2%; planner 96.6%; catalog 100%.
- Native Packer validation: all six Windows and three macOS ARM64 selections passed.
- Generated Windows audit/sealing script: passed the real PowerShell parser.
- Native Windows tests compiled for amd64 and arm64; they were not executed on Windows.
- The real construction acceptance test is opt-in. Its default skip is not a VM pass.

## Live macOS run

A real Packer restore completed successfully under:

    /Volumes/KING/weave-images/work/imageweave/macos-26-arm64-native/

Pinned source: macOS 26.6.2, build 25G83; source SHA256
`885503b7f4b06609e9a512f2befd40f59730640a3f1233e3892d60affdd51c95`.

The run reuses the cached IPSW. It independently rechecks the complete source
checksum and its BuildManifest before Apple restore. It does not assert that
local hash validation alone establishes Apple signing eligibility.

Executed development plugin SHA256:
`1d9a0c8716d71bd915c099a7c0753479d5f73828402a32ae9130abee7426169c`.
Template SHA256:
`985d830b6f69829a639bd8020d6f88da080b5e3e0568f14d56fdf944d93fc35e`.

This run uses the development binary built before the final SDK scheduling
test seam, explicit firmware-policy metadata and post-build provisioner-hook
changes. Those subsequent changes passed the unit and Packer validation gates;
the live run does not certify the final binary or all three macOS releases.

The live Packer process exited successfully at 2026-10-07T14:55:05Z.
Source checksum verification took 8m21s; Apple restore took 20m33s;
the complete build took 28m54s. Apple reported 100% and its successful
completion callback. Packer then wrote its manifest.

- Packer run UUID: 199e841f-f348-cc83-d6d3-fb0ed774cb41.
- Raw disk: candidate/disk0.img, 85,899,345,920 logical bytes (80 GiB sparse).
- Auxiliary storage: candidate/auxstorage.bin, 33,579,164 bytes.
- Result: candidate/build-result.json, version 26.6.2/build 25G83.
- Manifest: candidate/packer-manifest.json.
- Full progress log: packer.log.

Post-build inspection confirmed the expected files, sizes, nonempty Apple
hardware model and exact Packer artifact list. No per-VM machine identifier
is exported. This is construction evidence, not a claim that Setup Assistant
or clone/reboot acceptance has completed.

## Deferred runtime checks

Windows live installation remains tracked by
[incident #38](https://github.com/weaveplatform/weaveplatform-oci/issues/38).
The builder, templates and construction acceptance test are implemented;
matching Windows hosts remain necessary for execution.

Both native outputs require separate independent-clone first-boot/reboot
qualification before promotion. Intel macOS 26/15 remains blocked on a compatible
backend; macOS 27 amd64 is vendor-unsupported. No images were published.

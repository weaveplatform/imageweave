# Imageweave

Imageweave produces and qualifies VM images for defined execution scenarios using
Packer templates. It hands portable artifacts to weaveplatform-oci and native
cloud images to their provider catalogs. Hostweave selects and pins outputs;
compatible runtimes execute them.

## Release window

The reviewed baseline is 2026-10-07. N means the latest stable release in each
selected track, not numeric subtraction.

| Family | Track | N | N-1 | N-2 |
|---|---|---|---|---|
| Windows 11 | Annual general-purpose H2 | 26H2 | 25H2 | 24H2 |
| Ubuntu | LTS (user-selected) | 26.04 | 24.04 | 22.04 |
| macOS | Stable major | 27 | 26 | 15 |
| Fedora | Stable releases | 44 | 43 | 42 |

Every release is represented for amd64 and arm64. Representation is a requested
support target, not certification: macOS 27 amd64 is unsupported by Apple;
macOS 26/15 amd64 need a compatible Intel backend. Native Windows and macOS
ARM64 builders exist; release-wide runtime acceptance is still required.
Fedora 42 is archived/EOL; it remains in the requested window without a claim
of upstream maintenance. Fedora 45 beta and hardware-specific Windows 26H1
do not advance these stable/general-purpose tracks.

## Current foundation

- An embedded, reviewed release catalog and explicit architecture exceptions.
- A CLI that lists the matrix and resolves pinned Linux, Windows and macOS build plans.
- A pinned Packer QEMU template shared by Ubuntu and Fedora on both architectures.
- A native Packer plugin for Windows HCS installation/Sysprep and Apple IPSW restore.
- Explicit source checksums, firmware checksums and per-build SSH keys.
- A build result contract that does not confuse construction with acceptance.
- A Packer-to-OCI delivery command with exact-artifact Linux qualification and an explicitly gated publication workflow.
- OS-specific construction and qualification packages; see [platform boundaries](docs/platform-packages.md).
- Migrated source acquisition, package locks, Windows fallback and agent/desktop preparation commands under `imageweave image`.
- Unit tests and a 95% Go coverage gate; Packer and live VM validation are separate checks.

Native templates: [Windows](templates/windows/README.md) and
[macOS](templates/macos/README.md). The [native design record](docs/research/native-packer-builders.md)
explains library reuse and output compatibility. [Native validation evidence](docs/native-validation-checkpoint.md)
records the checks and remaining host requirements. Cloud provider templates,
Packer conversion of migrated agent/desktop scenarios, native runtime observation
adapters and Hostweave consumption remain integration work. Migrating an existing
command does not qualify a release/architecture combination.
Ubuntu 26.04 arm64 has passed a real Packer build followed by OCI pack/unpack
and two independent clone boots; see the [validation checkpoint](docs/validation-checkpoint.md).

## Use

Run from this repository, with Go 1.27:

    go run ./cmd/imageweave matrix
    go run ./cmd/imageweave plan --request build.yaml

See the [delivery validation checkpoint](docs/delivery-validation-checkpoint.md) for the four-boot Linux evidence.

See [scenarios](docs/scenarios.md) for the matrix, [architecture](docs/architecture.md)
for ownership, [research](docs/research/release-baseline.md) for primary sources,
and the [QEMU template](templates/qemu/README.md) for construction.
[examples/linux.yaml](examples/linux.yaml) describes the required inputs.
Choose an absolute workspace path on storage with enough capacity for source
media, sparse build disks, caches and acceptance outputs. Paths are supplied by
the build request; the project does not assume a particular disk or mount.

## Quality

    make gate
    make packer-check

`make gate` runs the local Go checks; `make packer-check` validates the templates.
CI additionally requires a real Ubuntu N arm64 Packer build, OCI import and
acceptance of two independent clones, including reboot checks. The required
quality gate merges this acceptance coverage with the unit coverage before
applying the 95% total and per-package thresholds.

Go statement coverage does not certify template bootability. A candidate remains
unverified until two independent deployments of its final artifact pass the
scenario's acceptance contract.

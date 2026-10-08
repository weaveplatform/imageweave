# Delivery migration validation — 2026-10-08

The local Imageweave quality gate passed after the OS package split:
`make gate` ran vet, lint, race/shuffle tests, the coverage gate, vulnerability
checking and both CLI/plugin builds for six OS/architecture combinations.
Coverage was **96.9% (3383/3492)**, with every measured package above 95%.
Linux was 96.2%, Windows 95.9%, macOS 99.0%, delivery 95.2%, CLI 98.0% and
shared imagebuild 97.1%. No coverage exclusions or threshold reductions were added.
Changed workflow files passed actionlint; delivery and workspace scripts passed
shellcheck. Unit tests do not establish native or cloud execution.

## Live Linux qualification

The enhanced validator repacked the existing sealed Imageweave Ubuntu 26.04
arm64 output, deeply verified and unpacked the exact artifact, then booted two
independent clones twice each on macOS/HVF. This reused the Packer image from
the [earlier construction checkpoint](validation-checkpoint.md); it did not
perform a fresh Packer construction run on 2026-10-08.

The four boots passed at `2026-10-08T05:34:15Z`:

| Clone | Identity, stable across both boots | Combined boot time |
|---|---|---|
| 1 | `ded880df0f984b9f871e95d7c490f836` | 46.761 seconds |
| 2 | `fb4bf3706ff142b883dcccf2266fd410` | 32.894 seconds |

Both clones passed OS family/version/architecture assertions, agent absence,
build-account/key removal, persistent identity and clean shutdown. Firmware
state was independently created per clone and retained across its reboot.
The image's vendor build `20260927` is metadata bound to the artifact; the guest
probe actively checks OS family, release and architecture.

- Index: `sha256:3a2d8ea66113334d7a3069fb1058e53f089865ca7125988dd0f57b4407f15cec`
- Platform: `sha256:7668cb489e639aa7b9a208eb469b6e35e53d0a5bdf41949a36ee958ae5d4ae91`
- Report schema: 3, profile: `base`, passed: true.
- Report: `/Volumes/KING/weave-images/work/imageweave/qualification-20261008-split-linux/acceptance.json`
- Clone serial logs and disk/firmware state are in the same workspace's `reports/` directory.

The first live attempt exposed a systemd ordering cycle: a service ordered after
`cloud-final.service` but wanted by `multi-user.target` was skipped on reboot.
The checker now belongs to `cloud-init.target`; a regression test preserves that
ordering and the subsequent live run passed. Timeout is per initial boot and
per reboot, rather than one shared budget for the clone.

## Limits

This is local runtime evidence, not a signed publication. The complete delivery
workflow is exercised separately in CI. Fedora source checksum authentication
was checked for six pinned sources; it is not Fedora runtime qualification.
Windows/cloud execution remains deferred under OCI incidents 38/39. Native macOS
needs the observation/onboarding adapter described in [platform packages](platform-packages.md).
Migrated agent/desktop commands retain their prior execution engines; Packer
base delivery does not imply all derived scenarios have Packer adapters.

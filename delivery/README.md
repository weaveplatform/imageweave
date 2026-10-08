# Packer to an accepted OCI candidate

The `images | Imageweave Linux candidate` workflow selects Ubuntu or Fedora from
`pkg/catalog/releases.json`, including all three reviewed releases and both
architectures. The default is Ubuntu N on arm64. Each architecture builds on a
matching GitHub runner. The workflow logs its selected accelerator; TCG fallback
is explicit and still subject to the acceptance deadline.

Ubuntu sources are acquired through OCI's generic `source fetch`: the Canonical
checksum signature must match the checked-in cloud-image signing key. A moving
`current` lookup resolves only the vendor serial; all downloaded bytes then use
the immutable serial URL and authenticated checksum. Firmware comes from the
runner's authenticated APT packages, with package versions and exact file hashes
recorded before Packer starts. This is an acquisition-time pin, not a claim that
future APT installs will reproduce identical firmware.

Fedora's six immutable `delivery/sources/fedora-VERSION-ARCH.json` files
pin Generic Cloud qcow2 artifacts for 44-1.7, 43-1.6 and archived 42-1.1.
Their SHA-256 hashes were extracted only after locally verifying all six
clear-signed vendor checksum manifests on 2026-10-08. Each build repeats that
signature verification through `source fetch --clearsigned` and compares the
result with its checked-in hash. Public release keys are limited to the
fingerprints published on [Fedora's security page](https://fedoraproject.org/security/):

- 44: `36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`
- 43: `C6E7F081CF80E13146676E88829B606631645531`
- 42: `B0F4950458F69E1150C6C5EDC8AC4916105EF944`

Current keys came from `https://fedoraproject.org/fedora.pgp`; the archived 42 key
came from Fedora's `fedora-repos` f42 source repository. Pins retain the exact
checksum URL, media URL, build and expected signer. N-2 remains explicitly
archived; its inclusion does not assert current vendor maintenance. Source
checksum authentication is complete; it does not substitute for each image's
runtime acceptance.

Every pull request runs Ubuntu N amd64 construction and acceptance through the
required quality gate's read-only reusable workflow. That gate verifies a paused
QEMU machine can initialize KVM before downloading source media, then requires the
resolved build request to select KVM. Missing or unusable KVM fails the gate;
there is no TCG fallback. The Packer build, OCI import, exact-artifact two-clone
reboot assertions and 95% total/per-package coverage thresholds are unchanged.

This hardware choice follows the 2026-10-08 investigation: the hosted Ubuntu
26.04 arm64 candidate timed out waiting for SSH after 20 minutes under TCG.
A local reproduction using the same source bytes, Ubuntu 24.04 AAVMF firmware,
QEMU 8.2 and the pinned Packer plugin completed the build under TCG in 11 minutes
35 seconds, with guest uptime around 489 seconds when SSH became available. This
shows the template can complete under TCG, but does not qualify the hosted runner.

The [hosted ARM diagnostic run](https://github.com/weaveplatform/imageweave/actions/runs/37736041417/job/113175736590)
was cancelled after its serial log had captured a guest boot failure: the kernel
enumerated `vda1`, `vda13` and `vda15`, but systemd's 90-second waits for
`/dev/disk/by-label/BOOT` and `/dev/disk/by-label/UEFI` expired. The `/boot`,
`/boot/efi` and local-filesystem dependencies failed, and the guest entered an
emergency shell instead of becoming available over SSH. The cause of the missing
device-label readiness is still unresolved; the evidence does not prove a TCG
performance fault. Hosted ARM/TCG remains unqualified pending diagnosis and a
successful full acceptance run. The workflows stream and retain guest serial
output, QEMU argument vectors and tool versions.
GitHub does not guarantee nested virtualization, so the gate verifies KVM rather
than inferring it from a runner label. See the [GitHub runner documentation](https://docs.github.com/en/actions/concepts/runners/github-hosted-runners#cloud-hosts-used-by-github-hosted-runners).

The amd64 KVM run also exposed a separate, reproducible template defect: the
pinned QEMU plugin's `virtio-scsi` CD path adds a `virtio-scsi-device` controller
that cannot attach on `q35` (QEMU reports that its virtio bus is full). The seed
CD now uses the built-in IDE/SATA controller on amd64 and retains virtio SCSI on
ARM. The all-row Packer check evaluates this bus selection, and workflow artifacts
retain QEMU stderr as well as its argument vector. See the [pinned plugin implementation](https://github.com/hashicorp/packer-plugin-qemu/blob/v1.1.7/builder/qemu/step_run.go#L266-L278).

The standalone candidate workflow retains its Ubuntu N arm64 default and weekly
run; dispatch can select any or all of the twelve reviewed Linux rows. Both
workflows share the same Packer/OCI build action. Publication is disabled in the
quality gate and by default in standalone runs. An unsuccessful ARM run cannot
publish a qualified candidate.
For local builds, supply a strict `plan.Request` YAML/JSON with authenticated
source/firmware hashes, existing ephemeral SSH key paths, and an absolute output
parent. Run from the checked-out recipe repository:

```sh
imageweave delivery --request request.json \
  --recipe-commit "$(git rev-parse HEAD)" \
  --source-uri https://vendor.example/immutable/image.img \
  --version 26.04-vendorserial-arm64-r1 \
  --out /path/to/imageweave/delivery/candidate-1
```

The command invokes the pinned Packer template and plugin, imports the manifest
through `weaveoci`, then packs, deeply checks, unpacks and boots the exact OCI
layout twice, including reboot persistence and build-credential removal checks.
Diagnostics stream to stderr; stdout carries one JSON result. The output remains
local until publication is explicitly requested. Local callers own removal of
their ephemeral SSH keys; the workflow always removes its generated keys.

## Publication boundary

Publication is disabled by default and requires the workflow on reviewed `main`,
the `image-candidates` environment with required reviewers, and the checked-in
`delivery/candidate-policy.json`. The preflight fails if the environment is absent
or unprotected. Build-only runs use a separate read-only job; package and signing permissions
belong only to the main-only publication job. Repository administrators choose
the reviewers; no environment or
signing-key hierarchy is created by the workflow.

The candidate policy trusts only the GitHub Actions OIDC issuer and
`https://github.com/weaveplatform/imageweave/.github/workflows/linux-candidate.yml@refs/heads/main`.
Its public Sigstore root is reviewed repository content; see
[trust acquisition and refresh](trust/README.md). This policy authenticates public
repository attestations with transparency-log and certificate-timestamp evidence.
It does not configure the different private-repository signing instance.

The workflow publishes the accepted layout without repacking. GitHub's attestation
action signs build provenance and the schema-3 acceptance predicate for that
same index digest and attaches both to GHCR. A fresh `verify-candidate` retrieval
must authenticate both statements, platform inventory and report before success.
Its result is `candidate-verification.json`. Failure may leave an unqualified
candidate in GHCR; it never moves a channel or dispatches promotion. Each
architecture uses a distinct immutable candidate tag; this workflow does not claim
a combined multiarch index.

Candidate authentication is separate from release-channel admission. OCI's
`verify-published` remains the stronger promotion gate, including signed-channel
and current-parent checks. An authenticated candidate alone does not authorize
Hostweave scheduling. No release-channel root key is needed for this publication
step, and none is generated.

The Imageweave Go library dependency is OCI v0.1.3. The candidate workflow also
uses a subsequent OCI CLI commit for publication and `image verify-candidate`; its
exact source revision is recorded in `tool-commits.txt`. The OCI companion change is merged. The verifier and complete publication
metadata are not present in v0.1.3. The publication CLI writes the reviewed signer identity and guest OS version directly
using the shared entry format; the workflow does not rewrite those fields.

Reports preserve tool commit pins, vendor source verification, firmware package
versions/hashes, Packer manifest, importer receipt and runtime observations. They
exclude disks and SSH key material. Additional platform boots must be executed
before claiming those paths accepted.

## Existing mechanisms used

- [Packer manifest post-processor](https://developer.hashicorp.com/packer/docs/post-processors/manifest): the standard artifact inventory and run identifier cross the builder boundary.
- [Packer GitHub Actions workflow](https://developer.hashicorp.com/packer/tutorials/cloud-production/github-actions): CI drives the existing Packer executable and templates.
- [GitHub attest action](https://github.com/actions/attest): immutable digest subjects and custom acceptance predicates are attached to the registry.
- [GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations): OIDC workflow identities authenticate the evidence; a bare JSON report never grants promotion.

## First registry publication: 2026-10-08

[Run 37747143545](https://github.com/weaveplatform/imageweave/actions/runs/37747143545)
built Ubuntu 26.04 amd64 from vendor serial `20260927`, passed two-clone KVM
acceptance, published and signed the image. Its final verification step failed
because the entry used `subjectRegexp` instead of `subject_regexp`; the publisher
also omitted the Linux guest OS version. The publication tooling now emits both
fields through OCI's typed entry format.

The unchanged published artifact was independently pulled and verified against
the reviewed policy using corrected local entry metadata. Both authenticated
signers matched this repository's main-branch publishing workflow, and its
schema-3 acceptance report passed. The original Actions run remains failed.

- Repository: `ghcr.io/weaveplatform/weave-images/ubuntu-26.04-base`
- Immutable tag: `26.04-20260927-amd64-r1`
- Index: `sha256:ee20b6cf06f297ad8ee51086a615ecc6fcf5217f1d57dd31a94a0c032a64a3ae`
- Platform: `sha256:0545c696ebac8e7c4e8f86f792bee1b0bd176bf6bc6725bde68688ec1e1a237d`

Revision `2` completed after the metadata fix (see below); neither existing
revision may be overwritten. No release channel has
been promoted, and Hostweave runtime consumption has not yet been demonstrated.

## Successful signed candidate: revision 2

[Run 37756233330](https://github.com/weaveplatform/imageweave/actions/runs/37756233330)
completed successfully on 2026-10-08. It rebuilt Ubuntu 26.04 amd64 from serial
`20260927`, passed independent-clone/reboot acceptance, published the accepted
layout, signed build and acceptance statements, then fetched and authenticated
both statements and the schema-3 report from GHCR.

- Tag: `26.04-20260927-amd64-r2`
- Index: `sha256:b46a3ebab827c106228a5e9d6c6d606d646057931f5ae2583727f85cb4418a61`
- Platform: `sha256:63621674698f56229aabfcc424b8af3588374dc7bf36550ba07c93a07d7e82e7`
- Both signers: the reviewed main-branch `linux-candidate.yml` workflow.

This qualifies that exact candidate, not every Linux release or a release channel.
The broader [twelve-row qualification run](https://github.com/weaveplatform/imageweave/actions/runs/37757271019)
was started separately with publication disabled. Its results must be checked
individually; scheduling the matrix is not proof that all rows passed.

## Native construction delivery

`imageweave delivery-native` connects the existing Windows HCS and macOS Apple VZ
Packer templates to OCI import, packing and deep integrity checking. Install the
native plugin from the reviewed recipe checkout first. Requests use the strict
[Windows](../examples/windows.yaml) or [macOS](../examples/macos.yaml) format,
with authenticated local media and its pinned hash/build. The command selects a
fresh Packer workspace under `--out` and refuses an existing output directory.

```sh
imageweave delivery-native --request native-request.yaml \
  --recipe-commit "$(git rev-parse HEAD)" \
  --source-uri https://vendor.example/immutable/media \
  --version native-build-arm64-r1 --out /path/to/image-storage/native-delivery
```

The result identifies the manifest, imported bundle and OCI layout and always
reports `qualification: unverified`. It does not run Linux acceptance, fabricate
native observations, sign or publish. OCI imports only manifest-listed files:
Windows exports raw sectors and a firmware policy; macOS retains its hardware
model and auxiliary storage. VM identities and private working containers stay
outside the published file contract.

The manual `Native image construction` workflow replaces the transitional
Windows-only installation workflow. It accepts an existing runner-local request,
its canonical source URI and an immutable candidate tag. It runs only from `main`
and requires a matching self-hosted runner with `weave-images` plus the platform
labels (`Windows`/`X64`, `Windows`/`ARM64`, or `macOS`/`ARM64`). Configure
`WEAVE_IMAGE_WORKSPACE` to existing image storage. Windows requires an elevated
runner with HCS and Git Bash; macOS requires Apple silicon with virtualization
support and `codesign`. Media acquisition/authentication remains a prerequisite.

Packer 1.16.0 is compiled from pinned source for the host architecture because
that release does not provide a Windows arm64 archive. OCI tooling remains pinned
to v0.1.3; the native plugin is built from the checked-out Imageweave commit and
signed with the virtualization entitlement on macOS. The workflow retains logs
and receipts as Actions artifacts; the full image stays on runner storage.

`TestNativeDelivery` is a real opt-in integration test. Set
`IMAGEWEAVE_NATIVE_REQUEST`, `IMAGEWEAVE_NATIVE_SOURCE_URI` and a new absolute
`IMAGEWEAVE_NATIVE_OUT`, install the plugin, and run:

```sh
go test -count=1 -timeout 4h -run '^TestNativeDelivery$' -v ./test/acceptance
```

A default skip is not acceptance. Matching Windows host execution remains
[deferred under OCI #38](https://github.com/weaveplatform/weaveplatform-oci/issues/38).
Native clone onboarding, independent identities and reboot observations must
still be implemented and exercised before native candidate publication.

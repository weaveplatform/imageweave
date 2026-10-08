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

Pull requests affecting this path and the weekly schedule execute the default
Ubuntu N arm64 build and acceptance without publication. Workflow dispatch can
select any or all of the twelve reviewed Linux rows.
For local builds, supply a strict `plan.Request` YAML/JSON with authenticated
source/firmware hashes, existing ephemeral SSH key paths, and an absolute output
parent. Run from the checked-out recipe repository:

```sh
imageweave delivery --request request.json \
  --recipe-commit "$(git rev-parse HEAD)" \
  --source-uri https://vendor.example/immutable/image.img \
  --version 26.04-vendorserial-arm64-r1 \
  --out /Volumes/KING/weave-images/delivery/candidate-1
```

The command invokes the pinned Packer template and plugin, imports the manifest
through `weaveoci`, then packs, deeply checks, unpacks and boots the exact OCI
layout twice, including reboot persistence and build-credential removal checks.
Diagnostics stream to stderr; stdout carries one JSON result. The output remains
local until publication is explicitly requested. Local callers own removal of
their ephemeral SSH keys; the workflow always removes its generated keys.

## Publication boundary

Publication is disabled by default and requires the workflow on reviewed `main`,
the `image-candidates` environment, and a reviewed
`delivery/admission-policy.json`. Configure required environment reviewers in
GitHub before enabling this path. No trust keys or channels are generated.
The policy follows OCI's admission schema: GHCR registry, relative local Sigstore
trusted-root file, anchored build and acceptance workflow identity expressions,
signed channel location and named public root anchors. Pin the workflow identity
to `linux-candidate.yml@refs/heads/main`; use the GitHub Actions OIDC issuer.

The workflow publishes the accepted layout without repacking. GitHub's attestation
action signs build provenance and the schema-3 acceptance predicate for that
same index digest and attaches both to GHCR. A fresh `verify-published` retrieval
must authenticate both statements, platform inventory, report and channel policy
before success. Failure may leave an unqualified candidate in GHCR; it never
moves a channel or dispatches promotion. Each architecture uses a distinct
immutable candidate tag; this workflow does not claim a combined multiarch index.

Reports preserve tool commit pins, vendor source verification, firmware package
versions/hashes, Packer manifest, importer receipt and runtime observations. They
exclude disks and SSH key material. Hosted signing/publication and additional
platform boots must be executed before claiming those paths accepted.

## Existing mechanisms used

- [Packer manifest post-processor](https://developer.hashicorp.com/packer/docs/post-processors/manifest): the standard artifact inventory and run identifier cross the builder boundary.
- [Packer GitHub Actions workflow](https://developer.hashicorp.com/packer/tutorials/cloud-production/github-actions): CI drives the existing Packer executable and templates.
- [GitHub attest action](https://github.com/actions/attest): immutable digest subjects and custom acceptance predicates are attached to the registry.
- [GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations): OIDC workflow identities authenticate the evidence; a bare JSON report never grants promotion.

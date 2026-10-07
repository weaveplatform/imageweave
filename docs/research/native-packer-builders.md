# Native Packer builders: implementation decision

Reviewed 2026-10-07. Scope: Windows 11 and macOS guest-base construction.

## Why a native Packer plugin

Packer owns template loading, plugin discovery, build execution, cancellation,
console output, host-side provisioner hooks and manifest post-processing.
Imageweave supplies the native restore/install boundary using the
[Packer Builder interface](https://developer.hashicorp.com/packer/docs/plugins/creation/custom-builders).
This keeps construction outside OCI and keeps consumer CLIs out of construction.

The existing [Tart builder](https://developer.hashicorp.com/packer/integrations/cirruslabs/tart/latest/components/builder/tart)
and [Parallels IPSW builder](https://developer.hashicorp.com/packer/integrations/Parallels/parallels/latest/components/builder/ipsw)
introduce their respective runtime/tool dependencies. The selected implementation
instead reuses the native library integration already developed for the Weave
consumers. This is a specific compatibility choice, not a claim that those
builders are unsuitable for their own runtimes.

Apple's [Mac platform configuration](https://developer.apple.com/documentation/virtualization/vzmacplatformconfiguration)
ties a restored guest to its hardware model and auxiliary storage, while the
machine identifier provides per-VM identity.
[VZMacOSInstaller](https://developer.apple.com/documentation/virtualization/vzmacosinstaller)
restores from an IPSW. The implementation preserves these distinctions in its
output contract; a raw disk alone is insufficient.

Windows uses [Host Compute System](https://learn.microsoft.com/en-us/virtualization/api/hcs/overview)
for build VM lifecycle. It retains Microsoft's supported
[Sysprep generalization](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/sysprep--generalize--a-windows-installation)
rather than copying a live configured machine. Secure Boot/TPM build state is
private and does not enter the artifact. A native disk container used while
building does not become the distribution format: Windows output is raw sectors.

## Code provenance and boundary

Native restore, HCS lifecycle, Windows audit/Sysprep recipes, VHD conversion
helpers and associated tests were extracted from weaveplatform-oci commit
47381c92a5b8d8c24831611ca6c31bf4386ff625. They now live in Imageweave's
pkg/nativebuild and internal/disk. The earlier OCI commands are left intact for
compatibility; removing them is a separate migration, not part of this change.
Imageweave imports neither OCI construction internals nor Guestweave.

Additional work here supplies strict native input validation, local source
checksums, IPSW manifest checks, exact Windows receipt matching, Packer integration,
artifact selection, catalog/planner integration and repeatable construction tests.
The builder does not implement automatic source discovery, guest provisioning,
registry promotion or cloud image import.

## Pinned integration and tests

- Packer 1.16.0; local imageweave plugin 0.1.0.
- Packer plugin SDK 0.6.12.
- Apple platform bindings 0.20.1; Win32 bindings 0.5.0; Media Foundry 0.8.0.
- The SDK requires the documented
  [go-cty RPC compatibility replacement](https://github.com/hashicorp/packer-plugin-sdk/issues/187).
  This is a pinned upstream requirement, not a local path replacement.
- Go unit coverage must pass 95% total and per package.
- Packer validation exercises all six Windows and three macOS ARM64 selections.
- Native construction acceptance is opt-in and fails if the selected host does
  not match. Missing input skips the Go acceptance test; make accept-native
  requires explicit input and never reports an absent host as accepted.
- Two-clone first-boot/reboot acceptance and publication remain separate gates.
  No construction result grants runtime_verified or promotion permission.

# Platform implementation boundaries

Packer owns construction orchestration. `pkg/delivery` connects its completed
outputs to OCI import and exact-artifact runtime qualification; it does not
implement a hypervisor or turn construction metadata into acceptance evidence.

The migrated implementation is split by operating system:

| Package | Responsibility |
|---|---|
| `internal/imagebuild` | Catalogs, package locks, authenticated payload acquisition, shared file/tool helpers, candidate inspection and export planning |
| `internal/imagebuild/linux` | QEMU boot lifecycle, Linux sealing, agent/desktop preparation and Linux runtime observations |
| `internal/imagebuild/windows` | Windows media selection, HCS installation/lifecycle, Windows agent preparation and PowerShell recipes |
| `internal/imagebuild/macos` | Apple restore-media selection and macOS agent installation/sealing recipes |
| `pkg/nativebuild` | Existing native Packer plugin backend for Apple restore and Windows construction |
| `internal/cli` | Select the platform implementation and wire its dependencies to commands |

OS packages depend on shared code. Shared code must not import an OS package.
Shared acquisition accepts a recipe strategy instead of dispatching into a
platform implementation. Tool execution, downloads and native operations use
the existing injected boundaries; tests exercise errors and cancellation without
pretending to boot an operating system. Platform tests live beside their code.

Moving a scenario into its owner's package does not replace its construction
engine. Migrated Windows fallback and agent/desktop commands preserve their
behavior while Packer scenario adapters replace them incrementally. Base-image
candidate delivery uses the pinned Packer templates. `delivery-native` imports
and deeply checks native construction output without granting runtime qualification;
the native workflow now uses this path for both Windows and macOS.

## Disk formats and host backends

Disk responsibilities are grouped by portable format and host capability,
independently of the guest operating system:

| Package | Responsibility |
|---|---|
| `weaveplatform-oci/pkg/disk/vhd` | Shared portable fixed-VHD footer, geometry and raw-sector codec; used on every supported host |
| `internal/disk/hosts/windows` (`windowsdisk`) | Windows virtdisk create/convert operations, native handles and source-container detection |

Native Packer construction, migrated Windows construction and destination export
all use this one local Windows host backend. It imports the shared VHD codec;
it does not implement another footer parser. Non-Windows builds preserve the
explicit unsupported-host error for native disk operations. Linux and macOS
continue to use their existing file/QEMU operations; no empty host packages or
unused backend interface are added.

The deleted local VHD codec had the same exported behavior and identical public
contract tests and fixtures as the pinned OCI codec. Disk-format tests belong
with that codec; Imageweave retains host-backend and caller integration tests.
VHDX remains a possible local Windows working container, not a requirement of
OCI artifacts or a cross-platform codec implemented by this project.

## Native acceptance boundary

`pkg/nativebuild` produces construction results marked `unverified`. A macOS
restore leaves Setup Assistant as the first-boot policy. The shared
`weaveplatform-oci/pkg/macsetup` planner supplies screen actions, but it does not
create a VM, observe its display, enter those actions or collect guest identity.
The current Linux acceptance adapter cannot qualify macOS or Windows; a regression
test rejects every native release/architecture matrix row before running tools.

A native macOS adapter still needs disposable-clone onboarding, a host-controlled
guest observation channel, observed OS/build and hardware identity, persistent
identity across reboot, credential checks and shutdown observation. Guestweave's
existing onboarding implementation depends on its private VNC runtime and is not
a standalone public platform-library adapter. Invoking its construction commands
would reintroduce the dependency this separation removes. Native restore tests
therefore remain construction tests, and native candidates remain unqualified.

Windows matching-host runtime execution and cloud destinations remain tracked in
[OCI #38](https://github.com/weaveplatform/weaveplatform-oci/issues/38) and
[OCI #39](https://github.com/weaveplatform/weaveplatform-oci/issues/39).

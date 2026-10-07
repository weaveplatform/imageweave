# Windows 11 native base builder

This template uses Imageweave's Packer SDK plugin and Microsoft's HCS/virtual-disk
APIs through the same Win32 bindings as Guestweave. It does not execute Guestweave,
Hyper-V PowerShell, QEMU, or an OCI build command.

Supported recipe selections: 26H2, 25H2, 24H2 on amd64 and arm64. Run on an
elevated Windows host with matching architecture, HCS, virtualization and isolated
guest-state support. A matrix entry is not evidence that a host passed acceptance.

## Inputs and execution

1. Acquire an authenticated Microsoft installation ISO with the requested
   edition, architecture and language. Media Foundry may produce the ISO from
   business-edition ESD media. This builder accepts an already prepared local ISO;
   it does not claim that changing a product key adds an absent edition.
2. Pin SHA256 and the exact installed build including update revision.
3. Set the paths in [examples/windows.yaml](../../examples/windows.yaml) to a
   mounted image-storage volume on the Windows host. K: is illustrative; it is
   not an assumed mapping of KING. Keep source and output on the large test disk.
4. Build/install the local plugin and resolve the plan:

       make native-plugin
       export PACKER_PLUGIN_PATH="$PWD/.packer.d/plugins"
       go run ./cmd/imageweave plan --request build.yaml > plan.json
       go run ./cmd/imageweave plan --request build.yaml --vars > build.pkrvars.json
       packer validate -var-file=build.pkrvars.json templates/windows
       packer build -var-file=build.pkrvars.json templates/windows

These commands use Bash; the plugin and builder themselves are Go executables.
Create the workspace parent first. Existing candidate directories are refused.

## Lifecycle and output

The builder rechecks source SHA256, copies the ISO into its private workspace
and uses Media Foundry to retarget the copy to efisys_noprompt.bin. The original
media remains unchanged. A UDF seed supplies an unattended answer file.

Setup selects the edition, partitions the fresh disk and enters audit mode.
No persistent build user, password, WinRM listener or network adapter is created.
The guest verifies the base is agent-free and unencrypted, prepares EFI fallback
boot, removes cached build answer files, runs Sysprep /generalize /oobe and checks
IMAGE_STATE_GENERALIZE_RESEAL_TO_OOBE. COM1 reports progress and the exact
installed edition/release/build/architecture. The host requires both the matching
receipt and VM exit before exporting.

HCS uses a dynamic VHDX as private working storage. It is flattened through
Windows' fixed-VHD conversion, then the footer is removed. The **published input
artifact is disk0.img: raw sectors**, with build-result.json. VHDX, installer ISO,
seed, COM1 log and build.vmgs are not in Packer's artifact file list. Never
recursively publish the candidate directory.

Secure Boot and TPM are enabled during construction; per-build firmware/TPM
state must not be reused by consumers. The output's required policy is recorded
in build-result.json. Consumers regenerate their own VM state.

This is a sealed base builder. Guest Packer provisioners fail explicitly;
post-build shell-local hooks and manifest post-processing are supported.
Derived agent images require a separate provisioning lifecycle.

## Validation

make packer-check checks every supported template combination.
make accept-native runs an actual Packer installation when
IMAGEWEAVE_NATIVE_FAMILY=windows-11 and IMAGEWEAVE_NATIVE_VARS names a real
Packer variable file. It requires a fresh output directory and matching host.

The live Windows installation remains deferred under the existing host
[acceptance incident #38](https://github.com/weaveplatform/weaveplatform-oci/issues/38). Unit/lifecycle tests and successful
template validation do not replace it. Two-clone first-boot/reboot qualification
is also required before promotion.

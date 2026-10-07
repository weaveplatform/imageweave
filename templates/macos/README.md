# macOS native base builder

This template uses Imageweave's Packer SDK plugin and Apple's
Virtualization.framework directly through go-bindings-macosplatform. No Tart,
Parallels or Guestweave executable is involved.

Recipes cover macOS 27, 26 and 15 on Apple silicon. Apple's macOS guest restore
API does not supply an Intel macOS backend. macOS 27 amd64 is vendor-unsupported;
26/15 amd64 remain blocked on a compatible Intel builder/runtime decision.

## Build

Use an Apple silicon Mac with virtualization support, a host-supported Apple
IPSW and enough free APFS storage for an 80 GiB sparse disk plus restore media.
For this test setup use /Volumes/KING/weave-images/work/imageweave/.

1. Authenticate and cache the IPSW; pin its SHA256 and Apple build identifier.
2. Create a fresh workspace parent and fill
   [examples/macos.yaml](../../examples/macos.yaml).
3. Build/install the local plugin. The installer signs it with the Apple
   virtualization entitlement; IMAGEWEAVE_SIGN_IDENTITY optionally selects
   a signing identity instead of the local ad-hoc default.

       make native-plugin
       export PACKER_PLUGIN_PATH="$PWD/.packer.d/plugins"
       go run ./cmd/imageweave plan --request build.yaml > plan.json
       go run ./cmd/imageweave plan --request build.yaml --vars > build.pkrvars.json
       packer validate -var-file=build.pkrvars.json templates/macos
       packer build -var-file=build.pkrvars.json templates/macos

The builder refuses existing output and rechecks the checksum immediately before
restore. It also verifies ProductVersion and ProductBuildVersion in the IPSW's
BuildManifest.plist. Apple independently decides whether the restore is supported
on the host; a locally valid checksum does not establish current signing eligibility.

## Output and identity

Packer returns disk0.img, auxstorage.bin and build-result.json. The result records
the Apple hardware model, minimum/default CPU and memory, observed restore version,
source digest and first-boot policy. This is a pristine restored base that starts
at Setup Assistant. It does not bake a build account or login credentials into
the image and does not pretend to have run post-login acceptance.

A compatible consumer must retain the hardware model and copy auxiliary storage,
then generate a new VZMacMachineIdentifier and its own MAC address for each VM.
The restore VM's machine identifier is not exported. Do not discard auxiliary
storage or substitute a generic UEFI profile.

Progress includes the actual Apple restore fraction, elapsed time and timeout.
Cancellation requests Apple's installer cancellation and waits for completion
before releasing the VM. Failed output remains for diagnosis and is not returned
as an artifact.

Guest Packer provisioners fail explicitly because this base has no login or guest
communicator. Host-side post-build hooks work; agent installation belongs to a
derived-image builder after controlled onboarding.

## Validation

make packer-check validates all three ARM64 templates on any host.
IMAGEWEAVE_NATIVE_FAMILY=macos and IMAGEWEAVE_NATIVE_VARS enable
make accept-native for an actual restore on Apple silicon.

Construction acceptance checks the output disk, hardware model, auxiliary
storage and pinned source/version metadata. Independent-clone onboarding,
identity persistence across reboot and runtime operations remain promotion checks;
qualification stays unverified until those checks pass.

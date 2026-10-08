# Transitional image scenarios

Packer templates and `imageweave matrix` define the maintained base-image
catalogue. The definitions in this directory preserve the former OCI agent,
desktop, Windows fallback, media and export scenarios during migration. This
legacy catalogue is not the current N/N−1/N−2 matrix.

Imageweave owns image construction and runtime acceptance. The pinned
`weaveplatform-oci` library owns guest artifact contracts, packaging, unpacking,
registry transport and admission. Imageweave does not invoke Guestweave to build.

## Base construction

Use `imageweave plan` and the Packer templates for Ubuntu/Fedora and native
macOS/Windows builds. `imageweave delivery` automates the Linux candidate handoff.
Completed Packer output is imported with `weaveoci bundle import-imageweave`.
The importer hashes the imported bytes; it does not authenticate the producer
or qualify the result for promotion.

`imageweave image validate-linux BUNDLE... --out NEW_DIRECTORY --arches arm64`
packs the candidate, deeply checks the OCI content, unpacks those exact bytes
and boots independent clones. Progress and guest console output are streamed
on stderr; the final report is JSON on stdout. Acceptance reports remain
unsigned until the delivery workflow signs them. OCI admission verifies signed
evidence for the exact published index.

The historical Ubuntu source definitions under `linux/` preserve reviewed
vendor signing keys and acquisition settings. They are input references, not an
alternative maintained base-image matrix or an automatic publication path.

## Agent and desktop construction

`packages.lock.json` pins core and all eight module versions, SHA256 hashes and
sizes. Missing native installers are represented explicitly; construction fails
before installation when a package or signature is missing. Refresh and review
a lock before building:

```sh
imageweave image lock --out packages-next.json
imageweave image check-lock --lock packages-next.json --require-packages linux/arm64
imageweave image prepare-agent --lock packages-next.json --platform linux/arm64 \
  --cache /Volumes/KING/weave-images/work/cache/packages --out /path/to/new-payload
imageweave image build-linux-agent --base /path/to/verified-layout \
  --base-name ghcr.io/weaveplatform/weave-images/ubuntu-base --arch arm64 \
  --lock packages-next.json --cache /path/to/cache --out /path/to/new-candidate
```

The builder verifies publisher signatures and locked module binaries, installs
an offline payload and removes guest identity and host trust. `build-windows-agent`
is the corresponding Windows HCS scenario. `build-linux-desktop` derives Xfce/X11
from a pinned agent parent with a reviewed `--desktop-lock` package closure.
These commands remain transitional while equivalent Packer scenarios are
validated. They do not publish or promote images.

`imageweave image validate-agent-linux LAYOUT --tag TAG --out NEW_DIRECTORY`
performs authenticated agent lifecycle acceptance. `imageweave image rebuild-plan
INPUTS --accepted RECORDS` computes a plan from pinned inputs and authenticated
completion records; it does not execute the plan.

## Native source selection and fallback

```sh
imageweave image ipsw --version 26 --list
imageweave image ipsw --version 26.6.2 --out apple-source.json
imageweave image windows --from-windows enterprise-25h2 --arch amd64 --list
imageweave image windows --from-windows enterprise-26200 --arch amd64 \
  --cache /path/to/media --out microsoft-source.json
```

Apple discovery records the catalogue's exact restore version, build and hash.
Windows discovery uses the same Media Foundry sources as Guestweave. ESD
acquisition checks Microsoft's SHA1 and records SHA256; retail acquisition
records SHA256 after checking download size. An acquired lock and cached bytes
support repeatable builds. Expired URLs do not silently change a locked source.

The fallback `imageweave image build-windows --source-lock LOCK --from-windows
SELECTOR --arch ARCH --cache CACHE --out NEW_DIRECTORY` remains available on a
matching elevated HCS host. It installs into a build-local fixed VHD and emits
raw guest sectors after Sysprep and shutdown. The raw bundle excludes build
firmware and TPM identity; VHDX is not a published image requirement.
`.github/workflows/image-native-candidate.yml` exposes this explicit fallback.
The native Packer plugin remains the preferred base builder.

Pristine macOS candidates stop at Setup Assistant. Restore completion is not
proof of a clean first boot, independent guest identity or reboot persistence.
Native guest observation is required before admitting those candidates.
Windows host execution and cloud acceptance remain deferred under OCI
[incident #38](https://github.com/weaveplatform/weaveplatform-oci/issues/38) and
[incident #39](https://github.com/weaveplatform/weaveplatform-oci/issues/39).

## Destination export

`imageweave image export LAYOUT --expected-digest sha256:... --platform OS/ARCH
--target TARGET --out NEW_DIRECTORY` prepares destination disk files from an
exact OCI index. Targets are `guestweave-windows`, `guestweave-macos`, `azure`,
`aws`, `gcp`, `openstack` and `vsphere`. The receipt binds output hashes to the
source; acceptance is explicitly pending. Cloud registration, provider guest
preparation and native boot evidence are separate requirements. VMDK export
alone does not produce an OVA. See the
[delivery decision](https://github.com/weaveplatform/weaveplatform-oci/blob/main/docs/research/decisions/0015-target-image-delivery.md).

## KING workspace

Use `/Volumes/KING/weave-images/` for local media, disks and build outputs.
`scripts/images/workspace.sh init` mounts the existing APFS sparse bundle on
KING at `/Volumes/KING/weave-images/work`. Its wrapper checks the backing mount,
keeps a 32 GiB reserve and routes build caches and temporary files to KING:

```sh
scripts/images/workspace.sh init
scripts/images/workspace.sh check 80
scripts/images/workspace.sh run make image-builder \
  BIN_DIR=/Volumes/KING/weave-images/work/cache
```

Choose a fresh candidate directory. Local files are not automatically published.

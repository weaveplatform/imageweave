# Product boundaries

Status: foundation; reviewed 2026-10-07.

Imageweave owns scenario definitions, pinned image inputs, Packer templates,
guest preparation and sealing, build results and destination acceptance.
Packer and its plugins own supported construction mechanics. New native plugins
need evidence that existing integrations cannot satisfy the scenario.

weaveplatform-oci owns VM packaging, artifact integrity, registry transport,
verification primitives, cache and offline distribution. It must not depend on
a running Imageweave builder.

Hostweave owns workload placement, operational image inventory, authorization,
pinned selection, execution and capacity. A host worker image differs from a
workload guest image. Hostweave may submit build requests and supply leased
builders; Imageweave must also run independently.

Guestweave consumes images and creates independent local VMs. Its native media
acquisition remains available. Image construction does not invoke a Guestweave CLI.

Portable path: scenario -> Packer -> candidate disk -> OCI packaging -> exact
artifact acceptance -> publication -> Hostweave pin -> compatible runtime.
Native cloud path: scenario -> provider builder/import -> registered candidate ->
destination acceptance -> approved provider image -> Hostweave pin.
Cloud disks need not pass through OCI; evidence distribution needs its own
versioned handoff.

Start with guest-base / qemu. Worker, agent and desktop are separate scenarios,
not flags that implicitly certify new behavior. Container construction is outside
this VM foundation; preserve Hostweave's existing container consumption.

The native restore/install library extraction now lives in Imageweave behind a
Packer plugin; see [the design decision](research/native-packer-builders.md).
Consumer migration and removal of legacy OCI build commands have not happened. Hostweave's inspected
snapshot 73b0dc240c9c4e4711f8a8f306278af6f50537de still recognizes older VM formats.
Its ADR 0029 assigns pending construction responsibilities that must be reconciled
with this product boundary before deleting code in either project.

See the full destination-first research:
https://github.com/weaveplatform/weaveplatform-oci/blob/feat/validated-image-delivery/docs/research/17-destination-first-image-research.md
That research was inspected locally; the branch document may not yet be published.

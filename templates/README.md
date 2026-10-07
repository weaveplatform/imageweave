# Packer scenarios

Use HCL2 directly. Share guest preparation by OS; keep destination-specific
source builders and runtime requirements explicit.

Implemented template: qemu / guest-base, Ubuntu and Fedora.
Windows and macOS requirements are in docs/scenarios.md; no empty placeholder
template pretends those backends exist.

Templates use immutable input checksums and exact plugin versions. Catalog
refreshes must not silently change completed plans. Build output is a candidate;
publication needs separate acceptance of the final runtime artifact.

Future templates should reuse provider-native builders for AWS, Azure and GCP.
Select private-cloud templates from actual supported customer targets.

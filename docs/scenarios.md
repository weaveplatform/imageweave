# Scenario and support policy

Release snapshot: 2026-10-07. The machine-readable catalog is pkg/catalog/releases.json.
N/N-1/N-2 are ordered reviewed releases, never arithmetic or runtime "latest".
Refreshing the catalog requires a reviewed commit and new qualification.
A plan records the concrete release and catalog date, so a retry cannot silently
resolve a newer N. Never mutate an already recorded plan.

Ubuntu tracks the latest three LTS releases, as explicitly requested.
Windows tracks annual H2 general-purpose releases; device-specific 26H1 is
excluded. macOS tracks 27, 26, 15 despite the numbering jump. Fedora tracks
stable 44, 43, 42; beta 45 does not advance N.

Both amd64 and arm64 are represented. Guest architecture is not the architecture
of the Imageweave executable. Packer/QEMU host firmware and acceleration must
match the selected target; hardware emulation is explicit, not inferred.

| Family | amd64 | arm64 | Foundation backend |
|---|---|---|---|
| Ubuntu LTS | Template available, unqualified | Template available, unqualified | Packer QEMU |
| Fedora | Template available, unqualified | Template available, unqualified | Packer QEMU |
| Windows 11 | Native template available, unqualified | Native template available, unqualified | Packer Imageweave / HCS |
| macOS 27 | Vendor unsupported | Native template available, unqualified | Packer Imageweave / Apple VZ |
| macOS 26/15 | Blocked: no Intel VZ macOS backend | Native template available, unqualified | Packer Imageweave / Apple VZ |

Intel macOS OS support does not prove Virtualization.framework supports an Intel
macOS guest. Do not fall back to Hackintosh or pretend an ARM guest meets AMD64.
Tart/Anka runtime adoption remains a separate decision; this foundation introduces
neither dependency.

Fedora 42 remains available as an archived source target. EOL is recorded
separately from template readiness and acceptance. Availability of media does
not restore upstream maintenance. Each Windows edition has its own lifecycle;
24H2 Pro reaches its documented end of updates on 2026-10-13. Requested edition
and media must be selected before implementing the Windows installer template.

A scenario records purpose, destination/runtime, OS release/build/edition,
architecture, firmware, software lock, first boot, management transport,
output representation and acceptance profile. Current executable plans allow
guest-base/qemu for Linux only; other combinations fail with a reason.

Cloud targets (AWS, Azure, GCP) and private clouds have separate profiles.
An OS/architecture pair in this catalog is not a claim of support on every cloud.

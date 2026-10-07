# Release baseline

Checked 2026-10-07. This is a reviewed snapshot, not an automatic latest-release
resolver. Catalog coverage and vendor support are distinct from live validation.

| Family | Window | Primary evidence |
|---|---|---|
| Windows 11 | 26H2, 25H2, 24H2 | https://learn.microsoft.com/en-us/windows/release-health/windows11-release-information |
| Ubuntu LTS | 26.04, 24.04, 22.04 | https://ubuntu.com/project/docs/release-team/list-of-releases/ |
| macOS | 27, 26, 15 | https://support.apple.com/en-us/127455 and https://support.apple.com/en-us/122867 |
| Fedora stable | 44, 43, 42 | https://fedoramagazine.org/announcing-fedora-linux-44/ and https://fedoramagazine.org/announcing-fedora-linux-45-beta/ |

Microsoft identifies 26H1 as hardware-specific; the general-purpose annual H2
track deliberately excludes it. The Windows edition has not been silently
changed to Server or Enterprise. Microsoft lists different support dates by
edition.

Apple states macOS 27 requires Apple silicon. Therefore macOS 27/amd64 is an
explicit unsupported catalog entry. macOS 26 supports selected Intel models;
that does not establish a native Intel virtualization builder.

Fedora 45 was still a beta in the reviewed release evidence. Fedora's lifecycle
documentation is protected by an access challenge in this environment; the
English lifecycle page could not be read directly. The Fedora project discussion
records Fedora 42 reaching EOL:
https://discussion.fedoraproject.org/t/is-it-worth-contributing-to-the-fedora-project/193521
This is an official project-hosted community report, not a vendor support
commitment. The exact EOL date is deliberately not encoded.

Packer HCL and manifest contracts:
https://developer.hashicorp.com/packer/docs/templates/hcl_templates
https://developer.hashicorp.com/packer/docs/post-processors/manifest
QEMU plugin documentation (observed 1.1.7):
https://developer.hashicorp.com/packer/integrations/hashicorp/qemu/latest/components/builder/qemu

The repository starts with template validation and unit tests; no native or cloud
VM acceptance is claimed. Deferred Windows/cloud infrastructure from OCI remains
tracked in that repository's issues 38 and 39; their current remote status has
not been rechecked here.

# Build result and acceptance

Version: 1, foundation proposal.

A resolved build plan contains the catalog date, exact OS release, architecture,
scenario and template path; immutable media URL/build/checksum; firmware file
checksums; and the Packer variable values needed for the candidate. The request
uses a fresh per-build SSH key pair. Private key bytes never enter the plan.

Plans identify inputs; they do not grant approval. The plan command verifies local
firmware hashes, key-file presence and scope but cannot prove a remote media
signature. Source authentication must happen before supplying its pinned hash.
Packer checks downloaded source bytes against that hash.

Packer's manifest captures the artifact identifier and input metadata. Its
qualification value is unverified. Candidate construction cannot generate
runtime_verified evidence or promote itself. A receipt is not a signature.

The next handoff implementation must bind:
- Exact recipe commit, executable/plugin versions and package inputs.
- Concrete output files/digests, or provider/account/region/resource identity.
- Applicable firmware and runtime constraints.
- Two clone identities, observed OS/build, boot/reboot outcomes and sealing checks.
- Required agent/desktop operation outcomes where applicable.
- Evidence signature and publication identity.

Qualification of the final OCI artifact must cover pack/unpack and fresh clones.
No repack after acceptance without rebinding and repeating the relevant checks.
For cloud images, test the registered image after any importer adaptation.

A boot test must verify first boot completes, two guest identities differ,
identity persists across reboot within each clone, and build access is absent.
Build accounts, SSH host keys, cloud-init history and credentials must not leak.
Failed/skipped acceptance is never a pass.

This foundation does not publish images, create Hostweave image versions, or
fabricate acceptance receipts. Those integrations will implement this contract.

Offline source disks may use an absolute local path with the same authenticated
SHA256 requirement. The planner streams and verifies local source bytes.

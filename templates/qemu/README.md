# Linux guest-base template

Packer 1.16.0 and hashicorp/qemu 1.1.7 are pinned for this initial template.
Ubuntu LTS and Fedora use vendor cloud disks with cloud-init already installed.
Both amd64 and arm64 are explicit template inputs. Ubuntu 26.04 arm64 has a
[recorded smoke run](../../docs/validation-checkpoint.md); full qualification
across the release matrix remains pending.

Requires QEMU, qemu-img, an ISO creation utility supported by Packer, matching
EFI firmware, and a fresh SSH key pair. Use a unique workspace for each attempt.
Packer must refuse an existing candidate directory; do not add -force.

1. Obtain an authenticated vendor image checksum and immutable source build.
2. Copy matching firmware under the workspace and record its SHA256 values.
3. Generate a disposable SSH key pair outside the future image.
4. Fill examples/linux.yaml with actual paths and immutable inputs.
5. Generate a plan and Packer variables:

       imageweave plan --request build.yaml > plan.json
       imageweave plan --request build.yaml --vars > build.pkrvars.json

6. Set PACKER_CACHE_DIR to a cache directory on the selected build storage, then run:

       packer init templates/qemu
       packer validate -var-file=build.pkrvars.json templates/qemu
       packer build -var-file=build.pkrvars.json templates/qemu

Source bytes are checked by Packer. The planner verifies local firmware hashes.
Treat the request, variable file and firmware as immutable between planning and
execution; the future execution wrapper must recheck them before each run.
The private SSH key remains on the builder. The seed contains only its public key.

The output is candidate/disk.raw plus firmware state and manifest.json.
Build firmware state is not automatically safe clone identity: the destination
adapter must regenerate appropriate per-VM state. The seed and builder SSH key
must not be distributed. Packer's manifest records qualification=unverified.

The preparation step verifies OS family/version and architecture, then sealing
removes build access, cloud-init seed/state, SSH host keys and machine identity.
A real two-clone test is still required to prove first boot and identity behavior.
Do not publish the candidate as qualified on the basis of packer build success.

Validate template structure with make packer-check. This checks all twelve Linux
release/architecture selections without downloading guest images or booting VMs.

Offline source disks may use an absolute local path with the same authenticated
SHA256 requirement. The planner streams and verifies local source bytes.

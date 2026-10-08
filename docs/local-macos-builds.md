# Local macOS image builds

The repository owns the workflow. Run the Go entry point whenever images are
needed; an LLM, interactive setup operator or hosted CI job is not part of the
build process.

## Run

From a clean, committed Imageweave checkout:

```console
GOWORK=off go run ./cmd/build-macos --workspace auto --release 26 --tier all
```

This builds a pristine base and a prepared image for macOS 26. Use `--release all`
for the reviewed 27/26/15 matrix, or select `27`, `26`, `15`, an exact version,
`n`, `n-1` or `n-2`. Use `--tier base` for pristine images only. `--tier prepared`
ensures its base exists and passes validation before creating the derived image.
The same command is available as `imageweave build-macos`.

Prerequisites:

- An Apple Silicon Mac supporting the selected guest's Apple hardware model.
  macOS 27 native first-boot provisioning also requires a macOS 27 host.
- Go 1.27, Git, Apple's `codesign`, and Packer **1.16.0** on `PATH`.
  `--packer /absolute/path/to/packer` selects an alternate installation.
- Network access for Go dependencies, source discovery and Apple restore media.
- Mounted writable APFS/HFS+ storage with enough capacity for the requested job.
  The driver discovers local and external volumes automatically. Unmounted disks
  are not mounted or formatted by the builder.

The driver builds the Imageweave worker and native Packer plugin from the current
commit, signs both with the virtualization entitlement, installs the plugin into
the workspace, and builds `weaveoci` at the version pinned by `go.mod`.
`--weaveoci /absolute/path/to/weaveoci` is an explicit developer override.
`--repository /absolute/path/to/imageweave` selects the checkout used for tools
and templates. Uncommitted changes are rejected so image provenance names the
recipe that actually ran.

## Storage planning and sequential execution

`--workspace auto` is the default. The Go planner inventories mounted local and
external volumes, probes writability, measures available space, and chooses the
compatible image workspace with the most free space. APFS volumes in the same
container share one capacity pool. Mounted sparse disk images are also limited
by the available space on their backing filesystem, including nested images.
Unwritable or incompatible volumes are reported and excluded.

To inspect the plan without downloading media or creating disks:

```console
GOWORK=off go run ./cmd/imageweave plan-macos-storage --release 26 --tier base
```

The plan returns JSON on stdout and progress/errors on stderr. Insufficient space
returns a nonzero exit code before tool installation, downloads or VM creation.
Capacity is checked again before each download and construction. This is a
preflight check, not a reservation against unrelated processes consuming space.

Sizing uses the 80 GiB logical disk size without assuming compression or sparse
allocation savings. Each retained image budgets three copies (Packer output,
imported bundle and OCI), plus 40 GiB restore media. One additional 80 GiB copy
covers sequential clone acceptance or publication read-back. A base-only job
therefore requires 360 GiB image capacity, plus an 8 GiB free-space reserve.
Host tool scratch requires 4 GiB plus the reserve; shared pools add both demands
and count the reserve once. Oversized restore media is rejected before download.
This deliberately conservative bound can exceed the eventual physical usage.

Local multi-image runs retain their outputs, so the estimate includes all of
them: `--release all --tier all` budgets six images, or 1760 GiB plus reserve.
The publishing workflow builds, accepts, publishes and removes each successful
job's workspace before beginning the next image. Failed and build-only outputs
are retained. No pre-existing images are deleted to make room.

`--workspace /absolute/path` pins storage instead of selecting it automatically.
Use a pinned workspace to reliably reuse source locks and outputs: automatic
selection can choose a different volume when available capacity changes.
`--scratch-root /short/native/path` selects tool scratch; its default is the
user cache's `imageweave/scratch` directory. Scratch must remain on APFS/HFS+ for
Go linker output and Packer Unix sockets. Keep its path short enough for macOS
Unix sockets. Concurrent commands sharing a scratch root are rejected by an
exclusive build lock, even when their image workspaces differ. Normal exit and
errors release it; after a hard kill, inspect running builders before removing
`scratch-root/imageweave-build/build.lock`.

Releases, tiers and acceptance clones execute sequentially. Both candidate
workflows use `max-parallel: 1` and a fixed concurrency group across refs. The
macOS workflow preserves its storage plan as an artifact even when preflight
fails, then stops the remaining matrix when a job fails.

## What completes successfully

The command resolves and pins Apple media, downloads and verifies its checksum,
runs the native Packer build, imports its manifest into OCI, packs the output,
and deeply verifies it. It then unpacks two independent copies of the exact
artifact and performs first-boot, reboot, identity and shutdown acceptance.

A base retains Setup Assistant and has no pre-created account. Onboarding during
base acceptance modifies disposable clones only. A prepared image derives from
that exact base platform digest and contains the `weave` administrator with
password `weave`, automatic desktop login and SSH. It contains no Weave agent or
modules. Temporary build access and SSH host keys are removed before packing;
prepared acceptance must observe the configured system without repairing it.

Success returns JSON containing each image's layout, tag, index digest and
acceptance-report path. Stage progress and child-process diagnostics go to
stderr. For a combined terminal log, redirect with `2>&1`. Any failed stage returns
a nonzero exit code. Images are not published by this local command.

## Repeat runs and failures

Each release's first source selection is saved in `macos-<release>-source.json`
inside the workspace. Later runs use that record, so a new Apple catalog entry
does not silently change the input. Use a new workspace to select newer media;
retain the old source record and artifacts for reproduction.

Output directories are named from the exact OS version, Apple build, tier and
recipe commit. On a repeat run the command verifies the existing OCI content,
source/parent and recipe bindings, and complete digest-bound acceptance evidence.
Only then does it reuse that output. This is operational idempotence; independently
reinstalled macOS disks are not promised to have identical bytes or timestamps.

An exclusive workspace lock covers tool installation and builds. Normal exit or
an interrupt releases it. After a hard process kill, confirm no builder or VM is
still using the workspace before removing `build.lock`. An incomplete or corrupt
candidate is preserved and rejected. To retry, retain its diagnostics and move
that candidate directory out of the workspace, or choose a new workspace. The
command never treats a timeout, missing evidence or skipped boot as success.

`--timeout 90m` defaults to a native build timeout and a separate timeout for each
clone's acceptance, not a deadline for the whole release matrix. Cancellation
propagates to workers; the native worker is responsible for stopping its VM.

## Validation status

The standalone orchestration is tested with real small OCI artifacts and fake
external build/VM boundaries. These tests cover exact-parent derivation, reuse,
source locks, concurrent-run exclusion and rejection of incomplete evidence.
They do not certify macOS bootability. The macOS 26 live restore, OCI packaging
and deep inspection have passed; native runtime acceptance and the full 27/26/15
base/prepared matrix still require live validation before being called complete.

## GitHub Actions on an Apple Silicon runner

`images | Imageweave macOS candidate` runs the same Go entry point on a
self-hosted Mac. Linux continues to use GitHub-hosted Linux runners. Both paths
build with Packer, package OCI, require two independent clone/reboot checks,
publish the exact accepted index to GHCR, attest build and acceptance, and fetch
and verify the registry result. Neither pipeline promotes release channels.

The macOS workflow selects N, N−1, N−2 or all from the reviewed catalog and
publishes base images to `ghcr.io/weaveplatform/weave-images/macos-VERSION-base`.
It runs only on `main`, serializes work on the physical Mac, and uses the existing
`image-candidates` approval environment when publication is selected. Build-only
runs have no package-write or OIDC permissions. macOS signing identities are
limited to `macos-candidate.yml@refs/heads/main` in
`delivery/macos-candidate-policy.json`; Linux's trust policy is unchanged.

Each job passes `--revision RUN_ID-ATTEMPT` to the Go driver. This adds an immutable
`-rRUN_ID-ATTEMPT` tag suffix, so rerunning a workflow does not overwrite a previous
image built from the same recipe and Apple media. Local commands may also use
`--revision 1` (or `1-2` for an attempt); leaving it unset preserves existing local
naming and reuse. Reusing the same revision in the same workspace re-verifies the
existing artifact before reuse.

Prepared-image construction and acceptance remain available locally. Publication
of a prepared image requires a signed parent release channel under OCI's existing
lineage policy. Candidate attestations alone do not satisfy that policy; this
workflow therefore publishes base images only.

### Runner containment and installation

Use a dedicated organization runner group named `imageweave-macos`. Its final
server-side settings must be:

| Setting | Required value |
| --- | --- |
| Repository access | Selected: `weaveplatform/imageweave` only |
| Allow public repositories | Enabled (the selected repository is public) |
| Restrict to workflows | Enabled |
| Selected workflows | `weaveplatform/imageweave/.github/workflows/macos-candidate.yml@refs/heads/main` |
| Runner labels | `self-hosted`, `macOS`, `ARM64`, `imageweave-macos` |

These group restrictions enforce pipeline access; labels only select a runner.
Do not register the Mac in the default group or as a repository-wide runner.
Pull requests, forks, other workflows and other branches are not authorized to
use this pool. This is GitHub job-access containment, not an operating-system
sandbox: reviewed jobs execute as the runner's macOS user. Use a separate macOS
account or machine if host filesystem isolation is required.

Install the checksum-verified `osx-arm64` runner from the
[official runner releases](https://github.com/actions/runner/releases), outside
any source checkout, in a path without spaces (for example,
`~/.local/share/imageweave-runner`). The pinned `actions/setup-go` failed its
`go version` invocation when the runner was under `Application Support`.
Register it at organization scope using GitHub's short-lived
registration token and `--runnergroup imageweave-macos --labels imageweave-macos`.
No fixed image-volume environment variable is needed. The workflow runs the Go
storage planner and puts the job on the selected volume. Keep the runner
application and checkout on the host filesystem. `TMPDIR` and `GOTMPDIR` use
`RUNNER_TEMP` during construction. Live tests found Go 1.27.0 producing zero-filled
linker output on ExFAT, which also rejects Packer's Unix sockets. Image outputs
use the selected APFS/HFS+ workspace; publication and registry verification use
its temporary directory for their large read-back layouts. For local `go run`,
retain native host defaults for the initial compiler's `TMPDIR` and `GOTMPDIR`;
the Go driver applies the selected scratch root to all child build processes.
Ensure `jq`, Git, Go prerequisites and
Apple's command-line tools are accessible to the service. The workflow installs
checksum-pinned Packer and the Go version from `go.mod`.

From the runner installation directory, `./svc.sh install` and `./svc.sh start`
install and start the official user LaunchAgent. `./svc.sh status` checks it and
`./svc.sh stop` takes the Mac offline. The user session must be available; the
workflow uses `caffeinate` during native construction and acceptance.

When image storage is on an external volume, macOS may prompt for removable-volume
access for the runner's bundled `externals/node24/bin/node` process. Approve that
once in the logged-in user session. Until approval, directory operations can
block, including the workflow's initial workspace creation. This is a macOS
privacy permission, separate from GitHub runner-group and publication approvals;
repeatedly dispatching jobs will not resolve it. Grant removable-volume access
only; this workflow does not require broad Full Disk Access.

GitHub requires a selected workflow to exist at its specified ref. Before this
workflow is merged, create the dedicated group with selected repository access
but **zero repositories**, `restricted_to_workflows: true` and an empty workflow
list. Registering an online runner in that group does not authorize any jobs.
After merge, run `bash scripts/configure-macos-runner-group.sh` with an organization
administrator's GitHub CLI session. The script closes repository access, applies
the exact main-branch workflow restriction, then grants only Imageweave access.
It fails closed if a configuration step fails. Runner registration credentials
and organization administration tokens are never passed to image jobs.

Dispatch without publication first, or select publication and approve the
existing environment:

```console
gh workflow run macos-candidate.yml --repo weaveplatform/imageweave --ref main -f release=all -f publish=true
```

Build progress streams into the Actions job. Logs, source pins and digest-bound
acceptance evidence are retained as Actions artifacts for 14 days. Successfully
published jobs reclaim their own temporary image workspace only after registry
verification and evidence upload; failed and build-only workspaces remain for
inspection. Registry credentials use a job-specific directory and are removed
on completion. The runner processes one release at a time.

GitHub documents the enforced boundary in
[runner-group access controls](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/manage-access)
and the corresponding
[runner-group API](https://docs.github.com/en/rest/actions/self-hosted-runner-groups).

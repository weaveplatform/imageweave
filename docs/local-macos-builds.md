# Local macOS image builds

The repository owns the workflow. Run the Go entry point whenever images are
needed; an LLM, interactive setup operator or hosted CI job is not part of the
build process.

## Run

From a clean, committed Imageweave checkout:

```console
GOWORK=off go run ./cmd/build-macos --workspace /absolute/path/to/images --release 26 --tier all
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
- An absolute workspace on storage with capacity for downloaded IPSWs, restored
  disks, OCI data and two unpacked acceptance clones per artifact. The base disk
  is 80 GiB logical size; actual usage depends on sparse-file support and content.

The driver builds the Imageweave worker and native Packer plugin from the current
commit, signs both with the virtualization entitlement, installs the plugin into
the workspace, and builds `weaveoci` at the version pinned by `go.mod`.
`--weaveoci /absolute/path/to/weaveoci` is an explicit developer override.
`--repository /absolute/path/to/imageweave` selects the checkout used for tools
and templates. Uncommitted changes are rejected so image provenance names the
recipe that actually ran.

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

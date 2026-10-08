package macos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/opencontainers/go-digest"
	"oras.land/oras-go/v2/content/oci"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/conformance"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

var ErrPrepared = errors.New("macOS prepared image")

// Parent pins a platform manifest inside a previously authenticated OCI index.
// Local verification detects corruption; it does not authenticate its publisher.
type Parent struct {
	Layout string `mapstructure:"parent_layout" json:"layout"`
	Ref    string `mapstructure:"parent_ref"    json:"ref"`
	Name   string `mapstructure:"parent_name"   json:"name"`
	Digest string `mapstructure:"parent_digest" json:"digest"`
}

// Session owns a disposable VM, its display and its authenticated SSH transport.
// Close must release native file locks even after a cancelled guest operation.
type Session interface {
	Connect(context.Context, bool) (packer.Communicator, error)
	Observe(context.Context, string) (Observation, error)
	Restart(context.Context) error
	Stop(context.Context) error
	Close() error
	Identities() map[string]string
}

type (
	StartSession func(context.Context, string, spec.Config, io.Writer) (Session, error)
	Provision    func(context.Context, packer.Communicator) error
)

// Prepared keeps the parent identity separate from guest-observed evidence.
type Prepared struct {
	Parent      spec.BaseImage
	Config      spec.Config
	Observation Observation
}

// Prepare unpacks its parent privately, keeps the VM live through Packer's hooks,
// and seals only after inspecting the intended account. No source disk is edited.
func Prepare(
	ctx context.Context,
	parent Parent,
	out string,
	start StartSession,
	provision Provision,
	log io.Writer,
) (result Prepared, err error) {
	progress := common.NewNativeProgress(ctx, log, "macOS prepared construction")
	finish := progress.Start()
	defer func() { finish(err) }()
	progress.Step("checking input and starting independent native clone")
	if start == nil || provision == nil {
		return result, fmt.Errorf("%w: runtime and Packer provisioner required", ErrPrepared)
	}
	cfg, err := unpackParent(ctx, parent, out)
	if err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	result.Parent, result.Config = spec.BaseImage{Name: parent.Name, Digest: parent.Digest}, cfg
	vm, err := start(ctx, out, cfg, log)
	if err != nil {
		return result, fmt.Errorf("start prepared clone: %w", err)
	}
	defer func() { err = errors.Join(err, vm.Close()) }()
	comm, err := vm.Connect(ctx, true)
	if err != nil {
		return result, fmt.Errorf("prepare first boot: %w", err)
	}
	progress.Step("running Packer provisioners")
	if err = provision(ctx, comm); err != nil {
		return result, fmt.Errorf("packer provisioners: %w", err)
	}
	if err = runGuest(ctx, comm, configurePrepared); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	// A restart proves the configured session can reappear without UI repair.
	progress.Step("restarting guest and verifying persistent identity")
	if err = vm.Restart(ctx); err != nil {
		return result, fmt.Errorf("restart prepared guest: %w", err)
	}
	comm, err = vm.Connect(ctx, false)
	if err != nil {
		return result, fmt.Errorf("prepared login after restart: %w", err)
	}
	observed, err := vm.Observe(ctx, "WEAVE-PREPARED-CHECK")
	if err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if err = observed.Check(cfg, "WEAVE-PREPARED-CHECK"); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	result.Observation = observed
	progress.Step("removing temporary build state and sealing")
	if err = runGuest(ctx, comm, sealPrepared); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if err = vm.Stop(ctx); err != nil {
		return result, fmt.Errorf("stop sealed guest: %w", err)
	}
	return result, nil
}

func unpackParent(ctx context.Context, p Parent, out string) (spec.Config, error) {
	var empty spec.Config
	if p.Name == "" || p.Ref == "" || digest.Digest(p.Digest).Validate() != nil ||
		digest.Digest(p.Digest).Algorithm() != digest.SHA256 {
		return empty, fmt.Errorf(
			"%w: repository, reference and SHA-256 platform digest required",
			ErrPrepared,
		)
	}
	info, err := os.Stat(p.Layout)
	if err != nil || !info.IsDir() {
		return empty, fmt.Errorf("%w: existing parent layout required", ErrPrepared)
	}
	store, err := oci.New(p.Layout)
	if err != nil {
		return empty, fmt.Errorf("open parent: %w", err)
	}
	root, err := store.Resolve(ctx, p.Ref)
	if err != nil {
		return empty, fmt.Errorf("resolve parent: %w", err)
	}
	report, err := conformance.Check(ctx, store, root, conformance.Options{Deep: true})
	if err != nil || !report.OK() || root.MediaType != spec.MediaTypeIndex {
		return empty, fmt.Errorf(
			"%w: invalid parent index: %w; %v",
			ErrPrepared,
			err,
			report.Problems(),
		)
	}
	for _, child := range report.Children {
		if child.Descriptor.Digest.String() != p.Digest {
			continue
		}
		cfg := child.Description.Config
		if cfg.Guest.OS != "darwin" || cfg.Guest.Arch != "arm64" || cfg.Guest.Variant != "base" ||
			cfg.Provisioning.Agent != nil ||
			cfg.Provisioning.DefaultUser != "" ||
			cfg.Provisioning.CredentialHint != "set-at-first-boot" ||
			cfg.Firmware.Type != "apple" {
			return empty, fmt.Errorf("%w: pristine Apple ARM64 base required", ErrPrepared)
		}
		if err = os.Mkdir(out, 0o700); err != nil {
			return empty, fmt.Errorf("create private prepared clone: %w", err)
		}
		if _, err = pack.Unpack(
			ctx,
			store,
			child.Descriptor,
			out,
			chunk.AssembleOptions{},
		); err != nil {
			return empty, fmt.Errorf("unpack private prepared clone: %w", err)
		}
		return cfg, nil
	}
	return empty, fmt.Errorf("%w: platform digest absent from parent index", ErrPrepared)
}

func runGuest(ctx context.Context, comm packer.Communicator, script string) error {
	cmd := &packer.RemoteCmd{Command: script, Stdout: io.Discard, Stderr: io.Discard}
	if err := comm.Start(ctx, cmd); err != nil {
		return fmt.Errorf("prepared guest command: %w", err)
	}
	cmd.Wait()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("prepared guest command: %w", err)
	}
	if cmd.ExitStatus() != 0 {
		return fmt.Errorf("%w: guest command exited %d", ErrPrepared, cmd.ExitStatus())
	}
	return nil
}

// Password is the explicit template credential. Never pass scripts to progress logs.
const configurePrepared = `printf '%s\n' weave | sudo -S -p '' /bin/sh -eu -c '
uid=$(id -u weave)
launchctl asuser "$uid" /usr/sbin/sysadminctl -autologin set -userName weave -password weave -adminUser weave -adminPassword weave
'`

// Preserve account/Preboot security material. Only known build artifacts and
// SSH host keys are removed; the OS regenerates host keys on the next boot.
const sealPrepared = `printf '%s\n' weave | sudo -S -p '' /bin/sh -eu -c '
rm -f /Users/weave/.ssh/authorized_keys /var/root/.ssh/authorized_keys
rm -f /etc/ssh/ssh_host_*_key /etc/ssh/ssh_host_*_key.pub
rm -f /Users/weave/.zsh_history /Users/weave/.bash_history
scutil --set ComputerName Mac
scutil --set LocalHostName Mac
scutil --set HostName Mac
sync
'`

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("macOS operation: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

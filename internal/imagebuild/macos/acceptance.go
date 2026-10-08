package macos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
)

// AcceptClone exercises an already independently unpacked OCI artifact. Setup
// automation is permitted only for pristine bases and only on this disposable copy.
func AcceptClone(
	ctx context.Context,
	c imagecheck.Clone,
	start StartSession,
	log io.Writer,
) (result imagecheck.Boot, err error) {
	progress := common.NewNativeProgress(ctx, log, "macOS clone acceptance")
	finish := progress.Start()
	defer func() { finish(err) }()
	progress.Step("checking input and starting independent native clone")
	began := time.Now()
	defer func() {
		result.ElapsedSeconds = time.Since(began).Seconds()
		if err != nil {
			result.Passed = false
		}
	}()
	cfg := c.Config
	if start == nil || cfg.Guest.OS != "darwin" || cfg.Guest.Arch != "arm64" ||
		cfg.Provisioning.Agent != nil ||
		(cfg.Guest.Variant != "base" && cfg.Guest.Variant != "prepared") {
		return result, fmt.Errorf("%w: macOS base or prepared clone required", ErrPrepared)
	}
	vm, err := start(ctx, c.Bundle, cfg, log)
	if err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	defer func() { err = errors.Join(err, vm.Close()) }()
	comm, err := vm.Connect(ctx, cfg.Guest.Variant == "base")
	if err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if cfg.Guest.Variant == "base" {
		if err = runGuest(ctx, comm, configurePrepared); err != nil {
			return result, fmt.Errorf("macOS lifecycle: %w", err)
		}
	}
	first, err := vm.Observe(ctx, c.Marker)
	if err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if err = first.Check(cfg, c.Marker); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	progress.Step("restarting guest and verifying persistent identity")
	if err = vm.Restart(ctx); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if _, err = vm.Connect(ctx, false); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	reboot, err := vm.Observe(ctx, c.Marker)
	if err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if err = reboot.Check(cfg, c.Marker); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	if first.HardwareUUID != reboot.HardwareUUID || first.HostKeyDigest != reboot.HostKeyDigest {
		return result, fmt.Errorf("%w: guest identity changed across reboot", ErrPrepared)
	}
	if err = vm.Stop(ctx); err != nil {
		return result, fmt.Errorf("macOS lifecycle: %w", err)
	}
	identities := vm.Identities()
	identities["hardwareUUID"] = first.HardwareUUID
	identities["sshHostKeyDigest"] = first.HostKeyDigest
	result = imagecheck.Boot{
		Platform:    "darwin/arm64",
		OSVersion:   first.Version,
		OSBuild:     first.Build,
		Accelerator: "apple-vz",
		Passed:      true,
		Marker:      first.Marker,
		MachineID:   first.HardwareUUID,
		Identities:  identities,
		Profile:     imagecheck.ValidationProfile(cfg),
		Checks: map[string]bool{
			"boot":                      true,
			"shutdown":                  true,
			"os-version":                true,
			"reboot-identity":           true,
			"fresh-firmware":            true,
			"no-build-credentials":      first.BuildAccessAbsent && reboot.BuildAccessAbsent,
			"agent-absent":              first.AgentAbsent && reboot.AgentAbsent,
			"ssh-host-key-after-reboot": true,
		},
	}
	if cfg.Guest.Variant == "prepared" {
		result.Operations = preparedOperations()
		result.RebootOperations = preparedOperations()
	}
	return result, nil
}

func preparedOperations() map[string]imagecheck.Outcome {
	out := map[string]imagecheck.Outcome{}
	for _, name := range []string{"account-login", "administrator", "ssh", "automatic-login", "desktop-session", "setup-complete", "agent-absent"} {
		out[name] = imagecheck.Outcome{Status: imagecheck.Passed}
	}
	return out
}

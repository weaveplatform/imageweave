package macosbuild

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/weaveplatform/imageweave/pkg/delivery"
)

func prepareTools(ctx context.Context, o Options, log io.Writer) (tools, error) {
	var result tools
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return result, fmt.Errorf("%w: an Apple silicon Mac is required", ErrBuild)
	}
	repository, err := filepath.Abs(o.Repository)
	if err != nil {
		return result, fmt.Errorf("resolve repository: %w", err)
	}
	toolsDir := filepath.Join(o.Workspace, "tools")
	if err = os.MkdirAll(toolsDir, 0o700); err != nil {
		return result, fmt.Errorf("create tool directory: %w", err)
	}
	execute := func(ctx context.Context, binary string, args []string, output io.Writer) error {
		if log != nil {
			_, _ = fmt.Fprintf(log, "macOS workflow: running %s\n", filepath.Base(binary))
		}
		cmd := exec.CommandContext(ctx, binary, args...)
		// Let Packer and the native worker stop their VMs before the hard kill.
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 75 * time.Second
		cmd.Dir = repository
		cmd.Env = append(
			os.Environ(),
			"GOWORK=off",
			"GOBIN="+toolsDir,
			"PACKER_PLUGIN_PATH="+filepath.Join(toolsDir, "plugins"),
		)
		cmd.Stdout, cmd.Stderr = output, log
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("run %s: %w", filepath.Base(binary), err)
		}
		return nil
	}
	return installTools(ctx, o, toolsDir, repository, execute, exec.LookPath, log)
}

func installTools(
	ctx context.Context,
	o Options,
	toolsDir, repository string,
	execute delivery.Runner,
	lookPath func(string) (string, error),
	log io.Writer,
) (tools, error) {
	var result tools
	var err error
	var output bytes.Buffer
	if err = execute(ctx, "git", []string{"rev-parse", "HEAD"}, &output); err != nil {
		return result, err
	}
	result.commit = strings.TrimSpace(output.String())
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(result.commit) {
		return result, fmt.Errorf("%w: full repository commit required", ErrBuild)
	}
	output.Reset()
	if err = execute(
		ctx,
		"git",
		[]string{"status", "--porcelain", "--untracked-files=normal"},
		&output,
	); err != nil {
		return result, err
	}
	if output.Len() != 0 {
		return result, fmt.Errorf(
			"%w: commit repository changes before building so artifacts identify an exact recipe",
			ErrBuild,
		)
	}
	if _, err = lookPath(o.Packer); err != nil {
		return result, fmt.Errorf(
			"Packer 1.16.0 is required (install Packer or set --packer): %w",
			err,
		)
	}
	result.imageweave = filepath.Join(toolsDir, "imageweave")
	plugin := filepath.Join(toolsDir, "packer-plugin-imageweave")
	commands := [][]string{
		{"go", "build", "-trimpath", "-o", result.imageweave, "./cmd/imageweave"},
		{"go", "build", "-trimpath", "-o", plugin, "./cmd/packer-plugin-imageweave"},
		{
			"codesign",
			"--force",
			"--sign",
			"-",
			"--entitlements",
			filepath.Join(repository, "scripts", "entitlements.plist"),
			result.imageweave,
		},
		{
			"codesign",
			"--force",
			"--sign",
			"-",
			"--entitlements",
			filepath.Join(repository, "scripts", "entitlements.plist"),
			plugin,
		},
		{o.Packer, "plugins", "install", "--path", plugin, "github.com/weaveplatform/imageweave"},
	}
	for _, command := range commands {
		if err = execute(ctx, command[0], command[1:], log); err != nil {
			return result, err
		}
	}
	result.oci = o.OCI
	if result.oci == "" {
		output.Reset()
		if err = execute(
			ctx,
			"go",
			[]string{
				"list",
				"-m",
				"-f",
				"{{.Version}}",
				"github.com/weaveplatform/weaveplatform-oci",
			},
			&output,
		); err != nil {
			return result, err
		}
		version := strings.TrimSpace(output.String())
		if version == "" {
			return result, fmt.Errorf("%w: pinned OCI Go module required", ErrBuild)
		}
		if err = execute(
			ctx,
			"go",
			[]string{
				"install",
				"github.com/weaveplatform/weaveplatform-oci/cmd/weaveoci@" + version,
			},
			log,
		); err != nil {
			return result, err
		}
		result.oci = filepath.Join(toolsDir, "weaveoci")
	}
	if o.OCI != "" {
		if _, err = lookPath(o.OCI); err != nil {
			return result, fmt.Errorf("find OCI executable: %w", err)
		}
	}
	result.runner = execute
	return result, nil
}

package packerplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/opencontainers/go-digest"
	"github.com/zclconf/go-cty/cty"

	"github.com/weaveplatform/imageweave/internal/imagebuild/macos"
	"github.com/weaveplatform/imageweave/pkg/nativebuild"
)

type preparedConfiguration struct {
	common.PackerConfig `       mapstructure:",squash"`
	macos.Parent        `       mapstructure:",squash"`
	Output              string `mapstructure:"output_directory"`
	Timeout             string `mapstructure:"timeout"`
}

// PreparedBuilder holds the guest communicator open for normal Packer provisioners.
type PreparedBuilder struct {
	config preparedConfiguration
	start  macos.StartSession
}

func (*PreparedBuilder) ConfigSpec() hcldec.ObjectSpec {
	spec := hcldec.ObjectSpec{}
	for _, name := range []string{"parent_layout", "parent_ref", "parent_name", "parent_digest", "output_directory", "timeout"} {
		spec[name] = &hcldec.AttrSpec{Name: name, Type: cty.String, Required: true}
	}
	return spec
}

func (b *PreparedBuilder) Prepare(raws ...any) ([]string, []string, error) {
	b.config = preparedConfiguration{}
	normalized, err := normalizeConfiguration(raws)
	if err != nil {
		return nil, nil, err
	}
	if err = config.Decode(
		&b.config,
		&config.DecodeOpts{Interpolate: false},
		normalized...); err != nil {
		return nil, nil, fmt.Errorf("decode prepared builder: %w", err)
	}
	c := b.config
	duration, err := time.ParseDuration(c.Timeout)
	if err != nil || duration <= 0 || !filepath.IsAbs(c.Output) ||
		filepath.Clean(c.Output) != c.Output ||
		!filepath.IsAbs(c.Layout) ||
		c.Ref == "" ||
		c.Name == "" ||
		digest.Digest(c.Digest).Validate() != nil ||
		!strings.HasPrefix(c.Digest, "sha256:") {
		return nil, nil, fmt.Errorf(
			"%w: complete pinned parent, new absolute output and positive timeout required",
			macos.ErrPrepared,
		)
	}
	return nil, nil, nil
}

func (b *PreparedBuilder) Run(
	ctx context.Context,
	ui packer.Ui,
	hook packer.Hook,
) (packer.Artifact, error) {
	timeout, _ := time.ParseDuration(b.config.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := b.start
	if start == nil {
		start = macos.StartNative
	}
	prepared, err := macos.Prepare(
		ctx,
		b.config.Parent,
		b.config.Output,
		start,
		func(ctx context.Context, comm packer.Communicator) error {
			if hook == nil {
				return nil
			}
			return hook.Run(
				ctx,
				packer.HookProvision,
				ui,
				comm,
				map[string]any{"output_directory": b.config.Output},
			)
		},
		uiWriter{ui},
	)
	if err != nil {
		return nil, fmt.Errorf("prepare macOS: %w", err)
	}
	c := prepared.Config
	result := nativebuild.Result{
		SchemaVersion: 2,
		Qualification: "unverified",
		OSVersion:     prepared.Observation.Version,
		FirstBoot:     "desktop",
		Config: nativebuild.Config{
			Family:          "macos",
			Arch:            "arm64",
			Release:         strings.Split(c.Guest.OSVersion, ".")[0],
			SourcePath:      b.config.Layout,
			SourceSHA256:    strings.TrimPrefix(b.config.Digest, "sha256:"),
			SourceBuild:     c.Guest.OSBuild,
			OutputDirectory: b.config.Output,
			Timeout:         b.config.Timeout,
		},
		Firmware: nativebuild.FirmwarePolicy{
			Type:          "apple",
			TPM:           "none",
			CloneIdentity: "new-machine-identifier; copy auxiliary storage",
		},
		Files: []string{"disk0.img", "auxstorage.bin"},
		Mac: &nativebuild.MacRestoreResult{
			HardwareModel: c.Firmware.HardwareModel,
			CPUCountMin:   c.Resources.CPU.Min,
			CPUCount:      c.Resources.CPU.Default,
			MemorySizeMin: c.Resources.Memory.Min,
			MemorySize:    c.Resources.Memory.Default,
		},
		Prepared: &nativebuild.PreparedResult{
			Parent:         prepared.Parent,
			User:           "weave",
			AutomaticLogin: true,
			RemoteLogin:    true,
		},
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode prepared result: %w", err)
	}
	if err = os.WriteFile(
		filepath.Join(b.config.Output, "build-result.json"),
		append(data, '\n'),
		0o600,
	); err != nil {
		return nil, fmt.Errorf("write prepared result: %w", err)
	}
	return &artifact{result: result}, nil
}

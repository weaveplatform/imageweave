// Package packerplugin implements Packer's native base-image builder contract.
package packerplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"github.com/weaveplatform/imageweave/pkg/nativebuild"
)

type configuration struct {
	common.PackerConfig `mapstructure:",squash"`
	nativebuild.Config  `mapstructure:",squash"`
}

// Builder creates a sealed base; it deliberately has no guest provisioning communicator.
type Builder struct {
	config configuration
	build  func(context.Context, nativebuild.Config, io.Writer) (nativebuild.Result, error)
}

func (*Builder) ConfigSpec() hcldec.ObjectSpec {
	spec := hcldec.ObjectSpec{}
	for _, name := range []string{"family", "release", "arch", "source_path", "source_sha256", "source_build", "output_directory", "timeout", "edition", "language"} {
		spec[name] = &hcldec.AttrSpec{
			Name:     name,
			Type:     cty.String,
			Required: name != "edition" && name != "language",
		}
	}
	return spec
}

func (b *Builder) Prepare(raws ...any) ([]string, []string, error) {
	b.config = configuration{}
	// The SDK decoder's cty path expects generated flattened config. This small
	// all-string schema normalizes HCL values before using the SDK's strict decoder.
	normalized := make([]any, len(raws))
	for i, raw := range raws {
		if value, ok := raw.(cty.Value); ok {
			encoded, err := (ctyjson.SimpleJSONValue{Value: value}).MarshalJSON()
			if err != nil {
				return nil, nil, fmt.Errorf("encode HCL configuration: %w", err)
			}
			if err := json.Unmarshal(encoded, &raw); err != nil {
				return nil, nil, fmt.Errorf("decode HCL configuration: %w", err)
			}
		}
		normalized[i] = raw
	}
	if err := config.Decode(
		&b.config,
		&config.DecodeOpts{Interpolate: false},
		normalized...); err != nil {
		return nil, nil, fmt.Errorf("decode Packer configuration: %w", err)
	}
	if err := b.config.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate Packer configuration: %w", err)
	}
	return nil, nil, nil
}

func (b *Builder) Run(
	ctx context.Context,
	ui packer.Ui,
	hook packer.Hook,
) (packer.Artifact, error) {
	run := b.build
	if run == nil {
		run = nativebuild.Build
	}
	result, err := run(ctx, b.config.Config, uiWriter{ui})
	if err != nil {
		return nil, err
	}
	if hook != nil {
		if err := hook.Run(
			ctx,
			packer.HookProvision,
			ui,
			sealedCommunicator{},
			map[string]any{"output_directory": result.Config.OutputDirectory},
		); err != nil {
			return nil, fmt.Errorf("post-build provisioning: %w", err)
		}
	}
	return &artifact{result: result}, nil
}

type uiWriter struct{ ui packer.Ui }

func (w uiWriter) Write(p []byte) (int, error) {
	w.ui.Say(strings.TrimSuffix(string(p), "\n"))
	return len(p), nil
}

type artifact struct{ result nativebuild.Result }

func (a *artifact) BuilderId() string { return "weaveplatform.imageweave.native" }
func (a *artifact) Id() string        { return a.result.Config.OutputDirectory }
func (a *artifact) String() string {
	return "Unverified " + a.result.Config.Family + " base: " + a.Id()
}

func (a *artifact) Files() []string {
	names := append([]string{"build-result.json"}, a.result.Files...)
	for i, name := range names {
		names[i] = filepath.Join(a.Id(), name)
	}
	return names
}

func (a *artifact) State(name string) any {
	if name == "generated_data" {
		return map[string]any{
			"qualification": "unverified",
			"source_sha256": a.result.Config.SourceSHA256,
		}
	}
	return nil
}

func (a *artifact) Destroy() error {
	// Remove only declared artifacts, never recursively delete the caller's workspace.
	for _, name := range a.Files() {
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove artifact: %w", err)
		}
	}
	return nil
}

// The base is already sealed. Run Packer hooks so host-side provisioners work,
// but reject guest operations explicitly instead of silently skipping them.
var errSealed = errors.New(
	"native base is sealed; guest provisioners are unsupported; use a derived-image builder",
)

type sealedCommunicator struct{}

func (sealedCommunicator) Start(context.Context, *packer.RemoteCmd) error { return errSealed }
func (sealedCommunicator) Upload(string, io.Reader, *os.FileInfo) error   { return errSealed }
func (sealedCommunicator) UploadDir(string, string, []string) error       { return errSealed }
func (sealedCommunicator) Download(string, io.Writer) error               { return errSealed }
func (sealedCommunicator) DownloadDir(string, string, []string) error     { return errSealed }

package packerplugin

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/zclconf/go-cty/cty"

	"github.com/weaveplatform/imageweave/pkg/nativebuild"
)

func values(t *testing.T) map[string]any {
	return map[string]any{
		"family":           "macos",
		"release":          "26",
		"arch":             "arm64",
		"source_path":      filepath.Join(t.TempDir(), "source.ipsw"),
		"source_sha256":    strings.Repeat("a", 64),
		"source_build":     "25G83",
		"output_directory": filepath.Join(t.TempDir(), "candidate"),
		"timeout":          "1m",
	}
}

func TestPrepareAndArtifact(t *testing.T) {
	b := &Builder{}
	if len(b.ConfigSpec()) != 10 {
		t.Fatal(b.ConfigSpec())
	}
	v := values(t)
	if _, _, err := b.Prepare(v); err != nil {
		t.Fatal(err)
	}
	// Exercise Packer's actual HCL/cty transport as well as map input.
	hcl := map[string]cty.Value{}
	for k, value := range v {
		hcl[k] = cty.StringVal(value.(string))
	}
	if _, _, err := b.Prepare(cty.ObjectVal(hcl)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{map[string]any{"unknown": true}, map[string]any{"family": []int{1}}, cty.ObjectVal(map[string]cty.Value{"family": cty.UnknownVal(cty.String)}), cty.StringVal("wrong")} {
		if _, _, err := b.Prepare(bad); err == nil {
			t.Fatal("accepted malformed Packer config")
		}
	}
	if _, _, err := b.Prepare(v); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	ui := &packer.BasicUi{Reader: strings.NewReader(""), Writer: &output, ErrorWriter: &output}
	b.build = func(_ context.Context, c nativebuild.Config, w io.Writer) (nativebuild.Result, error) {
		if _, err := w.Write([]byte("native progress\n")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(c.OutputDirectory, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"disk0.img", "build-result.json", "keep.log"} {
			if err := os.WriteFile(
				filepath.Join(c.OutputDirectory, name),
				[]byte("test"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
		}
		return nativebuild.Result{Config: c, Files: []string{"disk0.img"}}, nil
	}
	a, err := b.Run(t.Context(), ui, &packer.DispatchHook{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "native progress") ||
		a.BuilderId() != "weaveplatform.imageweave.native" ||
		a.Id() != v["output_directory"] ||
		len(a.Files()) != 2 ||
		!strings.Contains(a.String(), "Unverified") {
		t.Fatal(a)
	}
	if a.State("unknown") != nil ||
		a.State("generated_data").(map[string]any)["qualification"] != "unverified" {
		t.Fatal("false qualification")
	}
	if err := a.Destroy(); err != nil {
		t.Fatal(err)
	}
	if err := a.Destroy(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Id(), "keep.log")); err != nil {
		t.Fatal("destroy removed diagnostics")
	}
	if err := os.Mkdir(filepath.Join(a.Id(), "disk0.img"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Id(), "disk0.img", "child"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if a.Destroy() == nil {
		t.Fatal("ignored cleanup error")
	}
	b.build = func(context.Context, nativebuild.Config, io.Writer) (nativebuild.Result, error) {
		return nativebuild.Result{}, context.Canceled
	}
	if a, err := b.Run(t.Context(), ui, nil); a != nil || err == nil {
		t.Fatal("false success")
	}
	b.build = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if a, err := b.Run(ctx, ui, nil); a != nil || err == nil {
		t.Fatal("cancelled default builder")
	}
}

type failHook struct{}

func (failHook) Run(
	_ context.Context,
	name string,
	_ packer.Ui,
	comm packer.Communicator,
	_ any,
) error {
	if name != packer.HookProvision {
		panic("wrong hook")
	}
	return comm.Upload("guest", nil, nil)
}

func TestSealedBaseRejectsGuestProvisioning(t *testing.T) {
	b := &Builder{
		build: func(context.Context, nativebuild.Config, io.Writer) (nativebuild.Result, error) {
			return nativebuild.Result{}, nil
		},
	}
	if _, err := b.Run(t.Context(), nil, failHook{}); err == nil {
		t.Fatal("guest provisioner silently skipped")
	}
	comm := sealedCommunicator{}
	for _, err := range []error{comm.Start(t.Context(), nil), comm.Upload("", nil, nil), comm.UploadDir("", "", nil), comm.Download("", nil), comm.DownloadDir("", "", nil)} {
		if err != errSealed {
			t.Fatal(err)
		}
	}
}

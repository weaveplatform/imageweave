package delivery

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Construction contains a deeply checked OCI artifact, without runtime evidence.
// It deliberately has no acceptance field: native restore/install completion is
// not a successful first boot, independent identity or reboot observation.
type Construction struct {
	Layout        string `json:"layout"`
	Bundle        string `json:"bundle"`
	Manifest      string `json:"manifest"`
	RecipeCommit  string `json:"recipeCommit"`
	Qualification string `json:"qualification"`
}

// BuildNative runs the installed native Packer plugin and imports only its
// manifest-listed outputs. The caller authenticates source media and installs
// the plugin from the reviewed recipe commit before invoking this operation.
// No registry credentials, publication or Linux acceptance adapter are used.
func BuildNative(ctx context.Context, o Options, run Runner, log io.Writer) (Construction, error) {
	var result Construction
	if err := validateCommon(o, run); err != nil {
		return result, err
	}
	if o.Request.Family != "windows-11" && o.Request.Family != "macos" {
		return result, fmt.Errorf("%w: native construction requires Windows 11 or macOS", ErrInput)
	}
	resolved, vars, err := prepare(ctx, o, log)
	if err != nil {
		return result, err
	}
	// The native plugin creates only its candidate directory. Packer's QEMU
	// builder creates its own parents, so Linux tests did not expose this boundary.
	if err := os.Mkdir(filepath.Join(o.Out, "packer"), 0o700); err != nil {
		return result, fmt.Errorf("create native Packer workspace: %w", err)
	}
	manifest := filepath.Join(o.Out, "packer", "candidate", "packer-manifest.json")
	bundle, layout := filepath.Join(o.Out, "bundle"), filepath.Join(o.Out, "layout")
	commands := []command{
		{o.Packer, []string{"init", resolved.Template}},
		{o.Packer, []string{"validate", "-var-file=" + vars, resolved.Template}},
		{o.Packer, []string{"build", "-color=false", "-var-file=" + vars, resolved.Template}},
		{
			o.OCI,
			[]string{
				"bundle",
				"import-imageweave",
				manifest,
				"--recipe-commit",
				o.RecipeCommit,
				"--source-uri",
				o.SourceURI,
				"--version",
				o.Version,
				"--out",
				bundle,
			},
		},
		{o.OCI, []string{"pack", bundle, "--out", layout, "--tag", o.Version}},
		{o.OCI, []string{"inspect", layout, "--ref", o.Version, "--deep", "--strict", "--json"}},
	}
	if err := execute(ctx, commands, run, log); err != nil {
		return result, err
	}
	return Construction{
		Layout: layout, Bundle: bundle, Manifest: manifest,
		RecipeCommit: o.RecipeCommit, Qualification: "unverified",
	}, nil
}

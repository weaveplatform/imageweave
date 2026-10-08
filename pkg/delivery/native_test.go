package delivery_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/delivery"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

// A native construction template is not a runtime observation adapter. None
// of the native release/architecture rows may publish Linux qualification.
func TestNativeMatrixCannotUseLinuxQualification(t *testing.T) {
	for _, selection := range catalog.Current().Matrix() {
		if selection.Family != "macos" && selection.Family != "windows-11" {
			continue
		}
		t.Run(selection.Family+"/"+selection.Version+"/"+selection.Arch, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "candidate")
			options := delivery.Options{
				Request:      plan.Request{Family: selection.Family, Release: selection.Version, Arch: selection.Arch},
				RecipeCommit: strings.Repeat("a", 40), SourceURI: "https://vendor.example/media",
				Version: "candidate-r1", Out: out, Packer: "packer", OCI: "weaveoci", Imageweave: "imageweave",
			}
			run := func(context.Context, string, []string, io.Writer) error {
				t.Fatal("native row reached Linux build/acceptance runner")
				return nil
			}
			result, err := delivery.Build(t.Context(), options, run, io.Discard)
			if !errors.Is(err, delivery.ErrInput) || result.Qualification != "" {
				t.Fatalf("unqualified native row accepted: %+v %v", result, err)
			}
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected candidate output: %v", err)
			}
		})
	}
}

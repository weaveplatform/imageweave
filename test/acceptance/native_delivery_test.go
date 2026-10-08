package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/delivery"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

// This opt-in exercises the real native plugin and OCI executable. Its result
// proves construction and artifact integrity, never guest runtime qualification.
func TestNativeDelivery(t *testing.T) {
	request := os.Getenv("IMAGEWEAVE_NATIVE_REQUEST")
	if request == "" {
		t.Skip(
			"set IMAGEWEAVE_NATIVE_REQUEST, IMAGEWEAVE_NATIVE_SOURCE_URI and IMAGEWEAVE_NATIVE_OUT on a matching host",
		)
	}
	file, err := os.Open(request)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, err := plan.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if r.Arch != runtime.GOARCH || (r.Family == "macos" && runtime.GOOS != "darwin") ||
		(r.Family == "windows-11" && runtime.GOOS != "windows") {
		t.Fatal("native delivery requires a matching host")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	commit, err := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	packer, oci := os.Getenv("PACKER"), os.Getenv("WEAVEOCI")
	if packer == "" {
		packer = "packer"
	}
	if oci == "" {
		oci = "weaveoci"
	}
	result, err := delivery.BuildNative(t.Context(), delivery.Options{
		Request: r, RecipeCommit: strings.TrimSpace(string(commit)),
		SourceURI: os.Getenv("IMAGEWEAVE_NATIVE_SOURCE_URI"), Version: "native-construction-test",
		Out: os.Getenv("IMAGEWEAVE_NATIVE_OUT"), Packer: packer, OCI: oci,
	}, delivery.Exec, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if result.Qualification != "unverified" {
		t.Fatal("construction granted qualification", result)
	}
	for _, path := range []string{result.Manifest, filepath.Join(result.Bundle, "imageweave-import.json"), filepath.Join(result.Layout, "index.json")} {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("missing construction evidence %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(result.Layout, "acceptance.json")); !os.IsNotExist(err) {
		t.Fatal("construction must not fabricate runtime acceptance", err)
	}
}

package acceptance_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weaveplatform/imageweave/pkg/nativebuild"
)

// Explicit media/workspace opt-in prevents CI from silently consuming licensed
// media or claiming native VM execution on a runner without virtualization.
func TestPackerNativeConstruction(t *testing.T) {
	request := os.Getenv("IMAGEWEAVE_NATIVE_VARS")
	if request == "" {
		t.Skip("set IMAGEWEAVE_NATIVE_VARS to pinned Packer vars on a matching native host")
	}
	b, err := os.ReadFile(request)
	if err != nil {
		t.Fatal(err)
	}
	var variables map[string]string
	if err := json.Unmarshal(b, &variables); err != nil {
		t.Fatal(err)
	}
	family := os.Getenv("IMAGEWEAVE_NATIVE_FAMILY")
	template := "windows"
	if family == "macos" {
		template = "macos"
		if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
			t.Fatal("macOS restore requires Apple silicon")
		}
	} else if family != "windows-11" || runtime.GOOS != "windows" || runtime.GOARCH != variables["arch"] {
		t.Fatal("Windows acceptance requires matching Windows architecture")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	request, err = filepath.Abs(request)
	if err != nil {
		t.Fatal(err)
	}
	packer := os.Getenv("PACKER")
	if packer == "" {
		packer = "packer"
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Hour)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		packer,
		"build",
		"-color=false",
		"-var-file="+request,
		"templates/"+template,
	)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal("native Packer build", err)
	}
	out := variables["output_directory"]
	b, err = os.ReadFile(filepath.Join(out, "build-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result nativebuild.Result
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Qualification != "unverified" ||
		result.Config.SourceSHA256 != variables["source_sha256"] ||
		result.Config.SourceBuild != variables["source_build"] ||
		result.OSVersion == "" {
		t.Fatalf("invalid construction result %+v", result)
	}
	for _, name := range result.Files {
		if filepath.Base(name) != name || strings.Contains(name, "vmgs") ||
			strings.Contains(name, "vhdx") {
			t.Fatal("unsafe export", name)
		}
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Fatal("empty output", name)
		}
	}
	info, err := os.Stat(filepath.Join(out, "disk0.img"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 64<<30 || info.Size()%512 != 0 {
		t.Fatal("invalid raw system disk size")
	}
	if family == "macos" {
		if result.Mac == nil || result.Mac.HardwareModel == "" || len(result.Files) != 2 ||
			!strings.HasPrefix(result.OSVersion, variables["release"]+".") {
			t.Fatal("missing Apple restore state")
		}
	} else {
		if result.Windows == nil || !result.Windows.Generalized ||
			result.Windows.Arch != variables["arch"] ||
			result.Windows.Build != variables["source_build"] {
			t.Fatal("Windows did not confirm requested generalized image")
		}
	}
	// This proves construction, not independent-clone runtime qualification.
}

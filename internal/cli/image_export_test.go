package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/internal/testbundle"
	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

func TestWindowsAgentCLIRequiresCompleteInputs(t *testing.T) {
	for _, args := range [][]string{
		{"image", "build-windows-agent"},
		{"image", "build-windows-agent", "unexpected"},
		{"image", "build-windows-agent", "--base", "layout", "--base-name", "windows-base", "--arch", "arm64", "--cache", "cache", "--out", "out", "--timeout", "0"},
		{"image", "build-windows-agent", "--base", "layout", "--base-name", "windows-base", "--arch", "arm64", "--cache", "cache", "--out", "out", "--catalogue", "absent"},
	} {
		if code, _, stderr := runImage(t, args...); code == 0 {
			t.Fatal("invalid Windows builder invocation accepted", args, stderr)
		}
	}
	if code, stdout, stderr := runImage(
		t,
		"image",
		"build-windows-agent",
		"--help",
	); code != 0 ||
		!strings.Contains(stdout, "--base-name") {
		t.Fatal(stdout, stderr)
	}
}

func TestImageExportCLI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	if err := testbundle.Write(
		dir,
		testbundle.Options{OS: spec.OSWindows, Arch: spec.ArchAMD64},
	); err != nil {
		t.Fatal(err)
	}
	b, err := pack.LoadBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	layout := filepath.Join(t.TempDir(), "layout")
	store, err := pack.OpenLayout(t.Context(), layout)
	if err != nil {
		t.Fatal(err)
	}
	child, err := pack.Manifest(t.Context(), b, store, chunk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := pack.Index(t.Context(), store, []ocispec.Descriptor{child}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Tag(t.Context(), root, "r1")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "export")
	args := []string{
		"image",
		"export",
		layout,
		"--out",
		out,
		"--platform",
		"windows/amd64",
		"--expected-digest",
		root.Digest.String(),
		"--target",
		"aws",
	}
	code, stdout, stderr := runImage(t, args...)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	var result imagebuild.ExportResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Acceptance != "pending" || result.IndexDigest != root.Digest.String() ||
		len(result.Files) != 1 {
		t.Fatal(stdout)
	}
	if code, _, _ := runImage(t, args...); code == 0 {
		t.Fatal("reused export destination")
	}
	if code, _, stderr := runImage(
		t,
		"image",
		"export",
		layout,
	); code != 2 ||
		!strings.Contains(stderr, "--target") {
		t.Fatal(code, stderr)
	}
}

func TestPrepareAgentUsageAndLockFailures(t *testing.T) {
	if code, _, stderr := runImage(t, "image", "prepare-agent"); code != 2 {
		t.Fatal(code, stderr)
	}
	args := []string{
		"image",
		"prepare-agent",
		"--platform",
		"linux/arm64",
		"--cache",
		t.TempDir(),
		"--out",
		filepath.Join(t.TempDir(), "payload"),
	}
	if code, _, _ := runImage(t, args...); code == 0 {
		t.Fatal("accepted missing default catalogue")
	}
	args = append(
		args,
		"--catalogue",
		"../../images/catalogue.json",
		"--lock",
		"../../images/packages.lock.json",
	)
	if code, _, stderr := runImage(
		t,
		args...); code == 0 ||
		!strings.Contains(stderr, "installer missing") {
		t.Fatal(code, stderr)
	}
}

package linux

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

func TestBootFilesystemFailures(t *testing.T) {
	fakeFirmware(t)
	for _, mode := range []string{"disk-escape", "workspace", "firmware-copy", "result-write", "log-write", "serial-write"} {
		t.Run(mode, func(t *testing.T) {
			bundle := bootBundle(t, "arm64")
			out := filepath.Join(t.TempDir(), "report")
			if mode == "disk-escape" {
				disk := filepath.Join(bundle, "disk0.img")
				must(t, os.Remove(disk))
				external := filepath.Join(t.TempDir(), "original")
				must(t, os.WriteFile(external, []byte("keep"), 0o600))
				must(t, os.Symlink(external, disk))
			}
			if mode == "workspace" {
				missing := filepath.Join(t.TempDir(), "missing")
				for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
					t.Setenv(name, missing)
				}
			}
			fake := &fakeQEMU{t: t}
			tools := Tools{
				Run: func(ctx context.Context, w, e io.Writer, name string, args ...string) error {
					if name == "qemu-img" && args[0] == "resize" && mode == "firmware-copy" {
						must(t, os.Mkdir(filepath.Join(filepath.Dir(args[2]), "vars.fd"), 0o700))
					}
					if strings.HasPrefix(name, "qemu-system") && mode == "result-write" {
						must(t, os.Mkdir(filepath.Join(out, "result.json"), 0o700))
					}
					if name == "qemu-img" && args[0] == "resize" && mode == "log-write" {
						must(t, os.Mkdir(filepath.Join(out, "qemu.log"), 0o700))
					}
					if name == "qemu-img" && args[0] == "resize" && mode == "serial-write" {
						must(t, os.Mkdir(filepath.Join(out, "serial.log"), 0o700))
					}
					return fake.run(ctx, w, e, name, args...)
				},
			}
			if _, err := tools.BootLinux(
				t.Context(),
				BootOptions{Bundle: bundle, Report: out, Timeout: time.Second},
			); err == nil {
				t.Fatal("ignored " + mode)
			}
		})
	}
	work := t.TempDir()
	must(t, os.Mkdir(filepath.Join(work, "seed"), 0o700))
	if err := (Tools{}).seed(t.Context(), work, "", "", ""); err == nil {
		t.Fatal("reused seed")
	}
	payload := t.TempDir()
	must(t, os.Symlink("missing", filepath.Join(payload, "link")))
	if err := common.CopyTree(payload, filepath.Join(t.TempDir(), "copy")); err == nil {
		t.Fatal("copied payload symlink")
	}
}

func TestLinuxValidationFailureReports(t *testing.T) {
	fakeFirmware(t)
	for _, mode := range []string{"input", "existing", "cancelled", "boot", "acceptance-write"} {
		t.Run(mode, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "candidate")
			o := ValidateOptions{
				Bundles: []string{bootBundle(t, "arm64")},
				Arches:  []string{"arm64"},
				Out:     out,
				Timeout: time.Second,
			}
			ctx := t.Context()
			switch mode {
			case "input":
				o.Bundles = nil
			case "existing":
				must(t, os.Mkdir(out, 0o700))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			fake := &fakeQEMU{t: t, missingMarker: mode == "boot"}
			tools := Tools{
				Run: func(ctx context.Context, w, e io.Writer, name string, args ...string) error {
					if mode == "acceptance-write" && strings.HasPrefix(name, "qemu-system") &&
						fake.boots == 0 {
						must(t, os.Mkdir(filepath.Join(out, "acceptance.json"), 0o700))
					}
					return fake.run(ctx, w, e, name, args...)
				},
			}
			result, err := tools.ValidateLinux(ctx, o)
			if err == nil {
				t.Fatal("accepted " + mode)
			}
			if mode == "boot" {
				if result.Passed {
					t.Fatal("failed boot passed")
				}
				var saved Acceptance
				must(t, common.ReadJSON(filepath.Join(out, "acceptance.json"), &saved))
				if saved.Passed || len(saved.Platforms["linux/arm64"]) != 1 {
					t.Fatal(saved)
				}
			}
		})
	}
}

func TestSystemDiskRequiresExistingBundleRoot(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "disk.img"), []byte("original"), 0o600))
	b := pack.Bundle{
		Dir:  filepath.Join(root, "missing"),
		File: pack.BundleFile{Disks: []pack.BundleDisk{{Role: "system", Path: "../disk.img"}}},
	}
	if _, err := common.SystemDisk(b); err == nil {
		t.Fatal("nonexistent bundle root accepted")
	}
}

func TestCloneUnpackRefusals(t *testing.T) {
	store, err := pack.OpenLayout(t.Context(), t.TempDir())
	must(t, err)
	if _, err := (Tools{}).validateClones(
		t.Context(),
		store,
		ocispec.Descriptor{},
		"arm64",
		ValidateOptions{Out: "missing"},
	); err == nil {
		t.Fatal("missing unpack output")
	}
	if _, err := (Tools{}).validateClones(
		t.Context(),
		store,
		ocispec.Descriptor{},
		"arm64",
		ValidateOptions{Out: t.TempDir()},
	); err == nil {
		t.Fatal("missing manifest")
	}
}

func TestSeedRefusesFileParent(t *testing.T) {
	src := filepath.Join(t.TempDir(), "file")
	must(t, os.WriteFile(src, []byte("file"), 0o600))
	if err := (Tools{Run: func(context.Context, io.Writer, io.Writer, string, ...string) error { return nil }}).seed(
		t.Context(),
		src,
		"",
		"",
		"",
	); err == nil {
		t.Fatal("seed under file")
	}
}

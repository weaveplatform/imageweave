package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/weaveplatform/imageweave/internal/testbundle"
	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

func TestMacOSAcceptanceCommand(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(testbundle.Write(bundle, testbundle.Options{OS: "darwin", Arch: "arm64"}))
	b, err := pack.LoadBundle(bundle)
	check(err)
	b.File.Guest.Variant = "base"
	check(pack.WriteBundleFile(bundle, b.File))
	b, err = pack.LoadBundle(bundle)
	check(err)
	layout := filepath.Join(dir, "layout")
	store, err := pack.OpenLayout(t.Context(), layout)
	check(err)
	child, err := pack.Manifest(t.Context(), b, store, chunk.Options{})
	check(err)
	root, err := pack.Index(t.Context(), store, []ocispec.Descriptor{child}, nil)
	check(err)
	check(store.Tag(t.Context(), root, "r1"))
	for _, mode := range []string{"success", "missing", "index", "ref", "boot", "emit"} {
		t.Run(mode, func(t *testing.T) {
			called := 0
			emit := false
			cmd := newImageMacOS(
				func(_ context.Context, c imagecheck.Clone) (imagecheck.Boot, error) {
					called++
					if mode == "boot" {
						return imagecheck.Boot{}, fmt.Errorf("%w: test boot failure", errUsage)
					}
					id := fmt.Sprint(c.Number)
					return imagecheck.Boot{
						Platform:       "darwin/arm64",
						OSVersion:      c.Config.Guest.OSVersion,
						OSBuild:        c.Config.Guest.OSBuild,
						Passed:         true,
						Marker:         c.Marker,
						MachineID:      id,
						Profile:        "base",
						ElapsedSeconds: 1,
						Identities: map[string]string{
							"hardwareUUID":      id,
							"machineIdentifier": id,
							"macAddress":        id,
						},
						Checks: map[string]bool{
							"boot":                 true,
							"shutdown":             true,
							"os-version":           true,
							"reboot-identity":      true,
							"fresh-firmware":       true,
							"no-build-credentials": true,
							"agent-absent":         true,
						},
					}, nil
				},
				func(any) error {
					emit = true
					if mode == "emit" {
						return errUsage
					}
					return nil
				},
				io.Discard,
			)
			path := layout
			ref := "r1"
			if mode == "missing" {
				path = filepath.Join(dir, "missing")
			}
			if mode == "index" {
				path = t.TempDir()
				check(os.WriteFile(filepath.Join(path, "index.json"), []byte("bad"), 0o600))
			}
			if mode == "ref" {
				ref = "absent"
			}
			cmd.SetArgs(
				[]string{path, "--ref", ref, "--out", filepath.Join(t.TempDir(), "evidence")},
			)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.ExecuteContext(t.Context())
			if mode == "success" {
				check(err)
				if !emit || called != 2 {
					t.Fatal("missing clone evidence")
				}
			} else if err == nil {
				t.Fatal("failure accepted", mode)
			}
		})
	}
	if _, err := macOSBoot(io.Discard)(t.Context(), imagecheck.Clone{}); err == nil {
		t.Fatal("invalid native clone accepted")
	}
}

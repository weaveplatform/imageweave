package windows

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/disk/vhd"
)

func TestWindowsOutputFailures(t *testing.T) {
	_, source := windowsFixture()
	marker := "WEAVE-IMAGE-READY-0123456789abcdef01234567"
	for _, mode := range []string{"existing-seed", "invalid-recipe", "iso-output"} {
		out := t.TempDir()
		s := source
		switch mode {
		case "existing-seed":
			must(t, os.Mkdir(filepath.Join(out, "seed"), 0o700))
		case "invalid-recipe":
			s.Edition = "unknown"
		case "iso-output":
			must(t, os.Mkdir(filepath.Join(out, "seed.iso"), 0o700))
		}
		if _, err := windowsSeed(out, s, marker); err == nil {
			t.Fatal("accepted " + mode)
		}
	}
	for _, mode := range []string{"git", "bundle-write"} {
		out := t.TempDir()
		disk := filepath.Join(out, "disk.vhd")
		must(t, os.WriteFile(disk, make([]byte, 1024), 0o600))
		must(t, vhd.Append(disk, time.Now()))
		tools := Tools{Run: func(_ context.Context, w, _ io.Writer, _ string, _ ...string) error {
			if mode == "git" {
				return common.ErrInput
			}
			must(t, os.Mkdir(filepath.Join(out, "bundle", "bundle.json"), 0o700))
			_, err := fmt.Fprint(w, "abcdef")
			return err
		}}
		if _, err := tools.windowsBundle(
			t.Context(),
			WindowsOptions{Out: out},
			source,
			WindowsInstallResult{},
		); err == nil {
			t.Fatal(mode)
		}
	}
}

func TestSourceValidationFailures(t *testing.T) {
	selection, s := windowsFixture()
	if err := s.validate(WindowsSelection{}, false); err == nil {
		t.Fatal("invalid selection")
	}
	for _, mode := range []string{"kind", "url"} {
		bad := s
		if mode == "kind" {
			bad.Kind = "unknown"
		} else {
			bad.URI = "://invalid"
		}
		if err := bad.validate(selection, false); err == nil {
			t.Fatal(mode)
		}
	}
	s.SHA256 = ""
	cache := filepath.Join(t.TempDir(), "cache")
	must(t, os.WriteFile(cache, nil, 0o600))
	if _, _, err := (Packages{}).AcquireWindows(t.Context(), s, selection, cache); err == nil {
		t.Fatal("file cache")
	}
	if windowsProgress(io.Discard) != io.Discard {
		t.Fatal("lost progress writer")
	}
}

func TestWindowsMediaRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	must(t, err)
	if _, err := hashSizedFile(dir, info.Size()); err == nil {
		t.Fatal("directory hashed as Windows media")
	}
}

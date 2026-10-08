package buildstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"howett.net/plist"
)

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, e := plist.Marshal(v, plist.XMLFormat)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestDiscovery(t *testing.T) {
	for _, mode := range []string{"ok", "list failed", "list malformed", "images failed", "images malformed", "metadata failed", "metadata malformed", "readonly", "unidentified", "probe failed", "backing failed", "cyclic"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			host := filepath.Join(root, "host")
			external := filepath.Join(root, "external")
			nested := filepath.Join(external, "nested")
			for _, d := range []string{host, external, nested} {
				if e := os.MkdirAll(d, 0o700); e != nil {
					t.Fatal(e)
				}
			}
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "hdiutil" {
					if mode == "images failed" {
						return nil, errors.New("failed")
					}
					if mode == "images malformed" {
						return []byte("bad"), nil
					}
					path := filepath.Join(external, "workspace.sparsebundle")
					if mode == "cyclic" {
						path = filepath.Join(nested, "self")
					}
					return encode(
						t,
						map[string]any{
							"images": []any{
								map[string]any{
									"image-path": path,
									"system-entities": []any{
										map[string]any{"mount-point": nested},
										map[string]any{"dev-entry": "disk6"},
									},
								},
							},
						},
					), nil
				}
				if args[0] == "list" {
					if mode == "list failed" {
						return nil, errors.New("failed")
					}
					if mode == "list malformed" {
						return []byte("bad"), nil
					}
					return encode(
						t,
						map[string]any{
							"disks": []any{
								map[string]any{"MountPoint": host},
								map[string]any{"MountPoint": external},
								map[string]any{"MountPoint": "relative"},
								map[string]any{"MountPoint": false},
							},
						},
					), nil
				}
				path := args[2]
				if mode == "metadata failed" || mode == "backing failed" && path == external {
					return nil, errors.New("metadata unavailable")
				}
				if mode == "metadata malformed" {
					return []byte("bad"), nil
				}
				d := diskInfo{
					Mount:      host,
					Pool:       "host",
					Device:     "disk1",
					Filesystem: "apfs",
					Internal:   true,
					Writable:   true,
				}
				if strings.HasPrefix(path, external) {
					d.Mount = external
					d.Pool = ""
					d.Device = "disk4"
					d.Filesystem = "exfat"
					d.Internal = false
				}
				if strings.HasPrefix(path, nested) {
					d.Mount = nested
					d.Pool = "virtual"
					d.Device = "disk6"
					d.Filesystem = "apfs"
				}
				if mode == "readonly" {
					d.Writable = false
				}
				if mode == "unidentified" {
					d.Pool = ""
					d.Device = ""
				}
				return encode(t, d), nil
			}
			check := func(path string) (uint64, string, error) {
				if mode == "probe failed" {
					return 0, "", errors.New("permission denied")
				}
				if path == external {
					return 100 * GiB, path, nil
				}
				return 500 * GiB, path, nil
			}
			in, err := discover(
				t.Context(),
				host,
				filepath.Join(nested, "chosen"),
				host,
				run,
				check,
			)
			if strings.HasPrefix(mode, "list ") || strings.HasPrefix(mode, "images ") {
				if err == nil {
					t.Fatal("discovery failure hidden")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			last := in.Volumes[len(in.Volumes)-1]
			if mode == "ok" {
				if last.Free != 100*GiB || len(last.Pools) != 2 || in.Pools["disk4"] != 100*GiB {
					t.Fatalf("backing capacity counted twice: %+v", in)
				}
			} else if last.Reason == "" {
				t.Fatalf("rejection lost: %+v", last)
			}
		})
	}
}

func TestSystemBoundaries(t *testing.T) {
	d := t.TempDir()
	if ancestor(filepath.Join(d, "missing", "child")) != d {
		t.Fatal("ancestor")
	}
	if ancestor(string(filepath.Separator)) != string(filepath.Separator) {
		t.Fatal("root ancestor")
	}
	// The process boundary must propagate cancellation/launch failures.
	if _, e := execute(t.Context(), filepath.Join(d, "absent")); e == nil {
		t.Fatal("missing executable")
	}
	b, e := execute(t.Context(), "go", "version")
	if e != nil || len(b) == 0 {
		t.Fatal(e)
	}
	free, mount, e := measure(d)
	if runtime.GOOS == "darwin" {
		if e != nil || free == 0 || !filepath.IsAbs(mount) {
			t.Fatal(free, e)
		}
	} else if e == nil {
		t.Fatal("non-macOS discovery accepted")
	}
	if _, _, e = measure(filepath.Join(d, "absent")); e == nil {
		t.Fatal("missing directory")
	}
	f := filepath.Join(d, "file")
	if e = os.WriteFile(f, nil, 0o600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = measure(f); e == nil {
		t.Fatal("file accepted as workspace")
	}
}

func TestDiscoverSystemFailures(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LocalAppData", "")
	if _, e := Discover(t.Context(), t.TempDir(), "auto"); e == nil {
		t.Fatal("missing cache accepted")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := Discover(ctx, t.TempDir(), "auto"); e == nil {
		t.Fatal("cancelled discovery accepted")
	}
}

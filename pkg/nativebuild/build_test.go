package nativebuild

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"howett.net/plist"

	"github.com/weaveplatform/weaveplatform-oci/pkg/disk/vhd"
)

func fixture(t *testing.T, family string) Config {
	t.Helper()
	dir := t.TempDir()
	c := Config{
		Family:          family,
		Release:         "26",
		Arch:            "arm64",
		SourcePath:      filepath.Join(dir, "source.img"),
		SourceBuild:     "25G83",
		OutputDirectory: filepath.Join(dir, "candidate"),
		Timeout:         "1m",
	}
	if family == "macos" {
		writeIPSW(t, c.SourcePath, "26.6.2", "25G83", "valid")
	} else {
		c.Release, c.Arch, c.SourceBuild, c.Edition, c.Language = "26H2", "amd64", "27000.1", "pro", "en-US"
		must(t, os.WriteFile(c.SourcePath, []byte("installation media"), 0o600))
	}
	b, err := os.ReadFile(c.SourcePath)
	must(t, err)
	sum := sha256.Sum256(b)
	c.SourceSHA256 = hex.EncodeToString(sum[:])
	return c
}

func writeIPSW(t *testing.T, path, version, build, mode string) {
	t.Helper()
	f, err := os.Create(path)
	must(t, err)
	z := zip.NewWriter(f)
	name := "BuildManifest.plist"
	if mode == "missing" {
		name = "other"
	}
	w, err := z.Create(name)
	must(t, err)
	b, err := plist.Marshal(
		map[string]string{"ProductVersion": version, "ProductBuildVersion": build},
		plist.XMLFormat,
	)
	must(t, err)
	if mode == "invalid" {
		b = []byte("invalid")
	}
	if mode == "oversized" {
		b = bytes.Repeat([]byte("x"), (64<<20)+1)
	}
	_, err = w.Write(b)
	must(t, err)
	must(t, z.Close())
	must(t, f.Close())
}

func TestConfigRejectsInvalidInputs(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"family":   func(c *Config) { c.Family = "unknown" },
		"linux":    func(c *Config) { c.Family = "ubuntu"; c.Release = "26.04" },
		"intel":    func(c *Config) { c.Arch = "amd64" },
		"alias":    func(c *Config) { c.Release = "n-1" },
		"build":    func(c *Config) { c.SourceBuild = "" },
		"hash":     func(c *Config) { c.SourceSHA256 = "bad" },
		"source":   func(c *Config) { c.SourcePath = "relative" },
		"output":   func(c *Config) { c.OutputDirectory += "\n" },
		"timeout":  func(c *Config) { c.Timeout = "invalid" },
		"negative": func(c *Config) { c.Timeout = "-1s" },
	} {
		t.Run(name, func(t *testing.T) {
			c := fixture(t, "macos")
			change(&c)
			if c.Validate() == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	c := fixture(t, "windows-11")
	must(t, c.Validate())
	c.Edition = "PRO"
	if c.Validate() == nil {
		t.Fatal("uppercase edition")
	}
	c.Edition = "unknown"
	if c.Validate() == nil {
		t.Fatal("invalid edition")
	}
	c = fixture(t, "windows-11")
	c.Language = "bad"
	if c.Validate() == nil {
		t.Fatal("invalid locale")
	}
}

func TestNativeBuild(t *testing.T) {
	for _, family := range []string{"macos", "windows-11"} {
		for _, mode := range []string{"success", "invalid", "host", "hash", "existing", "install", "receipt", "receipt-build", "receipt-version", "receipt-release", "receipt-edition", "receipt-generalized", "export", "retarget", "result-write", "manifest", "seed", "copy"} {
			t.Run(family+"/"+mode, func(t *testing.T) {
				c := fixture(t, family)
				api := operations{
					host: func(Config) error {
						if mode == "host" {
							return ErrInput
						}
						return nil
					},
					retarget: func(path, image string) error {
						if path == c.SourcePath ||
							image != "efi/microsoft/boot/efisys_noprompt.bin" {
							t.Fatal("unsafe installer patch")
						}
						if mode == "retarget" {
							return ErrInput
						}
						if mode == "seed" {
							must(t, os.Mkdir(filepath.Join(c.OutputDirectory, "seed"), 0o700))
						}
						return nil
					},
					mac: func(_ context.Context, r MacRestoreRequest) (MacRestoreResult, error) {
						if r.DiskSize != 80<<30 {
							t.Fatal(r)
						}
						if mode == "install" {
							return MacRestoreResult{}, ErrInput
						}
						must(
							t,
							os.WriteFile(
								filepath.Join(r.Directory, "disk0.img"),
								[]byte("disk"),
								0o600,
							),
						)
						must(
							t,
							os.WriteFile(
								filepath.Join(r.Directory, "auxstorage.bin"),
								[]byte("aux"),
								0o600,
							),
						)
						if mode == "result-write" {
							must(
								t,
								os.Mkdir(filepath.Join(r.Directory, "build-result.json"), 0o700),
							)
						}
						return MacRestoreResult{HardwareModel: "model"}, nil
					},
					windows: func(_ context.Context, r WindowsInstallRequest) (WindowsInstallResult, error) {
						if !strings.HasPrefix(r.Marker, "WEAVE-IMAGE-READY-") ||
							r.ISO == c.SourcePath {
							t.Fatal(r)
						}
						if mode == "install" {
							return WindowsInstallResult{}, ErrInput
						}
						receipt := WindowsInstallResult{
							OSVersion:   "10.0." + c.SourceBuild,
							Build:       c.SourceBuild,
							Edition:     "Professional",
							Release:     c.Release,
							Arch:        c.Arch,
							Generalized: true,
						}
						switch mode {
						case "receipt":
							receipt.Arch = "arm64"
						case "receipt-build":
							receipt.Build = "wrong"
						case "receipt-version":
							receipt.OSVersion = "wrong"
						case "receipt-release":
							receipt.Release = "wrong"
						case "receipt-edition":
							receipt.Edition = "wrong"
						case "receipt-generalized":
							receipt.Generalized = false
						}
						if mode != "export" {
							disk := filepath.Join(r.Directory, "disk.vhd")
							must(t, os.WriteFile(disk, make([]byte, 1024), 0o600))
							must(t, vhd.Append(disk, time.Now()))
						}
						if mode == "result-write" {
							must(
								t,
								os.Mkdir(filepath.Join(r.Directory, "build-result.json"), 0o700),
							)
						}
						return receipt, nil
					},
				}
				if mode == "invalid" {
					c.Timeout = "bad"
				}
				if mode == "hash" {
					c.SourceSHA256 = strings.Repeat("0", 64)
				}
				if mode == "existing" {
					must(t, os.Mkdir(c.OutputDirectory, 0o700))
				}
				if mode == "manifest" && family == "macos" {
					writeIPSW(t, c.SourcePath, "15", "24A1", "valid")
					b, _ := os.ReadFile(c.SourcePath)
					sum := sha256.Sum256(b)
					c.SourceSHA256 = hex.EncodeToString(sum[:])
				}
				if mode == "copy" && family == "windows-11" {
					// Host validation is before the checksum; retire input only when the output is created is covered in direct IO tests.
					c.SourcePath = filepath.Join(t.TempDir(), "missing")
				}
				result, err := build(t.Context(), c, io.Discard, api)
				wantFailure := mode == "invalid" || mode == "host" || mode == "hash" ||
					mode == "existing" ||
					mode == "install" ||
					mode == "result-write" ||
					(family == "windows-11" && (strings.HasPrefix(mode, "receipt") || mode == "export" || mode == "retarget" || mode == "seed" || mode == "copy")) ||
					(family == "macos" && mode == "manifest")
				if (err != nil) != wantFailure {
					t.Fatalf("result=%+v error=%v", result, err)
				}
				if !wantFailure {
					if result.Qualification != "unverified" || result.OSVersion == "" ||
						result.FirstBoot == "" ||
						result.Firmware.CloneIdentity == "" {
						t.Fatal(result)
					}
					raw, err := os.ReadFile(filepath.Join(c.OutputDirectory, "build-result.json"))
					must(t, err)
					if strings.Contains(string(raw), "build.vmgs") ||
						strings.Contains(string(raw), "installer.iso") {
						t.Fatal("build state exported")
					}
					if family == "windows-11" {
						info, err := os.Stat(filepath.Join(c.OutputDirectory, "disk0.img"))
						must(t, err)
						if info.Size() != 1024 {
							t.Fatal("export contains VHD footer")
						}
					}
				}
			})
		}
	}
}

func TestSourceAndFileFailures(t *testing.T) {
	c := fixture(t, "windows-11")
	must(t, verifySource(t.Context(), c.SourcePath, c.SourceSHA256))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(verifySource(ctx, c.SourcePath, c.SourceSHA256), context.Canceled) {
		t.Fatal("hash ignores cancellation")
	}
	if verifySource(t.Context(), filepath.Dir(c.SourcePath), c.SourceSHA256) == nil {
		t.Fatal("directory source")
	}
	for _, pair := range [][2]string{{"missing", c.OutputDirectory}, {c.SourcePath, c.SourcePath}, {filepath.Dir(c.SourcePath), c.OutputDirectory}} {
		if copyFileContext(t.Context(), pair[0], pair[1]) == nil {
			t.Fatal("invalid copy")
		}
	}
	if writeJSON(filepath.Join(c.OutputDirectory, "missing"), c) == nil {
		t.Fatal("missing parent")
	}
	if writeJSON(c.OutputDirectory, func() {}) == nil {
		t.Fatal("unsupported JSON")
	}
	raw := filepath.Join(t.TempDir(), "disk.vhd")
	must(t, os.WriteFile(raw, make([]byte, 1024), 0o600))
	must(t, vhd.Append(raw, time.Now()))
	if moveWindowsRaw(raw, filepath.Join(t.TempDir(), "missing", "disk")) == nil {
		t.Fatal("bad export")
	}
	if _, err := windowsSeedISO(t.TempDir(), "missing"); err == nil {
		t.Fatal("bad seed input")
	}
	out := t.TempDir()
	must(t, os.Mkdir(filepath.Join(out, "seed.iso"), 0o700))
	if _, err := windowsSeedISO(out, t.TempDir()); err == nil {
		t.Fatal("existing seed")
	}
}

func TestIPSWValidation(t *testing.T) {
	for _, mode := range []string{"valid", "invalid", "missing", "oversized", "wrong-version", "wrong-build", "not-zip", "bad-crc"} {
		t.Run(mode, func(t *testing.T) {
			c := fixture(t, "macos")
			if mode == "not-zip" {
				must(t, os.WriteFile(c.SourcePath, []byte("bad"), 0o600))
			} else {
				version, build := "26.6.2", "25G83"
				if mode == "wrong-version" {
					version = "15.0"
				}
				if mode == "wrong-build" {
					build = "other"
				}
				writeIPSW(t, c.SourcePath, version, build, mode)
				if mode == "bad-crc" {
					b, err := os.ReadFile(c.SourcePath)
					must(t, err)
					// Mutate the central-directory CRC so decompression succeeds but integrity fails.
					i := bytes.Index(b, []byte{'P', 'K', 1, 2})
					if i < 0 {
						t.Fatal("zip directory")
					}
					b[i+16] ^= 0xff
					must(t, os.WriteFile(c.SourcePath, b, 0o600))
				}
			}
			version, err := verifyIPSW(c)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%s: %s %v", mode, version, err)
			}
		})
	}
}

func TestHostAndDefaultBindings(t *testing.T) {
	for _, family := range []string{"windows-11", "macos"} {
		c := fixture(t, family)
		compatible := (family == "macos" && runtime.GOOS == "darwin" && runtime.GOARCH == "arm64") ||
			(family == "windows-11" && runtime.GOOS == "windows" && runtime.GOARCH == c.Arch)
		if (host(c) == nil) != compatible {
			t.Fatal("host mismatch")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := Build(ctx, c, nil); err == nil {
			t.Fatal("cancelled native build")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := restoreMacOSNative(ctx, MacRestoreRequest{}); err == nil {
		t.Fatal("cancelled restore")
	}
	if _, err := installWindowsNative(
		ctx,
		WindowsInstallRequest{Timeout: time.Minute},
	); err == nil {
		t.Fatal("cancelled install")
	}
}

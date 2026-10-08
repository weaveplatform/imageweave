// Package nativebuild constructs pristine macOS and generalized Windows bases.
// Packer owns orchestration; this package calls the native platform libraries directly.
package nativebuild

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/deploymenttheory/go-sdk-winmediafoundry/pkg/isoinspect"
	"howett.net/plist"

	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

var (
	ErrInput   = errors.New("invalid native build")
	shaPattern = regexp.MustCompile("^[a-f0-9]{64}$")
)

// Config is deliberately local and pinned: acquisition/authentication precedes construction.
type Config struct {
	Family          string `mapstructure:"family"           json:"family"`
	Release         string `mapstructure:"release"          json:"release"`
	Arch            string `mapstructure:"arch"             json:"arch"`
	SourcePath      string `mapstructure:"source_path"      json:"sourcePath"`
	SourceSHA256    string `mapstructure:"source_sha256"    json:"sourceSHA256"`
	SourceBuild     string `mapstructure:"source_build"     json:"sourceBuild"`
	Edition         string `mapstructure:"edition"          json:"edition,omitempty"`
	Language        string `mapstructure:"language"         json:"language,omitempty"`
	OutputDirectory string `mapstructure:"output_directory" json:"outputDirectory"`
	Timeout         string `mapstructure:"timeout"          json:"timeout"`
}

// Validate does not read files or touch native APIs, so Packer validate works on any host.
func (c Config) Validate() error {
	s, err := catalog.Current().Resolve(c.Family, c.Release, c.Arch)
	if err != nil {
		return fmt.Errorf("native selection: %w", err)
	}
	target := "hcs"
	if c.Family == "macos" {
		target = "apple-vz"
	}
	if _, err := s.Template("guest-base", target); err != nil {
		return err
	}
	if c.Release != s.Version || c.SourceBuild == "" || !shaPattern.MatchString(c.SourceSHA256) {
		return fmt.Errorf("%w: exact release, build and SHA256 required", ErrInput)
	}
	for _, path := range []string{c.SourcePath, c.OutputDirectory} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
			strings.ContainsAny(path, "\r\n\x00") {
			return fmt.Errorf("%w: clean absolute source and output paths required", ErrInput)
		}
	}
	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil || timeout <= 0 {
		return fmt.Errorf("%w: positive timeout required", ErrInput)
	}
	if c.Family == "windows-11" {
		if c.Edition != strings.ToLower(c.Edition) {
			return fmt.Errorf("%w: lowercase Windows edition required", ErrInput)
		}
		_, _, err := ParseWindowsSelection(
			WindowsSelection{
				FromWindows: c.Edition + "-" + c.Release,
				Arch:        c.Arch,
				Language:    c.Language,
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Result describes construction only; acceptance is independently attached downstream.
type FirmwarePolicy struct {
	Type          string `json:"type"`
	SecureBoot    bool   `json:"secureBoot"`
	TPM           string `json:"tpm"`
	CloneIdentity string `json:"cloneIdentity"`
}
type PreparedResult struct {
	Parent         spec.BaseImage `json:"parent"`
	User           string         `json:"user"`
	AutomaticLogin bool           `json:"automaticLogin"`
	RemoteLogin    bool           `json:"remoteLogin"`
}

type Result struct {
	Prepared  *PreparedResult `json:"prepared,omitempty"`
	Firmware  FirmwarePolicy  `json:"firmware"`
	FirstBoot string          `json:"firstBoot"`

	SchemaVersion int                   `json:"schemaVersion"`
	Config        Config                `json:"inputs"`
	OSVersion     string                `json:"osVersion"`
	Qualification string                `json:"qualification"`
	Files         []string              `json:"files"`
	Mac           *MacRestoreResult     `json:"mac,omitempty"`
	Windows       *WindowsInstallResult `json:"windows,omitempty"`
}
type WindowsInstallRequest struct {
	Directory, ISO, Seed, Marker string
	Timeout                      time.Duration
	Arch                         string
	Log                          io.Writer
}
type WindowsInstallResult struct {
	OSVersion   string `json:"osVersion"`
	Build       string `json:"build"`
	Edition     string `json:"edition"`
	Release     string `json:"release"`
	Arch        string `json:"arch"`
	Generalized bool   `json:"generalized"`
}

type operations struct {
	host     func(Config) error
	mac      MacRestoreFunc
	windows  func(context.Context, WindowsInstallRequest) (WindowsInstallResult, error)
	retarget func(string, string) error
}

func host(c Config) error {
	if (c.Family == "macos" && (runtime.GOOS != "darwin" || runtime.GOARCH != "arm64")) ||
		(c.Family == "windows-11" && (runtime.GOOS != "windows" || runtime.GOARCH != c.Arch)) {
		return fmt.Errorf(
			"%w: build host must match %s/%s (macOS requires Apple silicon)",
			ErrInput,
			c.Family,
			c.Arch,
		)
	}
	return nil
}

// Build refuses existing output, rechecks source bytes and never invokes a consumer CLI.
func Build(ctx context.Context, c Config, log io.Writer) (Result, error) {
	return build(
		ctx,
		c,
		log,
		operations{
			host:     host,
			mac:      restoreMacOSNative,
			windows:  installWindowsNative,
			retarget: isoinspect.RetargetElToritoUEFI,
		},
	)
}

func build(ctx context.Context, c Config, log io.Writer, api operations) (Result, error) {
	result := Result{SchemaVersion: 1, Config: c, Qualification: "unverified"}
	if err := c.Validate(); err != nil {
		return result, err
	}
	if err := api.host(c); err != nil {
		return result, err
	}
	timeout, _ := time.ParseDuration(c.Timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	progress := newNativeProgress(ctx, log, "verify "+c.Family+"/"+c.Arch)
	finish := progress.start()
	progress.step("hashing pinned source media")
	err := verifySource(ctx, c.SourcePath, c.SourceSHA256)
	finish(err)
	if err != nil {
		return result, err
	}
	if err := newDirectory(c.OutputDirectory); err != nil {
		return result, err
	}
	if c.Family == "macos" {
		version, err := verifyIPSW(c)
		if err != nil {
			return result, err
		}
		mac, err := api.mac(
			ctx,
			MacRestoreRequest{
				IPSW:      c.SourcePath,
				Directory: c.OutputDirectory,
				DiskSize:  80 << 30,
				Log:       log,
			},
		)
		if err != nil {
			return result, err
		}
		result.OSVersion, result.Mac = version, &mac
		result.Firmware = FirmwarePolicy{
			Type:          "apple",
			TPM:           "none",
			CloneIdentity: "new-machine-identifier; copy auxiliary storage",
		}
		result.FirstBoot = "setup-assistant"
		result.Files = []string{"disk0.img", "auxstorage.bin"}
	} else {
		windows, err := buildWindows(ctx, c, log, api)
		if err != nil {
			return result, err
		}
		result.OSVersion, result.Windows = windows.OSVersion, &windows
		result.Firmware = FirmwarePolicy{
			Type:          "uefi",
			SecureBoot:    true,
			TPM:           "required",
			CloneIdentity: "regenerate firmware and TPM state",
		}
		result.FirstBoot = "windows-oobe"
		result.Files = []string{"disk0.img"}
	}
	if err := writeJSON(filepath.Join(c.OutputDirectory, "build-result.json"), result); err != nil {
		return result, err
	}
	return result, nil
}

func buildWindows(
	ctx context.Context,
	c Config,
	log io.Writer,
	api operations,
) (WindowsInstallResult, error) {
	var empty WindowsInstallResult
	iso := filepath.Join(c.OutputDirectory, "installer.iso")
	if err := copyFileContext(ctx, c.SourcePath, iso); err != nil {
		return empty, err
	}
	// Patch only the private installer copy. The authenticated source remains immutable.
	if err := api.retarget(iso, "efi/microsoft/boot/efisys_noprompt.bin"); err != nil {
		return empty, fmt.Errorf("prepare unattended boot: %w", err)
	}
	marker := "WEAVE-IMAGE-READY-" + hex.EncodeToString(randomMarker())
	seed, err := windowsSeed(
		c.OutputDirectory,
		WindowsSource{Release: c.Release, Edition: c.Edition, Arch: c.Arch, Language: c.Language},
		marker,
	)
	if err != nil {
		return empty, err
	}
	timeout, _ := time.ParseDuration(c.Timeout)
	result, err := api.windows(
		ctx,
		WindowsInstallRequest{
			Directory: c.OutputDirectory,
			ISO:       iso,
			Seed:      seed,
			Marker:    marker,
			Arch:      c.Arch,
			Timeout:   timeout,
			Log:       log,
		},
	)
	if err != nil {
		return result, err
	}
	if !result.Generalized || result.Build != c.SourceBuild ||
		result.OSVersion != "10.0."+result.Build ||
		!strings.EqualFold(result.Release, c.Release) ||
		result.Edition != windowsEditions[c.Edition].ID ||
		result.Arch != c.Arch {
		return result, fmt.Errorf(
			"%w: installed Windows identity or generalization differs from pinned request",
			ErrInput,
		)
	}
	if err := moveWindowsRaw(
		filepath.Join(c.OutputDirectory, "disk.vhd"),
		filepath.Join(c.OutputDirectory, "disk0.img"),
	); err != nil {
		return result, err
	}
	return result, nil
}
func randomMarker() []byte { b := make([]byte, 12); _, _ = rand.Read(b); return b }

func newDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create isolated candidate: %w", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func verifySource(ctx context.Context, path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx, f}); err != nil {
		return fmt.Errorf("hash source: %w", err)
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return fmt.Errorf("%w: source checksum mismatch", ErrInput)
	}
	return nil
}

func copyFileContext(ctx context.Context, source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create private copy: %w", err)
	}
	_, err = io.Copy(out, contextReader{ctx, in})
	if err = errors.Join(err, out.Close()); err != nil {
		return fmt.Errorf("copy source: %w", err)
	}
	return nil
}

func verifyIPSW(c Config) (string, error) {
	z, err := zip.OpenReader(c.SourcePath)
	if err != nil {
		return "", fmt.Errorf("open IPSW: %w", err)
	}
	defer z.Close()
	f, err := z.Open("BuildManifest.plist")
	if err != nil {
		return "", fmt.Errorf("open restore manifest: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil {
		return "", fmt.Errorf("read restore manifest: %w", err)
	}
	if len(b) > 64<<20 {
		return "", fmt.Errorf("%w: restore manifest too large", ErrInput)
	}
	var manifest struct {
		Version string `plist:"ProductVersion"`
		Build   string `plist:"ProductBuildVersion"`
	}
	if _, err := plist.Unmarshal(b, &manifest); err != nil {
		return "", fmt.Errorf("decode restore manifest: %w", err)
	}
	if strings.Split(manifest.Version, ".")[0] != c.Release || manifest.Build != c.SourceBuild {
		return "", fmt.Errorf("%w: IPSW version/build differs from request", ErrInput)
	}
	return manifest.Version, nil
}

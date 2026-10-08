package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/weaveplatform/imageweave/internal/imagebuild"
	linuximage "github.com/weaveplatform/imageweave/internal/imagebuild/linux"
	macosimage "github.com/weaveplatform/imageweave/internal/imagebuild/macos"
	windowsimage "github.com/weaveplatform/imageweave/internal/imagebuild/windows"
)

func executeImageCommand(t *testing.T, c *cobra.Command, args ...string) error {
	t.Helper()
	c.SetContext(t.Context())
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	c.SetArgs(args)
	return c.Execute()
}

type mediaBoundary struct {
	err        error
	acquireErr error
	source     windowsimage.WindowsSource
	selection  windowsimage.WindowsSelection
	cache      string
}

func (m *mediaBoundary) WindowsESDSources(
	_ context.Context,
	s windowsimage.WindowsSelection,
) ([]windowsimage.WindowsSource, error) {
	m.selection = s
	return []windowsimage.WindowsSource{m.source}, m.err
}

func (m *mediaBoundary) ResolveWindows(
	_ context.Context,
	s windowsimage.WindowsSelection,
) (windowsimage.WindowsSource, error) {
	m.selection = s
	return m.source, m.err
}

func (m *mediaBoundary) AcquireWindows(
	_ context.Context,
	s windowsimage.WindowsSource,
	sel windowsimage.WindowsSelection,
	cache string,
) (windowsimage.WindowsSource, string, error) {
	m.selection = sel
	m.cache = cache
	return s, "media.esd", m.acquireErr
}

func TestWindowsMediaCommandSelectionAndReceipt(t *testing.T) {
	for _, mode := range []string{"resolve", "list", "list-output", "list-error", "resolve-error", "acquire-error", "lock", "lock-error"} {
		t.Run(mode, func(t *testing.T) {
			m := &mediaBoundary{
				source: windowsimage.WindowsSource{
					Edition: "enterprise",
					Release: "25h2",
					Arch:    "arm64",
					SHA256:  strings.Repeat("a", 64),
				},
			}
			args := []string{
				"--from-windows",
				"enterprise-25h2",
				"--arch",
				"arm64",
				"--language",
				"en-GB",
			}
			out := filepath.Join(t.TempDir(), "source.json")
			switch mode {
			case "list", "list-error":
				args = append(args, "--list")
			case "list-output":
				args = append(args, "--list", "--out", out)
			case "acquire-error", "lock", "lock-error":
				args = append(args, "--cache", "cache", "--out", out)
			}
			if mode == "list-error" || mode == "resolve-error" {
				m.err = errors.New("catalogue unavailable")
			}
			if mode == "acquire-error" {
				m.acquireErr = errors.New("digest mismatch")
			}
			if mode == "lock-error" {
				out = filepath.Join(t.TempDir(), "missing", "lock")
				args[len(args)-1] = out
			}
			var emitted any
			err := executeImageCommand(
				t,
				newImageWindowsSource(m, func(v any) error { emitted = v; return nil }),
				args...)
			wantError := strings.Contains(mode, "error") || mode == "list-output"
			if (err != nil) != wantError {
				t.Fatalf("%s: %v", mode, err)
			}
			if wantError {
				if emitted != nil {
					t.Fatal("failed request emitted success")
				}
				return
			}
			if m.selection.FromWindows != "enterprise-25h2" || m.selection.Arch != "arm64" ||
				m.selection.Language != "en-GB" {
				t.Fatal(m.selection)
			}
			if mode == "list" {
				if len(emitted.([]windowsimage.WindowsSource)) != 1 {
					t.Fatal(emitted)
				}
			} else if emitted.(windowsimage.WindowsSource) != m.source {
				t.Fatal(emitted)
			}
			if mode == "lock" {
				raw, err := os.ReadFile(out)
				if err != nil || !bytes.Contains(raw, []byte(m.source.SHA256)) ||
					m.cache != "cache" {
					t.Fatal(string(raw), err)
				}
			}
		})
	}
}

type windowsBuildBoundary struct {
	options windowsimage.WindowsOptions
	err     error
}

func (b *windowsBuildBoundary) BuildWindows(
	_ context.Context,
	o windowsimage.WindowsOptions,
) (string, error) {
	b.options = o
	return "bundle", b.err
}

func TestWindowsBuildCommandForwardsPinnedInput(t *testing.T) {
	for _, fail := range []bool{false, true} {
		b := &windowsBuildBoundary{}
		if fail {
			b.err = errors.New("installation failed")
		}
		var result any
		err := executeImageCommand(
			t,
			newImageWindows(b, func(v any) error { result = v; return nil }),
			"--from-windows",
			"pro-26h2",
			"--source-lock",
			"source.json",
			"--arch",
			"arm64",
			"--cache",
			"media",
			"--out",
			"candidate",
			"--revision",
			"3",
			"--timeout",
			"45m",
		)
		if (err != nil) != fail {
			t.Fatal(err)
		}
		if b.options.SourceLock != "source.json" || b.options.Revision != 3 ||
			b.options.Timeout != 45*time.Minute ||
			b.options.Selection.Arch != "arm64" {
			t.Fatal(b.options)
		}
		if !fail && result.(map[string]string)["bundle"] != "bundle" {
			t.Fatal(result)
		}
	}
}

type imageRoundTripper func(*http.Request) (*http.Response, error)

func (f imageRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAppleCommandSelectsAndRecordsExactMedia(t *testing.T) {
	for _, mode := range []string{"selected", "list", "lock", "lock-error", "catalogue-error"} {
		t.Run(mode, func(t *testing.T) {
			source := macosimage.AppleSource{
				URL:     "https://updates.cdn-apple.com/restore.ipsw",
				Version: "26.6.2",
				Build:   "25G83",
				SHA256:  strings.Repeat("a", 64),
				Size:    100,
			}
			client := &http.Client{
				Transport: imageRoundTripper(func(r *http.Request) (*http.Response, error) {
					if mode == "catalogue-error" {
						return nil, errors.New("unavailable")
					}
					raw, _ := json.Marshal(
						map[string]any{
							"identifier": "VirtualMac2,1",
							"firmwares":  []macosimage.AppleSource{source},
						},
					)
					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewReader(raw)),
						Header:     http.Header{},
						Request:    r,
					}, nil
				}),
			}
			args := []string{"--version", "26"}
			out := filepath.Join(t.TempDir(), "source.json")
			if mode == "list" {
				args = append(args, "--list")
			}
			if strings.HasPrefix(mode, "lock") {
				if mode == "lock-error" {
					out = filepath.Join(out, "bad")
				}
				args = append(args, "--out", out)
			}
			var value any
			err := executeImageCommand(
				t,
				newImageIPSW(
					imagebuild.Packages{Downloader: imagebuild.Downloader{Client: client}},
					func(v any) error { value = v; return nil },
				),
				args...)
			if (err != nil) != strings.Contains(mode, "error") {
				t.Fatal(err)
			}
			if err != nil {
				return
			}
			if mode == "list" {
				if value.([]macosimage.AppleSource)[0] != source {
					t.Fatal(value)
				}
			} else if value.(macosimage.AppleSource) != source {
				t.Fatal(value)
			}
			if mode == "lock" {
				if _, err := os.Stat(out); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type bootBoundary struct {
	boot     linuximage.BootOptions
	validate linuximage.ValidateOptions
}

func (b *bootBoundary) BootLinux(
	_ context.Context,
	o linuximage.BootOptions,
) (linuximage.BootResult, error) {
	b.boot = o
	return linuximage.BootResult{Passed: true}, nil
}

func (b *bootBoundary) ValidateLinux(
	_ context.Context,
	o linuximage.ValidateOptions,
) (linuximage.Acceptance, error) {
	b.validate = o
	return linuximage.Acceptance{}, nil
}

func TestBootCommandsForwardDeadlineAndEmit(t *testing.T) {
	b := &bootBoundary{}
	emits := 0
	emit := func(any) error { emits++; return nil }
	if err := executeImageCommand(
		t,
		newImageBoot(b, emit),
		"bundle",
		"--report",
		"report",
		"--timeout",
		"45",
	); err != nil {
		t.Fatal(err)
	}
	if b.boot.Bundle != "bundle" || b.boot.Report != "report" || b.boot.Timeout != 45*time.Second {
		t.Fatal(b.boot)
	}
	if err := executeImageCommand(
		t,
		newImageValidate(b, emit),
		"one",
		"two",
		"--out",
		"candidate",
		"--arches",
		"arm64,amd64",
		"--timeout",
		"60",
	); err != nil {
		t.Fatal(err)
	}
	if len(b.validate.Bundles) != 2 || len(b.validate.Arches) != 2 ||
		b.validate.Timeout != time.Minute ||
		emits != 2 {
		t.Fatal(b.validate, emits)
	}
}

func TestAgentCommandBuildBoundary(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var observed imagebuild.AgentOptions
		emitted := false
		build := func(_ context.Context, o imagebuild.AgentOptions) (string, error) {
			observed = o
			if fail {
				return "", errors.New("install rejected")
			}
			return "bundle", nil
		}
		load := func() (imagebuild.Lock, error) { return imagebuild.Lock{CoreVersion: "1.2.3"}, nil }
		err := executeImageCommand(t, newImageAgentBuilder("linux", build, load, func(v any) error {
			emitted = true
			if v.(map[string]string)["bundle"] != "bundle" {
				t.Fatal(v)
			}
			return nil
		}), "--base", "layout", "--base-name", "ubuntu", "--arch", "arm64", "--cache", "media", "--out", "candidate", "--revision", "2", "--timeout", "90")
		if (err != nil) != fail || emitted == fail {
			t.Fatal(err, emitted)
		}
		if observed.Lock.CoreVersion != "1.2.3" || observed.Timeout != 90*time.Second ||
			observed.Revision != 2 {
			t.Fatal(observed)
		}
	}
}

type prepareBoundary struct{ platform, cache, out string }

func (p *prepareBoundary) Prepare(
	_ context.Context,
	_ imagebuild.Lock,
	platform, cache, out string,
) ([]imagebuild.Asset, error) {
	p.platform = platform
	p.cache = cache
	p.out = out
	return []imagebuild.Asset{{Name: "agent.deb"}}, nil
}

func TestPrepareCommandEmitsVerifiedAssets(t *testing.T) {
	p := &prepareBoundary{}
	var emitted any
	err := executeImageCommand(
		t,
		newImagePrepare(
			p,
			func() (imagebuild.Lock, error) { return imagebuild.Lock{}, nil },
			func(v any) error { emitted = v; return nil },
		),
		"--platform",
		"linux/arm64",
		"--cache",
		"cache",
		"--out",
		"payload",
	)
	if err != nil || p.platform != "linux/arm64" || p.cache != "cache" || p.out != "payload" ||
		emitted.([]imagebuild.Asset)[0].Name != "agent.deb" {
		t.Fatal(err, p, emitted)
	}
}

func TestImageLockResolutionAndOutputFailures(t *testing.T) {
	catalogue := "../../images/catalogue.json"
	for _, mode := range []string{"help", "bad-catalogue", "resolver-error", "resolve", "output-error"} {
		t.Run(mode, func(t *testing.T) {
			var output io.Writer = io.Discard
			if mode == "output-error" {
				output = imageFailWriter{}
			}
			resolver := func(_ context.Context, c imagebuild.Catalogue, _ imagebuild.Tools) (imagebuild.Lock, error) {
				if mode == "resolver-error" {
					return imagebuild.Lock{}, errors.New("release unavailable")
				}
				return imagebuild.LoadLock("../../images/packages.lock.json", c)
			}
			cmd := newImageWith(
				output,
				io.Discard,
				imagebuild.Tools{},
				imagebuild.Packages{},
				resolver,
			)
			out := filepath.Join(t.TempDir(), "lock.json")
			args := []string{"lock", "--catalogue", catalogue, "--out", out}
			switch mode {
			case "help":
				args = nil
			case "bad-catalogue":
				args = []string{"lock", "--catalogue", "missing", "--out", out}
			case "output-error":
				args = []string{"matrix", "--catalogue", catalogue}
			}
			err := executeImageCommand(t, cmd, args...)
			if (err != nil) != (strings.Contains(mode, "error") || mode == "bad-catalogue") {
				t.Fatal(err)
			}
			if mode == "resolve" {
				if _, err := os.Stat(out); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type imageFailWriter struct{}

func (imageFailWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestNativeRecipeDispatchFailsClosed(t *testing.T) {
	for _, platform := range []string{"darwin/arm64", "windows/arm64", "other/arm64"} {
		if _, err := platformRecipe(imagebuild.Lock{}, platform); err == nil {
			t.Fatal("accepted absent native packages", platform)
		}
	}
}

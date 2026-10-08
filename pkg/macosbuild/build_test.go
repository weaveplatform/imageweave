package macosbuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/internal/imagebuild/macos"
	"github.com/weaveplatform/imageweave/pkg/delivery"
	"github.com/weaveplatform/imageweave/pkg/plan"
	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/conformance"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

var testFailure = errors.New("test stage failure")

const testCommit = "0123456789012345678901234567890123456789"

func options(t *testing.T) Options {
	t.Helper()
	return Options{
		Workspace:  t.TempDir(),
		Repository: ".",
		Release:    "26",
		Tier:       "all",
		Packer:     "packer",
		OCI:        "weaveoci",
		Timeout:    "1m",
	}
}

func source() macos.AppleSource {
	return macos.AppleSource{
		URL:     "https://updates.cdn-apple.com/test.ipsw",
		Version: "26.6.2",
		Build:   "25G83",
		SHA256:  strings.Repeat("a", 64),
		Size:    4096,
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Use real OCI packaging, deep verification, clone unpacking and evidence
// checking. Only external media, Packer execution and VM observations are faked.
func fixtureServices(t *testing.T, builds *int) services {
	t.Helper()
	return services{
		sources:  func(context.Context, string) ([]macos.AppleSource, error) { return []macos.AppleSource{source()}, nil },
		download: func(_ context.Context, _ common.Media, path string) (string, error) { return path, nil },
		build: func(ctx context.Context, o delivery.Options) (delivery.Construction, error) {
			*builds++
			dir := filepath.Join(o.Out, "bundle")
			must(t, os.MkdirAll(dir, 0o700))
			must(t, os.WriteFile(filepath.Join(dir, "disk.img"), make([]byte, 4096), 0o600))
			must(t, os.WriteFile(filepath.Join(dir, "aux.bin"), []byte("writable firmware"), 0o600))
			f := pack.BundleFile{
				SchemaVersion: 1,
				Annotations: map[string]string{
					spec.AnnotationVersion:  o.Version,
					spec.AnnotationRevision: o.RecipeCommit,
					spec.AnnotationSource:   "https://github.com/weaveplatform/imageweave",
				},
				Guest: spec.Guest{
					OS:        "darwin",
					Arch:      "arm64",
					OSVersion: source().Version,
					OSBuild:   source().Build,
					Variant:   "base",
				},
				Firmware: spec.Firmware{
					Type:          "apple",
					TPM:           "none",
					HardwareModel: "YnBsaXN0MDA=",
				},
				Resources: spec.Resources{
					CPU:    spec.MinDefault{Min: 2, Default: 4},
					Memory: spec.MinDefault{Min: 4 << 30, Default: 4 << 30},
				},
				Provisioning: spec.Provisioning{CredentialHint: "set-at-first-boot"},
				Build: spec.Build{
					Template:    "templates/macos/image.pkr.hcl",
					TemplateRef: "weaveplatform/imageweave@" + o.RecipeCommit,
					Created:     "2026-10-08T00:00:00Z",
					SourceMedia: []spec.SourceMedia{
						{Kind: "ipsw", URI: source().URL, Digest: "sha256:" + source().SHA256},
					},
				},
				Disks: []pack.BundleDisk{
					{Name: "disk0", Role: "system", Path: "disk.img"},
				},
				State: []pack.BundleState{
					{
						Name:      spec.StateAuxStorage,
						Path:      "aux.bin",
						Semantics: spec.SemanticsCarry,
						Required:  true,
					},
				},
			}
			if p := o.Request.Parent; p != nil {
				f.Guest.Variant = "prepared"
				f.Provisioning = spec.Provisioning{DefaultUser: "weave", CredentialHint: "baked"}
				f.Build.Base = &spec.BaseImage{Name: p.Name, Digest: p.Digest}
				f.Build.SourceMedia = nil
			}
			must(t, pack.WriteBundleFile(dir, f))
			b, err := pack.LoadBundle(dir)
			must(t, err)
			store, err := pack.OpenLayout(ctx, filepath.Join(o.Out, "layout"))
			must(t, err)
			child, err := pack.Manifest(ctx, b, store, chunk.Options{})
			must(t, err)
			root, err := pack.Index(ctx, store, []ocispec.Descriptor{child}, nil)
			must(t, err)
			must(t, store.Tag(ctx, root, o.Version))
			return delivery.Construction{}, nil
		},
		inspect: inspect,
		validate: func(ctx context.Context, layout, tag, out string) error {
			store, err := oci.New(layout)
			must(t, err)
			root, err := store.Resolve(ctx, tag)
			must(t, err)
			_, err = imagecheck.Validate(
				ctx,
				imagecheck.Candidate{
					Store:   store,
					Root:    root,
					Tag:     tag,
					Out:     out,
					Timeout: time.Minute,
				},
				func(_ context.Context, c imagecheck.Clone) (imagecheck.Boot, error) {
					id := fmt.Sprint(c.Number)
					b := imagecheck.Boot{
						Platform:       "darwin/arm64",
						OSVersion:      c.Config.Guest.OSVersion,
						OSBuild:        c.Config.Guest.OSBuild,
						Passed:         true,
						Marker:         c.Marker,
						MachineID:      id,
						ElapsedSeconds: 1,
						Profile:        imagecheck.ValidationProfile(c.Config),
						Accelerator:    "apple-vz",
						Identities: map[string]string{
							"hardwareUUID":      id,
							"machineIdentifier": id,
							"macAddress":        id,
							"sshHostKeyDigest":  "sha256:" + strings.Repeat(id, 64),
						},
						Checks: map[string]bool{},
					}
					for _, key := range []string{"boot", "shutdown", "os-version", "reboot-identity", "fresh-firmware", "no-build-credentials", "agent-absent", "ssh-host-key-after-reboot"} {
						b.Checks[key] = true
					}
					if c.Config.Guest.Variant == "prepared" {
						b.Operations = map[string]imagecheck.Outcome{}
						b.RebootOperations = map[string]imagecheck.Outcome{}
						for _, op := range []string{"account-login", "administrator", "ssh", "automatic-login", "desktop-session", "setup-complete", "agent-absent"} {
							b.Operations[op] = imagecheck.Outcome{Status: imagecheck.Passed}
							b.RebootOperations[op] = imagecheck.Outcome{Status: imagecheck.Passed}
						}
					}
					return b, nil
				},
			)
			return err
		},
	}
}

func TestWorkflowBuildAndReuse(t *testing.T) {
	for _, tier := range []string{"base", "prepared", "all"} {
		t.Run(tier, func(t *testing.T) {
			o := options(t)
			o.Tier = tier
			builds := 0
			api := fixtureServices(t, &builds)
			installed := tools{commit: testCommit}
			results, err := run(t.Context(), o, []string{"26"}, installed, api, io.Discard)
			must(t, err)
			count := 1
			if tier == "all" {
				count = 2
			}
			if len(results) != count {
				t.Fatal(results)
			}
			before := builds
			api.sources = func(context.Context, string) ([]macos.AppleSource, error) {
				t.Fatal("must use pinned source")
				return nil, nil
			}
			results, err = run(t.Context(), o, []string{"26"}, installed, api, nil)
			must(t, err)
			if builds != before {
				t.Fatal("rebuilt a verified artifact")
			}
			for _, r := range results {
				if !r.Reused || r.Digest == "" {
					t.Fatal(r)
				}
			}
			must(t, os.WriteFile(results[0].Acceptance, []byte(`{"passed":true}`), 0o600))
			if _, err = run(t.Context(), o, []string{"26"}, installed, api, nil); err == nil {
				t.Fatal("accepted stale evidence")
			}
		})
	}
}

func TestWorkflowFailures(t *testing.T) {
	for _, stage := range []string{"source", "download", "build", "validate", "inspect", "missing evidence", "invalid evidence", "existing incomplete", "existing missing evidence"} {
		t.Run(stage, func(t *testing.T) {
			o := options(t)
			o.Tier = "base"
			builds := 0
			api := fixtureServices(t, &builds)
			installed := tools{commit: testCommit}
			switch stage {
			case "source":
				api.sources = func(context.Context, string) ([]macos.AppleSource, error) { return nil, testFailure }
			case "download":
				api.download = func(context.Context, common.Media, string) (string, error) { return "", testFailure }
			case "build":
				api.build = func(context.Context, delivery.Options) (delivery.Construction, error) {
					return delivery.Construction{}, testFailure
				}
			case "validate":
				api.validate = func(context.Context, string, string, string) error { return testFailure }
			case "inspect":
				api.inspect = func(context.Context, string, string) (conformance.Report, error) {
					return conformance.Report{}, testFailure
				}
			case "missing evidence":
				api.validate = func(context.Context, string, string, string) error { return nil }
			case "invalid evidence":
				api.validate = func(_ context.Context, _, _, out string) error {
					must(t, os.MkdirAll(out, 0o700))
					return os.WriteFile(filepath.Join(out, "acceptance.json"), []byte(`{}`), 0o600)
				}
			case "existing incomplete":
				must(
					t,
					os.Mkdir(
						filepath.Join(o.Workspace, "26.6.2-25G83-base-"+testCommit[:12]),
						0o700,
					),
				)
			case "existing missing evidence":
				r, err := run(t.Context(), o, []string{"26"}, installed, api, nil)
				must(t, err)
				must(t, os.Remove(r[0].Acceptance))
			}
			if _, err := run(t.Context(), o, []string{"26"}, installed, api, nil); err == nil {
				t.Fatal("accepted failed stage")
			}
		})
	}
}

func TestSelections(t *testing.T) {
	for _, release := range []string{"all", "26", "26.6.2", "n", "n-1", "n-2"} {
		o := options(t)
		o.Release = release
		r, err := selections(o)
		must(t, err)
		if len(r) == 0 {
			t.Fatal("empty matrix")
		}
	}
	for _, change := range []func(*Options){func(o *Options) { o.Workspace = "relative" }, func(o *Options) { o.Tier = "unknown" }, func(o *Options) { o.Timeout = "bad" }, func(o *Options) { o.Timeout = "-1s" }, func(o *Options) { o.Release = "26.1/../../../other" }, func(o *Options) { o.Release = "99" }, func(o *Options) { o.Repository = "" }, func(o *Options) { o.Packer = "" }} {
		o := options(t)
		change(&o)
		if _, err := selections(o); err == nil {
			t.Fatal(o)
		}
	}
}

func TestWorkspaceLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace")
	release, err := lockWorkspace(dir)
	must(t, err)
	if _, err = lockWorkspace(dir); err == nil {
		t.Fatal("concurrent run allowed")
	}
	release()
	release, err = lockWorkspace(dir)
	must(t, err)
	release()
	path := filepath.Join(t.TempDir(), "file")
	must(t, os.WriteFile(path, nil, 0o600))
	if _, err = lockWorkspace(path); err == nil {
		t.Fatal("file accepted")
	}
}

func TestSourceLocks(t *testing.T) {
	lookup := func(context.Context, string) ([]macos.AppleSource, error) { return []macos.AppleSource{source()}, nil }
	for _, data := range []string{`{}`, `{`, string(mustJSON(t, source())) + `{}`, strings.TrimSuffix(string(mustJSON(t, source())), "}") + `,"unknown":true}`} {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "macos-26-source.json"), []byte(data), 0o600))
		if _, err := sourceLock(t.Context(), dir, "26", lookup); err == nil {
			t.Fatal(data)
		}
	}
	for _, found := range [][]macos.AppleSource{nil, {{Version: "15"}}} {
		_, err := sourceLock(
			t.Context(),
			t.TempDir(),
			"26",
			func(context.Context, string) ([]macos.AppleSource, error) { return found, nil },
		)
		if err == nil {
			t.Fatal("invalid catalogue accepted")
		}
	}
	dir := t.TempDir()
	must(t, os.Mkdir(filepath.Join(dir, "macos-26-source.json"), 0o700))
	if _, err := sourceLock(t.Context(), dir, "26", lookup); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := sourceLock(
		t.Context(),
		filepath.Join(t.TempDir(), "absent"),
		"26",
		lookup,
	); err == nil {
		t.Fatal("missing workspace accepted")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	must(t, err)
	return data
}

func TestArtifactBindings(t *testing.T) {
	builds := 0
	o := options(t)
	o.Tier = "base"
	api := fixtureServices(t, &builds)
	r, err := run(t.Context(), o, []string{"26"}, tools{commit: testCommit}, api, nil)
	must(t, err)
	original, err := inspect(t.Context(), r[0].Layout, r[0].Version)
	must(t, err)
	for _, change := range []func(*spec.Config){func(c *spec.Config) { c.Guest.OS = "linux" }, func(c *spec.Config) { c.Guest.Arch = "amd64" }, func(c *spec.Config) { c.Guest.OSVersion = "15" }, func(c *spec.Config) { c.Build.TemplateRef = "other" }, func(c *spec.Config) { c.Build.SourceMedia = nil }, func(c *spec.Config) { c.Build.Base = &spec.BaseImage{} }} {
		var report conformance.Report
		must(t, json.Unmarshal(mustJSON(t, original), &report))
		change(&report.Children[0].Description.Config)
		if err = matchArtifact(report, source(), "base", nil, testCommit); err == nil {
			t.Fatal("wrong artifact accepted")
		}
	}
	if err = matchArtifact(conformance.Report{}, source(), "base", nil, testCommit); err == nil {
		t.Fatal("empty artifact")
	}
	if err = matchArtifact(
		original,
		source(),
		"base",
		&plan.Parent{Name: "wrong", Digest: "wrong"},
		testCommit,
	); err == nil {
		t.Fatal("wrong parent")
	}
	must(t, os.RemoveAll(filepath.Join(r[0].Layout, "blobs")))
	if _, err = inspect(t.Context(), r[0].Layout, r[0].Version); err == nil {
		t.Fatal("missing content accepted")
	}
	if _, err = inspect(t.Context(), t.TempDir(), "missing"); err == nil {
		t.Fatal("missing tag accepted")
	}
}

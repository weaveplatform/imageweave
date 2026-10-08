package macosbuild

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/pkg/delivery"
)

func TestToolInstallation(t *testing.T) {
	for _, scenario := range []string{"success", "external OCI", "commit failure", "bad commit", "status failure", "dirty", "missing Packer", "build failure", "module failure", "missing version", "install failure", "missing OCI"} {
		t.Run(scenario, func(t *testing.T) {
			o := options(t)
			o.OCI = ""
			if scenario == "external OCI" || scenario == "missing OCI" {
				o.OCI = "external"
			}
			runner := func(_ context.Context, binary string, args []string, out io.Writer) error {
				op := binary + " " + strings.Join(args, " ")
				switch {
				case strings.HasPrefix(op, "git rev-parse"):
					if scenario == "commit failure" {
						return testFailure
					}
					if scenario == "bad commit" {
						_, _ = io.WriteString(out, "unknown")
					} else {
						_, _ = io.WriteString(out, testCommit)
					}
				case strings.HasPrefix(op, "git status"):
					if scenario == "status failure" {
						return testFailure
					}
					if scenario == "dirty" {
						_, _ = io.WriteString(out, " M file")
					}
				case strings.HasPrefix(op, "go build"):
					if scenario == "build failure" {
						return testFailure
					}
				case strings.HasPrefix(op, "go list"):
					if scenario == "module failure" {
						return testFailure
					}
					if scenario != "missing version" {
						_, _ = io.WriteString(out, "v0.1.3")
					}
				case strings.HasPrefix(op, "go install"):
					if scenario == "install failure" {
						return testFailure
					}
				}
				return nil
			}
			look := func(s string) (string, error) {
				if scenario == "missing Packer" || scenario == "missing OCI" && s == "external" {
					return "", testFailure
				}
				return s, nil
			}
			r, err := installTools(t.Context(), o, "/tools", "/repo", runner, look, io.Discard)
			if scenario == "success" || scenario == "external OCI" {
				must(t, err)
				if r.commit != testCommit || r.runner == nil || r.oci == "" {
					t.Fatal(r)
				}
			} else if err == nil {
				t.Fatal("failed prerequisite accepted")
			}
		})
	}
}

func TestWorkflowOwnsWorkspaceBeforeTools(t *testing.T) {
	o := options(t)
	builds := 0
	setup := func(_ context.Context, o Options, _ io.Writer) (tools, error) {
		if _, err := lockWorkspace(o.Workspace); err == nil {
			t.Fatal("tools installed without workspace lock")
		}
		return tools{commit: testCommit}, nil
	}
	factory := func(Options, tools, io.Writer) services { return fixtureServices(t, &builds) }
	r, err := workflow(t.Context(), o, nil, setup, factory)
	must(t, err)
	if len(r) != 2 {
		t.Fatal(r)
	}
	if _, err = os.Stat(filepath.Join(o.Workspace, "build.lock")); !os.IsNotExist(err) {
		t.Fatal("lock leaked", err)
	}
	_, err = workflow(
		t.Context(),
		o,
		nil,
		func(context.Context, Options, io.Writer) (tools, error) { return tools{}, testFailure },
		factory,
	)
	if err == nil {
		t.Fatal("setup failed open")
	}
	release, err := lockWorkspace(o.Workspace)
	must(t, err)
	if _, err = workflow(t.Context(), o, nil, setup, factory); err == nil {
		t.Fatal("concurrent run accepted")
	}
	release()
	o.Timeout = "bad"
	if _, err = Run(t.Context(), o, nil); err == nil {
		t.Fatal("invalid options accepted")
	}
}

func TestProductionServices(t *testing.T) {
	called := false
	api := productionServices(
		Options{Timeout: "1m"},
		tools{runner: func(_ context.Context, _ string, args []string, _ io.Writer) error {
			called = true
			if strings.Join(
				args,
				" ",
			) != "image validate-macos layout --ref tag --out evidence --timeout 1m" {
				t.Fatal(args)
			}
			return testFailure
		}},
		nil,
	)
	if _, err := api.inspect(
		t.Context(),
		filepath.Join(t.TempDir(), "missing"),
		"tag",
	); err == nil {
		t.Fatal("missing artifact accepted")
	}
	if err := api.validate(t.Context(), "layout", "tag", "evidence"); err == nil || !called {
		t.Fatal(err)
	}
	if _, err := api.sources(t.Context(), "invalid"); err == nil {
		t.Fatal("invalid source accepted")
	}
	if _, err := api.build(t.Context(), delivery.Options{}); err == nil {
		t.Fatal("invalid build accepted")
	}
	if _, err := api.download(t.Context(), common.Media{}, ""); err == nil {
		t.Fatal("invalid media accepted")
	}
}

func TestPrepareToolsPreflight(t *testing.T) {
	o := options(t)
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		if _, err := prepareTools(t.Context(), o, nil); err == nil {
			t.Fatal("unsupported host")
		}
		return
	}
	// Exercise the real subprocess runner without compiling or restoring a VM.
	o.Repository = t.TempDir()
	if _, err := prepareTools(t.Context(), o, io.Discard); err == nil {
		t.Fatal("non-repository accepted")
	}
	command := exec.CommandContext(t.Context(), "git", "init", o.Repository)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	for _, args := range [][]string{{"-c", "user.name=test", "-c", "user.email=test@example.test", "commit", "--allow-empty", "-m", "fixture"}} {
		command = exec.CommandContext(t.Context(), "git", args...)
		command.Dir = o.Repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, output)
		}
	}
	must(t, os.WriteFile(filepath.Join(o.Repository, "dirty"), nil, 0o600))
	if _, err := prepareTools(t.Context(), o, nil); err == nil {
		t.Fatal("dirty recipe accepted")
	}
	o.Workspace = filepath.Join(t.TempDir(), "file")
	must(t, os.WriteFile(o.Workspace, nil, 0o600))
	if _, err := prepareTools(t.Context(), o, nil); err == nil {
		t.Fatal("workspace file accepted")
	}
}

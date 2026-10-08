package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/plan"
)

func options(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	file := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	firmware := func(name string) plan.File {
		h := sha256.Sum256([]byte(name))
		return plan.File{Path: file(name), SHA256: hex.EncodeToString(h[:])}
	}
	return Options{Request: plan.Request{SchemaVersion: 1, Family: "ubuntu", Release: "26.04", Arch: "arm64", Purpose: "guest-base", Target: "qemu", SourceURL: "https://vendor.example/image", SourceSHA256: strings.Repeat("a", 64), SourceBuild: "20261008", SSHPrivateKey: file("key"), SSHPublicKey: file("key.pub"), FirmwareCode: firmware("code"), FirmwareVars: firmware("vars"), Accelerator: "tcg"}, RecipeCommit: strings.Repeat("b", 40), Version: "26.04-20261008-r1", SourceURI: "https://vendor.example/image", Out: filepath.Join(dir, "delivery"), Packer: "packer", OCI: "weaveoci", Imageweave: "imageweave"}
}

func TestBuildCommandsBindExactCandidate(t *testing.T) {
	for _, family := range []string{"ubuntu", "fedora"} {
		t.Run(family, func(t *testing.T) {
			o := options(t)
			o.Request.Family = family
			if family == "fedora" {
				o.Request.Release = "44"
			}
			var calls [][]string
			var log bytes.Buffer
			result, err := Build(t.Context(), o, func(_ context.Context, binary string, args []string, _ io.Writer) error {
				calls = append(calls, append([]string{binary}, args...))
				return nil
			}, &log)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 5 || calls[0][1] != "init" || calls[1][1] != "validate" || calls[2][1] != "build" || calls[3][2] != "import-imageweave" || calls[4][2] != "validate-linux" {
				t.Fatal(calls)
			}
			if result.Layout != filepath.Join(o.Out, "validated", "layout") || result.Qualification != "local-acceptance" || result.RecipeCommit != o.RecipeCommit {
				t.Fatal(result)
			}
			if !strings.Contains(strings.Join(calls[3], " "), o.SourceURI) || !strings.Contains(strings.Join(calls[3], " "), o.RecipeCommit) || !strings.Contains(log.String(), "imageweave image started") {
				t.Fatal(calls, log.String())
			}
			content, err := os.ReadFile(filepath.Join(o.Out, "variables.pkrvars.json"))
			if err != nil {
				t.Fatal(err)
			}
			var vars map[string]any
			if err := json.Unmarshal(content, &vars); err != nil {
				t.Fatal(err)
			}
			if vars["workspace"] != filepath.Join(o.Out, "packer") || vars["source_sha256"] != o.Request.SourceSHA256 {
				t.Fatal(vars)
			}
			if !reflect.DeepEqual(calls[4][3:5], []string{filepath.Join(o.Out, "bundle"), "--arches"}) {
				t.Fatal(calls[4])
			}
		})
	}
}

func TestBuildStopsAtEveryFailure(t *testing.T) {
	failure := errors.New("failed command")
	for failAt := range 5 {
		o := options(t)
		calls := 0
		_, err := Build(t.Context(), o, func(context.Context, string, []string, io.Writer) error {
			calls++
			if calls == failAt+1 {
				return failure
			}
			return nil
		}, nil)
		if !errors.Is(err, failure) || calls != failAt+1 {
			t.Fatalf("calls=%d error=%v", calls, err)
		}
	}
}

func TestBuildRejectsInputBeforeExecution(t *testing.T) {
	mutations := []func(*Options){func(o *Options) { o.RecipeCommit = "main" }, func(o *Options) { o.Version = "bad version" }, func(o *Options) { o.SourceURI = "https://user:pass@example.com/a" }, func(o *Options) { o.SourceURI = "http://example.com/a" }, func(o *Options) { o.SourceURI = "https://example.com/a?secret=yes" }, func(o *Options) { o.SourceURI = "https://example.com/a#fragment" }, func(o *Options) { o.SourceURI = "%" }, func(o *Options) { o.Out = "relative" }, func(o *Options) { o.Packer = "" }, func(o *Options) { o.OCI = "" }, func(o *Options) { o.Imageweave = "" }, func(o *Options) { o.Request.Family = "macos" }, func(o *Options) { o.Request.FirmwareCode.SHA256 = strings.Repeat("0", 64) }, func(o *Options) { o.Out = filepath.Dir(o.Out) }}
	for _, change := range mutations {
		o := options(t)
		change(&o)
		_, err := Build(t.Context(), o, func(context.Context, string, []string, io.Writer) error {
			t.Fatal("executed invalid request")
			return nil
		}, nil)
		if err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	_, err := Build(t.Context(), options(t), nil, nil)
	if !errors.Is(err, ErrInput) {
		t.Fatal(err)
	}
}

func TestExec(t *testing.T) {
	if os.Getenv("IMAGEWEAVE_EXEC_HELPER") == "yes" {
		return
	}
	t.Setenv("IMAGEWEAVE_EXEC_HELPER", "yes")
	var output bytes.Buffer
	if err := Exec(t.Context(), os.Args[0], []string{"-test.run=^TestExec$"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := Exec(t.Context(), filepath.Join(t.TempDir(), "missing"), nil, &output); err == nil {
		t.Fatal("missing executable accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Exec(ctx, os.Args[0], nil, &output); err == nil {
		t.Fatal("cancelled process accepted")
	}
}

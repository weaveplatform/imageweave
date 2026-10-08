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

	"github.com/weaveplatform/imageweave/pkg/catalog"
)

func nativeOptions(t *testing.T) Options {
	t.Helper()
	o := options(t)
	o.Request.Family, o.Request.Release, o.Request.Target = "macos", "26", "apple-vz"
	o.Request.SourceURL = filepath.Join(t.TempDir(), "restore.ipsw")
	if err := os.WriteFile(o.Request.SourceURL, []byte("authenticated media"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("authenticated media"))
	o.Request.SourceSHA256 = hex.EncodeToString(hash[:])
	o.Request.SourceBuild = "25G83"
	o.Request.SSHPrivateKey, o.Request.SSHPublicKey, o.Imageweave = "", "", ""
	return o
}

func TestNativeConstructionMatrix(t *testing.T) {
	for _, selection := range catalog.Current().Matrix() {
		if selection.Status != "template" ||
			(selection.Family != "macos" && selection.Family != "windows-11") {
			continue
		}
		t.Run(selection.Family+"/"+selection.Version+"/"+selection.Arch, func(t *testing.T) {
			o := nativeOptions(t)
			o.Request.Family, o.Request.Release, o.Request.Arch = selection.Family, selection.Version, selection.Arch
			template := "templates/macos"
			if selection.Family == "windows-11" {
				o.Request.Target, o.Request.Edition, o.Request.Language = "hcs", "enterprise", "en-US"
				template = "templates/windows"
			}
			var calls [][]string
			var log bytes.Buffer
			result, err := BuildNative(
				t.Context(),
				o,
				func(_ context.Context, binary string, args []string, output io.Writer) error {
					if output != &log {
						t.Fatal("diagnostics writer lost")
					}
					calls = append(calls, append([]string{binary}, args...))
					return nil
				},
				&log,
			)
			if err != nil {
				t.Fatal(err)
			}
			vars := filepath.Join(o.Out, "variables.pkrvars.json")
			want := [][]string{
				{o.Packer, "init", template},
				{o.Packer, "validate", "-var-file=" + vars, template},
				{o.Packer, "build", "-color=false", "-var-file=" + vars, template},
				{
					o.OCI,
					"bundle",
					"import-imageweave",
					filepath.Join(o.Out, "packer", "candidate", "packer-manifest.json"),
					"--recipe-commit",
					o.RecipeCommit,
					"--source-uri",
					o.SourceURI,
					"--version",
					o.Version,
					"--out",
					filepath.Join(o.Out, "bundle"),
				},
				{
					o.OCI,
					"pack",
					filepath.Join(o.Out, "bundle"),
					"--out",
					filepath.Join(o.Out, "layout"),
					"--tag",
					o.Version,
				},
				{
					o.OCI,
					"inspect",
					filepath.Join(o.Out, "layout"),
					"--ref",
					o.Version,
					"--deep",
					"--strict",
					"--json",
				},
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("commands: %#v", calls)
			}
			if result.Qualification != "unverified" || result.RecipeCommit != o.RecipeCommit ||
				result.Layout != filepath.Join(o.Out, "layout") ||
				result.Manifest != want[3][3] ||
				result.Bundle != want[4][2] {
				t.Fatal(result)
			}
			data, err := os.ReadFile(vars)
			if err != nil {
				t.Fatal(err)
			}
			var variables map[string]any
			if err := json.Unmarshal(data, &variables); err != nil {
				t.Fatal(err)
			}
			if variables["output_directory"] != filepath.Join(o.Out, "packer", "candidate") ||
				variables["source_sha256"] != o.Request.SourceSHA256 ||
				variables["release"] != selection.Version {
				t.Fatal(variables)
			}
			if !strings.Contains(log.String(), "weaveoci inspect started") {
				t.Fatal(log.String())
			}
		})
	}
}

func TestNativeConstructionFailures(t *testing.T) {
	failure := errors.New("construction failed")
	for failAt := range 6 {
		o := nativeOptions(t)
		calls := 0
		result, err := BuildNative(
			t.Context(),
			o,
			func(context.Context, string, []string, io.Writer) error {
				calls++
				if calls == failAt+1 {
					return failure
				}
				return nil
			},
			nil,
		)
		if !errors.Is(err, failure) || calls != failAt+1 || result != (Construction{}) {
			t.Fatalf("calls=%d result=%+v error=%v", calls, result, err)
		}
	}
	for _, mutate := range []func(*Options){
		func(o *Options) { o.RecipeCommit = "main" },
		func(o *Options) { o.Request.Family = "ubuntu" },
		func(o *Options) { o.Request.Arch = "amd64" },
		func(o *Options) { o.Request.SourceSHA256 = strings.Repeat("0", 64) },
		func(o *Options) { o.Request.Target = "qemu" },
		func(o *Options) { o.Out = filepath.Dir(o.Out) },
	} {
		o := nativeOptions(t)
		mutate(&o)
		if _, err := BuildNative(
			t.Context(),
			o,
			func(context.Context, string, []string, io.Writer) error {
				t.Fatal("executed invalid native input")
				return nil
			},
			nil,
		); err == nil {
			t.Fatal("invalid native input accepted")
		}
	}
}

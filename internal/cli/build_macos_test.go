package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/macosbuild"
)

func TestStandaloneMacOSFlags(t *testing.T) {
	var out bytes.Buffer
	c := newBuildMacOS(
		func(_ context.Context, o macosbuild.Options, log io.Writer) ([]macosbuild.Result, error) {
			if o.Workspace != "/images" || o.Repository != "/repo" || o.Release != "26" ||
				o.Tier != "base" ||
				o.Timeout != "2h" ||
				o.Packer != "/tools/packer" ||
				o.OCI != "/tools/oci" {
				t.Fatal(o)
			}
			_, _ = io.WriteString(log, "stage progress")
			return []macosbuild.Result{{Tier: "base", Layout: "/images/layout"}}, nil
		},
	)
	c.SetOut(&out)
	c.SetErr(io.Discard)
	c.SetArgs(
		[]string{
			"--workspace",
			"/images",
			"--repository",
			"/repo",
			"--release",
			"26",
			"--tier",
			"base",
			"--timeout",
			"2h",
			"--packer",
			"/tools/packer",
			"--weaveoci",
			"/tools/oci",
		},
	)
	if err := c.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"layout": "/images/layout"`) {
		t.Fatal(out.String())
	}
}

func TestStandaloneMacOSFailures(t *testing.T) {
	failed := errors.New("build failed")
	c := newBuildMacOS(
		func(_ context.Context, o macosbuild.Options, _ io.Writer) ([]macosbuild.Result, error) {
			if o.Release != "all" || o.Tier != "all" || o.Timeout != "90m" || o.Packer != "packer" {
				t.Fatal(o)
			}
			return nil, failed
		},
	)
	c.SetArgs(nil)
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	if err := c.ExecuteContext(t.Context()); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	c = newBuildMacOS(
		func(context.Context, macosbuild.Options, io.Writer) ([]macosbuild.Result, error) {
			t.Fatal("unexpected build")
			return nil, nil
		},
	)
	c.SetArgs([]string{"extra"})
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	if err := c.ExecuteContext(t.Context()); err == nil {
		t.Fatal("positional argument accepted")
	}
}

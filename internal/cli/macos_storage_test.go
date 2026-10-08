package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/buildstorage"
	"github.com/weaveplatform/imageweave/pkg/macosbuild"
)

func TestMacOSStorageCommand(t *testing.T) {
	failed := errors.New("capacity")
	for _, mode := range []string{"ok", "failed", "write failed"} {
		t.Run(mode, func(t *testing.T) {
			c := newMacOSStoragePlan(
				func(_ context.Context, o macosbuild.Options, _ io.Writer) (buildstorage.Plan, error) {
					if o.Workspace != "auto" || o.ScratchRoot != "/host" {
						t.Fatal(o)
					}
					if mode == "failed" {
						return buildstorage.Plan{}, failed
					}
					return buildstorage.Plan{Workspace: "/images", Serial: true}, nil
				},
			)
			var out bytes.Buffer
			c.SetOut(&out)
			c.SetErr(io.Discard)
			c.SetArgs([]string{"--scratch-root", "/host"})
			if mode == "write failed" {
				c.SetOut(storageFailWriter{})
			}
			e := c.ExecuteContext(t.Context())
			if (e == nil) != (mode == "ok") {
				t.Fatal(e)
			}
			if mode != "write failed" && out.Len() == 0 {
				t.Fatal("missing capacity report")
			}
		})
	}
}

type storageFailWriter struct{}

func (storageFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

//go:build darwin && arm64

package nativebuild

import (
	"context"
	"path/filepath"
	"testing"

	vz "github.com/deploymenttheory/go-bindings-macosplatform/bindings/frameworks/virtualization"
	"github.com/deploymenttheory/go-bindings-macosplatform/opinionated/tools/grandcentraldispatch/serialqueue"
)

func TestMacInstallerQueueLifetime(t *testing.T) {
	for _, failure := range []bool{false, true} {
		config := &vz.VirtualMachineConfiguration{}
		installer := &vz.MacOSInstaller{}
		created, installed, cancelled := false, false, false
		session := macStartWith(config, "restore.ipsw", macInstallerCalls{
			create: func(c *vz.VirtualMachineConfiguration, q *serialqueue.Queue, path string) (*vz.VirtualMachine, *vz.MacOSInstaller) {
				if c != config || q == nil || path != "restore.ipsw" {
					t.Error("lost installation inputs")
				}
				created = true
				return &vz.VirtualMachine{}, installer
			},
			install: func(i *vz.MacOSInstaller, done func(error)) {
				if !created || i != installer {
					t.Error("installation ordering")
				}
				installed = true
				if failure {
					done(context.Canceled)
				} else {
					done(nil)
				}
			},
			progress: func(i *vz.MacOSInstaller) float64 {
				if i != installer {
					t.Error("progress identity")
				}
				return 0.5
			},
			cancel: func(i *vz.MacOSInstaller) {
				if i != installer {
					t.Error("cancel identity")
				}
				cancelled = true
			},
		})
		if err := <-session.done; (err != nil) != failure {
			t.Fatal(err)
		}
		if !installed || session.fraction() != 0.5 || len(session.keep) != 4 {
			t.Fatal("lost lifecycle state")
		}
		session.cancel()
		if !cancelled {
			t.Fatal("cancel dropped")
		}
	}
	if _, err := restoreMacOSWith(
		t.Context(),
		MacRestoreRequest{IPSW: filepath.Join(t.TempDir(), "absent")},
		macNativeCalls{},
	); err == nil {
		t.Fatal("missing restore path")
	}
}

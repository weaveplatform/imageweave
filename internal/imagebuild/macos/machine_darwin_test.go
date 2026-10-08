//go:build darwin && arm64

package macos

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	vz "github.com/deploymenttheory/go-bindings-macosplatform/bindings/frameworks/virtualization"

	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

// A real Apple hardware-model serialization with minimum supported OS 13.
const testHardware = "YnBsaXN0MDDTAQIDBAQFXxAZRGF0YVJlcHJlc2VudGF0aW9uVmVyc2lvbl8QD1BsYXRmb3JtVmVyc2lvbl8QEk1pbmltdW1TdXBwb3J0ZWRPUxACowYHBxANEAAIDys9UlRYWgAAAAAAAAEBAAAAAAAAAAgAAAAAAAAAAAAAAAAAAABc"

func TestAppleConfiguration(t *testing.T) {
	p, cfg := parentFixture(t)
	dir := filepath.Join(t.TempDir(), "clone")
	_, err := unpackParent(t.Context(), p, dir)
	must(t, err)
	cfg.Firmware.HardwareModel = testHardware
	for _, mode := range []string{"valid", "invalid guest", "missing bundle", "extra disk", "bad base64", "bad hardware", "unsupported hardware", "missing disk", "configuration error"} {
		t.Run(mode, func(t *testing.T) {
			selected := cfg
			path := dir
			calls := nativeAppleCalls()
			called := false
			calls.supported = func(*vz.MacHardwareModel) bool { return mode != "unsupported hardware" }
			calls.validate = func(c *vz.VirtualMachineConfiguration) error {
				if mode == "configuration error" {
					return ErrPrepared
				}
				if c.CPUCount() != 4 || c.MemorySize() != 4<<30 || len(c.StorageDevices()) != 1 ||
					len(c.NetworkDevices()) != 1 {
					t.Fatal("incorrect VM resources/devices")
				}
				return nil
			}
			calls.create = func(*appleMachine) { called = true }
			calls.release = func(m *appleMachine) { m.config.Release() }
			switch mode {
			case "invalid guest":
				selected.Guest.OS = "linux"
			case "missing bundle":
				path = t.TempDir()
			case "extra disk":
				path = filepath.Join(t.TempDir(), "clone")
				_, err := unpackParent(t.Context(), p, path)
				must(t, err)
				b, err := pack.LoadBundle(path)
				must(t, err)
				b.File.State = nil
				must(t, pack.WriteBundleFile(path, b.File))
			case "bad base64":
				selected.Firmware.HardwareModel = "?"
			case "bad hardware":
				selected.Firmware.HardwareModel = base64.StdEncoding.EncodeToString(
					[]byte("invalid"),
				)
			case "missing disk":
				path = filepath.Join(t.TempDir(), "clone")
				_, err := unpackParent(t.Context(), p, path)
				must(t, err)
				must(t, os.Remove(filepath.Join(path, "disk0.img")))
			}
			m, err := newAppleMachineWith(path, selected, calls)
			if mode != "valid" {
				if err == nil {
					_ = m.close()
					t.Fatal("invalid configuration accepted")
				}
				return
			}
			must(t, err)
			defer m.close()
			id, mac := m.identity()
			if !called || id == "" || mac == "" {
				t.Fatal("missing independent identities")
			}
			m2, err := newAppleMachineWith(path, selected, calls)
			must(t, err)
			defer m2.close()
			id2, mac2 := m2.identity()
			if id == id2 || mac == mac2 {
				t.Fatal("clones share identity")
			}
		})
	}
	native := nativeAppleCalls()
	raw, _ := base64.StdEncoding.DecodeString(testHardware)
	h := vz.NewMACHardwareModelWithDataRepresentation(raw)
	_ = native.supported(h)
	invalid := vz.NewVirtualMachineConfiguration()
	defer invalid.Release()
	if native.validate(invalid) == nil {
		t.Fatal("invalid native configuration passed")
	}
	if native.major() >= 27 {
		options := vz.NewMacOSVirtualMachineStartOptions()
		defer options.Release()
		must(t, native.provision(options))
	}
}

func TestAppleLifecycle(t *testing.T) {
	for _, mode := range []string{"success", "old host", "provision error", "start error", "start cancelled", "stop error", "stop cancelled", "already stopped", "guest error", "wait cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			state := vz.VirtualMachineStateRunning
			calls := nativeAppleCalls()
			calls.major = func() int {
				if mode == "old host" {
					return 26
				}
				return 27
			}
			calls.provision = func(*vz.MacOSVirtualMachineStartOptions) error {
				if mode == "provision error" {
					return ErrPrepared
				}
				return nil
			}
			calls.start = func(_ *appleMachine, _ *vz.MacOSVirtualMachineStartOptions, done func(error)) {
				if mode == "start cancelled" {
					cancel()
					return
				}
				if mode == "start error" {
					done(ErrPrepared)
				} else {
					done(nil)
				}
			}
			calls.state = func(*appleMachine) vz.VirtualMachineState { return state }
			calls.stop = func(_ *appleMachine, done func(error)) {
				if mode == "stop cancelled" {
					cancel()
					return
				}
				if mode == "stop error" {
					done(ErrPrepared)
				} else {
					done(nil)
				}
			}
			calls.wait = func(context.Context, time.Duration) error {
				if mode == "wait cancelled" {
					return context.Canceled
				}
				state = vz.VirtualMachineStateStopped
				return nil
			}
			released := false
			calls.release = func(*appleMachine) { released = true }
			m := &appleMachine{calls: calls, display: &appleDisplay{}}
			err := m.start(ctx, true)
			if mode == "old host" || mode == "provision error" || mode == "start error" ||
				mode == "start cancelled" {
				if err == nil {
					t.Fatal("start failed open")
				}
				return
			}
			must(t, err)
			if mode == "already stopped" {
				state = vz.VirtualMachineStateStopped
			}
			err = m.stop(ctx, true)
			if mode == "stop error" || mode == "stop cancelled" {
				if err == nil {
					t.Fatal("stop failed open")
				}
				return
			}
			must(t, err)
			if mode == "guest error" {
				state = vz.VirtualMachineStateError
			}
			err = m.stop(ctx, false)
			if mode == "guest error" || mode == "wait cancelled" {
				if err == nil {
					t.Fatal("shutdown failed open")
				}
				return
			}
			must(t, err)
			must(t, m.start(ctx, false))
			must(t, m.close())
			if !released || m.display != nil {
				t.Fatal("native resources retained")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := StartNative(
		ctx,
		"",
		spec.Config{},
		io.Discard,
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}
	if _, err := StartNative(t.Context(), "", spec.Config{}, io.Discard); err == nil {
		t.Fatal("invalid input")
	}
}

//go:build darwin && arm64

package macos

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"
	"time"

	foundation "github.com/deploymenttheory/go-bindings-macosplatform/bindings/frameworks/foundation"
	vz "github.com/deploymenttheory/go-bindings-macosplatform/bindings/frameworks/virtualization"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/libraries/dispatch"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/obj"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/purego"
	"github.com/deploymenttheory/go-bindings-macosplatform/opinionated/tools/grandcentraldispatch/serialqueue"

	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

type appleMachine struct {
	vm              *vz.VirtualMachine
	config          *vz.VirtualMachineConfiguration
	queue           *serialqueue.Queue
	identifier, mac string
	display         *appleDisplay
	calls           *appleCalls
	displayCalls    *displayCalls
}

// Keep host API calls at a narrow boundary so lifecycle failures and cancellation
// can be tested without requiring nested virtualization on hosted CI runners.
type appleCalls struct {
	supported func(*vz.MacHardwareModel) bool
	validate  func(*vz.VirtualMachineConfiguration) error
	create    func(*appleMachine)
	start     func(*appleMachine, *vz.MacOSVirtualMachineStartOptions, func(error))
	stop      func(*appleMachine, func(error))
	state     func(*appleMachine) vz.VirtualMachineState
	release   func(*appleMachine)
	major     func() int
	provision func(*vz.MacOSVirtualMachineStartOptions) error
	wait      func(context.Context, time.Duration) error
}

func nativeAppleCalls() *appleCalls {
	return &appleCalls{
		supported: func(h *vz.MacHardwareModel) bool { return h.IsSupported() },
		validate:  func(c *vz.VirtualMachineConfiguration) error { return c.Validate() },
		create: func(m *appleMachine) {
			m.queue.Do(func() {
				m.vm = vz.NewVirtualMachineWithConfigurationQueue(
					m.config,
					dispatch.WrapQueue(purego.CFRef(purego.ID(m.queue.Handle()))),
				)
			})
		},
		start: func(m *appleMachine, o *vz.MacOSVirtualMachineStartOptions, done func(error)) {
			m.queue.Do(func() {
				obj.ID(m.vm).
					Send(purego.RegisterName("startWithOptions:completionHandler:"), obj.ID(o), purego.NewBlock(func(_ purego.Block, e purego.ID) { done(purego.NSErrorToError(e)) }))
			})
		},
		stop: func(m *appleMachine, done func(error)) {
			m.queue.Do(func() {
				obj.ID(m.vm).
					Send(purego.RegisterName("stopWithCompletionHandler:"), purego.NewBlock(func(_ purego.Block, e purego.ID) { done(purego.NSErrorToError(e)) }))
			})
		},
		state: func(m *appleMachine) (state vz.VirtualMachineState) {
			m.queue.Do(func() { state = m.vm.State() })
			return
		},
		release: func(m *appleMachine) { m.queue.Do(func() { m.vm.Release(); m.config.Release() }) },
		major:   func() int { return int(foundation.NSProcessInfoProcessInfo().OperatingSystemVersion().MajorVersion) },
		provision: func(o *vz.MacOSVirtualMachineStartOptions) error {
			return o.SetGuestProvisioning(
				vz.NewMacGuestProvisioningOptions().
					WithFullName("weave").
					WithUsername("weave").
					WithPassword("weave").
					WithLogsInAutomatically(true).
					WithEnablesRemoteLogin(true),
			)
		},
		wait: pause,
	}
}

// StartNative uses Apple Virtualization directly. The binary must have the
// virtualization entitlement and service the main run loop for its UI adapter.
func StartNative(
	ctx context.Context,
	bundle string,
	cfg spec.Config,
	log io.Writer,
) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("native machine: %w", err)
	}
	m, err := newAppleMachine(bundle, cfg)
	if err != nil {
		return nil, fmt.Errorf("native machine: %w", err)
	}
	return &nativeSession{vm: m, cfg: cfg, log: log}, nil
}

func newAppleMachine(dir string, cfg spec.Config) (*appleMachine, error) {
	return newAppleMachineWith(dir, cfg, nativeAppleCalls())
}

func newAppleMachineWith(dir string, cfg spec.Config, calls *appleCalls) (*appleMachine, error) {
	if cfg.Guest.OS != "darwin" || cfg.Guest.Arch != "arm64" || cfg.Firmware.Type != "apple" ||
		cfg.Resources.Memory.Default <= 0 {
		return nil, fmt.Errorf("%w: Apple ARM64 guest required", ErrPrepared)
	}
	b, err := pack.LoadBundle(dir)
	if err != nil {
		return nil, fmt.Errorf("load native clone: %w", err)
	}
	if len(b.File.Disks) != 1 || len(b.File.State) != 1 ||
		b.File.State[0].Name != spec.StateAuxStorage {
		return nil, fmt.Errorf("%w: one disk and Apple auxiliary storage required", ErrPrepared)
	}
	data, err := base64.StdEncoding.DecodeString(cfg.Firmware.HardwareModel)
	if err != nil {
		return nil, fmt.Errorf("decode Apple model: %w", err)
	}
	hardware := vz.NewMACHardwareModelWithDataRepresentation(data)
	if hardware == nil || !calls.supported(hardware) {
		return nil, fmt.Errorf("%w: Apple hardware model unsupported", ErrPrepared)
	}
	disk, err := vz.NewDiskImageStorageDeviceAttachmentWithURLReadOnly(
		filepath.Join(dir, b.File.Disks[0].Path),
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("attach private clone: %w", err)
	}
	aux := vz.NewMACAuxiliaryStorageWithContentsOfURL(filepath.Join(dir, b.File.State[0].Path))
	id := vz.NewMacMachineIdentifier()
	network := vz.NewVirtioNetworkDeviceConfiguration().
		WithAttachment(vz.NewNATNetworkDeviceAttachment())
	config := vz.NewVirtualMachineConfiguration().
		WithPlatform(vz.NewMacPlatformConfiguration().WithHardwareModel(hardware).WithAuxiliaryStorage(aux).WithMachineIdentifier(id)).
		WithBootLoader(vz.NewMacOSBootLoader()).
		WithCPUCount(int(cfg.Resources.CPU.Default)).
		WithMemorySize(uint64(cfg.Resources.Memory.Default)).
		WithStorageDevices(vz.NewVirtioBlockDeviceConfigurationWithAttachment(vz.StorageDeviceAttachmentFromID(obj.ID(disk)))).
		WithNetworkDevices(network).WithEntropyDevices(vz.NewVirtioEntropyDeviceConfiguration()).
		WithGraphicsDevices(vz.NewMacGraphicsDeviceConfiguration().WithDisplays(vz.NewMACGraphicsDisplayConfigurationWithWidthInPixelsHeightInPixelsPixelsPerInch(1024, 768, 72))).
		WithKeyboards(vz.NewUSBKeyboardConfiguration()).
		WithPointingDevices(vz.NewUSBScreenCoordinatePointingDeviceConfiguration())
	if err = calls.validate(config); err != nil {
		return nil, fmt.Errorf("validate native clone: %w", err)
	}
	q := serialqueue.New("io.weaveplatform.imageweave.prepared")
	m := &appleMachine{
		config:     config,
		calls:      calls,
		queue:      q,
		identifier: base64.StdEncoding.EncodeToString(id.DataRepresentation()),
		mac:        config.NetworkDevices()[0].MACAddress().String(),
	}
	calls.create(m)
	return m, nil
}
func (m *appleMachine) identity() (string, string) { return m.identifier, m.mac }
func (m *appleMachine) start(ctx context.Context, native bool) error {
	options := vz.NewMacOSVirtualMachineStartOptions()
	defer options.Release()
	if native {
		if m.calls.major() < 27 {
			return fmt.Errorf("%w: macOS 27 host required for native provisioning", ErrPrepared)
		}
		if err := m.calls.provision(options); err != nil {
			return fmt.Errorf("apple guest provisioning: %w", err)
		}
	}
	done := make(chan error, 1)
	m.calls.start(m, options, func(err error) { done <- err })
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("start Apple guest: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("macOS operation: %w", ctx.Err())
	}
}

func (m *appleMachine) stop(ctx context.Context, force bool) error {
	if force {
		done := make(chan error, 1)
		if m.calls.state(m) == vz.VirtualMachineStateStopped {
			return nil
		}
		m.calls.stop(m, func(err error) { done <- err })
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return fmt.Errorf("macOS operation: %w", ctx.Err())
		}
	}
	for {
		state := m.calls.state(m)
		if state == vz.VirtualMachineStateStopped {
			return nil
		}
		if state == vz.VirtualMachineStateError {
			return fmt.Errorf("%w: VM entered error state before shutdown", ErrPrepared)
		}
		if err := m.calls.wait(ctx, time.Second); err != nil {
			return err
		}
	}
}

func (m *appleMachine) close() error {
	if m.display != nil {
		m.display.close()
		m.display = nil
	}
	// These references must be released while the VM queue remains valid.
	m.calls.release(m)
	return nil
}

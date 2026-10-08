//go:build darwin && arm64

package macos

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"

	vz "github.com/deploymenttheory/go-bindings-macosplatform/bindings/frameworks/virtualization"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/obj"
	"github.com/deploymenttheory/go-bindings-macosplatform/bindings/runtime/purego"
	"github.com/deploymenttheory/go-bindings-macosplatform/opinionated/tools/grandcentraldispatch/mainthread"
	"github.com/deploymenttheory/go-bindings-macosplatform/opinionated/tools/grandcentraldispatch/serialqueue"
)

func init() { runtime.LockOSThread() }
func TestMain(m *testing.M) {
	done := make(chan int, 1)
	go func() { done <- m.Run() }()
	for {
		select {
		case code := <-done:
			os.Exit(code)
		default:
			mainthread.PumpMainRunLoop(0.01)
		}
	}
}

var testClassNumber atomic.Uint64

func objcClass(t *testing.T, methods []purego.MethodDef) purego.Class {
	t.Helper()
	c, err := purego.RegisterClass(
		fmt.Sprintf("ImageweaveTest%v", testClassNumber.Add(1)),
		purego.GetClass("NSObject"),
		nil,
		nil,
		methods,
	)
	must(t, err)
	return c
}

// Exercise real Objective-C dispatch, block callbacks and serial queue use
// against a test object instead of launching a guest on the CI runner.
func TestNativeLifecycleBindings(t *testing.T) {
	c := objcClass(t, []purego.MethodDef{
		{
			Cmd: purego.RegisterName("startWithOptions:completionHandler:"),
			Fn: func(_ purego.ID, _ purego.SEL, options purego.ID, completion purego.Block) {
				if options == 0 {
					panic("missing start options")
				}
				completion.Invoke(purego.ID(0))
			},
		},
		{
			Cmd: purego.RegisterName("stopWithCompletionHandler:"),
			Fn:  func(_ purego.ID, _ purego.SEL, completion purego.Block) { completion.Invoke(purego.ID(0)) },
		},
		{
			Cmd: purego.RegisterName("state"),
			Fn:  func(purego.ID, purego.SEL) int64 { return int64(vz.VirtualMachineStateRunning) },
		},
	})
	id := purego.ID(c).Send(purego.RegisterName("new"))
	vm := vz.VirtualMachineFromID(id)
	id.Send(purego.RegisterName("release"))
	m := &appleMachine{
		vm:     vm,
		config: vz.NewVirtualMachineConfiguration(),
		queue:  serialqueue.New("io.weaveplatform.imageweave.binding-test"),
	}
	calls := nativeAppleCalls()
	o := vz.NewMacOSVirtualMachineStartOptions()
	defer o.Release()
	completed := make(chan error, 1)
	calls.start(m, o, func(err error) { completed <- err })
	must(t, <-completed)
	calls.stop(m, func(err error) { completed <- err })
	must(t, <-completed)
	if calls.state(m) != vz.VirtualMachineStateRunning {
		t.Fatal("state selector mismatch")
	}
	calls.release(m)
}

func TestNativeDisplayBindings(t *testing.T) {
	var assigned purego.ID
	started, stopped := false, false
	serverClass := objcClass(t, []purego.MethodDef{
		{
			Cmd: purego.RegisterName("initWithPort:queue:securityConfiguration:"),
			Fn: func(self purego.ID, _ purego.SEL, port uint16, queue uintptr, security purego.ID) purego.ID {
				if port != 0 || queue == 0 || security == 0 {
					panic("invalid VNC configuration")
				}
				return self
			},
		},
		{
			Cmd: purego.RegisterName("setVirtualMachine:"),
			Fn:  func(_ purego.ID, _ purego.SEL, vm purego.ID) { assigned = vm },
		},
		{Cmd: purego.RegisterName("start"), Fn: func(purego.ID, purego.SEL) { started = true }},
		{Cmd: purego.RegisterName("stop"), Fn: func(purego.ID, purego.SEL) { stopped = true }},
		{Cmd: purego.RegisterName("port"), Fn: func(purego.ID, purego.SEL) uint16 { return 5907 }},
	})
	securityClass := objcClass(
		t,
		[]purego.MethodDef{
			{
				Cmd: purego.RegisterName("initWithPassword:"),
				Fn: func(self purego.ID, _ purego.SEL, password purego.ID) purego.ID {
					if purego.GoString(password) != "testpass" {
						panic("wrong VNC password")
					}
					return self
				},
			},
		},
	)
	id := purego.ID(purego.GetClass("NSObject")).Send(purego.RegisterName("new"))
	vm := vz.VirtualMachineFromID(id)
	id.Send(purego.RegisterName("release"))
	defer vm.Release()
	m := &appleMachine{vm: vm}
	server, err := createVNCWith(m, "testpass", func(name string) purego.Class {
		if name == "_VZVNCServer" {
			return serverClass
		}
		return securityClass
	})
	must(t, err)
	calls := nativeDisplayCalls(m)
	if !started || assigned != obj.ID(vm) || calls.port(server) != 5907 {
		t.Fatal("native VNC selectors changed")
	}
	calls.stop(server)
	if !stopped {
		t.Fatal("native VNC did not stop")
	}
	if _, err := createVNCWith(m, "testpass", func(string) purego.Class { return 0 }); err == nil {
		t.Fatal("missing private API accepted")
	}
}

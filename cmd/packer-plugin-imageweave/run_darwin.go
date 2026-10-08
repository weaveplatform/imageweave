//go:build darwin && arm64

package main

import (
	"runtime"

	"github.com/deploymenttheory/go-bindings-macosplatform/opinionated/tools/grandcentraldispatch/mainthread"
)

func init() { runtime.LockOSThread() }
func runMain(work func()) {
	done := make(chan struct{})
	go func() { defer close(done); work() }()
	for {
		select {
		case <-done:
			return
		default:
			mainthread.PumpMainRunLoop(0.05)
		}
	}
}

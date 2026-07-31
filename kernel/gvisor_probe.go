//go:build tamago && arm64

package main

import (
	"sync"

	"github.com/spr-networks/spr-gvisor-demo/kernel/gvisorplatform"
	"gvisor.dev/gvisor/pkg/ring0"
	sentrykernel "gvisor.dev/gvisor/pkg/sentry/kernel"
)

var gvisorSentry sentrykernel.Kernel
var gvisorPlatform *gvisorplatform.Platform
var gvisorStatusMu sync.RWMutex
var gvisorEL0Output string
var gvisorStage = "platform"
var gvisorSentryError string

func setGVisorStage(stage string) {
	gvisorStatusMu.Lock()
	gvisorStage = stage
	gvisorStatusMu.Unlock()
}

func setGVisorOutput(output string) {
	gvisorStatusMu.Lock()
	gvisorEL0Output = output
	gvisorStatusMu.Unlock()
}

func setGVisorFailure(err string) {
	gvisorStatusMu.Lock()
	gvisorSentryError = err
	gvisorStage = "failed"
	gvisorStatusMu.Unlock()
}

func gvisorStatusSnapshot() (stage, failure, output string) {
	gvisorStatusMu.RLock()
	defer gvisorStatusMu.RUnlock()
	return gvisorStage, gvisorSentryError, gvisorEL0Output
}

// gvisorKernelProbe exercises gVisor's ARM64 ring-0 address validation in the
// direct-boot image. It is intentionally retained after initialization so the
// linker cannot discard the gVisor kernel package while the platform backend
// is brought up.
func gvisorKernelProbe() bool {
	if !ring0.IsCanonical(0x400000) {
		return false
	}
	var err error
	gvisorPlatform, err = gvisorplatform.New()
	if err != nil {
		return false
	}
	output, err := gvisorPlatform.RunEL0Smoke()
	setGVisorOutput(output)
	return err == nil && output == sentryHello
}

func startGVisorSentry() {
	setGVisorStage("sentry-starting")
	output, err := runSentryHello()
	if err != nil {
		setGVisorFailure(err.Error())
		return
	}
	setGVisorOutput(output)
	if output != sentryHello || gvisorSentry.GlobalInit() == nil {
		setGVisorFailure("Sentry task returned an invalid result")
		return
	}
	setGVisorStage("ready")
}

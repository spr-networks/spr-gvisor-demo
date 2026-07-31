//go:build tamago && arm64

package main

import (
	"unsafe"
	_ "unsafe"

	"github.com/usbarmory/tamago/arm64"
	"github.com/usbarmory/tamago/dma"
)

const (
	ramBase  = 0x80000000
	ramBytes = 0x0c000000 // 192 MiB managed by the Go runtime
	dmaBase  = 0x8c000000
	dmaBytes = 0x02000000 // 32 MiB reserved for virtio queues

	pl011Base = 0x0a000000
	pl011DR   = 0x000
	pl011FR   = 0x018
	pl011TXFF = 1 << 5
)

var (
	CPU = &arm64.CPU{}

	// The demo has no entropy device dependency. This deterministic seed is
	// sufficient for runtime hash randomization but is not cryptographic RNG.
	randomState uint64 = 0x5350522d54414d41
)

func psciSystemOff()

func terminate(_ int32) {
	// libkrun advertises PSCI 0.2 with the HVC conduit. A fatal kernel error
	// should stop the VM instead of leaving its only vCPU in a busy loop.
	psciSystemOff()
	for {
	}
}

//go:linkname runtimeExit runtime/goos.Exit
var runtimeExit = terminate

//go:linkname runtimeRAMStart runtime/goos.RamStart
var runtimeRAMStart uint64 = ramBase

//go:linkname runtimeRAMSize runtime/goos.RamSize
var runtimeRAMSize uint64 = ramBytes

//go:linkname runtimeStackOffset runtime/goos.RamStackOffset
var runtimeStackOffset uint64 = 0x1000

//go:linkname boardInit runtime/goos.Hwinit1
func boardInit() {
	CPU.Init()
	CPU.EnableCache()
	CPU.InitGenericTimers(0, 0)
}

//go:linkname nanotime runtime/goos.Nanotime
func nanotime() int64 {
	return CPU.GetTime()
}

//go:linkname initRNG runtime/goos.InitRNG
func initRNG() {}

//go:linkname getRandomData runtime/goos.GetRandomData
func getRandomData(buf []byte) {
	for i := range buf {
		randomState ^= randomState << 13
		randomState ^= randomState >> 7
		randomState ^= randomState << 17
		buf[i] = byte(randomState)
	}
}

func read32(addr uintptr) uint32 {
	return *(*uint32)(unsafe.Pointer(addr))
}

func write32(addr uintptr, value uint32) {
	*(*uint32)(unsafe.Pointer(addr)) = value
}

//go:linkname printk runtime/goos.Printk
func printk(c byte) {
	for read32(pl011Base+pl011FR)&pl011TXFF != 0 {
	}
	write32(pl011Base+pl011DR, uint32(c))
}

func init() {
	dma.Init(dmaBase, dmaBytes)
}

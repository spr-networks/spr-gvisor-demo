//go:build tamago && arm64

// Package gvisorplatform runs gVisor application contexts directly at EL0.
// libkrun provides EL1 and the virtio devices; this package is the platform
// backend that a normal runsc process gets from KVM or ptrace on Linux.
package gvisorplatform

import (
	"fmt"
	"sync"
	"unsafe"

	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/ring0"
	"gvisor.dev/gvisor/pkg/ring0/pagetables"
	"gvisor.dev/gvisor/pkg/sentry/arch"
	"gvisor.dev/gvisor/pkg/sentry/memmap"
	"gvisor.dev/gvisor/pkg/sentry/platform"
)

const (
	ramStart = uintptr(0x80000000)
	ramSize  = uintptr(256 << 20)

	// Matches gVisor's ARM64 KVM configuration: 48-bit TTBR0/TTBR1, 4 KiB
	// pages, inner-shareable WBWA walks, 40-bit physical addresses and TBI0.
	tcrEL1 = uintptr(0x32b5103510)
	// Attr0=normal WB, Attr1=normal non-cacheable, Attr2=device nGnRnE.
	mairEL1 = uintptr(0x000044ff)
)

// Platform is a single-vCPU, direct EL0 implementation of platform.Platform.
type Platform struct {
	platform.NoCPUPreemptionDetection
	platform.NoCPUNumbers

	mu        sync.Mutex
	allocator *pagetables.RuntimeAllocator
	upper     *pagetables.PageTables
	kernelPT  *pagetables.PageTables
	switcher  switchState
	exStack   [4096]byte
}

// switchState is shared with registers_arm64.s. Keep offsets synchronized
// with the constants in that file.
type switchState struct {
	UserRegs    [31]uint64
	UserSP      uint64
	UserPC      uint64
	UserPstate  uint64
	KernelTTBR  uint64
	AppTTBR     uint64
	KernelSP    uint64
	ExceptionSP uint64
	KernelRegs  [12]uint64 // x19-x30
	ESR         uint64
	FAR         uint64
	Vector      uint64
	UserTLS     uint64
}

// New installs the 48-bit split address space used by ring0. Low addresses
// remain identity-mapped for TamaGo and virtio MMIO; RAM is also mapped at
// ring0.KernelStartAddress so exception entry remains reachable after TTBR0
// switches to an application address space.
func New() (*Platform, error) {
	arch.ConfigureAddressSpace(1 << 48)

	p := &Platform{allocator: pagetables.NewRuntimeAllocator()}
	p.upper = pagetables.New(p.allocator)
	p.upper.Map(
		hostarch.Addr(ring0.KernelStartAddress|ramStart),
		ramSize,
		pagetables.MapOpts{AccessType: hostarch.AnyAccess, Global: true, MemoryType: hostarch.MemoryTypeWriteBack},
		ramStart,
	)
	p.upper.MarkReadOnlyShared()

	p.kernelPT = pagetables.NewWithUpper(p.allocator, p.upper, ring0.KernelStartAddress)
	// Device space required by the krun virtio transports.
	p.kernelPT.Map(0, ramStart, pagetables.MapOpts{
		AccessType: hostarch.AnyAccess,
		Global:     true,
		MemoryType: hostarch.MemoryTypeUncached,
	}, 0)
	// Direct-boot RAM. Physical and TamaGo virtual addresses are identical.
	p.kernelPT.Map(hostarch.Addr(ramStart), ramSize, pagetables.MapOpts{
		AccessType: hostarch.AnyAccess,
		Global:     true,
		MemoryType: hostarch.MemoryTypeWriteBack,
	}, ramStart)
	// Preserve access to the rest of the 32-bit MMIO window.
	p.kernelPT.Map(hostarch.Addr(ramStart+ramSize), uintptr(1<<32)-ramStart-ramSize, pagetables.MapOpts{
		AccessType: hostarch.AnyAccess,
		Global:     true,
		MemoryType: hostarch.MemoryTypeUncached,
	}, ramStart+ramSize)

	installMMU(
		uintptr(p.kernelPT.TTBR0_EL1(false, 1)),
		uintptr(p.kernelPT.TTBR1_EL1(false, 1)),
		tcrEL1,
		mairEL1,
		ring0.KernelStartAddress|addrOfVectors(),
		ring0.KernelStartAddress|uintptr(unsafe.Pointer(&p.switcher)),
	)
	return p, nil
}

func (*Platform) SupportsAddressSpaceIO() bool  { return false }
func (*Platform) HaveGlobalMemoryBarrier() bool { return false }
func (*Platform) GlobalMemoryBarrier() error    { panic("global memory barrier unsupported") }
func (*Platform) MapUnit() uint64               { return 16 << 20 }
func (*Platform) MinUserAddress() hostarch.Addr { return hostarch.PageSize }
func (*Platform) MaxUserAddress() hostarch.Addr { return hostarch.Addr(ring0.MaximumUserAddress) }
func (*Platform) ConcurrencyCount() int         { return 1 }
func (*Platform) SeccompInfo() platform.SeccompInfo {
	return platform.StaticSeccompInfo{PlatformName: "tamago-ring0"}
}

func (p *Platform) NewAddressSpace() (platform.AddressSpace, error) {
	return &addressSpace{
		p:  p,
		pt: pagetables.NewWithUpper(p.allocator, p.upper, ring0.KernelStartAddress),
	}, nil
}

func (p *Platform) NewContext(context.Context) platform.Context {
	return &platformContext{p: p}
}

// RunEL0Smoke executes a tiny Linux/AArch64 program with two SVC instructions
// (write and exit). It is used as an early boot assertion for the same ring0
// transition and page-table path used by Sentry tasks.
func (p *Platform) RunEL0Smoke() (string, error) {
	const userBase = hostarch.Addr(0x400000)
	const dataBase = hostarch.Addr(0x500000)
	message := []byte("Hello World from gVisor Sentry!\n")
	storage := make([]byte, 2*hostarch.PageSize)
	start := uintptr(unsafe.Pointer(&storage[0]))
	offset := -start & uintptr(hostarch.PageSize-1)
	page := storage[offset : offset+hostarch.PageSize]

	copy(page, message)
	entryPhysical := addrOfSmokeEntry()
	entryPage := entryPhysical &^ uintptr(hostarch.PageSize-1)
	entryOffset := entryPhysical - entryPage

	as, err := p.NewAddressSpace()
	if err != nil {
		return "", err
	}
	bmas := as.(*addressSpace)
	bmas.pt.Map(userBase, hostarch.PageSize, pagetables.MapOpts{
		AccessType: hostarch.ReadExecute,
		User:       true,
		MemoryType: hostarch.MemoryTypeWriteBack,
	}, entryPage)
	bmas.pt.Map(dataBase, hostarch.PageSize, pagetables.MapOpts{
		AccessType: hostarch.ReadWrite,
		User:       true,
		MemoryType: hostarch.MemoryTypeWriteBack,
	}, uintptr(unsafe.Pointer(&page[0])))
	defer as.Release()

	ac := arch.New(arch.ARM64)
	ac.SetIP(uintptr(userBase) + entryOffset)
	ac.SetStack(uintptr(dataBase + hostarch.PageSize - 16))

	var output []byte
	for exits := 0; exits < 4; exits++ {
		vector := p.switchToUser(bmas, ac)
		if vector != ring0.Syscall {
			return "", fmt.Errorf("EL0 smoke exception vector %d", vector)
		}
		ac.SyscallSaveOrig()
		args := ac.SyscallArgs()
		switch ac.SyscallNo() {
		case 64: // write
			addr, length := hostarch.Addr(args[1].Value), int(args[2].Value)
			if args[0].Value != 1 || addr < dataBase || addr+hostarch.Addr(length) > dataBase+hostarch.PageSize {
				return "", fmt.Errorf("invalid write(%d, %#x, %d)", args[0].Value, addr, length)
			}
			pageOffset := int(addr - dataBase)
			output = append(output, page[pageOffset:pageOffset+length]...)
			ac.SetReturn(uintptr(length))
		case 93: // exit
			return string(output), nil
		default:
			return "", fmt.Errorf("unexpected syscall %d", ac.SyscallNo())
		}
	}
	return "", fmt.Errorf("EL0 smoke did not exit")
}

type addressSpace struct {
	platform.NoAddressSpaceIO
	p  *Platform
	mu sync.Mutex
	pt *pagetables.PageTables
}

func (as *addressSpace) MapFile(addr hostarch.Addr, f memmap.File, fr memmap.FileRange, at hostarch.AccessType, precommit bool) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	blocks, err := f.MapInternal(fr, hostarch.AccessType{Read: at.Read || at.Execute || precommit, Write: at.Write})
	if err != nil {
		return err
	}
	mt := f.MemoryType()
	for !blocks.IsEmpty() {
		block := blocks.Head()
		length := uintptr(block.Len())
		if block.Addr()&uintptr(hostarch.PageSize-1) != 0 || length&uintptr(hostarch.PageSize-1) != 0 {
			return fmt.Errorf("unaligned application backing: addr=%#x len=%#x", block.Addr(), length)
		}
		as.pt.Map(addr, length, pagetables.MapOpts{AccessType: at, User: true, MemoryType: mt}, block.Addr())
		addr += hostarch.Addr(length)
		blocks = blocks.Tail()
	}
	ring0.FlushTlbAll()
	return nil
}

func (as *addressSpace) Unmap(addr hostarch.Addr, length uint64) {
	as.mu.Lock()
	as.pt.Unmap(addr, uintptr(length))
	as.pt.Allocator.Recycle()
	as.mu.Unlock()
	ring0.FlushTlbAll()
}

func (as *addressSpace) Release() {
	as.Unmap(0, uint64(ring0.KernelStartAddress))
}
func (*addressSpace) PreFork()  {}
func (*addressSpace) PostFork() {}

type platformContext struct{ p *Platform }

func (c *platformContext) Switch(_ context.Context, mm platform.MemoryManager, ac *arch.Context64, _ int32) (*linux.SignalInfo, hostarch.AccessType, error) {
	as, ok := mm.AddressSpace().(*addressSpace)
	if !ok {
		return nil, hostarch.NoAccess, fmt.Errorf("unexpected address space %T", mm.AddressSpace())
	}

	vector := c.p.switchToUser(as, ac)

	switch vector {
	case ring0.Syscall:
		return nil, hostarch.NoAccess, nil
	case ring0.PageFault:
		info := &linux.SignalInfo{Signo: int32(linux.SIGSEGV), Code: 1} // SEGV_MAPERR
		info.SetAddr(c.p.switcher.FAR)
		return info, hostarch.ESRAccessType(c.p.switcher.ESR), platform.ErrContextSignal
	default:
		return nil, hostarch.NoAccess, fmt.Errorf("unexpected ARM64 exception vector %d", vector)
	}
}

func (p *Platform) switchToUser(as *addressSpace, ac *arch.Context64) ring0.Vector {
	p.mu.Lock()
	defer p.mu.Unlock()
	regs := ac.StateData().Regs
	copy(p.switcher.UserRegs[:], regs.Regs[:])
	p.switcher.UserSP = regs.Sp
	p.switcher.UserPC = regs.Pc
	p.switcher.UserPstate = regs.Pstate
	p.switcher.UserTLS = uint64(ac.TLS())
	p.switcher.KernelTTBR = p.kernelPT.TTBR0_EL1(false, 1)
	p.switcher.AppTTBR = as.pt.TTBR0_EL1(false, 2)
	p.switcher.ExceptionSP = uint64(ring0.KernelStartAddress | (uintptr(unsafe.Pointer(&p.exStack[0])) + uintptr(len(p.exStack))))
	p.switcher.Vector = 0
	p.switcher.ESR = 0
	p.switcher.FAR = 0
	ring0.FlushTlbAll()
	runUser(&p.switcher)
	copy(regs.Regs[:], p.switcher.UserRegs[:])
	regs.Sp = p.switcher.UserSP
	regs.Pc = p.switcher.UserPC
	regs.Pstate = p.switcher.UserPstate
	ac.StateData().Regs = regs
	ac.SetTLS(uintptr(p.switcher.UserTLS))

	switch (p.switcher.ESR >> 26) & 0x3f {
	case 0x15: // SVC64
		return ring0.Syscall
	case 0x20, 0x24: // instruction/data abort from lower EL
		return ring0.PageFault
	default:
		return ring0.El0SyncInv
	}
}

func (*platformContext) PullFullState(platform.AddressSpace, *arch.Context64) error { return nil }
func (*platformContext) FullStateChanged()                                          {}
func (*platformContext) Interrupt()                                                 {}
func (*platformContext) Preempt()                                                   {}
func (*platformContext) Release()                                                   {}
func (*platformContext) PrepareSleep()                                              {}
func (*platformContext) PrepareUninterruptibleSleep()                               {}
func (*platformContext) PrepareStop()                                               {}
func (*platformContext) PrepareExecve()                                             {}
func (*platformContext) PrepareExit()                                               {}
func (*platformContext) LastCPUNumber() int32                                       { return 0 }

// installMMU is implemented in registers_arm64.s.
func installMMU(ttbr0, ttbr1, tcr, mair, vbar, tpidr uintptr)

func runUser(state *switchState)
func addrOfVectors() uintptr
func addrOfSmokeEntry() uintptr

// smokeEntry is implemented in registers_arm64.s and is never called with
// the Go ABI. It issues Linux/AArch64 write and exit system calls at EL0.
func smokeEntry()

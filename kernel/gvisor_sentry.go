//go:build tamago && arm64

package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/cpuid"
	"gvisor.dev/gvisor/pkg/fspath"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/sentry/fsimpl/cgroup2fs"
	"gvisor.dev/gvisor/pkg/sentry/fsimpl/pipefs"
	"gvisor.dev/gvisor/pkg/sentry/fsimpl/tmpfs"
	sentrykernel "gvisor.dev/gvisor/pkg/sentry/kernel"
	"gvisor.dev/gvisor/pkg/sentry/kernel/auth"
	"gvisor.dev/gvisor/pkg/sentry/limits"
	"gvisor.dev/gvisor/pkg/sentry/loader"
	"gvisor.dev/gvisor/pkg/sentry/pgalloc"
	_ "gvisor.dev/gvisor/pkg/sentry/syscalls/linux"
	sentrytime "gvisor.dev/gvisor/pkg/sentry/time"
	"gvisor.dev/gvisor/pkg/sentry/vfs"
	"gvisor.dev/gvisor/pkg/usermem"
)

const sentryHello = "Hello World from gVisor Sentry!\n"

// directClocks supplies Sentry time without relying on Linux host syscalls.
// VDSO calibration is intentionally disabled; syscall time reads use GetTime.
type directClocks struct {
	started time.Time
}

func (*directClocks) Update(bool) (sentrytime.Parameters, bool, sentrytime.Parameters, bool) {
	return sentrytime.Parameters{}, false, sentrytime.Parameters{}, false
}

func (c *directClocks) GetTime(id sentrytime.ClockID) (int64, error) {
	switch id {
	case sentrytime.Monotonic:
		return time.Since(c.started).Nanoseconds(), nil
	case sentrytime.Realtime:
		return time.Now().UnixNano(), nil
	default:
		return 0, fmt.Errorf("unsupported clock %d", id)
	}
}

// runSentryHello creates a real Sentry task from an embedded Linux/AArch64
// ELF, runs it at EL0 through gvisorplatform, and returns bytes written by the
// task through gVisor's write(2) implementation.
func runSentryHello() (string, error) {
	setGVisorStage("memory-file")
	mf := pgalloc.NewMemoryFileBareMetal()
	gvisorSentry.Platform = gvisorPlatform
	gvisorSentry.SetMemoryFile(mf)

	setGVisorStage("vdso")
	vdso, err := loader.PrepareVDSO(mf)
	if err != nil {
		return "", fmt.Errorf("prepare VDSO: %w", err)
	}
	params := sentrykernel.NewVDSOParamPage(mf, vdso.ParamPage.FileRange())
	setGVisorStage("timekeeper")
	tk := sentrykernel.NewTimekeeper()
	tk.SetClocks(&directClocks{started: time.Now()}, params)

	userNS := auth.NewRootUserNamespace()
	creds := auth.NewRootCredentials(userNS)
	setGVisorStage("kernel-init")
	if err := gvisorSentry.Init(sentrykernel.InitKernelArgs{
		FeatureSet:        cpuid.HostFeatureSet().Fixed(),
		Timekeeper:        tk,
		RootUserNamespace: userNS,
		ApplicationCores:  1,
		Vdso:              vdso,
		VdsoParams:        params,
		RootUTSNamespace:  sentrykernel.NewUTSNamespace("spr-gvisor-demo", "", userNS),
		RootIPCNamespace:  sentrykernel.NewIPCNamespace(userNS),
		RootPIDNamespace:  sentrykernel.NewRootPIDNamespace(userNS),
		Cgroup2FSInit:     cgroup2fs.NewFilesystem,
	}); err != nil {
		return "", fmt.Errorf("initialize Sentry kernel: %w", err)
	}

	ctx := gvisorSentry.SupervisorContext()
	setGVisorStage("rootfs")
	gvisorSentry.VFS().MustRegisterFilesystemType(tmpfs.Name, &tmpfs.FilesystemType{}, &vfs.RegisterFilesystemTypeOptions{
		AllowUserMount: true,
		AllowUserList:  true,
	})
	mntns, err := gvisorSentry.VFS().NewMountNamespace(ctx, creds, "root", tmpfs.Name, &vfs.MountOptions{}, &gvisorSentry)
	if err != nil {
		return "", fmt.Errorf("mount Sentry root: %w", err)
	}
	root := mntns.Root(ctx)
	pop := vfs.PathOperation{
		Root:               root,
		Start:              root,
		Path:               fspath.Parse("hello"),
		FollowFinalSymlink: true,
	}
	execFD, err := gvisorSentry.VFS().OpenAt(ctx, creds, &pop, &vfs.OpenOptions{
		Flags: linux.O_WRONLY | linux.O_CREAT | linux.O_EXCL,
		Mode:  0755,
	})
	root.DecRef(ctx)
	if err != nil {
		mntns.DecRef(ctx)
		return "", fmt.Errorf("create embedded executable: %w", err)
	}
	elf := linuxHelloELF([]byte(sentryHello))
	if n, err := execFD.Write(ctx, usermem.BytesIOSequence(elf), vfs.WriteOptions{}); err != nil || n != int64(len(elf)) {
		execFD.DecRef(ctx)
		mntns.DecRef(ctx)
		return "", fmt.Errorf("write embedded executable: wrote %d/%d: %v", n, len(elf), err)
	}
	execFD.DecRef(ctx)

	setGVisorStage("stdio")
	readFD, writeFD, err := pipefs.NewConnectedPipeFDs(ctx, gvisorSentry.PipeMount(), 0)
	if err != nil {
		mntns.DecRef(ctx)
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}
	fdTable := gvisorSentry.NewFDTable()
	if displaced, err := fdTable.NewFDAt(ctx, 1, writeFD, sentrykernel.FDFlags{}); err != nil {
		return "", fmt.Errorf("install stdout: %w", err)
	} else if displaced != nil {
		displaced.DecRef(ctx)
	}
	if displaced, err := fdTable.NewFDAt(ctx, 2, writeFD, sentrykernel.FDFlags{}); err != nil {
		return "", fmt.Errorf("install stderr: %w", err)
	} else if displaced != nil {
		displaced.DecRef(ctx)
	}
	writeFD.DecRef(ctx)

	limitSet, err := limits.NewLinuxLimitSet()
	if err != nil {
		return "", fmt.Errorf("create task limits: %w", err)
	}
	setGVisorStage("elf-load")
	tg, _, err := gvisorSentry.CreateProcess(sentrykernel.CreateProcessArgs{
		Filename:             "/hello",
		Argv:                 []string{"/hello"},
		Envv:                 []string{"PATH=/"},
		WorkingDirectory:     "/",
		Credentials:          creds,
		FDTable:              fdTable,
		Umask:                0022,
		Limits:               limitSet,
		MaxSymlinkTraversals: linux.MaxSymlinkTraversals,
		UTSNamespace:         gvisorSentry.RootUTSNamespace(),
		IPCNamespace:         gvisorSentry.RootIPCNamespace(),
		PIDNamespace:         gvisorSentry.RootPIDNamespace(),
		MountNamespace:       mntns,
		ContainerID:          "spr-gvisor-demo",
	})
	fdTable.DecRef(ctx)
	if err != nil {
		readFD.DecRef(ctx)
		return "", fmt.Errorf("create Sentry task: %w", err)
	}
	setGVisorStage("task-running")
	if err := gvisorSentry.Start(); err != nil {
		readFD.DecRef(ctx)
		return "", fmt.Errorf("start Sentry task: %w", err)
	}
	tg.WaitExited()
	setGVisorStage("task-exited")
	if status := tg.ExitStatus(); !status.Exited() || status.ExitStatus() != 0 {
		readFD.DecRef(ctx)
		return "", fmt.Errorf("Sentry task exited with status %#x", uint32(status))
	}

	buf := make([]byte, len(sentryHello))
	n, err := readFD.Read(ctx, usermem.BytesIOSequence(buf), vfs.ReadOptions{})
	readFD.DecRef(ctx)
	if err != nil {
		return "", fmt.Errorf("read Sentry stdout: %w", err)
	}
	return string(buf[:n]), nil
}

// linuxHelloELF constructs a deterministic static ET_EXEC containing only
// write(1, message) and exit(0). The Sentry still performs normal ELF loading,
// memory mapping, task setup and Linux syscall dispatch.
func linuxHelloELF(message []byte) []byte {
	const (
		loadAddr = uint64(0x400000)
		entryOff = uint64(0x80)
	)
	codeWords := []uint32{
		movz(0, 1), // x0 = stdout
		0,          // adr x1, message (filled below)
		movz(2, uint16(len(message))),
		movz(8, 64), // __NR_write
		0xd4000001,  // svc #0
		movz(0, 0),
		movz(8, 93), // __NR_exit
		0xd4000001,  // svc #0
	}
	messageOff := entryOff + uint64(4*len(codeWords))
	codeWords[1] = adr(1, int64(messageOff-(entryOff+4)))
	fileSize := messageOff + uint64(len(message))
	elf := make([]byte, fileSize)
	copy(elf[0:16], []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(elf[16:18], 2)   // ET_EXEC
	binary.LittleEndian.PutUint16(elf[18:20], 183) // EM_AARCH64
	binary.LittleEndian.PutUint32(elf[20:24], 1)
	binary.LittleEndian.PutUint64(elf[24:32], loadAddr+entryOff)
	binary.LittleEndian.PutUint64(elf[32:40], 64)
	binary.LittleEndian.PutUint16(elf[52:54], 64)
	binary.LittleEndian.PutUint16(elf[54:56], 56)
	binary.LittleEndian.PutUint16(elf[56:58], 1)
	// Single executable PT_LOAD containing the ELF header and payload.
	binary.LittleEndian.PutUint32(elf[64:68], 1)
	binary.LittleEndian.PutUint32(elf[68:72], 4|1) // PF_R | PF_X
	binary.LittleEndian.PutUint64(elf[72:80], 0)
	binary.LittleEndian.PutUint64(elf[80:88], loadAddr)
	binary.LittleEndian.PutUint64(elf[88:96], loadAddr)
	binary.LittleEndian.PutUint64(elf[96:104], fileSize)
	binary.LittleEndian.PutUint64(elf[104:112], fileSize)
	binary.LittleEndian.PutUint64(elf[112:120], hostarch.PageSize)
	for i, word := range codeWords {
		binary.LittleEndian.PutUint32(elf[int(entryOff)+4*i:], word)
	}
	copy(elf[messageOff:], message)
	return elf
}

func movz(reg uint32, imm uint16) uint32 {
	return 0xd2800000 | uint32(imm)<<5 | reg
}

func adr(reg uint32, delta int64) uint32 {
	imm := uint32(delta) & 0x1fffff
	return 0x10000000 | (imm&3)<<29 | ((imm>>2)&0x7ffff)<<5 | reg
}

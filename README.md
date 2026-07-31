# spr-gvisor-demo

[![Build and verify](https://github.com/spr-networks/spr-gvisor-demo/actions/workflows/ci.yml/badge.svg)](https://github.com/spr-networks/spr-gvisor-demo/actions/workflows/ci.yml)

A single-service SPR plugin that boots gVisor Sentry directly as the
application kernel under krun. There is no Linux kernel, Linux init process,
guest distribution, gateway, or sidecar service.

The demo starts a real gVisor task from an embedded static Linux/AArch64 ELF.
gVisor loads the ELF, creates its address space and task, resolves page faults,
and handles the task's `write(2)` and `exit(2)` syscalls. The captured task
output is displayed by the plugin UI:

```text
Hello World from gVisor Sentry!
```

## Architecture

```text
SPR API
  -> /state/plugins/spr-gvisor-demo/socket.sock
  -> libkrun VirtIO-vsock port 4040
  -> one direct-boot ARM64 guest image
       EL1: TamaGo boot/runtime + gVisor Sentry application kernel
       EL0: embedded Linux/AArch64 hello task
```

krun remains the VMM. TamaGo supplies the minimal bare-metal Go runtime,
early ARM64 setup, and VirtIO MMIO transport needed to enter Go without a
Linux guest. gVisor Sentry is linked into that image and provides the Linux
application-kernel semantics. The custom `gvisorplatform` backend replaces
gVisor's normal host KVM/ptrace entry path with a direct EL1/EL0 context switch
and gVisor page tables.

This is not gVisor running as Linux userspace and it is not `runsc` inside a
microVM. The Linux program is an EL0 task of Sentry; no Linux kernel is present.

## What is in the image

The one `scratch` image contains:

- `/gvisor-kernel`, the raw ARM64 direct-boot image;
- `/unused`, the corresponding ELF retained for inspection; and
- `/.krun_vm.json`, an upstream-runtime test hint.

The image contains no Linux root filesystem or executable guest userspace.
The container command is deliberately `/unused`: SPR's trusted krun policy
selects `/gvisor-kernel` before a container process can execute.

The direct image includes:

- the pinned TamaGo compiler and runtime;
- the pinned gVisor Sentry source;
- a bare-metal gVisor memory-file implementation backed by page-aligned Go
  heap chunks instead of Linux `memfd`/`mmap`;
- an ARM64 EL1/EL0 platform backend;
- gVisor tmpfs, ELF loader, task model, FD table, pipe, and Linux syscall table;
- the ARM64 gVisor VDSO and a direct clock source; and
- the in-tree VirtIO-vsock HTTP server used by SPR.

## SPR runtime prerequisite

The plugin uses the standard SPR UI upcall annotations:

```yaml
krun.vsock_path: /state/plugins/spr-gvisor-demo/socket.sock
krun.vsock_port: "4040"
```

It also asks SPR's trusted krun policy to direct-boot the raw image:

```yaml
krun.kernel_path: /gvisor-kernel
krun.kernel_format: "0"
```

The [`tamago` branch of `spr-networks/super`](https://github.com/spr-networks/super/tree/tamago)
adds these external-kernel policy fields to `superd`. Rebuild `superd` from
that branch. No additional gVisor-specific superd changes are required.

Image-controlled `/.krun_vm.json` is intentionally ignored by SPR's hardened
runtime. It is retained only for equivalent testing with an unmodified
upstream krun runtime.

## Build

The builder requires an ARM64 target and Docker Buildx:

```sh
./build_docker_compose.sh
```

This builds and loads:

```text
ghcr.io/spr-networks/spr-gvisor-demo:latest
```

Publish it with:

```sh
./build_docker_compose.sh --push
```

The base image, Go version, TamaGo module/compiler commits, gVisor pseudo
version and commit, and x/sys version are recorded in `reproducible.env`.
The builder copies the pinned modules to temporary directories and applies
the checked-in TamaGo/gVisor/x/sys overlays there; downloaded module sources
are never modified in place.

GitHub Actions performs source tests, two clean reproducibility builds, and
publishes commit-addressed and `latest` images from `main`.

## Run in SPR

Install this repository through **Plugins → + New Plugin**. `plugin.json`
selects the KVM runtime and adds `spr-gvisor-demo` to the sidebar.
`docker-compose-kvm.yml` contains exactly one service and does not configure a
Docker network, TAP device, fixed IP, gateway, or second Linux service.

Open **spr-gvisor-demo** in the sidebar. The page is served by the direct guest
over VirtIO-vsock and shows the output captured from the gVisor task.

The status endpoint should report:

```json
{
  "kernel": "gvisor-sentry",
  "substrate": "tamago",
  "linux_kernel": false,
  "gvisor": "ready",
  "output": "Hello World from gVisor Sentry!\n",
  "ipc": "virtio-vsock",
  "port": 4040
}
```

Responses include `X-GVisor-Kernel: true`.

## Verify

Run the unit, manifest, Compose, overlay, and reproducible-input checks:

```sh
./test.sh
```

Run the two-clean-build comparison with:

```sh
./reproducibility_test.sh
```

The direct local build sequence used by the container is:

```sh
work_dir="$(mktemp -d)"
tamago_dir="$(go list -m -f '{{.Dir}}' github.com/usbarmory/tamago)"
gvisor_dir="$(go list -m -f '{{.Dir}}' gvisor.dev/gvisor)"
xsys_dir="$(go list -m -f '{{.Dir}}' golang.org/x/sys)"

go run ./tools/prepare_tamago.go \
  -tamago-dir "${tamago_dir}" -out-dir "${work_dir}/tamago"
go run ./tools/prepare_gvisor.go \
  -gvisor-dir "${gvisor_dir}" -xsys-dir "${xsys_dir}" \
  -out-gvisor "${work_dir}/gvisor" -out-xsys "${work_dir}/xsys"

cp go.mod "${work_dir}/kernel.mod"
cp go.sum "${work_dir}/kernel.sum"
go mod edit -modfile="${work_dir}/kernel.mod" \
  -replace=github.com/usbarmory/tamago="${work_dir}/tamago" \
  -replace=gvisor.dev/gvisor="${work_dir}/gvisor" \
  -replace=golang.org/x/sys="${work_dir}/xsys"

GOOS=tamago GOOSPKG=github.com/usbarmory/tamago GOARCH=arm64 \
  "${TAMAGO}" build -modfile="${work_dir}/kernel.mod" \
  -buildvcs=false -trimpath -tags=tamago \
  -ldflags='-T 0x80010000 -R 0x1000 -s -w -buildid=' \
  -o "${work_dir}/gvisor-kernel.elf" ./kernel

go run ./tools/elf2raw.go \
  -in "${work_dir}/gvisor-kernel.elf" \
  -out gvisor-kernel -base 0x80000000
```

`TAMAGO` above is the `go tool -n github.com/usbarmory/tamago/cmd/tamago`
compiler command produced by the pinned TamaGo Go toolchain.

## Demo scope

This repository proves the direct-boot architecture and the complete SPR UI
path with a small static task. The compatibility shims intentionally return
unsupported errors for Linux-host integration features such as donated host
FDs, host epoll, host networking, and checkpoint host files. Expanding those
features requires native bare-metal backends; it does not require adding a
Linux guest or removing krun.

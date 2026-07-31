# spr-tamago-demo

[![Build and verify](https://github.com/spr-networks/spr-tamago-demo/actions/workflows/ci.yml/badge.svg)](https://github.com/spr-networks/spr-tamago-demo/actions/workflows/ci.yml)

A Hello World SPR plugin implemented as a single
[TamaGo](https://github.com/usbarmory/tamago) ARM64 kernel running under krun.
There is no Linux kernel, init process, guest userspace, or sidecar service.

The kernel itself terminates a VirtIO-vsock stream on port 4040 and serves the
plugin HTML and `/status` endpoint. SPR and its krun runtime map the plugin's
host Unix socket to that guest port:

```text
SPR API -> /state/plugins/spr-tamago-demo/socket.sock
        -> libkrun VirtIO-vsock port 4040
        -> TamaGo kernel HTTP handler
```

No guest or container IP address is configured by this plugin. The UI upcall
does not use TCP, a TAP device, or a Docker bridge.

## What is in the image

The one `scratch` image contains:

- a raw ARM64 TamaGo kernel at `/tamago-kernel`;
- the corresponding ELF at `/unused`, retained for inspection; and
- no Linux filesystem or executable userspace.

The Docker command is deliberately `/unused`: the trusted SPR krun policy
selects `/tamago-kernel` as the VM kernel before a container process could run.
If `/unused` is ever executed and exits with `SIGILL`/`SIGSEGV`, the
external-kernel policy was not supplied and the image was started as an
ordinary Linux process.

## SPR runtime prerequisite

SPR already supports the standard UI upcall annotations used here:

```yaml
krun.vsock_path: /state/plugins/spr-tamago-demo/socket.sock
krun.vsock_port: "4040"
```

Upstream crun/libkrun also support `kernel_path` and raw external kernels, but
SPR's hardened `spr-krun` runtime correctly ignores image-controlled
`/.krun_vm.json`. Manager-issued policy must authorize the kernel requested by
Compose:

```yaml
krun.kernel_path: /tamago-kernel
krun.kernel_format: "0"
```

The [`tamago` branch of `spr-networks/super`](https://github.com/spr-networks/super/tree/tamago)
adds those two trusted policy fields to `superd`. Use that branch and rebuild
the `superd` service. The runtime then supplies both the external raw kernel
and SPR-owned listening Unix socket to libkrun.

The checked-in `.krun_vm.json` is only an equivalent external-kernel hint for
testing with an unmodified upstream `krun` runtime. Hardened SPR ignores it.

## TamaGo and VirtIO

The kernel build uses the three settings required by TamaGo:

```text
GOOS=tamago
GOARCH=arm64
GOOSPKG=github.com/usbarmory/tamago
```

The kernel uses TamaGo's generic VirtIO MMIO transport and split queues to
drive device ID 19, VirtIO-vsock. Its small in-tree stream implementation
handles connection setup, credit accounting, request reassembly, response
framing, and shutdown directly in bare-metal Go.

`usbarmory/go-net/virtio` is a VirtIO network-device driver (device ID 1). It
was appropriate for an earlier TCP prototype but is intentionally not used by
this direct vsock architecture.

TamaGo's current `kvm/virtio` directory also contains AMD64 PCI transport
files without architecture build constraints. The builder makes a temporary
copy of the pinned TamaGo module and replaces only those two PCI files with
ARM64 package stubs. The MMIO and queue implementations remain the upstream
pinned TamaGo code.

The kernel's fatal-exit path uses `PSCI_0_2_FN_SYSTEM_OFF` through the HVC
conduit advertised by libkrun. It no longer uses the incorrect SMC conduit.

This krun configuration does not attach a legacy PL011 serial device. The
TamaGo `Printk` hook is therefore deliberately silent instead of touching an
unmapped MMIO address; an empty `docker logs spr-tamago-demo` is expected.
The plugin's observable interface is its host Unix socket and vsock HTTP
service.

## Build

Build or publish the single ARM64 image:

```sh
./build_docker_compose.sh
./build_docker_compose.sh --push
```

The default tag is `ghcr.io/spr-networks/spr-tamago-demo:latest`. The builder
image, TamaGo module, and TamaGo compiler are pinned. The first build compiles
the matching TamaGo Go toolchain and can take several minutes.

The complete build environment and two-build digest check are documented in
[`REPRODUCIBLE_BUILDS.md`](REPRODUCIBLE_BUILDS.md). GitHub Actions verifies
pull requests and publishes immutable `sha-<commit>` tags plus `latest` from
`main`.

## Run in SPR

Install the standard `spr-krun-runtime`, run `superd` from the SPR `tamago`
branch above, and install this repository through **Plugins → + New Plugin**.
The supported launch path is SPR's plugin manager because it signs the trusted
runtime override and creates the SPR-owned Unix socket mapping.

`plugin.json` selects the KVM runtime and adds **spr-tamago-demo** to the
sidebar. `docker-compose-kvm.yml` contains exactly one `spr-krun` service and
declares no plugin network capability or fixed IP.

Open **spr-tamago-demo** in the SPR sidebar. A successful response includes
`X-TamaGo-Kernel: true`; `/status` reports `"role":"kernel"`,
`"linux":false`, and `"ipc":"virtio-vsock"`.

## Verify

Run the unit, manifest, Compose, source, and reproducible-input checks:

```sh
./test.sh
```

To build the kernel without Docker, use the matching TamaGo compiler wrapper:

```sh
BUILD_DIR="$(mktemp -d)"
TAMAGO_MODULE="$(go list -m -f '{{.Dir}}' github.com/usbarmory/tamago)"

go run ./tools/prepare_tamago.go \
  -tamago-dir "${TAMAGO_MODULE}" \
  -out-dir "${BUILD_DIR}/tamago-arm64"
cp go.mod "${BUILD_DIR}/kernel.mod"
cp go.sum "${BUILD_DIR}/kernel.sum"
go mod edit -modfile="${BUILD_DIR}/kernel.mod" \
  -replace=github.com/usbarmory/tamago="${BUILD_DIR}/tamago-arm64"

GOOS=tamago GOOSPKG=github.com/usbarmory/tamago GOARCH=arm64 \
  "${TAMAGO}" build -modfile="${BUILD_DIR}/kernel.mod" \
  -buildvcs=false -trimpath -tags=tamago \
  -ldflags='-T 0x80010000 -R 0x1000 -s -w -buildid=' \
  -o "${BUILD_DIR}/tamago-kernel.elf" ./kernel

go run ./tools/elf2raw.go \
  -in "${BUILD_DIR}/tamago-kernel.elf" \
  -out tamago-kernel -base 0x80000000
```

The raw image begins with a four-byte AArch64 branch to the TamaGo ELF entry
point. Loadable segments retain their linked physical addresses, preserving
TamaGo's early page-table arena below `0x80010000`.

# spr-tamago-demo

[![Build and verify](https://github.com/spr-networks/spr-tamago-demo/actions/workflows/ci.yml/badge.svg)](https://github.com/spr-networks/spr-tamago-demo/actions/workflows/ci.yml)

A Hello World SPR plugin whose krun microVM directly boots a
[TamaGo](https://github.com/usbarmory/tamago) ARM64 kernel. There is no Linux
kernel, init process, or Linux userspace inside the VM.

The TamaGo kernel:

- is compiled with `GOOS=tamago GOARCH=arm64`;
- boots at krun's raw ARM64 kernel address, `0x80000000`;
- discovers libkrun's virtio-net MMIO device;
- uses the official `usbarmory/go-net/virtio` network driver and TamaGo MMIO
  transport;
- runs usbarmory/go-net's pure-Go gVisor TCP/IP stack; and
- serves the plugin HTML and `/status` endpoint itself on TCP port 8080.

SPR's API currently routes plugin pages only to host Unix sockets. A tiny
gateway container outside the microVM therefore proxies the SPR Unix socket to
the kernel's HTTP listener. It does not render the page and is not part of the
guest.

```text
SPR API -> host gateway Unix socket -> private Docker bridge/TAP
        -> libkrun virtio-net -> TamaGo kernel HTTP server
```

## SPR runtime prerequisite

Upstream crun/libkrun already support `kernel_path` and raw external kernels.
SPR's hardened `spr-krun` runtime deliberately ignores image-controlled
`/.krun_vm.json`; manager-issued policy must authorize the kernel instead.

This Compose file requests:

```yaml
krun.kernel_path: /tamago-kernel
krun.kernel_format: "0"
```

The accompanying [`patches/spr-external-kernel-policy.patch`](patches/spr-external-kernel-policy.patch)
adds those two fields to SPR's trusted policy generator. Apply it to the
matching `spr-networks/super` tree and rebuild `superd`. The crun runtime patch
already passes trusted policy to `krun_set_kernel`, so no image-controlled
configuration is trusted.

The checked-in `.krun_vm.json` provides the equivalent configuration only for
testing with an unmodified upstream `krun` runtime. Hardened SPR ignores it.

## ARM64 VirtIO build note

TamaGo supports ARM64 bare metal, and this build sets all three required
values from its documentation:

```text
GOOS=tamago
GOARCH=arm64
GOOSPKG=github.com/usbarmory/tamago
```

The current TamaGo `kvm/virtio` package contains the architecture-neutral
`mmio.go`, `queue.go`, and `virtio.go` files in the same Go package as
AMD64-only `pci.go` and `legacy.go`. The PCI files do not yet have architecture
build constraints, so Go selects them on ARM64 and pulls in Intel PCI and KVM
clock code. This is also true on TamaGo's published `development` branch.

The builder therefore makes a temporary copy of the pinned TamaGo module and
replaces only those two AMD64 PCI files with empty ARM64 package files. The
kernel imports the linked `go-net/virtio` driver directly; there is no local
fork of the network driver, MMIO transport, or queue implementation.

## Build

The build produces two ARM64 images:

```sh
./build_docker_compose.sh
```

- `ghcr.io/spr-networks/spr-tamago-demo:kernel-latest` contains the raw TamaGo
  kernel and no Linux filesystem.
- `ghcr.io/spr-networks/spr-tamago-demo:gateway-latest` contains the external
  static Unix-socket proxy.

The builder and TamaGo/go-net revisions are pinned. The first build compiles
the matching TamaGo Go toolchain and can take several minutes.

The complete build environment, cold-build requirements, pin verification,
and two-build digest check are documented in
[`REPRODUCIBLE_BUILDS.md`](REPRODUCIBLE_BUILDS.md). GitHub Actions runs those
checks for pull requests and publishes commit-addressed ARM64 images from
`main`.

## Run in SPR

Install the standard `spr-krun-runtime`, apply the manager policy patch above
to the matching `superd` build, then install this repository through
**Plugins → + New Plugin**. The supported launch path is SPR's plugin manager,
because it signs the external-kernel policy and supplies the trusted runtime
override. Running the Compose file directly under hardened `spr-krun` will not
have that authorization.

`plugin.json` selects `docker-compose-kvm.yml` and adds
**spr-tamago-demo** to the sidebar.

Open **spr-tamago-demo** in the SPR sidebar. The response header
`X-TamaGo-Kernel: true` and `/status` response identify the direct-booted
kernel.

The demo uses an internal documentation subnet:

- TamaGo kernel: `192.0.2.2/24`
- host gateway container: `192.0.2.3/24`
- krun VMM namespace: `192.0.2.4/24`

It declares no SPR egress policy. The network exists only to carry the UI from
the kernel to the host gateway.

## Verify

Run static tests and Compose validation:

```sh
./test.sh
```

To build the kernel without Docker, use the matching TamaGo compiler and then
prepare the same ARM64-only dependency view. `TAMAGO` must name the compiler
wrapper built from the pinned TamaGo tool dependency:

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
point. The remaining loadable segments retain their linked physical
addresses, leaving TamaGo's early page-table arena below `0x80010000` intact.

//go:build arm64

// This file replaces TamaGo's AMD64-only VirtIO PCI transports when building
// the ARM64 MMIO kernel. The upstream files currently have no architecture
// build constraints, so without this overlay they pull Intel PCI and KVM clock
// code into an otherwise architecture-neutral MMIO build.
package virtio

// Shared by queue.go; upstream currently declares it in legacy.go.
const pageSize = 4096

//go:build ignore

// Command prepare_tamago makes a temporary, ARM64-buildable copy of the
// pinned TamaGo module. It replaces AMD64 PCI transport files which currently
// lack upstream architecture build constraints and applies the krun DMA map.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	mmuConstantsAnchor = `	l3pageTableOffset = 0x7000
	l3pageTableSize   = 512
)`
	mmuConstantsWithDMA = mmuConstantsAnchor + `

// sprDMAStart..sprDMAEnd is RAM reserved from the Go heap for VirtIO queues.
// It must retain Normal Memory attributes even though it lies outside the
// runtime region; bulk queue initialization faults on Device memory mappings.
const (
	sprDMAStart uint64 = 0x8c000000
	sprDMAEnd   uint64 = 0x8e000000
)`
	mmuDeviceDefault     = `		default:`
	mmuDMAClassification = `		case addr >= sprDMAStart && addr < sprDMAEnd:
			reg.Write64(page, addr|memoryRegion|TTE_EXECUTE_NEVER)
		default:`
)

func main() {
	var (
		tamagoDir  = flag.String("tamago-dir", "", "path to the downloaded github.com/usbarmory/tamago module")
		legacyStub = flag.String("legacy-stub", "overlays/virtio_arm64.go", "ARM64 legacy replacement source")
		pciStub    = flag.String("pci-stub", "overlays/virtio_arm64_empty.go", "ARM64 PCI replacement source")
		outDir     = flag.String("out-dir", "", "new temporary module directory")
	)
	flag.Parse()
	if *tamagoDir == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "-tamago-dir and -out-dir are required")
		os.Exit(2)
	}
	if _, err := os.Stat(*outDir); !os.IsNotExist(err) {
		panic("output directory must not already exist")
	}

	if err := filepath.WalkDir(*tamagoDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(*tamagoDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(*outDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in TamaGo module: %s", path)
		}
		return copyFile(path, dst)
	}); err != nil {
		panic(err)
	}

	for name, source := range map[string]string{
		"legacy.go": *legacyStub,
		"pci.go":    *pciStub,
	} {
		path := filepath.Join(*outDir, "kvm", "virtio", name)
		if _, err := os.Stat(path); err != nil {
			panic(err)
		}
		stubData, err := os.ReadFile(source)
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(path, stubData, 0o644); err != nil {
			panic(err)
		}
	}

	if err := patchARM64MMU(filepath.Join(*outDir, "arm64", "mmu.go")); err != nil {
		panic(err)
	}
}

func patchARM64MMU(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(source)
	if strings.Count(text, mmuConstantsAnchor) != 1 {
		return fmt.Errorf("unexpected TamaGo arm64/mmu.go constants")
	}
	text = strings.Replace(text, mmuConstantsAnchor, mmuConstantsWithDMA, 1)
	if strings.Count(text, mmuDeviceDefault) != 3 {
		return fmt.Errorf("unexpected TamaGo arm64/mmu.go classification logic")
	}
	text = strings.ReplaceAll(text, mmuDeviceDefault, mmuDMAClassification)
	return os.WriteFile(path, []byte(text), 0o644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

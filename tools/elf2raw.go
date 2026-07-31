package main

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const maxImageSize = 128 << 20

func branchInstruction(from, to uint64) (uint32, error) {
	if from&3 != 0 || to&3 != 0 {
		return 0, errors.New("branch addresses must be 4-byte aligned")
	}
	delta := int64(to) - int64(from)
	if delta < -(128<<20) || delta >= 128<<20 {
		return 0, errors.New("branch target is outside the AArch64 B range")
	}
	imm26 := uint32(delta>>2) & 0x03ffffff
	return 0x14000000 | imm26, nil
}

func programAddress(p *elf.Prog) uint64 {
	if p.Paddr != 0 {
		return p.Paddr
	}
	return p.Vaddr
}

func convert(input, output string, base uint64) error {
	file, err := elf.Open(input)
	if err != nil {
		return err
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Machine != elf.EM_AARCH64 || file.Data != elf.ELFDATA2LSB {
		return errors.New("input must be a little-endian ELF64 AArch64 executable")
	}

	end := base + 4
	for _, prog := range file.Progs {
		if prog.Type != elf.PT_LOAD || prog.Filesz == 0 {
			continue
		}
		addr := programAddress(prog)
		if addr < base || addr+prog.Filesz < addr {
			return fmt.Errorf("load segment %#x..%#x is outside the raw image", addr, addr+prog.Filesz)
		}
		if addr+prog.Filesz > end {
			end = addr + prog.Filesz
		}
	}
	if end-base > maxImageSize {
		return fmt.Errorf("raw image is too large: %d bytes", end-base)
	}

	image := make([]byte, end-base)
	for _, prog := range file.Progs {
		if prog.Type != elf.PT_LOAD || prog.Filesz == 0 {
			continue
		}
		offset := programAddress(prog) - base
		reader := prog.Open()
		if _, err := io.ReadFull(reader, image[offset:offset+prog.Filesz]); err != nil {
			return fmt.Errorf("read load segment: %w", err)
		}
	}

	branch, err := branchInstruction(base, file.Entry)
	if err != nil {
		return fmt.Errorf("entry %#x: %w", file.Entry, err)
	}
	binary.LittleEndian.PutUint32(image[:4], branch)
	if err := os.WriteFile(output, image, 0755); err != nil {
		return err
	}
	return nil
}

func main() {
	input := flag.String("in", "", "input TamaGo ELF")
	output := flag.String("out", "", "output raw kernel image")
	base := flag.Uint64("base", 0x80000000, "krun raw-kernel load address")
	flag.Parse()
	if *input == "" || *output == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := convert(*input, *output, *base); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

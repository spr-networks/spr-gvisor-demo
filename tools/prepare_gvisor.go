//go:build ignore

// Command prepare_gvisor creates deterministic TamaGo-compatible copies of
// pinned gVisor and x/sys modules. The source modules remain unmodified.
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

func main() {
	var (
		gvisorDir = flag.String("gvisor-dir", "", "downloaded gvisor.dev/gvisor module")
		xsysDir   = flag.String("xsys-dir", "", "downloaded golang.org/x/sys module")
		outGvisor = flag.String("out-gvisor", "", "patched gVisor output directory")
		outXsys   = flag.String("out-xsys", "", "patched x/sys output directory")
		overlays  = flag.String("overlays", "overlays", "overlay directory")
	)
	flag.Parse()
	if *gvisorDir == "" || *xsysDir == "" || *outGvisor == "" || *outXsys == "" {
		panic("-gvisor-dir, -xsys-dir, -out-gvisor, and -out-xsys are required")
	}
	for _, out := range []string{*outGvisor, *outXsys} {
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			panic(fmt.Sprintf("output directory already exists: %s", out))
		}
	}
	if err := copyTree(*gvisorDir, *outGvisor); err != nil {
		panic(err)
	}
	if err := copyTree(*xsysDir, *outXsys); err != nil {
		panic(err)
	}
	if err := prepareXsys(*outXsys, filepath.Join(*overlays, "xsys")); err != nil {
		panic(err)
	}
	if err := prepareGVisor(*outGvisor, filepath.Join(*overlays, "gvisor")); err != nil {
		panic(err)
	}
}

func prepareXsys(out, overlay string) error {
	for _, name := range []string{
		"zerrors_linux.go",
		"zerrors_linux_arm64.go",
		"zsysnum_linux_arm64.go",
		"ztypes_linux.go",
		"ztypes_linux_arm64.go",
	} {
		src := filepath.Join(out, "unix", name)
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		text := string(data)
		tagStart := strings.Index(text, "//go:build ")
		if tagStart < 0 || strings.Count(text, "//go:build ") != 1 {
			return fmt.Errorf("missing build tag in %s", src)
		}
		lineEndRel := strings.IndexByte(text[tagStart:], '\n')
		if lineEndRel < 0 {
			return fmt.Errorf("unterminated build tag in %s", src)
		}
		lineEnd := tagStart + lineEndRel
		oldTag := text[tagStart:lineEnd]
		newTag := strings.ReplaceAll(oldTag, "linux", "tamago")
		text = text[:tagStart] + newTag + text[lineEnd:]
		text = strings.Replace(text, "// +build linux,arm64", "// +build tamago,arm64", 1)
		text = strings.Replace(text, "// +build linux", "// +build tamago", 1)
		dst := filepath.Join(out, "unix", strings.Replace(name, "_linux", "_tamago", 1))
		if err := os.WriteFile(dst, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return copyOverlay(overlay, out)
}

func prepareGVisor(out, overlay string) error {
	mmapPath := filepath.Join(out, "pkg", "sentry", "platform", "mmap_min_addr.go")
	if err := replaceOnce(mmapPath, "// Copyright", "//go:build !tamago\n\n// Copyright"); err != nil {
		return err
	}

	pgallocPath := filepath.Join(out, "pkg", "sentry", "pgalloc", "pgalloc.go")
	replacements := [][2]string{
		{"\t\"time\"\n", "\t\"time\"\n\t\"unsafe\"\n"},
		{"\tfile *os.File\n\n\t// chunks holds", "\tfile *os.File\n\n\t// bareMetal chunks are page-aligned Go heap allocations.\n\tbareMetal        bool\n\tbareMetalBacking [][]byte\n\n\t// chunks holds"},
		{"\tchunkShift = 30\n\tchunkSize  = 1 << chunkShift // 1 GB", "\tchunkShift = 24\n\tchunkSize  = 1 << chunkShift // 16 MB"},
		{"\tf.file.Close()\n\t// Ensure that any attempts", "\tif f.file != nil {\n\t\tf.file.Close()\n\t}\n\t// Ensure that any attempts"},
		{"\tfor i := range chunks {\n\t\tchunk := &chunks[i]", "\tfor i := range chunks {\n\t\tif f.bareMetal {\n\t\t\tchunks[i].mapping = 0\n\t\t\tcontinue\n\t\t}\n\t\tchunk := &chunks[i]"},
		{"\t\tnewChunks[i].huge = alloc.huge\n\t\tif f.file != nil {", "\t\tnewChunks[i].huge = alloc.huge\n\t\tif f.bareMetal {\n\t\t\tbacking := make([]byte, chunkSize+hostarch.PageSize)\n\t\t\tbase := uintptr(unsafe.Pointer(&backing[0]))\n\t\t\tnewChunks[i].mapping = (base + hostarch.PageSize - 1) &^ uintptr(hostarch.PageSize-1)\n\t\t\tf.bareMetalBacking = append(f.bareMetalBacking, backing)\n\t\t} else if f.file != nil {"},
		{"func (f *MemoryFile) commitFile(fr memmap.FileRange) error {\n", "func (f *MemoryFile) commitFile(fr memmap.FileRange) error {\n\tif f.bareMetal {\n\t\treturn nil\n\t}\n"},
		{"func (f *MemoryFile) decommitFile(fr memmap.FileRange) error {\n", "func (f *MemoryFile) decommitFile(fr memmap.FileRange) error {\n\tif f.bareMetal {\n\t\tf.manuallyZero(fr)\n\t\treturn nil\n\t}\n"},
	}
	for _, replacement := range replacements {
		if err := replaceOnce(pgallocPath, replacement[0], replacement[1]); err != nil {
			return err
		}
	}
	return copyOverlay(overlay, out)
}

func replaceOnce(path, old, new string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	if strings.Count(text, old) != 1 {
		return fmt.Errorf("expected one patch anchor in %s: %q", path, old)
	}
	return os.WriteFile(path, []byte(strings.Replace(text, old, new, 1)), 0o644)
}

func copyOverlay(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target, false)
	})
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected module symlink: %s", path)
		}
		return copyFile(path, target, true)
	})
}

func copyFile(src, dst string, exclusive bool) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if exclusive {
		flags = os.O_CREATE | os.O_EXCL | os.O_WRONLY
	}
	out, err := os.OpenFile(dst, flags, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

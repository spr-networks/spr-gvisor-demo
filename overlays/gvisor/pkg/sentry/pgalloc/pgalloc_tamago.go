//go:build tamago

package pgalloc

func NewMemoryFileBareMetal() *MemoryFile {
	f := &MemoryFile{
		opts: MemoryFileOpts{
			DelayedEviction:         DelayedEvictionManual,
			DisableIMAWorkAround:    true,
			DisableMemoryAccounting: true,
		},
		bareMetal: true,
	}
	f.initFields()
	return f
}

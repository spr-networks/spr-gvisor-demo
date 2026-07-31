//go:build tamago

package sighandling

import "golang.org/x/sys/unix"

func ReplaceSignalHandler(_ unix.Signal, _ uintptr, previous *uintptr) error {
	*previous = 0
	return nil
}

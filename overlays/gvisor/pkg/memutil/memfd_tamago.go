//go:build tamago

package memutil

import "golang.org/x/sys/unix"

func CreateMemFD(string, int) (int, error) { return -1, unix.EINVAL }

//go:build tamago

package platform

import "gvisor.dev/gvisor/pkg/hostarch"

const systemMMapMinAddr = uint64(0x10000)

func SystemMMapMinAddr() hostarch.Addr { return hostarch.Addr(systemMMapMinAddr) }

type MMapMinAddr struct{}

func (*MMapMinAddr) MinUserAddress() hostarch.Addr { return SystemMMapMinAddr() }

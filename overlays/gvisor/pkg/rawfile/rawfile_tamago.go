//go:build tamago

package rawfile

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

const MaxIovs = 1024
const SizeofIovec = unsafe.Sizeof(unix.Iovec{})

func IovecFromBytes(bs []byte) unix.Iovec { return unix.Iovec{Base: &bs[0], Len: uint64(len(bs))} }
func AppendIovecFromBytes(iovs []unix.Iovec, bs []byte, max int) []unix.Iovec {
	if len(bs) == 0 {
		return iovs
	}
	if len(iovs) < max {
		return append(iovs, IovecFromBytes(bs))
	}
	return iovs
}

type MMsgHdr struct {
	Msg unix.Msghdr
	Len uint32
	_   [4]byte
}

const SizeofMMsgHdr = unsafe.Sizeof(MMsgHdr{})

type PollEvent struct {
	FD      int32
	Events  int16
	Revents int16
}

func GetMTU(string) (uint32, error)                                      { return 0, unix.EINVAL }
func NonBlockingWrite(int, []byte) unix.Errno                            { return unix.EINVAL }
func NonBlockingWriteIovec(int, []unix.Iovec) unix.Errno                 { return unix.EINVAL }
func NonBlockingSendMMsg(int, []MMsgHdr) (int, unix.Errno)               { return 0, unix.EINVAL }
func BlockingRead(int, []byte) (int, unix.Errno)                         { return 0, unix.EINVAL }
func BlockingReadvUntilStopped(int, int, []unix.Iovec) (int, unix.Errno) { return 0, unix.EINVAL }
func BlockingRecvMMsgUntilStopped(int, int, []MMsgHdr) (int, unix.Errno) { return 0, unix.EINVAL }
func BlockingPollUntilStopped(int, int, int16) (bool, unix.Errno)        { return false, unix.EINVAL }

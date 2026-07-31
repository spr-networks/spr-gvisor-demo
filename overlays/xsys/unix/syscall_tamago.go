//go:build tamago

package unix

import (
	"syscall"
	"unsafe"
)

type Errno = syscall.Errno
type Signal = syscall.Signal

func (iov *Iovec) SetLen(length int)            { iov.Len = uint64(length) }
func (msghdr *Msghdr) SetControllen(length int) { msghdr.Controllen = uint64(length) }
func (msghdr *Msghdr) SetIovlen(length int)     { msghdr.Iovlen = uint64(length) }
func (cmsg *Cmsghdr) SetLen(length int)         { cmsg.Len = uint64(length) }
func (ts *Timespec) Nano() int64                { return ts.Sec*1e9 + ts.Nsec }

func Getpagesize() int { return 4096 }

// A direct-boot guest has no host syscall ABI. Bare-metal backends do not
// reach these compatibility entry points.
func RawSyscall(trap, a1, a2, a3 uintptr) (uintptr, uintptr, Errno) { return 0, 0, EINVAL }
func RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (uintptr, uintptr, Errno) {
	return 0, 0, EINVAL
}
func Syscall(trap, a1, a2, a3 uintptr) (uintptr, uintptr, Errno) { return 0, 0, EINVAL }
func Syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (uintptr, uintptr, Errno) {
	return 0, 0, EINVAL
}

func Read(int, []byte) (int, error)                  { return 0, EINVAL }
func Pread(int, []byte, int64) (int, error)          { return 0, EINVAL }
func Write(int, []byte) (int, error)                 { return 0, EINVAL }
func Pwrite(int, []byte, int64) (int, error)         { return 0, EINVAL }
func Dup(int) (int, error)                           { return -1, EINVAL }
func Open(string, int, uint32) (int, error)          { return -1, EINVAL }
func Openat(int, string, int, uint32) (int, error)   { return -1, EINVAL }
func Close(int) error                                { return nil }
func SetNonblock(int, bool) error                    { return nil }
func Ftruncate(int, int64) error                     { return EINVAL }
func Seek(int, int64, int) (int64, error)            { return 0, EINVAL }
func Getdents(int, []byte) (int, error)              { return 0, EINVAL }
func FcntlInt(uintptr, int, int) (int, error)        { return 0, EINVAL }
func Mmap(int, int64, int, int, int) ([]byte, error) { return nil, EINVAL }
func Munmap([]byte) error                            { return nil }

func BytePtrFromString(s string) (*byte, error) {
	if len(s) > 0 && s[len(s)-1] == 0 {
		return nil, EINVAL
	}
	b := append([]byte(s), 0)
	return &b[0], nil
}

type Sockaddr interface{}
type SockaddrUnix struct{ Name string }

func Socket(int, int, int) (int, error)                 { return -1, EINVAL }
func Socketpair(int, int, int) ([2]int, error)          { return [2]int{-1, -1}, EINVAL }
func Connect(int, Sockaddr) error                       { return EINVAL }
func Shutdown(int, int) error                           { return nil }
func Bind(int, Sockaddr) error                          { return EINVAL }
func Listen(int, int) error                             { return EINVAL }
func Accept(int) (int, Sockaddr, error)                 { return -1, nil, EINVAL }
func Fallocate(int, uint32, int64, int64) error         { return EINVAL }
func Fstat(int, *Stat_t) error                          { return EINVAL }
func Fsync(int) error                                   { return EINVAL }
func Statx(int, string, int, int, *Statx_t) error       { return ENOSYS }
func EpollCreate1(int) (int, error)                     { return -1, EINVAL }
func EpollCtl(int, int, int, *EpollEvent) error         { return EINVAL }
func GetsockoptInt(int, int, int) (int, error)          { return 0, EINVAL }
func Ppoll([]PollFd, *Timespec, *Sigset_t) (int, error) { return 0, EINVAL }

type SocketControlMessage struct {
	Header Cmsghdr
	Data   []byte
}

func CmsgSpace(datalen int) int                                        { return datalen + int(unsafe.Sizeof(Cmsghdr{})) }
func CmsgLen(datalen int) int                                          { return datalen + int(unsafe.Sizeof(Cmsghdr{})) }
func ParseSocketControlMessage([]byte) ([]SocketControlMessage, error) { return nil, EINVAL }
func ParseUnixRights(*SocketControlMessage) ([]int, error)             { return nil, EINVAL }
func UnixRights(...int) []byte                                         { return nil }

const SYS_FSTATAT = SYS_NEWFSTATAT

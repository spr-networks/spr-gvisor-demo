//go:build tamago

package aio

import "golang.org/x/sys/unix"

type LinuxQueue struct{ capacity int }

func NewLinuxQueue(capacity int) (*LinuxQueue, error)                 { return &LinuxQueue{capacity: capacity}, nil }
func (*LinuxQueue) Destroy()                                          {}
func (q *LinuxQueue) Cap() int                                        { return q.capacity }
func (*LinuxQueue) Add(Request)                                       {}
func (*LinuxQueue) Wait(cs []Completion, _ int) ([]Completion, error) { return cs, unix.EINVAL }

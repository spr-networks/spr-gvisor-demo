//go:build tamago

package fdnotifier

import "gvisor.dev/gvisor/pkg/waiter"

func AddFD(int32, *waiter.Queue) error                                { return nil }
func UpdateFD(int32) error                                            { return nil }
func RemoveFD(int32)                                                  {}
func HasFD(int32) bool                                                { return false }
func Pause()                                                          {}
func Resume()                                                         {}
func NonBlockingPoll(_ int32, mask waiter.EventMask) waiter.EventMask { return mask }

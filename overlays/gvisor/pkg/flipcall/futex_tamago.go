//go:build tamago

package flipcall

import "runtime"

func (*Endpoint) futexSetPeerActive() error      { return nil }
func (*Endpoint) futexWakePeer() error           { return nil }
func (*Endpoint) futexWaitUntilActive() error    { return nil }
func (*Endpoint) futexWakeConnState(int32) error { return nil }
func yieldThread()                               { runtime.Gosched() }

//go:build tamago

package fdchannel

import "errors"

var errUnsupported = errors.New("fd passing is unavailable on bare metal")

type Endpoint struct{}

func NewConnectedSockets() ([2]int, error)     { return [2]int{-1, -1}, errUnsupported }
func NewEndpoint(int) *Endpoint                { return &Endpoint{} }
func (*Endpoint) Init(int)                     {}
func (*Endpoint) Destroy()                     {}
func (*Endpoint) Shutdown()                    {}
func (*Endpoint) SendFD(int) error             { return errUnsupported }
func (*Endpoint) RecvFD() (int, error)         { return -1, errUnsupported }
func (*Endpoint) RecvFDNonblock() (int, error) { return -1, errUnsupported }

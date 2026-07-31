// Package vsock implements the minimal VirtIO-vsock stream protocol needed by
// the SPR plugin HTTP endpoint.
package vsock

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	DeviceID = 19

	ReceiveQueue  = 0
	TransmitQueue = 1
	EventQueue    = 2

	TypeStream = 1

	OpRequest       = 1
	OpResponse      = 2
	OpReset         = 3
	OpShutdown      = 4
	OpReadWrite     = 5
	OpCreditUpdate  = 6
	OpCreditRequest = 7

	ShutdownReceive = 1
	ShutdownSend    = 2

	HeaderSize       = 44
	MaxPacketPayload = 64 * 1024
	DefaultBuffer    = 256 * 1024
	DefaultMaxConns  = 64

	// VirtIO 1.0+ transports require the driver to acknowledge VERSION_1.
	FeatureVersion1 uint64 = 1 << 32
)

// Header is the little-endian virtio_vsock_hdr wire structure.
type Header struct {
	SrcCID   uint64
	DstCID   uint64
	SrcPort  uint32
	DstPort  uint32
	Len      uint32
	Type     uint16
	Op       uint16
	Flags    uint32
	BufAlloc uint32
	FwdCnt   uint32
}

func decodePacket(packet []byte) (header Header, payload []byte, err error) {
	if len(packet) < HeaderSize {
		return header, nil, errors.New("short VirtIO-vsock packet")
	}
	if _, err = binary.Decode(packet[:HeaderSize], binary.LittleEndian, &header); err != nil {
		return header, nil, err
	}
	if uint64(header.Len) > uint64(len(packet)-HeaderSize) {
		return header, nil, errors.New("invalid VirtIO-vsock payload length")
	}
	payload = packet[HeaderSize : HeaderSize+int(header.Len)]
	return
}

func encodePacket(header Header, payload []byte) []byte {
	header.Len = uint32(len(payload))
	packet := make([]byte, HeaderSize+len(payload))
	_, _ = binary.Encode(packet[:HeaderSize], binary.LittleEndian, &header)
	copy(packet[HeaderSize:], payload)
	return packet
}

type connectionKey struct {
	cid  uint64
	port uint32
}

type connection struct {
	remoteCID  uint64
	remotePort uint32
	peerAlloc  uint32
	peerFwd    uint32
	txCount    uint32
	rxFwd      uint32
	request    []byte
	response   []byte
	sent       int
	closing    bool
}

// Endpoint terminates host-initiated VirtIO-vsock streams on one guest port.
// Handler receives one complete HTTP request header and returns the complete
// HTTP response byte stream.
type Endpoint struct {
	CID       uint64
	Port      uint32
	Handler   func(request []byte) []byte
	BufferLen uint32
	MaxConns  int

	connections map[connectionKey]*connection
}

func NewEndpoint(cid uint64, port uint32, handler func([]byte) []byte) *Endpoint {
	return &Endpoint{
		CID:         cid,
		Port:        port,
		Handler:     handler,
		BufferLen:   DefaultBuffer,
		MaxConns:    DefaultMaxConns,
		connections: map[connectionKey]*connection{},
	}
}

func (e *Endpoint) Reset() {
	clear(e.connections)
}

func (e *Endpoint) replyHeader(c *connection, op uint16, flags uint32) Header {
	return Header{
		SrcCID:   e.CID,
		DstCID:   c.remoteCID,
		SrcPort:  e.Port,
		DstPort:  c.remotePort,
		Type:     TypeStream,
		Op:       op,
		Flags:    flags,
		BufAlloc: e.BufferLen,
		FwdCnt:   c.rxFwd,
	}
}

func resetFor(header Header) []byte {
	return encodePacket(Header{
		SrcCID:  header.DstCID,
		DstCID:  header.SrcCID,
		SrcPort: header.DstPort,
		DstPort: header.SrcPort,
		Type:    TypeStream,
		Op:      OpReset,
	}, nil)
}

func (e *Endpoint) updatePeer(c *connection, header Header) {
	c.peerAlloc = header.BufAlloc
	c.peerFwd = header.FwdCnt
}

func (e *Endpoint) sendSpace(c *connection) uint32 {
	limit := c.peerAlloc
	if limit > e.BufferLen {
		limit = e.BufferLen
	}
	if c.peerFwd > c.txCount {
		return limit
	}
	inFlight := c.txCount - c.peerFwd
	if inFlight >= limit {
		return 0
	}
	return limit - inFlight
}

// Handle consumes one packet and returns control/data packets ready for the
// transmit queue.
func (e *Endpoint) Handle(packet []byte) (out [][]byte, err error) {
	header, payload, err := decodePacket(packet)
	if err != nil {
		return nil, err
	}
	if header.Type != TypeStream || header.DstCID != e.CID || header.DstPort != e.Port {
		return [][]byte{resetFor(header)}, nil
	}

	key := connectionKey{header.SrcCID, header.SrcPort}
	c := e.connections[key]
	if header.Op == OpRequest {
		if c != nil || len(e.connections) >= e.MaxConns {
			return [][]byte{resetFor(header)}, nil
		}
		c = &connection{remoteCID: header.SrcCID, remotePort: header.SrcPort}
		e.updatePeer(c, header)
		e.connections[key] = c
		out = append(out, encodePacket(e.replyHeader(c, OpResponse, 0), nil))
		return append(out, e.Drain(8)...), nil
	}
	if c == nil {
		return [][]byte{resetFor(header)}, nil
	}
	e.updatePeer(c, header)

	switch header.Op {
	case OpReadWrite:
		if uint64(len(c.request))+uint64(len(payload)) > uint64(e.BufferLen) {
			delete(e.connections, key)
			return [][]byte{resetFor(header)}, nil
		}
		c.request = append(c.request, payload...)
		c.rxFwd += uint32(len(payload))
		if c.response == nil && bytes.Contains(c.request, []byte("\r\n\r\n")) {
			if e.Handler == nil {
				return nil, errors.New("VirtIO-vsock endpoint has no handler")
			}
			c.response = e.Handler(c.request)
		}
		if c.response == nil {
			out = append(out, encodePacket(e.replyHeader(c, OpCreditUpdate, 0), nil))
		}
	case OpCreditRequest:
		out = append(out, encodePacket(e.replyHeader(c, OpCreditUpdate, 0), nil))
	case OpCreditUpdate:
	case OpShutdown:
		c.closing = true
	case OpReset:
		delete(e.connections, key)
		return nil, nil
	default:
		delete(e.connections, key)
		return [][]byte{resetFor(header)}, nil
	}

	return append(out, e.Drain(8)...), nil
}

// Drain returns queued stream data while respecting the peer's advertised
// receive credit.
func (e *Endpoint) Drain(limit int) (out [][]byte) {
	if limit <= 0 {
		return nil
	}
	for key, c := range e.connections {
		for len(out) < limit && c.sent < len(c.response) {
			space := int(e.sendSpace(c))
			if space == 0 {
				break
			}
			remaining := len(c.response) - c.sent
			chunkLen := min(remaining, space, MaxPacketPayload)
			chunk := c.response[c.sent : c.sent+chunkLen]
			out = append(out, encodePacket(e.replyHeader(c, OpReadWrite, 0), chunk))
			c.sent += chunkLen
			c.txCount += uint32(chunkLen)
		}
		if len(out) >= limit {
			break
		}
		if c.response != nil && c.sent == len(c.response) && !c.closing {
			out = append(out, encodePacket(e.replyHeader(c, OpShutdown, ShutdownReceive|ShutdownSend), nil))
			c.closing = true
		}
		if c.closing && c.response == nil {
			out = append(out, encodePacket(e.replyHeader(c, OpReset, 0), nil))
			delete(e.connections, key)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (h Header) String() string {
	return fmt.Sprintf("%d:%d -> %d:%d op=%d len=%d", h.SrcCID, h.SrcPort, h.DstCID, h.DstPort, h.Op, h.Len)
}

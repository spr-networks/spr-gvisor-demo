package vsock

import (
	"bytes"
	"testing"
)

func hostPacket(op uint16, payload []byte) []byte {
	return encodePacket(Header{
		SrcCID:   2,
		DstCID:   3,
		SrcPort:  49152,
		DstPort:  4040,
		Type:     TypeStream,
		Op:       op,
		BufAlloc: DefaultBuffer,
	}, payload)
}

func TestEndpointServesHTTPStream(t *testing.T) {
	endpoint := NewEndpoint(3, 4040, func(request []byte) []byte {
		if !bytes.HasPrefix(request, []byte("GET / HTTP/1.1\r\n")) {
			t.Fatalf("unexpected request: %q", request)
		}
		return []byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello")
	})

	response, err := endpoint.Handle(hostPacket(OpRequest, nil))
	if err != nil || len(response) != 1 {
		t.Fatalf("request handshake: packets=%d err=%v", len(response), err)
	}
	header, _, err := decodePacket(response[0])
	if err != nil || header.Op != OpResponse || header.SrcCID != 3 || header.DstCID != 2 {
		t.Fatalf("invalid handshake response: %v %v", header, err)
	}

	response, err = endpoint.Handle(hostPacket(OpReadWrite, []byte("GET / HTTP/1.1\r\nHost: spr\r\n\r\n")))
	if err != nil || len(response) != 2 {
		t.Fatalf("HTTP response: packets=%d err=%v", len(response), err)
	}
	header, payload, _ := decodePacket(response[0])
	if header.Op != OpReadWrite || !bytes.Contains(payload, []byte("hello")) {
		t.Fatalf("invalid HTTP data packet: %v %q", header, payload)
	}
	header, _, _ = decodePacket(response[1])
	if header.Op != OpShutdown || header.Flags != ShutdownReceive|ShutdownSend {
		t.Fatalf("invalid shutdown packet: %v", header)
	}
}

func TestEndpointRejectsWrongPort(t *testing.T) {
	packet := hostPacket(OpRequest, nil)
	header, payload, _ := decodePacket(packet)
	header.DstPort = 8080

	response, err := NewEndpoint(3, 4040, nil).Handle(encodePacket(header, payload))
	if err != nil || len(response) != 1 {
		t.Fatalf("packets=%d err=%v", len(response), err)
	}
	header, _, _ = decodePacket(response[0])
	if header.Op != OpReset {
		t.Fatalf("wrong-port response op=%d", header.Op)
	}
}

func TestEndpointLimitsConcurrentStreams(t *testing.T) {
	endpoint := NewEndpoint(3, 4040, nil)
	endpoint.MaxConns = 1
	_, _ = endpoint.Handle(hostPacket(OpRequest, nil))

	packet := hostPacket(OpRequest, nil)
	header, payload, _ := decodePacket(packet)
	header.SrcPort++
	response, err := endpoint.Handle(encodePacket(header, payload))
	if err != nil || len(response) != 1 {
		t.Fatalf("packets=%d err=%v", len(response), err)
	}
	header, _, _ = decodePacket(response[0])
	if header.Op != OpReset {
		t.Fatalf("over-limit response op=%d", header.Op)
	}
}

func TestEndpointReassemblesRequestAndUpdatesCredit(t *testing.T) {
	called := 0
	endpoint := NewEndpoint(3, 4040, func([]byte) []byte {
		called++
		return []byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
	})
	_, _ = endpoint.Handle(hostPacket(OpRequest, nil))

	response, err := endpoint.Handle(hostPacket(OpReadWrite, []byte("GET /status HTTP/1.1\r\n")))
	if err != nil || len(response) != 1 {
		t.Fatalf("fragment one: packets=%d err=%v", len(response), err)
	}
	header, _, _ := decodePacket(response[0])
	if header.Op != OpCreditUpdate || header.FwdCnt == 0 || called != 0 {
		t.Fatalf("unexpected credit update: %v called=%d", header, called)
	}

	_, err = endpoint.Handle(hostPacket(OpReadWrite, []byte("Host: spr\r\n\r\n")))
	if err != nil || called != 1 {
		t.Fatalf("fragment two: err=%v called=%d", err, called)
	}
}

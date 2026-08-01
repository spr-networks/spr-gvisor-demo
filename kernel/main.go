//go:build tamago && arm64

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"

	guestvsock "github.com/spr-networks/spr-gvisor-demo/kernel/vsock"
	"github.com/usbarmory/tamago/kvm/virtio"
)

const (
	vsockPort = 4040

	virtioMMIOStart = 0x0a002000
	virtioMMIOEnd   = 0x0a020000
	virtioMMIOStep  = 0x1000
)

var (
	tamagoVersion = "unknown"
	gvisorVersion = "unknown"
)

// pluginUI is the single-file SPR Plugin UI SDK build copied into this
// package by the reproducible Docker frontend stage.
//
//go:embed ui/index.html
var pluginUI []byte

func findVsockDevice() (*guestvsock.Device, uint32, error) {
	for base := uint32(virtioMMIOStart); base < virtioMMIOEnd; base += virtioMMIOStep {
		transport := &virtio.MMIO{Base: base}
		if transport.DeviceID() != guestvsock.DeviceID {
			continue
		}
		dev := &guestvsock.Device{Transport: transport}
		if err := dev.Init(); err != nil {
			return nil, base, err
		}
		return dev, base, nil
	}
	return nil, 0, fmt.Errorf("virtio-vsock device not found")
}

func httpResponse(status, contentType string, body []byte) []byte {
	header := fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\nX-GVisor-Kernel: true\r\n\r\n", status, contentType, len(body))
	return append([]byte(header), body...)
}

func handleRequest(request []byte) []byte {
	lineEnd := bytes.Index(request, []byte("\r\n"))
	if lineEnd < 0 {
		return httpResponse("400 Bad Request", "text/plain; charset=utf-8", []byte("bad request\n"))
	}
	fields := strings.Fields(string(request[:lineEnd]))
	if len(fields) != 3 || fields[0] != "GET" {
		return httpResponse("405 Method Not Allowed", "text/plain; charset=utf-8", []byte("method not allowed\n"))
	}

	switch fields[1] {
	case "/status":
		stage, failure, output := gvisorStatusSnapshot()
		network := networkStatusSnapshot()
		body := new(bytes.Buffer)
		_ = json.NewEncoder(body).Encode(map[string]any{
			"runtime":        runtime.GOOS,
			"arch":           runtime.GOARCH,
			"role":           "application-kernel",
			"kernel":         "gvisor-sentry",
			"substrate":      "tamago",
			"linux_kernel":   false,
			"tamago_version": tamagoVersion,
			"gvisor_version": gvisorVersion,
			"gvisor":         stage,
			"error":          failure,
			"output":         output,
			"ipc":            "virtio-vsock",
			"port":           vsockPort,
			"network":        network,
		})
		return httpResponse("200 OK", "application/json", body.Bytes())
	case "/", "/index.html":
		return httpResponse("200 OK", "text/html; charset=utf-8", pluginUI)
	default:
		return httpResponse("404 Not Found", "text/plain; charset=utf-8", []byte("not found\n"))
	}
}

func main() {
	log.SetFlags(0)
	if !gvisorKernelProbe() {
		log.Fatal("gVisor ARM64 ring-0 initialization failed")
	}
	log.Printf("gVisor direct-boot platform ready GOOS=%s GOARCH=%s", runtime.GOOS, runtime.GOARCH)

	dev, base, err := findVsockDevice()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("virtio-vsock MMIO=%#x CID=%d HTTP port=%d", base, dev.CID(), vsockPort)
	go startGVisorSentry()
	if err := dev.Serve(vsockPort, handleRequest); err != nil {
		log.Fatal(err)
	}
}

//go:build tamago && arm64

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime"

	gnet "github.com/usbarmory/go-net"
	vnet "github.com/usbarmory/go-net/virtio"
	"github.com/usbarmory/tamago/kvm/virtio"
)

const (
	guestCIDR = "192.0.2.2/24"
	guestMAC  = "02:53:50:52:54:47"
	httpPort  = 8080

	virtioMMIOStart = 0x0a002000
	virtioMMIOEnd   = 0x0a020000
	virtioMMIOStep  = 0x1000
)

var page = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>SPR TamaGo Kernel Demo</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; color: #eef7f2;
      background: radial-gradient(circle at 20% 15%, #164d45 0, transparent 32%), #071714; }
    main { width: min(760px, calc(100% - 32px)); padding: 40px; border: 1px solid #2d6358;
      border-radius: 20px; background: rgba(9, 31, 27, .94); box-shadow: 0 24px 80px #0008; }
    .eyebrow { color: #79e2bd; font-size: .8rem; font-weight: 700; letter-spacing: .16em; text-transform: uppercase; }
    h1 { margin: 12px 0 24px; font-size: clamp(2rem, 6vw, 4rem); line-height: 1; }
    .hello { padding: 20px; border-radius: 12px; background: #06110f; color: #b8ffdf;
      font: 600 clamp(.9rem, 2.5vw, 1.15rem)/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; }
    dl { display: grid; grid-template-columns: max-content 1fr; gap: 12px 20px; margin: 28px 0 0; }
    dt { color: #8eaaa2; } dd { margin: 0; overflow-wrap: anywhere; } .ok { color: #79e2bd; }
    @media (max-width: 560px) { main { padding: 26px; } dl { grid-template-columns: 1fr; gap: 5px; } dd { margin-bottom: 9px; } }
  </style>
</head>
<body>
  <main>
    <div class="eyebrow">Direct-booted kernel · no Linux guest</div>
    <h1>Hello, SPR.</h1>
    <div class="hello">Hello World from the TamaGo kernel!</div>
    <dl>
      <dt>Runtime</dt><dd class="ok">{{.GOOS}}/{{.GOARCH}}</dd>
      <dt>Role</dt><dd>krun guest kernel</dd>
      <dt>Network</dt><dd>virtio-net · {{.Address}}</dd>
      <dt>Linux in VM</dt><dd>none</dd>
    </dl>
  </main>
</body>
</html>`))

type pageData struct {
	GOOS    string
	GOARCH  string
	Address string
}

func findNetworkDevice() (*vnet.Net, uint32, error) {
	for base := uint32(virtioMMIOStart); base < virtioMMIOEnd; base += virtioMMIOStep {
		transport := &virtio.MMIO{Base: base}
		if transport.DeviceID() != vnet.DeviceID {
			continue
		}
		dev := &vnet.Net{
			Transport: transport,
			MTU:       gnet.MTU,
		}
		if err := dev.Init(); err != nil {
			return nil, base, err
		}
		return dev, base, nil
	}
	return nil, 0, fmt.Errorf("virtio-net device not found")
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-TamaGo-Kernel", "true")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"runtime": runtime.GOOS,
			"arch":    runtime.GOARCH,
			"role":    "kernel",
			"linux":   false,
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-TamaGo-Kernel", "true")
		_ = page.Execute(w, pageData{runtime.GOOS, runtime.GOARCH, guestCIDR})
	})
	return mux
}

func main() {
	log.SetFlags(0)
	log.Printf("Hello World from the TamaGo kernel! GOOS=%s GOARCH=%s", runtime.GOOS, runtime.GOARCH)

	dev, base, err := findNetworkDevice()
	if err != nil {
		log.Fatal(err)
	}
	stack := gnet.NewGVisorStack(1)
	iface := &gnet.Interface{NetworkDevice: dev, Stack: stack}
	if err := iface.Init(guestCIDR, guestMAC, ""); err != nil {
		log.Fatalf("configure network: %v", err)
	}
	if err := stack.EnableICMP(); err != nil {
		log.Fatalf("enable ICMP: %v", err)
	}
	listener, err := stack.ListenerTCP4(httpPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	dev.Start()
	go func() {
		if err := iface.Start(context.Background()); err != nil {
			log.Printf("network loop stopped: %v", err)
		}
	}()

	log.Printf("virtio-net MMIO=%#x MAC=%s HTTP=%s:%d", base, guestMAC, guestCIDR, httpPort)
	server := &http.Server{Handler: handler()}
	if err := server.Serve(listener); err != nil {
		log.Fatal(err)
	}
}

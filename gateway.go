package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	defaultSocketPath = "/state/plugins/spr-tamago-demo/socket.sock"
	defaultTamaGoURL  = "http://192.0.2.2:8080"
)

func newProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("TamaGo kernel is not ready: %v", err)
		http.Error(w, "TamaGo kernel is starting; retry in a moment.", http.StatusBadGateway)
	}
	return proxy
}

func main() {
	log.SetFlags(0)

	targetText := os.Getenv("TAMAGO_URL")
	if targetText == "" {
		targetText = defaultTamaGoURL
	}
	target, err := url.Parse(targetText)
	if err != nil || target.Scheme != "http" || target.Host == "" {
		log.Fatalf("invalid TAMAGO_URL %q", targetText)
	}

	socketPath := os.Getenv("SPR_PLUGIN_SOCKET")
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	if !filepath.IsAbs(socketPath) {
		log.Fatalf("plugin socket must be absolute: %s", socketPath)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		log.Fatalf("create socket directory: %v", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("remove stale socket: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("listen on %s: %v", socketPath, err)
	}
	defer os.Remove(socketPath)
	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Fatalf("set socket permissions: %v", err)
	}

	server := &http.Server{
		Handler:           newProxy(target),
		ReadHeaderTimeout: 5 * time.Second,
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	log.Printf("proxying %s to SPR on %s", target, socketPath)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(fmt.Errorf("serve plugin UI: %w", err))
	}
}

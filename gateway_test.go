package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestProxyReturnsKernelUI(t *testing.T) {
	proxy := newProxy(&url.URL{Scheme: "http", Host: "tamago-kernel"})
	proxy.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/status" {
			t.Fatalf("path = %q, want /status", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Tamago-Kernel": []string{"true"}},
			Body:       io.NopCloser(strings.NewReader(`{"runtime":"tamago","role":"kernel"}`)),
		}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("X-TamaGo-Kernel") != "true" {
		t.Fatal("response did not come from the TamaGo kernel")
	}
	if got := response.Body.String(); got != `{"runtime":"tamago","role":"kernel"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestProxyReportsUnavailableKernel(t *testing.T) {
	target, _ := url.Parse("http://127.0.0.1:1")
	proxy := newProxy(target)
	proxy.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("kernel unavailable")
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
}

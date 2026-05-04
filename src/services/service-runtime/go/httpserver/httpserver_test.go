package httpserver_test

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/verself/service-runtime/httpserver"
	"golang.org/x/net/http2"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewAppliesStandardTimeouts(t *testing.T) {
	srv := httpserver.New("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != httpserver.ReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout: got %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != httpserver.ReadTimeout {
		t.Errorf("ReadTimeout: got %v", srv.ReadTimeout)
	}
	if srv.WriteTimeout != httpserver.WriteTimeout {
		t.Errorf("WriteTimeout: got %v", srv.WriteTimeout)
	}
	if srv.IdleTimeout != httpserver.IdleTimeout {
		t.Errorf("IdleTimeout: got %v", srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != httpserver.MaxHeaderBytes {
		t.Errorf("MaxHeaderBytes: got %d", srv.MaxHeaderBytes)
	}
}

func TestRunPairServesBothPlanes(t *testing.T) {
	publicLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	internalLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	publicHit := make(chan struct{}, 1)
	internalHit := make(chan struct{}, 1)

	public := httpserver.New(publicLn.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		publicHit <- struct{}{}
	}))
	internal := httpserver.New(internalLn.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHit <- struct{}{}
	}))
	// Use pre-bound listeners so the test doesn't race with port allocation.
	_ = publicLn.Close()
	_ = internalLn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- httpserver.RunPair(ctx, discardLogger(), public, internal) }()

	time.Sleep(100 * time.Millisecond)

	if _, err := http.Get("http://" + public.Addr); err != nil {
		t.Fatalf("public GET: %v", err)
	}
	if _, err := http.Get("http://" + internal.Addr); err != nil {
		t.Fatalf("internal GET: %v", err)
	}

	select {
	case <-publicHit:
	case <-time.After(time.Second):
		t.Fatal("public handler not invoked")
	}
	select {
	case <-internalHit:
	case <-time.After(time.Second):
		t.Fatal("internal handler not invoked")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunPair returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunPair did not return after context cancel")
	}
}

func TestRunPairRequiresAtLeastOneServer(t *testing.T) {
	if err := httpserver.RunPair(context.Background(), discardLogger(), nil, nil); err == nil {
		t.Fatal("expected error for nil,nil RunPair")
	}
}

// TestNewServesH2CPriorKnowledge exercises the cleartext HTTP/2 path that
// HAProxy uses to reach service backends. h2c.NewHandler runs only on the
// public plane (no TLS); a regression here would manifest as HAProxy speaking
// h2c to a backend that only knows HTTP/1.1, taking the public API down.
func TestNewServesH2CPriorKnowledge(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hits := make(chan string, 1)
	srv := httpserver.New(ln.Addr().String(), http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		hits <- r.Proto
	}))
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- httpserver.Run(ctx, discardLogger(), srv) }()

	time.Sleep(100 * time.Millisecond)

	client := &http.Client{Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}}
	resp, err := client.Get("http://" + srv.Addr)
	if err != nil {
		t.Fatalf("h2c GET: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case proto := <-hits:
		if proto != "HTTP/2.0" {
			t.Fatalf("handler saw %q, want HTTP/2.0", proto)
		}
	case <-time.After(time.Second):
		t.Fatal("handler not invoked")
	}

	cancel()
	<-done
}

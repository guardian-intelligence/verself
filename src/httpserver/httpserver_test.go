package httpserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/verself/httpserver"
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
	publicLn.Close()
	internalLn.Close()

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

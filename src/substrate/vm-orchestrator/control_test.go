package vmorchestrator

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
)

func TestProbeGuestPreControlReadyUsesDedicatedGuestPort(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "vsock.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer func() { _ = listener.Close() }()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer func() { _ = conn.Close() }()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			serverDone <- fmt.Errorf("read connect command: %w", err)
			return
		}
		want := fmt.Sprintf("CONNECT %d\n", vmproto.GuestPreControlReadyPort)
		if line != want {
			serverDone <- fmt.Errorf("connect command = %q, want %q", line, want)
			return
		}
		if _, err := fmt.Fprintf(conn, "OK %d\n%s boot_ms=12\n", vmproto.GuestPreControlReadyPort, vmproto.PreControlReadyMessage); err != nil {
			serverDone <- fmt.Errorf("write readiness: %w", err)
			return
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, err := probeGuestPreControlReady(ctx, socketPath, "lease-a")
	if err != nil {
		t.Fatalf("probe readiness: %v", err)
	}
	if got, want := msg, vmproto.PreControlReadyMessage+" boot_ms=12"; got != want {
		t.Fatalf("readiness message = %q, want %q", got, want)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

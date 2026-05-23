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
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

func TestInitLeaseRecordsClockSyncOnLeaseInitSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	oldTracer := tracer
	tracer = provider.Tracer("vm-orchestrator")
	t.Cleanup(func() {
		tracer = oldTracer
		_ = provider.Shutdown(context.Background())
	})

	hostConn, guestConn := net.Pipe()
	control := &guestControl{conn: hostConn, reader: hostConn, codec: vmproto.NewCodec(hostConn, hostConn)}
	t.Cleanup(func() { _ = control.close() })

	serverDone := make(chan error, 1)
	go func() {
		defer func() { _ = guestConn.Close() }()
		codec := vmproto.NewCodec(guestConn, guestConn)
		env, err := codec.ReadEnvelope()
		if err != nil {
			serverDone <- fmt.Errorf("read lease init: %w", err)
			return
		}
		msg, err := vmproto.DecodePayload[vmproto.LeaseInit](env)
		if err != nil {
			serverDone <- err
			return
		}
		if env.Type != vmproto.TypeLeaseInit {
			serverDone <- fmt.Errorf("message type = %s, want %s", env.Type, vmproto.TypeLeaseInit)
			return
		}
		result, err := vmproto.NewEnvelope(vmproto.TypeLeaseInitResult, 1, time.Now().UnixNano(), vmproto.LeaseInitResult{
			LeaseID:         msg.LeaseID,
			ProtocolVersion: vmproto.ProtocolVersion,
			Timings: &vmproto.LeaseInitTimings{
				SyncClockMS:      123,
				TotalLeaseInitMS: 456,
				ClockSync: &vmproto.ClockSyncResult{
					Status:     "synchronized",
					Source:     "KVM",
					OffsetNS:   12,
					SkewPPM:    0.003,
					LeapStatus: "Normal",
					WaitSyncMS: 123,
				},
			},
		})
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- codec.WriteEnvelope(result)
	}()

	ctx, parent := tracer.Start(context.Background(), "test.parent")
	results, err := control.initLease(ctx, "lease-a", vmproto.NetworkConfig{}, nil, ActivationModeColdBoot)
	parent.End()
	if err != nil {
		t.Fatalf("init lease: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0", len(results))
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}

	span := findRecordedSpan(t, recorder.Ended(), "vmorchestrator.guest.lease_init")
	attrs := span.Attributes()
	assertSpanAttr(t, attrs, "guest.clock_sync.status", "synchronized")
	assertSpanAttr(t, attrs, "guest.clock_sync.source", "KVM")
	assertSpanAttr(t, attrs, "guest.clock_sync.offset_ns", int64(12))
	assertSpanAttr(t, attrs, "guest.clock_sync.skew_ppm", 0.003)
	assertSpanAttr(t, attrs, "guest.clock_sync.leap_status", "Normal")
	assertSpanAttr(t, attrs, "guest.clock_sync.waitsync_ms", int64(123))
	assertSpanAttr(t, attrs, "guest.lease_init.sync_clock_ms", int64(123))
}

func findRecordedSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found in %d ended spans", name, len(spans))
	return nil
}

func assertSpanAttr(t *testing.T, attrs []attribute.KeyValue, key string, want any) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) != key {
			continue
		}
		got := attr.Value.AsInterface()
		if got != want {
			t.Fatalf("attribute %s = %#v, want %#v", key, got, want)
		}
		return
	}
	t.Fatalf("attribute %s not found", key)
}

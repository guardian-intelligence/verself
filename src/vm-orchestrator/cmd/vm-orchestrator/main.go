package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	fmotel "github.com/forge-metal/otel"
	"go.opentelemetry.io/otel"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var listenUnix string
	flag.StringVar(&listenUnix, "listen-unix", "/run/vm-orchestrator/api.sock", "Unix socket path for the vm-orchestrator API")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(filepath.Dir(listenUnix), 0o755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if err := os.RemoveAll(listenUnix); err != nil {
		return fmt.Errorf("remove stale socket %s: %w", listenUnix, err)
	}

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "vm-orchestrator",
		ServiceVersion: "0.1.0",
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	listener, err := net.Listen("unix", listenUnix)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenUnix, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(listenUnix)
	}()

	if err := os.Chmod(listenUnix, 0o660); err != nil {
		return fmt.Errorf("chmod socket %s: %w", listenUnix, err)
	}

	startupCtx, startupSpan := otel.Tracer("vm-orchestrator").Start(ctx, "daemon.startup")
	slog.InfoContext(startupCtx, "vm-orchestrator listening", "socket", listenUnix)
	startupSpan.End()

	errCh := make(chan error, 1)
	go acceptLoop(ctx, listener, logger, errCh)

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownSpan := otel.Tracer("vm-orchestrator").Start(context.Background(), "daemon.shutdown")
		slog.InfoContext(shutdownCtx, "vm-orchestrator stopping")
		shutdownSpan.End()
		return nil
	case err := <-errCh:
		return err
	}
}

func acceptLoop(ctx context.Context, listener net.Listener, logger *slog.Logger, errCh chan<- error) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Temporary() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			select {
			case errCh <- fmt.Errorf("accept vm-orchestrator socket: %w", err):
			default:
			}
			return
		}

		go func(conn net.Conn) {
			defer conn.Close()
			logger.Info("accepted placeholder vm-orchestrator client", "remote", conn.RemoteAddr().String())
		}(conn)
	}
}

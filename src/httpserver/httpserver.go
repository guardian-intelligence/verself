// Package httpserver provides the standard Forge Metal HTTP server timeouts
// and the public/internal dual-listener runtime used by every service that
// exposes both a customer-facing API and an SPIFFE-mTLS internal plane.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Standard server timeouts. Changing any of these changes them everywhere
// that uses New(); services should never override them piecemeal.
const (
	ReadHeaderTimeout = 2 * time.Second
	ReadTimeout       = 5 * time.Second
	WriteTimeout      = 5 * time.Second
	IdleTimeout       = 30 * time.Second
	MaxHeaderBytes    = 16 << 10
)

// ShutdownTimeout is the graceful-shutdown deadline applied to every server
// managed by Run/RunPair.
const ShutdownTimeout = 5 * time.Second

// New returns an *http.Server wired with the standard Forge Metal timeouts.
// Any handler wrapping (otelhttp, request-body limits, allowlists) and TLS
// configuration remain the caller's responsibility.
func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
		MaxHeaderBytes:    MaxHeaderBytes,
	}
}

// Run blocks serving a single HTTP(S) server until ctx is done or the server
// returns. It triggers a graceful shutdown with ShutdownTimeout on exit.
// A non-nil TLSConfig on the server switches to ListenAndServeTLS.
func Run(ctx context.Context, logger *slog.Logger, srv *http.Server) error {
	return RunPair(ctx, logger, srv, nil)
}

// RunPair serves a public plane and an SPIFFE-mTLS internal plane
// concurrently and blocks until ctx is done or either listener returns.
// Either server may be nil to opt that plane out (equivalent to Run).
// A non-nil TLSConfig on a server switches it to ListenAndServeTLS.
func RunPair(ctx context.Context, logger *slog.Logger, public, internal *http.Server) error {
	entries := []entry{{plane: "public", server: public}, {plane: "internal", server: internal}}
	active := 0
	for _, e := range entries {
		if e.server != nil {
			active++
		}
	}
	if active == 0 {
		return errors.New("httpserver: at least one server required")
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		for _, e := range entries {
			if e.server == nil {
				continue
			}
			if err := e.server.Shutdown(shutdownCtx); err != nil {
				logger.ErrorContext(context.Background(), "httpserver: shutdown", "plane", e.plane, "addr", e.server.Addr, "error", err)
			}
		}
	}()

	errCh := make(chan error, active)
	for _, e := range entries {
		if e.server == nil {
			continue
		}
		logger.InfoContext(ctx, "httpserver: listening", "plane", e.plane, "addr", e.server.Addr)
		go serve(e, errCh)
	}

	var firstErr error
	for range active {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
			cancelRun()
		}
	}
	return firstErr
}

type entry struct {
	plane  string
	server *http.Server
}

func serve(e entry, errCh chan<- error) {
	var err error
	if e.server.TLSConfig != nil {
		err = e.server.ListenAndServeTLS("", "")
	} else {
		err = e.server.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- fmt.Errorf("%s listen: %w", e.plane, err)
		return
	}
	errCh <- nil
}

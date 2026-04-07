// Package testharness provides in-process billing-service wiring for e2e tests.
// This is the public entry point — internal packages stay internal.
package testharness

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	tb "github.com/tigerbeetle/tigerbeetle-go"

	"github.com/forge-metal/billing"
	"github.com/forge-metal/billing-service/internal/billingapi"
	billingruntime "github.com/forge-metal/billing-service/internal/runtime"
)

// Server is an in-process billing-service for e2e tests.
type Server struct {
	*httptest.Server
	app *billingruntime.App
}

// NewServer constructs a billing-service HTTP test server with all routes
// registered and ready to accept requests. The caller is responsible for
// closing the server and cancelling the worker context.
func NewServer(
	pg *sql.DB,
	tbClient tb.Client,
	chConn driver.Conn,
	billingClient *billing.Client,
	reconcileQuerier billing.ClickHouseQuerier,
	logger *slog.Logger,
) *Server {
	app := billingruntime.New(pg, tbClient, chConn, billingClient, reconcileQuerier, "whsec_test_unused", logger)
	mux := http.NewServeMux()
	billingapi.NewAPI(mux, app)
	srv := httptest.NewServer(mux)
	return &Server{Server: srv, app: app}
}

// RunWorker starts the billing background worker. Blocks until ctx is cancelled.
func (s *Server) RunWorker(ctx context.Context, pollInterval time.Duration) error {
	return s.app.RunWorker(ctx, pollInterval)
}

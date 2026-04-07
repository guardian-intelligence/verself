// Package testharness provides in-process sandbox-rental-service wiring for e2e tests.
// This is the public entry point — internal packages stay internal.
package testharness

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-client"
	fastsandbox "github.com/forge-metal/fast-sandbox"

	sandboxapi "github.com/forge-metal/sandbox-rental-service/internal/api"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

// SandboxRunner abstracts VM execution for tests. Identical method set to
// jobs.SandboxRunner — Go structural typing means any value implementing
// this interface also satisfies the internal one.
type SandboxRunner interface {
	Run(ctx context.Context, job fastsandbox.JobConfig) (fastsandbox.JobResult, error)
}

// Config holds all dependencies for a test sandbox-rental-service.
type Config struct {
	PG            *sql.DB
	CH            driver.Conn
	CHDatabase    string
	Runner        SandboxRunner
	Billing       *billingclient.ServiceClient
	BillingVCPUs  int
	BillingMemMiB int
	AuthCfg       auth.Config
	Logger        *slog.Logger
}

// Server is an in-process sandbox-rental-service for e2e tests.
type Server struct {
	*httptest.Server
}

// NewServer constructs a sandbox-rental-service HTTP test server with auth
// middleware and all routes registered.
func NewServer(cfg Config) *Server {
	svc := &jobs.Service{
		PG:            cfg.PG,
		CH:            cfg.CH,
		CHDatabase:    cfg.CHDatabase,
		Orchestrator:  cfg.Runner,
		Billing:       cfg.Billing,
		BillingVCPUs:  cfg.BillingVCPUs,
		BillingMemMiB: cfg.BillingMemMiB,
		Logger:        cfg.Logger,
	}

	mux := http.NewServeMux()
	humaAPI := humago.New(mux, huma.DefaultConfig("Sandbox Rental Service", "1.0.0"))
	sandboxapi.RegisterRoutes(humaAPI, svc, cfg.Billing)

	authHandler := auth.Middleware(cfg.AuthCfg)(mux)
	srv := httptest.NewServer(authHandler)
	return &Server{Server: srv}
}

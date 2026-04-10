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

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"

	sandboxapi "github.com/forge-metal/sandbox-rental-service/internal/api"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

// Runner abstracts VM execution for tests. Identical method set to
// jobs.Runner — Go structural typing means any value implementing
// this interface also satisfies the internal one.
type Runner interface {
	Run(ctx context.Context, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error)
	RunWithConfig(ctx context.Context, cfg vmorchestrator.Config, job vmorchestrator.JobConfig) (vmorchestrator.JobResult, error)
	ExecRepo(ctx context.Context, req vmorchestrator.RepoExecRequest) (vmorchestrator.JobStatus, error)
	WarmGolden(ctx context.Context, req vmorchestrator.WarmGoldenRequest) (vmorchestrator.WarmGoldenResult, error)
}

// BillingClient abstracts the billing client dependency for tests.
type BillingClient interface {
	Reserve(
		ctx context.Context,
		jobID int64,
		orgID uint64,
		productID string,
		actorID string,
		concurrentCount uint64,
		sourceType string,
		sourceRef string,
		allocation map[string]float64,
		reqEditors ...billingclient.RequestEditorFn,
	) (billingclient.Reservation, error)
	Settle(ctx context.Context, reservation billingclient.Reservation, actualSeconds uint32, reqEditors ...billingclient.RequestEditorFn) error
	Void(ctx context.Context, reservation billingclient.Reservation, reqEditors ...billingclient.RequestEditorFn) error
}

// Config holds all dependencies for a test sandbox-rental-service.
type Config struct {
	PG                        *sql.DB
	CH                        driver.Conn
	CHDatabase                string
	Runner                    Runner
	Billing                   *billingclient.ServiceClient
	PlatformOrgID             uint64
	ForgejoWebhookSecret      string
	BillingVCPUs              int
	BillingMemMiB             int
	ForgejoURL                string
	ForgejoRunnerLabel        string
	ForgejoRunnerToken        string
	ForgejoRunnerBinaryURL    string
	ForgejoRunnerBinarySHA256 string
	AuthCfg                   auth.Config
	Logger                    *slog.Logger
}

// Server is an in-process sandbox-rental-service for e2e tests.
type Server struct {
	*httptest.Server
	service *jobs.Service
}

// NewServer constructs a sandbox-rental-service HTTP test server with auth
// middleware and all routes registered.
func NewServer(cfg Config) *Server {
	svc := &jobs.Service{
		PG:                        cfg.PG,
		CH:                        cfg.CH,
		CHDatabase:                cfg.CHDatabase,
		Orchestrator:              cfg.Runner,
		Billing:                   cfg.Billing,
		BillingVCPUs:              cfg.BillingVCPUs,
		BillingMemMiB:             cfg.BillingMemMiB,
		ForgejoURL:                cfg.ForgejoURL,
		ForgejoRunnerLabel:        cfg.ForgejoRunnerLabel,
		ForgejoRunnerToken:        cfg.ForgejoRunnerToken,
		ForgejoRunnerBinaryURL:    cfg.ForgejoRunnerBinaryURL,
		ForgejoRunnerBinarySHA256: cfg.ForgejoRunnerBinarySHA256,
		Logger:                    cfg.Logger,
	}

	rootMux := http.NewServeMux()
	privateMux := http.NewServeMux()
	sandboxapi.NewAPI(privateMux, "1.0.0", "127.0.0.1:0", svc, cfg.Billing)
	sandboxapi.RegisterPublicRoutes(rootMux, svc, sandboxapi.ForgejoWebhookConfig{
		PlatformOrgID: cfg.PlatformOrgID,
		ActorID:       "system:forgejo-webhook",
		Secret:        cfg.ForgejoWebhookSecret,
	})

	authHandler := auth.Middleware(cfg.AuthCfg)(privateMux)
	rootMux.Handle("/", authHandler)
	srv := httptest.NewServer(rootMux)
	return &Server{Server: srv, service: svc}
}

func (s *Server) Reconcile(ctx context.Context) error {
	if s == nil || s.service == nil {
		return nil
	}
	return s.service.Reconcile(ctx)
}

func (s *Server) SetBillingClient(client BillingClient) {
	if s == nil || s.service == nil || client == nil {
		return
	}
	s.service.Billing = client
}

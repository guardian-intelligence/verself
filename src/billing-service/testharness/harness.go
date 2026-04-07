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
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"

	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billingapi"
	billingruntime "github.com/forge-metal/billing-service/internal/runtime"
)

// Config holds all dependencies for a test billing-service.
// No billing domain types are exposed — raw infrastructure only.
type Config struct {
	PG              *sql.DB
	TBClient        tb.Client
	TBAddresses     []string
	TBClusterID     uint64
	CHConn          driver.Conn
	CHDatabase      string
	StripeSecretKey string
	Logger          *slog.Logger
}

// Server is an in-process billing-service for e2e tests.
type Server struct {
	*httptest.Server
	app            *billingruntime.App
	billingClient  *billing.Client
	meteringWriter *billing.AsyncMeteringWriter
}

// NewServer constructs a billing-service HTTP test server with all routes
// registered and ready to accept requests. The caller is responsible for
// closing the server and cancelling the worker context.
func NewServer(cfg Config) *Server {
	sc := stripe.NewClient(cfg.StripeSecretKey)
	meteringSink := billing.NewClickHouseMeteringWriter(cfg.CHConn, cfg.CHDatabase)
	meteringWriter := billing.NewAsyncMeteringWriter(meteringSink, billing.AsyncMeteringWriterConfig{})
	meteringQuerier := billing.NewClickHouseMeteringQuerier(cfg.CHConn, cfg.CHDatabase)
	reconcileQuerier := billing.NewClickHouseReconcileQuerier(cfg.CHConn, cfg.CHDatabase)

	billingCfg := billing.DefaultConfig()
	billingCfg.StripeSecretKey = cfg.StripeSecretKey
	billingCfg.TigerBeetleAddresses = cfg.TBAddresses
	billingCfg.TigerBeetleClusterID = cfg.TBClusterID

	billingClient, err := billing.NewClient(cfg.TBClient, cfg.PG, sc, meteringWriter, meteringQuerier, billingCfg)
	if err != nil {
		panic("testharness: create billing client: " + err.Error())
	}

	app := billingruntime.New(cfg.PG, cfg.TBClient, cfg.CHConn, billingClient, reconcileQuerier, "whsec_test_unused", cfg.Logger)
	mux := http.NewServeMux()
	billingapi.NewAPI(mux, app)
	srv := httptest.NewServer(mux)

	return &Server{
		Server:         srv,
		app:            app,
		billingClient:  billingClient,
		meteringWriter: meteringWriter,
	}
}

// RunWorker starts the billing background worker. Blocks until ctx is cancelled.
func (s *Server) RunWorker(ctx context.Context, pollInterval time.Duration) error {
	return s.app.RunWorker(ctx, pollInterval)
}

// SeedOrg creates TigerBeetle accounts for the given org.
func (s *Server) SeedOrg(ctx context.Context, orgID uint64, name string) error {
	return s.billingClient.EnsureOrg(ctx, billing.OrgID(orgID), name)
}

// SeedCredits deposits credits into the org's account. Returns true if a new
// grant was created (false on idempotent replay).
func (s *Server) SeedCredits(ctx context.Context, orgID uint64, productID string, amount uint64, source string, stripeRef string, expiresAt time.Time) (bool, error) {
	taskID := billing.TaskID(time.Now().UnixNano())
	return s.billingClient.DepositCredits(ctx, &taskID, billing.CreditGrant{
		OrgID:             billing.OrgID(orgID),
		ProductID:         productID,
		Amount:            amount,
		Source:            source,
		StripeReferenceID: stripeRef,
		ExpiresAt:         &expiresAt,
	})
}

// GetBalance returns the org's credit balance as (available, pending).
func (s *Server) GetBalance(ctx context.Context, orgID uint64) (available uint64, pending uint64, err error) {
	b, err := s.billingClient.GetOrgBalance(ctx, billing.OrgID(orgID))
	if err != nil {
		return 0, 0, err
	}
	return b.CreditAvailable, b.CreditPending, nil
}

// FlushMetering flushes the async metering writer and waits briefly for
// ClickHouse to process. Non-fatal errors are logged.
func (s *Server) FlushMetering(ctx context.Context) error {
	return s.meteringWriter.Close(ctx)
}

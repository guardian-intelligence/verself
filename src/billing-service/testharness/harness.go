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

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billingapi"
)

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

type Server struct {
	*httptest.Server
	client *billing.Client
}

func NewServer(cfg Config) *Server {
	meteringWriter := billing.NewClickHouseMeteringWriter(cfg.CHConn, cfg.CHDatabase)
	billingCfg := billing.DefaultConfig()
	billingCfg.StripeSecretKey = cfg.StripeSecretKey
	billingCfg.TigerBeetleAddresses = cfg.TBAddresses
	billingCfg.TigerBeetleClusterID = cfg.TBClusterID

	client, err := billing.NewClient(
		cfg.TBClient,
		cfg.PG,
		stripe.NewClient(cfg.StripeSecretKey),
		meteringWriter,
		billingCfg,
	)
	if err != nil {
		panic("testharness: create billing client: " + err.Error())
	}

	mux := http.NewServeMux()
	billingapi.NewAPI(mux, billingapi.Config{
		Version:      "2.0.0",
		ListenAddr:   "127.0.0.1:0",
		Client:       client,
		Logger:       cfg.Logger,
		InternalRole: "billing_internal",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := &auth.Identity{
			Subject: "test:sandbox-rental-service",
			Roles:   []string{"billing_internal"},
		}
		mux.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
	})

	return &Server{
		Server: httptest.NewServer(handler),
		client: client,
	}
}

func (s *Server) RunProjector(ctx context.Context, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.client.ProjectPendingWindows(ctx, 100); err != nil {
				return err
			}
		}
	}
}

func (s *Server) SeedOrg(ctx context.Context, orgID uint64, name string, trustTier string) error {
	return s.client.EnsureOrg(ctx, billing.OrgID(orgID), name, trustTier)
}

func (s *Server) SeedCredits(ctx context.Context, orgID uint64, productID string, amount uint64, source string, stripeRef string, expiresAt time.Time) (billing.GrantBalance, error) {
	return s.client.DepositCredits(ctx, billing.CreditGrant{
		OrgID:             billing.OrgID(orgID),
		ScopeType:         billing.GrantScopeProduct,
		ScopeProductID:    productID,
		Amount:            amount,
		Source:            source,
		StripeReferenceID: stripeRef,
		ExpiresAt:         &expiresAt,
	})
}

func (s *Server) GetBalance(ctx context.Context, orgID uint64) (available uint64, pending uint64, err error) {
	b, err := s.client.GetOrgBalance(ctx, billing.OrgID(orgID))
	if err != nil {
		return 0, 0, err
	}
	return b.CreditAvailable, b.CreditPending, nil
}

func (s *Server) ProjectPendingWindows(ctx context.Context) (int, error) {
	return s.client.ProjectPendingWindows(ctx, 100)
}

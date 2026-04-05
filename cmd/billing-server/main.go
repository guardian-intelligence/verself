package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"

	"github.com/forge-metal/forge-metal/internal/billing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// --- env vars aligned with Ansible credstore ---
	pgDSN := requireEnv("BILLING_PG_DSN")
	stripeKey := requireEnv("STRIPE_SECRET_KEY")
	webhookSecret := requireEnv("STRIPE_WEBHOOK_SECRET")

	tbAddress := envOr("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	tbClusterID := envUint64("BILLING_TB_CLUSTER_ID", 0)
	chAddress := envOr("BILLING_CH_ADDRESS", "127.0.0.1:9000")
	chPassword := envOr("BILLING_CH_PASSWORD", "")
	listenAddr := envOr("BILLING_LISTEN_ADDR", "127.0.0.1:4242")

	// --- open connections ---
	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	if err := pg.Ping(); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	tbAddresses := strings.Split(tbAddress, ",")
	tbClient, err := tb.NewClient(tbtypes.ToUint128(tbClusterID), tbAddresses)
	if err != nil {
		return fmt.Errorf("create tigerbeetle client: %w", err)
	}
	defer tbClient.Close()

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()

	sc := stripe.NewClient(stripeKey)

	meteringWriter := billing.NewClickHouseMeteringWriter(chConn, "forge_metal")
	meteringQuerier := billing.NewClickHouseMeteringQuerier(chConn, "forge_metal")
	reconcileQuerier := billing.NewClickHouseReconcileQuerier(chConn, "forge_metal")

	cfg := billing.DefaultConfig()
	cfg.StripeSecretKey = stripeKey
	cfg.StripeWebhookSecret = webhookSecret
	cfg.PgDSN = pgDSN
	cfg.TigerBeetleAddresses = tbAddresses
	cfg.TigerBeetleClusterID = tbClusterID

	client, err := billing.NewClient(tbClient, pg, sc, meteringWriter, meteringQuerier, cfg)
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	// --- Huma API ---
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Billing API", "1.0.0"))

	registerRoutes(api, client, reconcileQuerier)

	// Mount Stripe webhook as raw handler (needs raw body for signature verification).
	mux.Handle("POST /webhooks/stripe", client.WebhookHandler())

	// --- server lifecycle ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	workerDone := make(chan error, 1)
	go func() {
		workerDone <- client.RunWorker(workerCtx, 5*time.Second)
	}()

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("billing: shutdown: %v", err)
		}
	}()

	log.Printf("billing: listening on %s", listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		workerCancel()
		return fmt.Errorf("billing: listen: %w", err)
	}

	workerCancel()
	<-workerDone
	return nil
}

// --- Huma route registration ---

func registerRoutes(api huma.API, client *billing.Client, reconcileQuerier billing.ClickHouseQuerier) {
	// Read-only endpoints for Next.js apps.
	huma.Register(api, huma.Operation{
		OperationID: "get-org-balance",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/balance",
		Summary:     "Get org balance across all grants",
	}, getOrgBalance(client))

	huma.Register(api, huma.Operation{
		OperationID: "get-product-balance",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/products/{product_id}/balance",
		Summary:     "Get product-specific balance for an org",
	}, getProductBalance(client))

	huma.Register(api, huma.Operation{
		OperationID: "list-subscriptions",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/subscriptions",
		Summary:     "List subscriptions for an org",
	}, listSubscriptions(client))

	huma.Register(api, huma.Operation{
		OperationID: "list-grants",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/grants",
		Summary:     "List credit grants for an org",
	}, listGrants(client))

	huma.Register(api, huma.Operation{
		OperationID: "list-usage",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/usage",
		Summary:     "List billing events for an org",
	}, listUsage(client))

	// Write endpoints — Next.js apps call these to create Stripe sessions.
	huma.Register(api, huma.Operation{
		OperationID:   "create-checkout",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/checkout",
		Summary:       "Create a Stripe checkout session",
		DefaultStatus: 200,
	}, createCheckout(client))

	huma.Register(api, huma.Operation{
		OperationID:   "create-subscription",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/subscribe",
		Summary:       "Create a Stripe subscription checkout",
		DefaultStatus: 200,
	}, createSubscription(client))

	// Internal ops endpoints — invoked by cron or admin tooling.
	huma.Register(api, huma.Operation{
		OperationID:   "ops-deposit-credits",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/deposit-credits",
		Summary:       "Deposit subscription credits for the current period",
		DefaultStatus: 200,
	}, opsDepositCredits(client))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-expire-credits",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/expire-credits",
		Summary:       "Expire credit grants past their expiry date",
		DefaultStatus: 200,
	}, opsExpireCredits(client))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-reconcile",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/reconcile",
		Summary:       "Run 6-check reconciliation across PG, TigerBeetle, and ClickHouse",
		DefaultStatus: 200,
	}, opsReconcile(client, reconcileQuerier))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-trust-tier-evaluate",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/trust-tier-evaluate",
		Summary:       "Evaluate and update org trust tiers",
		DefaultStatus: 200,
	}, opsTrustTierEvaluate(client))
}

// --- typed inputs/outputs ---

type orgIDPath struct {
	OrgID uint64 `path:"org_id" doc:"Organization ID"`
}

type orgProductPath struct {
	OrgID     uint64 `path:"org_id" doc:"Organization ID"`
	ProductID string `path:"product_id" doc:"Product ID"`
}

type balanceOutput struct {
	Body struct {
		OrgID              string `json:"org_id"`
		FreeTierAvailable  uint64 `json:"free_tier_available"`
		FreeTierPending    uint64 `json:"free_tier_pending"`
		CreditAvailable    uint64 `json:"credit_available"`
		CreditPending      uint64 `json:"credit_pending"`
		TotalAvailable     uint64 `json:"total_available"`
	}
}

type productBalanceOutput struct {
	Body struct {
		OrgID             string `json:"org_id"`
		ProductID         string `json:"product_id"`
		FreeTierRemaining uint64 `json:"free_tier_remaining"`
		IncludedRemaining uint64 `json:"included_remaining"`
		PrepaidRemaining  uint64 `json:"prepaid_remaining"`
	}
}

type subscriptionsOutput struct {
	Body struct {
		OrgID         string                    `json:"org_id"`
		Subscriptions []billing.SubscriptionRow `json:"subscriptions"`
	}
}

type grantsInput struct {
	OrgID     uint64 `path:"org_id" doc:"Organization ID"`
	ProductID string `query:"product_id,omitempty" doc:"Filter by product"`
	Active    bool   `query:"active,omitempty" doc:"Only active grants"`
}

type grantsOutput struct {
	Body struct {
		OrgID  string             `json:"org_id"`
		Grants []billing.GrantRow `json:"grants"`
	}
}

type usageInput struct {
	OrgID     uint64 `path:"org_id" doc:"Organization ID"`
	ProductID string `query:"product_id,omitempty" doc:"Filter by product"`
	Since     string `query:"since,omitempty" doc:"RFC3339 lower bound on created_at"`
	Limit     int    `query:"limit,omitempty" doc:"Max results (1-1000, default 100)"`
}

type usageOutput struct {
	Body struct {
		OrgID  string                `json:"org_id"`
		Events []billing.UsageEvent  `json:"events"`
	}
}

type checkoutInput struct {
	Body struct {
		OrgID       uint64 `json:"org_id" required:"true"`
		ProductID   string `json:"product_id" required:"true"`
		AmountCents int64  `json:"amount_cents" minimum:"1" required:"true"`
		SuccessURL  string `json:"success_url" required:"true"`
		CancelURL   string `json:"cancel_url" required:"true"`
	}
}

type urlOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

type subscriptionInput struct {
	Body struct {
		OrgID      uint64 `json:"org_id" required:"true"`
		PlanID     string `json:"plan_id" required:"true"`
		Cadence    string `json:"cadence,omitempty"`
		SuccessURL string `json:"success_url" required:"true"`
		CancelURL  string `json:"cancel_url" required:"true"`
	}
}

type depositCreditsOutput struct {
	Body struct {
		SubscriptionsProcessed int `json:"subscriptions_processed"`
		CreditsDeposited       int `json:"credits_deposited"`
		CreditsSkipped         int `json:"credits_skipped"`
		CreditsFailed          int `json:"credits_failed"`
	}
}

type expireCreditsOutput struct {
	Body struct {
		GrantsChecked int    `json:"grants_checked"`
		GrantsExpired int    `json:"grants_expired"`
		GrantsFailed  int    `json:"grants_failed"`
		UnitsExpired  uint64 `json:"units_expired"`
	}
}

type reconcileCheckJSON struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Passed   bool   `json:"passed"`
	Details  string `json:"details,omitempty"`
}

type reconcileOutput struct {
	Body struct {
		Checks    []reconcileCheckJSON `json:"checks"`
		HasAlerts bool                 `json:"has_alerts"`
	}
}

type trustTierOutput struct {
	Body struct {
		OrgPromoted int `json:"org_promoted"`
		OrgDemoted  int `json:"org_demoted"`
	}
}

// --- handler factories ---

func getOrgBalance(client *billing.Client) func(context.Context, *orgIDPath) (*balanceOutput, error) {
	return func(ctx context.Context, input *orgIDPath) (*balanceOutput, error) {
		balance, err := client.GetOrgBalance(ctx, billing.OrgID(input.OrgID))
		if err != nil {
			return nil, huma.Error500InternalServerError("get balance", err)
		}
		out := &balanceOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.FreeTierAvailable = balance.FreeTierAvailable
		out.Body.FreeTierPending = balance.FreeTierPending
		out.Body.CreditAvailable = balance.CreditAvailable
		out.Body.CreditPending = balance.CreditPending
		out.Body.TotalAvailable = balance.TotalAvailable
		return out, nil
	}
}

func getProductBalance(client *billing.Client) func(context.Context, *orgProductPath) (*productBalanceOutput, error) {
	return func(ctx context.Context, input *orgProductPath) (*productBalanceOutput, error) {
		balance, err := client.GetProductBalance(ctx, billing.OrgID(input.OrgID), input.ProductID)
		if err != nil {
			return nil, huma.Error500InternalServerError("get product balance", err)
		}
		out := &productBalanceOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.ProductID = input.ProductID
		out.Body.FreeTierRemaining = balance.FreeTierRemaining
		out.Body.IncludedRemaining = balance.IncludedRemaining
		out.Body.PrepaidRemaining = balance.PrepaidRemaining
		return out, nil
	}
}

func listSubscriptions(client *billing.Client) func(context.Context, *orgIDPath) (*subscriptionsOutput, error) {
	return func(ctx context.Context, input *orgIDPath) (*subscriptionsOutput, error) {
		subs, err := client.ListSubscriptions(ctx, billing.OrgID(input.OrgID))
		if err != nil {
			return nil, huma.Error500InternalServerError("list subscriptions", err)
		}
		out := &subscriptionsOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.Subscriptions = subs
		return out, nil
	}
}

func listGrants(client *billing.Client) func(context.Context, *grantsInput) (*grantsOutput, error) {
	return func(ctx context.Context, input *grantsInput) (*grantsOutput, error) {
		grants, err := client.ListGrants(ctx, billing.OrgID(input.OrgID), input.ProductID, input.Active)
		if err != nil {
			return nil, huma.Error500InternalServerError("list grants", err)
		}
		out := &grantsOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.Grants = grants
		return out, nil
	}
}

func listUsage(client *billing.Client) func(context.Context, *usageInput) (*usageOutput, error) {
	return func(ctx context.Context, input *usageInput) (*usageOutput, error) {
		var since *time.Time
		if input.Since != "" {
			t, err := time.Parse(time.RFC3339, input.Since)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid since: " + err.Error())
			}
			since = &t
		}
		events, err := client.ListUsageEvents(ctx, billing.OrgID(input.OrgID), input.ProductID, since, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("list usage", err)
		}
		out := &usageOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.Events = events
		return out, nil
	}
}

func createCheckout(client *billing.Client) func(context.Context, *checkoutInput) (*urlOutput, error) {
	return func(ctx context.Context, input *checkoutInput) (*urlOutput, error) {
		url, err := client.CreateCheckoutSession(ctx, billing.OrgID(input.Body.OrgID), input.Body.ProductID, billing.CheckoutParams{
			AmountCents: input.Body.AmountCents,
			SuccessURL:  input.Body.SuccessURL,
			CancelURL:   input.Body.CancelURL,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("create checkout", err)
		}
		out := &urlOutput{}
		out.Body.URL = url
		return out, nil
	}
}

func createSubscription(client *billing.Client) func(context.Context, *subscriptionInput) (*urlOutput, error) {
	return func(ctx context.Context, input *subscriptionInput) (*urlOutput, error) {
		cadence := billing.BillingCadence(input.Body.Cadence)
		if cadence == "" {
			cadence = billing.CadenceMonthly
		}
		url, err := client.CreateSubscription(ctx, billing.OrgID(input.Body.OrgID), input.Body.PlanID, cadence, input.Body.SuccessURL, input.Body.CancelURL)
		if err != nil {
			return nil, huma.Error500InternalServerError("create subscription", err)
		}
		out := &urlOutput{}
		out.Body.URL = url
		return out, nil
	}
}

// --- ops handlers ---

func opsDepositCredits(client *billing.Client) func(context.Context, *struct{}) (*depositCreditsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*depositCreditsOutput, error) {
		result, err := client.DepositSubscriptionCredits(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("deposit credits", err)
		}
		out := &depositCreditsOutput{}
		out.Body.SubscriptionsProcessed = result.SubscriptionsProcessed
		out.Body.CreditsDeposited = result.CreditsDeposited
		out.Body.CreditsSkipped = result.CreditsSkipped
		out.Body.CreditsFailed = result.CreditsFailed
		return out, nil
	}
}

func opsExpireCredits(client *billing.Client) func(context.Context, *struct{}) (*expireCreditsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*expireCreditsOutput, error) {
		result, err := client.ExpireCredits(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("expire credits", err)
		}
		out := &expireCreditsOutput{}
		out.Body.GrantsChecked = result.GrantsChecked
		out.Body.GrantsExpired = result.GrantsExpired
		out.Body.GrantsFailed = result.GrantsFailed
		out.Body.UnitsExpired = result.UnitsExpired
		return out, nil
	}
}

func opsReconcile(client *billing.Client, chQuerier billing.ClickHouseQuerier) func(context.Context, *struct{}) (*reconcileOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*reconcileOutput, error) {
		result, err := client.Reconcile(ctx, chQuerier)
		if err != nil {
			return nil, huma.Error500InternalServerError("reconcile", err)
		}
		out := &reconcileOutput{}
		for _, check := range result.Checks {
			out.Body.Checks = append(out.Body.Checks, reconcileCheckJSON{
				Name:     check.Name,
				Severity: check.Severity,
				Passed:   check.Passed,
				Details:  check.Details,
			})
		}
		out.Body.HasAlerts = result.HasAlerts()
		return out, nil
	}
}

func opsTrustTierEvaluate(client *billing.Client) func(context.Context, *struct{}) (*trustTierOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*trustTierOutput, error) {
		result, err := client.EvaluateTrustTiers(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("evaluate trust tiers", err)
		}
		out := &trustTierOutput{}
		out.Body.OrgPromoted = result.OrgPromoted
		out.Body.OrgDemoted = result.OrgDemoted
		return out, nil
	}
}

// --- env helpers ---

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "required env var %s is not set\n", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envUint64(key string, fallback uint64) uint64 {
	s := os.Getenv(key)
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s: %v\n", key, err)
		os.Exit(1)
	}
	return v
}


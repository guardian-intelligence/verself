package billingapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/billing-service/internal/billing"
	billingruntime "github.com/forge-metal/billing-service/internal/runtime"
)

const serviceVersion = "1.1.0"

var tracer = otel.Tracer("billing-service")

func NewAPI(mux *http.ServeMux, app *billingruntime.App) huma.API {
	config := huma.DefaultConfig("Billing Service", serviceVersion)
	config.OpenAPI.Servers = []*huma.Server{
		{URL: "http://127.0.0.1:4242"},
	}
	api := humago.New(mux, config)
	registerRoutes(api, app)
	return api
}

func OpenAPIDowngradeYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), nil)
	return api.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), nil)
	return api.OpenAPI().YAML()
}

func registerRoutes(api huma.API, app *billingruntime.App) {
	huma.Register(api, huma.Operation{
		OperationID: "healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness probe",
	}, healthz())

	huma.Register(api, huma.Operation{
		OperationID: "readyz",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Readiness probe",
	}, readyz(app))

	huma.Register(api, huma.Operation{
		OperationID: "get-org-balance",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/balance",
		Summary:     "Get org balance across all grants",
	}, getOrgBalance(app))

	huma.Register(api, huma.Operation{
		OperationID: "get-product-balance",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/products/{product_id}/balance",
		Summary:     "Get product-specific balance for an org",
	}, getProductBalance(app))

	huma.Register(api, huma.Operation{
		OperationID: "list-subscriptions",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/subscriptions",
		Summary:     "List subscriptions for an org",
	}, listSubscriptions(app))

	huma.Register(api, huma.Operation{
		OperationID: "list-grants",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/grants",
		Summary:     "List credit grants for an org",
	}, listGrants(app))

	huma.Register(api, huma.Operation{
		OperationID: "list-usage",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/usage",
		Summary:     "List billing events for an org",
	}, listUsage(app))

	huma.Register(api, huma.Operation{
		OperationID:   "check-quotas",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/check-quotas",
		Summary:       "Check quota windows for a metered product",
		DefaultStatus: 200,
	}, checkQuotas(app))

	huma.Register(api, huma.Operation{
		OperationID:   "reserve",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/reserve",
		Summary:       "Reserve one metered billing window",
		DefaultStatus: 200,
	}, reserve(app))

	huma.Register(api, huma.Operation{
		OperationID:   "settle",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/settle",
		Summary:       "Settle a metered reservation",
		DefaultStatus: 200,
	}, settle(app))

	huma.Register(api, huma.Operation{
		OperationID:   "renew",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/renew",
		Summary:       "Renew a metered reservation window",
		DefaultStatus: 200,
	}, renew(app))

	huma.Register(api, huma.Operation{
		OperationID:   "void",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/void",
		Summary:       "Void a metered reservation",
		DefaultStatus: 200,
	}, voidReservation(app))

	huma.Register(api, huma.Operation{
		OperationID:   "create-checkout",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/checkout",
		Summary:       "Create a Stripe checkout session",
		DefaultStatus: 200,
	}, createCheckout(app))

	huma.Register(api, huma.Operation{
		OperationID:   "create-subscription",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/subscribe",
		Summary:       "Create a Stripe subscription checkout",
		DefaultStatus: 200,
	}, createSubscription(app))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-deposit-credits",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/deposit-credits",
		Summary:       "Deposit subscription credits for the current period",
		DefaultStatus: 200,
	}, opsDepositCredits(app))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-expire-credits",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/expire-credits",
		Summary:       "Expire credit grants past their expiry date",
		DefaultStatus: 200,
	}, opsExpireCredits(app))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-reconcile",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/reconcile",
		Summary:       "Run reconciliation across PG, TigerBeetle, and ClickHouse",
		DefaultStatus: 200,
	}, opsReconcile(app))

	huma.Register(api, huma.Operation{
		OperationID:   "ops-trust-tier-evaluate",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/ops/trust-tier-evaluate",
		Summary:       "Evaluate and update org trust tiers",
		DefaultStatus: 200,
	}, opsTrustTierEvaluate(app))
}

type orgIDPath struct {
	OrgID uint64 `path:"org_id" doc:"Organization ID"`
}

type orgProductPath struct {
	OrgID     uint64 `path:"org_id" doc:"Organization ID"`
	ProductID string `path:"product_id" doc:"Product ID"`
}

type healthOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

type balanceOutput struct {
	Body struct {
		OrgID             string `json:"org_id"`
		FreeTierAvailable uint64 `json:"free_tier_available"`
		FreeTierPending   uint64 `json:"free_tier_pending"`
		CreditAvailable   uint64 `json:"credit_available"`
		CreditPending     uint64 `json:"credit_pending"`
		TotalAvailable    uint64 `json:"total_available"`
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

type subscriptionJSON struct {
	SubscriptionID       int64      `json:"subscription_id"`
	PlanID               string     `json:"plan_id"`
	ProductID            string     `json:"product_id"`
	Cadence              string     `json:"cadence"`
	Status               string     `json:"status"`
	StripeSubscriptionID *string    `json:"stripe_subscription_id,omitempty"`
	CurrentPeriodStart   *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty"`
	OverageCapUnits      *int64     `json:"overage_cap_units,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type grantJSON struct {
	GrantID   string     `json:"grant_id"`
	ProductID string     `json:"product_id"`
	Amount    int64      `json:"amount"`
	Source    string     `json:"source"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type usageEventJSON struct {
	EventID   int64           `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type subscriptionsOutput struct {
	Body struct {
		OrgID         string             `json:"org_id"`
		Subscriptions []subscriptionJSON `json:"subscriptions"`
	}
}

type grantsInput struct {
	OrgID     uint64 `path:"org_id" doc:"Organization ID"`
	ProductID string `query:"product_id,omitempty" doc:"Filter by product"`
	Active    bool   `query:"active,omitempty" doc:"Only active grants"`
}

type grantsOutput struct {
	Body struct {
		OrgID  string      `json:"org_id"`
		Grants []grantJSON `json:"grants"`
	}
}

type usageInput struct {
	OrgID     uint64 `path:"org_id" doc:"Organization ID"`
	ProductID string `query:"product_id,omitempty" doc:"Filter by product"`
	Since     string `query:"since,omitempty" doc:"RFC3339 lower bound on created_at"`
	Limit     int    `query:"limit,omitempty" minimum:"1" maximum:"1000" doc:"Max results (1-1000, default 100)"`
}

type usageOutput struct {
	Body struct {
		OrgID  string           `json:"org_id"`
		Events []usageEventJSON `json:"events"`
	}
}

type quotaCheckInput struct {
	Body struct {
		OrgID           uint64 `json:"org_id" required:"true" minimum:"1"`
		ProductID       string `json:"product_id" required:"true" maxLength:"255"`
		ConcurrentCount uint64 `json:"concurrent_count" required:"true"`
	}
}

type quotaViolationJSON struct {
	Dimension string `json:"dimension"`
	Window    string `json:"window"`
	Limit     uint64 `json:"limit"`
	Current   uint64 `json:"current"`
}

type quotaCheckOutput struct {
	Body struct {
		Allowed    bool                 `json:"allowed"`
		Violations []quotaViolationJSON `json:"violations,omitempty"`
	}
}

type reserveInput struct {
	Body struct {
		JobID           int64              `json:"job_id" required:"true" minimum:"1"`
		OrgID           uint64             `json:"org_id" required:"true" minimum:"1"`
		ProductID       string             `json:"product_id" required:"true" maxLength:"255"`
		ActorID         string             `json:"actor_id" required:"true" maxLength:"255"`
		ConcurrentCount uint64             `json:"concurrent_count" required:"true"`
		SourceType      string             `json:"source_type" required:"true" maxLength:"255"`
		SourceRef       string             `json:"source_ref" required:"true" maxLength:"255"`
		Allocation      map[string]float64 `json:"allocation" required:"true"`
	}
}

type grantLegJSON struct {
	GrantID    string `json:"grant_id"`
	TransferID string `json:"transfer_id"`
	Amount     uint64 `json:"amount"`
	Source     string `json:"source"`
}

type reservationJSON struct {
	JobID               int64              `json:"job_id"`
	OrgID               uint64             `json:"org_id"`
	ProductID           string             `json:"product_id"`
	PlanID              string             `json:"plan_id"`
	ActorID             string             `json:"actor_id"`
	SourceType          string             `json:"source_type"`
	SourceRef           string             `json:"source_ref"`
	WindowSeq           uint32             `json:"window_seq"`
	WindowSecs          uint32             `json:"window_secs"`
	WindowStart         time.Time          `json:"window_start"`
	ExpiresAt           time.Time          `json:"expires_at"`
	RenewBy             time.Time          `json:"renew_by"`
	PricingPhase        string             `json:"pricing_phase"`
	Allocation          map[string]float64 `json:"allocation"`
	UnitRates           map[string]uint64  `json:"unit_rates"`
	CostPerSec          uint64             `json:"cost_per_sec"`
	GrantLegs           []grantLegJSON     `json:"grant_legs"`
	SpendCapPeriodStart *time.Time         `json:"spend_cap_period_start,omitempty"`
}

type reserveOutput struct {
	Body struct {
		Reservation reservationJSON `json:"reservation"`
	}
}

type settleInput struct {
	Body struct {
		Reservation   reservationJSON `json:"reservation" required:"true"`
		ActualSeconds uint32          `json:"actual_seconds" required:"true" minimum:"1"`
	}
}

type renewInput struct {
	Body struct {
		Reservation   reservationJSON `json:"reservation" required:"true"`
		ActualSeconds uint32          `json:"actual_seconds" required:"true" minimum:"1"`
	}
}

type voidInput struct {
	Body struct {
		Reservation reservationJSON `json:"reservation" required:"true"`
	}
}

type statusOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

type checkoutInput struct {
	Body struct {
		OrgID       uint64 `json:"org_id" required:"true" minimum:"1"`
		ProductID   string `json:"product_id" required:"true" maxLength:"255"`
		AmountCents int64  `json:"amount_cents" minimum:"1" required:"true"`
		SuccessURL  string `json:"success_url" required:"true" maxLength:"2048"`
		CancelURL   string `json:"cancel_url" required:"true" maxLength:"2048"`
	}
}

type urlOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

type subscriptionInput struct {
	Body struct {
		OrgID      uint64 `json:"org_id" required:"true" minimum:"1"`
		PlanID     string `json:"plan_id" required:"true" maxLength:"255"`
		Cadence    string `json:"cadence,omitempty" enum:"monthly,annual"`
		SuccessURL string `json:"success_url" required:"true" maxLength:"2048"`
		CancelURL  string `json:"cancel_url" required:"true" maxLength:"2048"`
	}
}

type depositCreditsOutput struct {
	Body struct {
		SubscriptionsProcessed int      `json:"subscriptions_processed"`
		CreditsDeposited       int      `json:"credits_deposited"`
		CreditsSkipped         int      `json:"credits_skipped"`
		CreditsFailed          int      `json:"credits_failed"`
		Errors                 []string `json:"errors,omitempty"`
	}
}

type expireCreditsOutput struct {
	Body struct {
		GrantsChecked int      `json:"grants_checked"`
		GrantsExpired int      `json:"grants_expired"`
		GrantsFailed  int      `json:"grants_failed"`
		UnitsExpired  uint64   `json:"units_expired"`
		Errors        []string `json:"errors,omitempty"`
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
		OrgPromoted int      `json:"org_promoted"`
		OrgDemoted  int      `json:"org_demoted"`
		Errors      []string `json:"errors,omitempty"`
	}
}

func healthz() func(context.Context, *struct{}) (*healthOutput, error) {
	return func(_ context.Context, _ *struct{}) (*healthOutput, error) {
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	}
}

func readyz(app *billingruntime.App) func(context.Context, *struct{}) (*healthOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		if app == nil {
			return nil, huma.Error500InternalServerError("billing runtime unavailable")
		}
		if err := app.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("billing runtime not ready: " + err.Error())
		}
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	}
}

func getOrgBalance(app *billingruntime.App) func(context.Context, *orgIDPath) (*balanceOutput, error) {
	return func(ctx context.Context, input *orgIDPath) (*balanceOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		bal, callErr := client.GetOrgBalance(ctx, billing.OrgID(input.OrgID))
		if callErr != nil {
			return nil, huma.Error500InternalServerError("get balance", callErr)
		}
		out := &balanceOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.FreeTierAvailable = bal.FreeTierAvailable
		out.Body.FreeTierPending = bal.FreeTierPending
		out.Body.CreditAvailable = bal.CreditAvailable
		out.Body.CreditPending = bal.CreditPending
		out.Body.TotalAvailable = bal.TotalAvailable
		return out, nil
	}
}

func getProductBalance(app *billingruntime.App) func(context.Context, *orgProductPath) (*productBalanceOutput, error) {
	return func(ctx context.Context, input *orgProductPath) (*productBalanceOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		bal, callErr := client.GetProductBalance(ctx, billing.OrgID(input.OrgID), input.ProductID)
		if callErr != nil {
			return nil, huma.Error500InternalServerError("get product balance", callErr)
		}
		out := &productBalanceOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.ProductID = input.ProductID
		out.Body.FreeTierRemaining = bal.FreeTierRemaining
		out.Body.IncludedRemaining = bal.IncludedRemaining
		out.Body.PrepaidRemaining = bal.PrepaidRemaining
		return out, nil
	}
}

func listSubscriptions(app *billingruntime.App) func(context.Context, *orgIDPath) (*subscriptionsOutput, error) {
	return func(ctx context.Context, input *orgIDPath) (*subscriptionsOutput, error) {
		pg, err := requirePG(app)
		if err != nil {
			return nil, err
		}
		rows, queryErr := listSubscriptionsQuery(ctx, pg, billing.OrgID(input.OrgID))
		if queryErr != nil {
			return nil, huma.Error500InternalServerError("list subscriptions", queryErr)
		}
		out := &subscriptionsOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.Subscriptions = rows
		return out, nil
	}
}

func listGrants(app *billingruntime.App) func(context.Context, *grantsInput) (*grantsOutput, error) {
	return func(ctx context.Context, input *grantsInput) (*grantsOutput, error) {
		pg, err := requirePG(app)
		if err != nil {
			return nil, err
		}
		rows, queryErr := listGrantsQuery(ctx, pg, billing.OrgID(input.OrgID), input.ProductID, input.Active)
		if queryErr != nil {
			return nil, huma.Error500InternalServerError("list grants", queryErr)
		}
		out := &grantsOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.Grants = rows
		return out, nil
	}
}

func listUsage(app *billingruntime.App) func(context.Context, *usageInput) (*usageOutput, error) {
	return func(ctx context.Context, input *usageInput) (*usageOutput, error) {
		pg, err := requirePG(app)
		if err != nil {
			return nil, err
		}
		var since *time.Time
		if input.Since != "" {
			t, parseErr := time.Parse(time.RFC3339, input.Since)
			if parseErr != nil {
				return nil, huma.Error400BadRequest("invalid since: " + parseErr.Error())
			}
			since = &t
		}
		rows, queryErr := listUsageQuery(ctx, pg, billing.OrgID(input.OrgID), input.ProductID, since, input.Limit)
		if queryErr != nil {
			return nil, huma.Error500InternalServerError("list usage", queryErr)
		}
		out := &usageOutput{}
		out.Body.OrgID = strconv.FormatUint(input.OrgID, 10)
		out.Body.Events = rows
		return out, nil
	}
}

func checkQuotas(app *billingruntime.App) func(context.Context, *quotaCheckInput) (*quotaCheckOutput, error) {
	return func(ctx context.Context, input *quotaCheckInput) (*quotaCheckOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		ctx, span := tracer.Start(ctx, "billing.CheckQuotas",
			trace.WithAttributes(
				attribute.Int64("billing.org_id", int64(input.Body.OrgID)),
				attribute.String("billing.product_id", input.Body.ProductID),
			))
		defer span.End()
		result, callErr := client.CheckQuotas(ctx, billing.OrgID(input.Body.OrgID), input.Body.ProductID, input.Body.ConcurrentCount)
		if callErr != nil {
			span.RecordError(callErr)
			span.SetStatus(codes.Error, callErr.Error())
			return nil, huma.Error500InternalServerError("check quotas", callErr)
		}
		span.SetAttributes(attribute.Bool("billing.allowed", result.Allowed))
		out := &quotaCheckOutput{}
		out.Body.Allowed = result.Allowed
		for _, violation := range result.Violations {
			out.Body.Violations = append(out.Body.Violations, quotaViolationJSON{
				Dimension: violation.Dimension,
				Window:    violation.Window,
				Limit:     violation.Limit,
				Current:   violation.Current,
			})
		}
		slog.InfoContext(ctx, "billing: quota check", "org_id", input.Body.OrgID, "product_id", input.Body.ProductID, "allowed", result.Allowed, "violations", len(result.Violations))
		return out, nil
	}
}

func reserve(app *billingruntime.App) func(context.Context, *reserveInput) (*reserveOutput, error) {
	return func(ctx context.Context, input *reserveInput) (*reserveOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		ctx, span := tracer.Start(ctx, "billing.Reserve",
			trace.WithAttributes(
				attribute.Int64("billing.job_id", input.Body.JobID),
				attribute.Int64("billing.org_id", int64(input.Body.OrgID)),
				attribute.String("billing.product_id", input.Body.ProductID),
				attribute.String("billing.source_ref", input.Body.SourceRef),
			))
		defer span.End()
		reservation, callErr := client.Reserve(ctx, billing.ReserveRequest{
			JobID:           billing.JobID(input.Body.JobID),
			OrgID:           billing.OrgID(input.Body.OrgID),
			ProductID:       input.Body.ProductID,
			ActorID:         input.Body.ActorID,
			ConcurrentCount: input.Body.ConcurrentCount,
			SourceType:      input.Body.SourceType,
			SourceRef:       input.Body.SourceRef,
			Allocation:      input.Body.Allocation,
		})
		if callErr != nil {
			span.RecordError(callErr)
			span.SetStatus(codes.Error, callErr.Error())
			return nil, mapReserveError(callErr)
		}
		span.SetAttributes(attribute.Int("billing.window_seq", int(reservation.WindowSeq)))
		out := &reserveOutput{}
		out.Body.Reservation = reservationFromDomain(reservation)
		slog.InfoContext(ctx, "billing: reserved",
			"org_id", input.Body.OrgID,
			"product_id", input.Body.ProductID,
			"job_id", input.Body.JobID,
			"source_type", input.Body.SourceType,
			"source_ref", input.Body.SourceRef,
			"window_seq", reservation.WindowSeq,
		)
		return out, nil
	}
}

func settle(app *billingruntime.App) func(context.Context, *settleInput) (*statusOutput, error) {
	return func(ctx context.Context, input *settleInput) (*statusOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		reservation, convErr := input.Body.Reservation.toDomain()
		if convErr != nil {
			return nil, huma.Error400BadRequest("invalid reservation: " + convErr.Error())
		}
		ctx, span := tracer.Start(ctx, "billing.Settle",
			trace.WithAttributes(
				attribute.Int64("billing.job_id", int64(reservation.JobID)),
				attribute.Int64("billing.org_id", int64(reservation.OrgID)),
				attribute.String("billing.product_id", reservation.ProductID),
				attribute.Int("billing.actual_seconds", int(input.Body.ActualSeconds)),
			))
		defer span.End()
		if callErr := client.Settle(ctx, reservation, input.Body.ActualSeconds); callErr != nil {
			span.RecordError(callErr)
			span.SetStatus(codes.Error, callErr.Error())
			return nil, huma.Error500InternalServerError("settle reservation", callErr)
		}
		out := &statusOutput{}
		out.Body.Status = "settled"
		slog.InfoContext(ctx, "billing: settled",
			"org_id", reservation.OrgID,
			"product_id", reservation.ProductID,
			"job_id", reservation.JobID,
			"source_type", reservation.SourceType,
			"source_ref", reservation.SourceRef,
			"window_seq", reservation.WindowSeq,
			"actual_seconds", input.Body.ActualSeconds,
		)
		return out, nil
	}
}

func renew(app *billingruntime.App) func(context.Context, *renewInput) (*reserveOutput, error) {
	return func(ctx context.Context, input *renewInput) (*reserveOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		reservation, convErr := input.Body.Reservation.toDomain()
		if convErr != nil {
			return nil, huma.Error400BadRequest("invalid reservation: " + convErr.Error())
		}
		ctx, span := tracer.Start(ctx, "billing.Renew",
			trace.WithAttributes(
				attribute.Int64("billing.job_id", int64(reservation.JobID)),
				attribute.Int64("billing.org_id", int64(reservation.OrgID)),
				attribute.String("billing.product_id", reservation.ProductID),
				attribute.Int("billing.actual_seconds", int(input.Body.ActualSeconds)),
				attribute.Int("billing.window_seq", int(reservation.WindowSeq)),
			))
		defer span.End()
		next, callErr := client.Renew(ctx, reservation, input.Body.ActualSeconds)
		if callErr != nil {
			span.RecordError(callErr)
			span.SetStatus(codes.Error, callErr.Error())
			return nil, mapRenewError(callErr)
		}
		span.SetAttributes(attribute.Int("billing.next_window_seq", int(next.WindowSeq)))
		out := &reserveOutput{}
		out.Body.Reservation = reservationFromDomain(next)
		slog.InfoContext(ctx, "billing: renewed",
			"org_id", reservation.OrgID,
			"product_id", reservation.ProductID,
			"job_id", reservation.JobID,
			"source_type", reservation.SourceType,
			"source_ref", reservation.SourceRef,
			"window_seq", reservation.WindowSeq,
			"next_window_seq", next.WindowSeq,
			"actual_seconds", input.Body.ActualSeconds,
		)
		return out, nil
	}
}

func voidReservation(app *billingruntime.App) func(context.Context, *voidInput) (*statusOutput, error) {
	return func(ctx context.Context, input *voidInput) (*statusOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		reservation, convErr := input.Body.Reservation.toDomain()
		if convErr != nil {
			return nil, huma.Error400BadRequest("invalid reservation: " + convErr.Error())
		}
		ctx, span := tracer.Start(ctx, "billing.Void",
			trace.WithAttributes(
				attribute.Int64("billing.job_id", int64(reservation.JobID)),
				attribute.Int64("billing.org_id", int64(reservation.OrgID)),
				attribute.String("billing.product_id", reservation.ProductID),
			))
		defer span.End()
		if callErr := client.Void(ctx, reservation); callErr != nil {
			span.RecordError(callErr)
			span.SetStatus(codes.Error, callErr.Error())
			return nil, huma.Error500InternalServerError("void reservation", callErr)
		}
		out := &statusOutput{}
		out.Body.Status = "voided"
		slog.InfoContext(ctx, "billing: voided",
			"org_id", reservation.OrgID,
			"product_id", reservation.ProductID,
			"job_id", reservation.JobID,
			"source_type", reservation.SourceType,
			"source_ref", reservation.SourceRef,
			"window_seq", reservation.WindowSeq,
		)
		return out, nil
	}
}

func createCheckout(app *billingruntime.App) func(context.Context, *checkoutInput) (*urlOutput, error) {
	return func(ctx context.Context, input *checkoutInput) (*urlOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		url, callErr := client.CreateCheckoutSession(ctx, billing.OrgID(input.Body.OrgID), input.Body.ProductID, billing.CheckoutParams{
			AmountCents: input.Body.AmountCents,
			SuccessURL:  input.Body.SuccessURL,
			CancelURL:   input.Body.CancelURL,
		})
		if callErr != nil {
			return nil, huma.Error500InternalServerError("create checkout", callErr)
		}
		out := &urlOutput{}
		out.Body.URL = url
		return out, nil
	}
}

func createSubscription(app *billingruntime.App) func(context.Context, *subscriptionInput) (*urlOutput, error) {
	return func(ctx context.Context, input *subscriptionInput) (*urlOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		cadence := billing.BillingCadence(input.Body.Cadence)
		if cadence == "" {
			cadence = billing.CadenceMonthly
		}
		url, callErr := client.CreateSubscription(ctx, billing.OrgID(input.Body.OrgID), input.Body.PlanID, cadence, input.Body.SuccessURL, input.Body.CancelURL)
		if callErr != nil {
			if errors.Is(callErr, billing.ErrNoPriceConfigured) {
				return nil, huma.Error400BadRequest(callErr.Error())
			}
			return nil, huma.Error500InternalServerError("create subscription", callErr)
		}
		out := &urlOutput{}
		out.Body.URL = url
		return out, nil
	}
}

func opsDepositCredits(app *billingruntime.App) func(context.Context, *struct{}) (*depositCreditsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*depositCreditsOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		result, callErr := client.DepositSubscriptionCredits(ctx)
		if callErr != nil {
			return nil, huma.Error500InternalServerError("deposit credits", callErr)
		}
		out := &depositCreditsOutput{}
		out.Body.SubscriptionsProcessed = result.SubscriptionsProcessed
		out.Body.CreditsDeposited = result.CreditsDeposited
		out.Body.CreditsSkipped = result.CreditsSkipped
		out.Body.CreditsFailed = result.CreditsFailed
		for _, problem := range result.Errors {
			out.Body.Errors = append(out.Body.Errors, problem.Error())
		}
		return out, nil
	}
}

func opsExpireCredits(app *billingruntime.App) func(context.Context, *struct{}) (*expireCreditsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*expireCreditsOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		result, callErr := client.ExpireCredits(ctx)
		if callErr != nil {
			return nil, huma.Error500InternalServerError("expire credits", callErr)
		}
		out := &expireCreditsOutput{}
		out.Body.GrantsChecked = result.GrantsChecked
		out.Body.GrantsExpired = result.GrantsExpired
		out.Body.GrantsFailed = result.GrantsFailed
		out.Body.UnitsExpired = result.UnitsExpired
		for _, problem := range result.Errors {
			out.Body.Errors = append(out.Body.Errors, problem.Error())
		}
		return out, nil
	}
}

func opsReconcile(app *billingruntime.App) func(context.Context, *struct{}) (*reconcileOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*reconcileOutput, error) {
		if app == nil || app.Billing == nil || app.ReconcileQuerier == nil {
			return nil, huma.Error500InternalServerError("billing runtime unavailable")
		}
		result, callErr := app.Billing.Reconcile(ctx, app.ReconcileQuerier)
		if callErr != nil {
			return nil, huma.Error500InternalServerError("reconcile", callErr)
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

func opsTrustTierEvaluate(app *billingruntime.App) func(context.Context, *struct{}) (*trustTierOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*trustTierOutput, error) {
		client, err := requireBilling(app)
		if err != nil {
			return nil, err
		}
		result, callErr := client.EvaluateTrustTiers(ctx)
		if callErr != nil {
			return nil, huma.Error500InternalServerError("evaluate trust tiers", callErr)
		}
		out := &trustTierOutput{}
		out.Body.OrgPromoted = result.OrgPromoted
		out.Body.OrgDemoted = result.OrgDemoted
		for _, problem := range result.Errors {
			out.Body.Errors = append(out.Body.Errors, problem.Error())
		}
		return out, nil
	}
}

func requireBilling(app *billingruntime.App) (*billing.Client, error) {
	if app == nil || app.Billing == nil {
		return nil, huma.Error500InternalServerError("billing runtime unavailable")
	}
	return app.Billing, nil
}

func requirePG(app *billingruntime.App) (*sql.DB, error) {
	if app == nil || app.PG == nil {
		return nil, huma.Error500InternalServerError("billing runtime unavailable")
	}
	return app.PG, nil
}

func mapReserveError(err error) error {
	switch {
	case errors.Is(err, billing.ErrInsufficientBalance), errors.Is(err, billing.ErrNoActiveSubscription), errors.Is(err, billing.ErrSpendCapExceeded):
		return huma.Error402PaymentRequired(err.Error())
	case errors.Is(err, billing.ErrOrgSuspended), errors.Is(err, billing.ErrConcurrentLimitExceeded):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, billing.ErrDimensionMismatch):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError("reserve", err)
	}
}

func mapRenewError(err error) error {
	switch {
	case errors.Is(err, billing.ErrInsufficientBalance), errors.Is(err, billing.ErrNoActiveSubscription), errors.Is(err, billing.ErrSpendCapExceeded):
		return huma.Error402PaymentRequired(err.Error())
	case errors.Is(err, billing.ErrOrgSuspended), errors.Is(err, billing.ErrConcurrentLimitExceeded):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, billing.ErrDimensionMismatch), errors.Is(err, billing.ErrPendingTransferExpired):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError("renew", err)
	}
}

func reservationFromDomain(reservation billing.Reservation) reservationJSON {
	out := reservationJSON{
		JobID:               int64(reservation.JobID),
		OrgID:               uint64(reservation.OrgID),
		ProductID:           reservation.ProductID,
		PlanID:              reservation.PlanID,
		ActorID:             reservation.ActorID,
		SourceType:          reservation.SourceType,
		SourceRef:           reservation.SourceRef,
		WindowSeq:           reservation.WindowSeq,
		WindowSecs:          reservation.WindowSecs,
		WindowStart:         reservation.WindowStart,
		ExpiresAt:           reservation.ExpiresAt,
		RenewBy:             reservation.RenewBy,
		PricingPhase:        string(reservation.PricingPhase),
		Allocation:          reservation.Allocation,
		UnitRates:           reservation.UnitRates,
		CostPerSec:          reservation.CostPerSec,
		SpendCapPeriodStart: reservation.SpendCapPeriodStart,
	}
	for _, leg := range reservation.GrantLegs {
		out.GrantLegs = append(out.GrantLegs, grantLegJSON{
			GrantID:    leg.GrantID.String(),
			TransferID: leg.TransferID.String(),
			Amount:     leg.Amount,
			Source:     leg.Source.String(),
		})
	}
	return out
}

func (reservation reservationJSON) toDomain() (billing.Reservation, error) {
	out := billing.Reservation{
		JobID:               billing.JobID(reservation.JobID),
		OrgID:               billing.OrgID(reservation.OrgID),
		ProductID:           reservation.ProductID,
		PlanID:              reservation.PlanID,
		ActorID:             reservation.ActorID,
		SourceType:          reservation.SourceType,
		SourceRef:           reservation.SourceRef,
		WindowSeq:           reservation.WindowSeq,
		WindowSecs:          reservation.WindowSecs,
		WindowStart:         reservation.WindowStart,
		ExpiresAt:           reservation.ExpiresAt,
		RenewBy:             reservation.RenewBy,
		PricingPhase:        billing.PricingPhase(reservation.PricingPhase),
		Allocation:          reservation.Allocation,
		UnitRates:           reservation.UnitRates,
		CostPerSec:          reservation.CostPerSec,
		SpendCapPeriodStart: reservation.SpendCapPeriodStart,
	}
	for _, leg := range reservation.GrantLegs {
		grantID, err := billing.ParseGrantID(leg.GrantID)
		if err != nil {
			return billing.Reservation{}, err
		}
		transferID, err := billing.ParseTransferID(leg.TransferID)
		if err != nil {
			return billing.Reservation{}, err
		}
		source, err := billing.ParseGrantSourceType(leg.Source)
		if err != nil {
			return billing.Reservation{}, err
		}
		out.GrantLegs = append(out.GrantLegs, billing.GrantLeg{
			GrantID:    grantID,
			TransferID: transferID,
			Amount:     leg.Amount,
			Source:     source,
		})
	}
	return out, nil
}

func listSubscriptionsQuery(ctx context.Context, pg *sql.DB, orgID billing.OrgID) ([]subscriptionJSON, error) {
	rows, err := pg.QueryContext(ctx, `
		SELECT subscription_id, plan_id, product_id, cadence, status,
		       stripe_subscription_id, current_period_start, current_period_end,
		       overage_cap_units, created_at
		FROM subscriptions
		WHERE org_id = $1
		ORDER BY created_at DESC
	`, strconv.FormatUint(uint64(orgID), 10))
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []subscriptionJSON
	for rows.Next() {
		var sub subscriptionJSON
		if err := rows.Scan(
			&sub.SubscriptionID, &sub.PlanID, &sub.ProductID, &sub.Cadence, &sub.Status,
			&sub.StripeSubscriptionID, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
			&sub.OverageCapUnits, &sub.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	if subs == nil {
		subs = []subscriptionJSON{}
	}
	return subs, nil
}

func listGrantsQuery(ctx context.Context, pg *sql.DB, orgID billing.OrgID, productID string, activeOnly bool) ([]grantJSON, error) {
	query := `
		SELECT grant_id, product_id, amount, source, expires_at, closed_at, created_at
		FROM credit_grants
		WHERE org_id = $1
	`
	args := []any{strconv.FormatUint(uint64(orgID), 10)}
	argIdx := 2

	if productID != "" {
		query += fmt.Sprintf(" AND product_id = $%d", argIdx)
		args = append(args, productID)
		argIdx++
	}
	if activeOnly {
		query += " AND closed_at IS NULL"
	}
	query += " ORDER BY created_at DESC"

	rows, err := pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query grants: %w", err)
	}
	defer rows.Close()

	var grants []grantJSON
	for rows.Next() {
		var grant grantJSON
		if err := rows.Scan(&grant.GrantID, &grant.ProductID, &grant.Amount, &grant.Source, &grant.ExpiresAt, &grant.ClosedAt, &grant.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grants: %w", err)
	}
	if grants == nil {
		grants = []grantJSON{}
	}
	return grants, nil
}

func listUsageQuery(ctx context.Context, pg *sql.DB, orgID billing.OrgID, productID string, since *time.Time, limit int) ([]usageEventJSON, error) {
	query := `
		SELECT event_id, event_type, payload, created_at
		FROM billing_events
		WHERE org_id = $1
	`
	args := []any{strconv.FormatUint(uint64(orgID), 10)}
	argIdx := 2

	if productID != "" {
		query += fmt.Sprintf(" AND payload->>'product_id' = $%d", argIdx)
		args = append(args, productID)
		argIdx++
	}
	if since != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *since)
		argIdx++
	}
	query += " ORDER BY created_at DESC"

	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage events: %w", err)
	}
	defer rows.Close()

	var events []usageEventJSON
	for rows.Next() {
		var event usageEventJSON
		if err := rows.Scan(&event.EventID, &event.EventType, &event.Payload, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	if events == nil {
		events = []usageEventJSON{}
	}
	return events, nil
}

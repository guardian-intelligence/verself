package billingapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/billing-service/internal/billing"
)

type Config struct {
	Version      string
	ListenAddr   string
	Client       *billing.Client
	Logger       *slog.Logger
	InternalRole string
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "2.0.0"
	}
	config := huma.DefaultConfig("Billing Service", version)
	if cfg.ListenAddr != "" {
		config.OpenAPI.Servers = []*huma.Server{{URL: "http://" + cfg.ListenAddr}}
	}
	api := humago.New(mux, config)
	registerRoutes(api, cfg)
	return api
}

func OpenAPIDowngradeYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0", ListenAddr: "127.0.0.1:4242", InternalRole: "billing_internal"})
	return api.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0", ListenAddr: "127.0.0.1:4242", InternalRole: "billing_internal"})
	return api.OpenAPI().YAML()
}

type ErrorModel struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type EmptyInput struct{}

type BalanceOutput struct {
	Body BalanceOutputBody
}

type BalanceOutputBody struct {
	OrgId             int64 `json:"org_id"`
	FreeTierAvailable int64 `json:"free_tier_available"`
	FreeTierPending   int64 `json:"free_tier_pending"`
	CreditAvailable   int64 `json:"credit_available"`
	CreditPending     int64 `json:"credit_pending"`
	TotalAvailable    int64 `json:"total_available"`
}

type OrgPath struct {
	OrgId int64 `path:"org_id"`
}

type GrantsInput struct {
	OrgPath
	ProductId string `query:"product_id,omitempty"`
	Active    bool   `query:"active,omitempty"`
}

type GrantJSON struct {
	GrantId   string  `json:"grant_id"`
	Source    string  `json:"source"`
	Available int64   `json:"available"`
	Pending   int64   `json:"pending"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

type GrantsOutput struct {
	Body GrantsOutputBody
}

type GrantsOutputBody struct {
	Grants []GrantJSON `json:"grants"`
}

type SubscriptionsOutput struct {
	Body SubscriptionsOutputBody
}

type SubscriptionsOutputBody struct {
	Subscriptions []SubscriptionJSON `json:"subscriptions"`
}

type SubscriptionJSON struct {
	SubscriptionId     int64   `json:"subscription_id"`
	ProductId          string  `json:"product_id"`
	PlanId             string  `json:"plan_id"`
	Cadence            string  `json:"cadence"`
	Status             string  `json:"status"`
	CurrentPeriodStart *string `json:"current_period_start,omitempty"`
	CurrentPeriodEnd   *string `json:"current_period_end,omitempty"`
}

type CheckoutInput struct {
	Body CreateCheckoutBody
}

type CreateCheckoutBody struct {
	OrgId       int64  `json:"org_id" required:"true"`
	ProductId   string `json:"product_id" required:"true" maxLength:"255"`
	AmountCents int64  `json:"amount_cents" required:"true" minimum:"1"`
	SuccessUrl  string `json:"success_url" required:"true" maxLength:"2048"`
	CancelUrl   string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type CreateSubscriptionInput struct {
	Body CreateSubscriptionBody
}

type CreateSubscriptionBody struct {
	OrgId      int64  `json:"org_id" required:"true"`
	PlanId     string `json:"plan_id" required:"true" maxLength:"255"`
	Cadence    string `json:"cadence,omitempty" enum:"monthly,annual"`
	SuccessUrl string `json:"success_url" required:"true" maxLength:"2048"`
	CancelUrl  string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type URLOutput struct {
	Body struct {
		Url string `json:"url"`
	}
}

type ReserveInput struct {
	Body ReserveInputBody
}

type ReserveInputBody struct {
	OrgId           int64              `json:"org_id" required:"true"`
	ProductId       string             `json:"product_id" required:"true" maxLength:"255"`
	ActorId         string             `json:"actor_id" required:"true" maxLength:"255"`
	ConcurrentCount int64              `json:"concurrent_count"`
	SourceType      string             `json:"source_type" required:"true" maxLength:"255"`
	SourceRef       string             `json:"source_ref" required:"true" maxLength:"255"`
	Allocation      map[string]float64 `json:"allocation" required:"true"`
}

type WindowReservationJSON struct {
	WindowId            string             `json:"window_id"`
	OrgId               int64              `json:"org_id"`
	ProductId           string             `json:"product_id"`
	PlanId              string             `json:"plan_id"`
	ActorId             string             `json:"actor_id"`
	SourceType          string             `json:"source_type"`
	SourceRef           string             `json:"source_ref"`
	WindowSeq           int32              `json:"window_seq"`
	ReservationShape    string             `json:"reservation_shape"`
	ReservedQuantity    int32              `json:"reserved_quantity"`
	ReservedChargeUnits int64              `json:"reserved_charge_units"`
	PricingPhase        string             `json:"pricing_phase"`
	Allocation          map[string]float64 `json:"allocation"`
	UnitRates           map[string]int64   `json:"unit_rates"`
	CostPerUnit         int64              `json:"cost_per_unit"`
	WindowStart         string             `json:"window_start"`
	ExpiresAt           string             `json:"expires_at"`
	RenewBy             *string            `json:"renew_by,omitempty"`
}

type ReserveOutput struct {
	Body struct {
		Reservation WindowReservationJSON `json:"reservation"`
	}
}

type SettleInput struct {
	Body SettleInputBody
}

type SettleInputBody struct {
	WindowId       string         `json:"window_id" required:"true" maxLength:"255"`
	ActualQuantity int32          `json:"actual_quantity" required:"true" minimum:"0"`
	UsageSummary   map[string]any `json:"usage_summary,omitempty"`
}

type SettleOutput struct {
	Body struct {
		WindowId            string `json:"window_id"`
		ActualQuantity      int32  `json:"actual_quantity"`
		BillableQuantity    int32  `json:"billable_quantity"`
		WriteoffQuantity    int32  `json:"writeoff_quantity"`
		BilledChargeUnits   int64  `json:"billed_charge_units"`
		WriteoffChargeUnits int64  `json:"writeoff_charge_units"`
		SettledAt           string `json:"settled_at"`
	}
}

type VoidInput struct {
	Body struct {
		WindowId string `json:"window_id" required:"true" maxLength:"255"`
	}
}

type VoidOutput struct {
	Body struct {
		WindowId string `json:"window_id"`
	}
}

func registerRoutes(api huma.API, cfg Config) {
	huma.Register(api, huma.Operation{
		OperationID: "get-balance",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/balance",
		Summary:     "Get org grant balance",
	}, func(ctx context.Context, input *OrgPath) (*BalanceOutput, error) {
		if cfg.Client == nil {
			return nil, huma.Error500InternalServerError("billing client unavailable")
		}
		balance, err := cfg.Client.GetOrgBalance(ctx, billing.OrgID(input.OrgId))
		if err != nil {
			return nil, huma.Error500InternalServerError("get balance", err)
		}
		return &BalanceOutput{Body: BalanceOutputBody{
			OrgId:             input.OrgId,
			FreeTierAvailable: int64(balance.FreeTierAvailable),
			FreeTierPending:   int64(balance.FreeTierPending),
			CreditAvailable:   int64(balance.CreditAvailable),
			CreditPending:     int64(balance.CreditPending),
			TotalAvailable:    int64(balance.TotalAvailable),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-grants",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/grants",
		Summary:     "List credit grants for an org",
	}, func(ctx context.Context, input *GrantsInput) (*GrantsOutput, error) {
		if cfg.Client == nil {
			return nil, huma.Error500InternalServerError("billing client unavailable")
		}
		grants, err := cfg.Client.ListGrantBalances(ctx, billing.OrgID(input.OrgId), input.ProductId)
		if err != nil {
			return nil, huma.Error500InternalServerError("list grants", err)
		}
		out := make([]GrantJSON, 0, len(grants))
		for _, grant := range grants {
			entry := GrantJSON{
				GrantId:   grant.GrantID.String(),
				Source:    grant.Source.String(),
				Available: int64(grant.Available),
				Pending:   int64(grant.Pending),
			}
			if grant.ExpiresAt != nil {
				value := grant.ExpiresAt.UTC().Format(time.RFC3339Nano)
				entry.ExpiresAt = &value
			}
			out = append(out, entry)
		}
		return &GrantsOutput{Body: GrantsOutputBody{Grants: out}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-subscriptions",
		Method:      http.MethodGet,
		Path:        "/internal/billing/v1/orgs/{org_id}/subscriptions",
		Summary:     "List subscriptions for an org",
	}, func(ctx context.Context, input *OrgPath) (*SubscriptionsOutput, error) {
		if cfg.Client == nil {
			return nil, huma.Error500InternalServerError("billing client unavailable")
		}
		subscriptions, err := cfg.Client.ListSubscriptions(ctx, billing.OrgID(input.OrgId))
		if err != nil {
			return nil, huma.Error500InternalServerError("list subscriptions", err)
		}
		out := make([]SubscriptionJSON, 0, len(subscriptions))
		for _, subscription := range subscriptions {
			entry := SubscriptionJSON{
				SubscriptionId: subscription.SubscriptionID,
				ProductId:      subscription.ProductID,
				PlanId:         subscription.PlanID,
				Cadence:        subscription.Cadence,
				Status:         subscription.Status,
			}
			if subscription.CurrentPeriodStart != nil {
				value := subscription.CurrentPeriodStart.UTC().Format(time.RFC3339Nano)
				entry.CurrentPeriodStart = &value
			}
			if subscription.CurrentPeriodEnd != nil {
				value := subscription.CurrentPeriodEnd.UTC().Format(time.RFC3339Nano)
				entry.CurrentPeriodEnd = &value
			}
			out = append(out, entry)
		}
		return &SubscriptionsOutput{Body: SubscriptionsOutputBody{Subscriptions: out}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-checkout",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/checkout",
		Summary:       "Create a Stripe checkout session",
		DefaultStatus: 200,
	}, func(ctx context.Context, input *CheckoutInput) (*URLOutput, error) {
		if cfg.Client == nil {
			return nil, huma.Error500InternalServerError("billing client unavailable")
		}
		url, err := cfg.Client.CreateCheckoutSession(ctx, billing.OrgID(input.Body.OrgId), input.Body.ProductId, billing.CheckoutParams{
			AmountCents: input.Body.AmountCents,
			SuccessURL:  input.Body.SuccessUrl,
			CancelURL:   input.Body.CancelUrl,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("create checkout", err)
		}
		output := &URLOutput{}
		output.Body.Url = url
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-subscription",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/subscribe",
		Summary:       "Create a Stripe subscription checkout",
		DefaultStatus: 200,
	}, func(ctx context.Context, input *CreateSubscriptionInput) (*URLOutput, error) {
		if cfg.Client == nil {
			return nil, huma.Error500InternalServerError("billing client unavailable")
		}
		url, err := cfg.Client.CreateSubscription(ctx, billing.OrgID(input.Body.OrgId), input.Body.PlanId, billing.BillingCadence(input.Body.Cadence), input.Body.SuccessUrl, input.Body.CancelUrl)
		if err != nil {
			return nil, huma.Error501NotImplemented("subscription checkout", err)
		}
		output := &URLOutput{}
		output.Body.Url = url
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "reserve-window",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/reserve",
		Summary:       "Reserve a billing window",
		DefaultStatus: 200,
	}, func(ctx context.Context, input *ReserveInput) (*ReserveOutput, error) {
		if err := requireInternalRole(ctx, cfg.InternalRole); err != nil {
			return nil, err
		}
		reservation, err := cfg.Client.ReserveWindow(ctx, billing.ReserveRequest{
			OrgID:           billing.OrgID(input.Body.OrgId),
			ProductID:       input.Body.ProductId,
			ActorID:         input.Body.ActorId,
			Allocation:      input.Body.Allocation,
			ConcurrentCount: uint64(input.Body.ConcurrentCount),
			SourceType:      input.Body.SourceType,
			SourceRef:       input.Body.SourceRef,
		})
		if err != nil {
			switch {
			case errors.Is(err, billing.ErrInsufficientBalance):
				return nil, huma.Error402PaymentRequired("reserve", err)
			case errors.Is(err, billing.ErrOrgSuspended):
				return nil, huma.Error403Forbidden("reserve", err)
			default:
				return nil, huma.Error500InternalServerError("reserve", err)
			}
		}
		output := &ReserveOutput{}
		output.Body.Reservation = toWindowReservationJSON(reservation)
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "settle-window",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/settle",
		Summary:       "Settle a reserved billing window",
		DefaultStatus: 200,
	}, func(ctx context.Context, input *SettleInput) (*SettleOutput, error) {
		if err := requireInternalRole(ctx, cfg.InternalRole); err != nil {
			return nil, err
		}
		result, err := cfg.Client.SettleWindow(ctx, input.Body.WindowId, uint32(input.Body.ActualQuantity), input.Body.UsageSummary)
		if err != nil {
			switch {
			case errors.Is(err, billing.ErrWindowNotFound):
				return nil, huma.Error404NotFound("window not found")
			case errors.Is(err, billing.ErrWindowAlreadyVoided):
				return nil, huma.Error400BadRequest("window already voided")
			default:
				if cfg.Logger != nil {
					cfg.Logger.ErrorContext(ctx, "settle billing window", "window_id", input.Body.WindowId, "actual_quantity", input.Body.ActualQuantity, "error", err)
				}
				return nil, huma.Error500InternalServerError("settle", err)
			}
		}
		output := &SettleOutput{}
		output.Body.WindowId = result.WindowID
		output.Body.ActualQuantity = int32(result.ActualQuantity)
		output.Body.BillableQuantity = int32(result.BillableQuantity)
		output.Body.WriteoffQuantity = int32(result.WriteoffQuantity)
		output.Body.BilledChargeUnits = int64(result.BilledChargeUnits)
		output.Body.WriteoffChargeUnits = int64(result.WriteoffChargeUnits)
		output.Body.SettledAt = result.SettledAt.UTC().Format(time.RFC3339Nano)
		return output, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "void-window",
		Method:        http.MethodPost,
		Path:          "/internal/billing/v1/void",
		Summary:       "Void a reserved billing window",
		DefaultStatus: 200,
	}, func(ctx context.Context, input *VoidInput) (*VoidOutput, error) {
		if err := requireInternalRole(ctx, cfg.InternalRole); err != nil {
			return nil, err
		}
		if err := cfg.Client.VoidWindow(ctx, input.Body.WindowId); err != nil {
			switch {
			case errors.Is(err, billing.ErrWindowNotFound):
				return nil, huma.Error404NotFound("window not found")
			case errors.Is(err, billing.ErrWindowAlreadySettled):
				return nil, huma.Error400BadRequest("window already settled")
			default:
				if cfg.Logger != nil {
					cfg.Logger.ErrorContext(ctx, "void billing window", "window_id", input.Body.WindowId, "error", err)
				}
				return nil, huma.Error500InternalServerError("void", err)
			}
		}
		output := &VoidOutput{}
		output.Body.WindowId = input.Body.WindowId
		return output, nil
	})
}

func requireInternalRole(ctx context.Context, role string) error {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return huma.Error401Unauthorized("missing identity")
	}
	if role == "" {
		role = "billing_internal"
	}
	for _, candidate := range identity.Roles {
		if candidate == role {
			return nil
		}
	}
	return huma.Error403Forbidden("missing internal billing role")
}

func toWindowReservationJSON(in billing.WindowReservation) WindowReservationJSON {
	var renewBy *string
	if in.RenewBy != nil {
		value := in.RenewBy.UTC().Format(time.RFC3339Nano)
		renewBy = &value
	}
	unitRates := make(map[string]int64, len(in.UnitRates))
	for key, value := range in.UnitRates {
		unitRates[key] = int64(value)
	}
	return WindowReservationJSON{
		WindowId:            in.WindowID,
		OrgId:               int64(in.OrgID),
		ProductId:           in.ProductID,
		PlanId:              in.PlanID,
		ActorId:             in.ActorID,
		SourceType:          in.SourceType,
		SourceRef:           in.SourceRef,
		WindowSeq:           int32(in.WindowSeq),
		ReservationShape:    string(in.ReservationShape),
		ReservedQuantity:    int32(in.ReservedQuantity),
		ReservedChargeUnits: int64(in.ReservedChargeUnits),
		PricingPhase:        string(in.PricingPhase),
		Allocation:          in.Allocation,
		UnitRates:           unitRates,
		CostPerUnit:         int64(in.CostPerUnit),
		WindowStart:         in.WindowStart.UTC().Format(time.RFC3339Nano),
		ExpiresAt:           in.ExpiresAt.UTC().Format(time.RFC3339Nano),
		RenewBy:             renewBy,
	}
}

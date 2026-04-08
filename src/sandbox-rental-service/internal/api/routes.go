// Package api registers sandbox-rental-service HTTP routes on a Huma API.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

// RegisterRoutes wires all sandbox-rental-service endpoints onto the Huma API.
func RegisterRoutes(api huma.API, svc *jobs.Service, billing *billingclient.ServiceClient) {
	huma.Register(api, huma.Operation{
		OperationID:   "submit-execution",
		Method:        http.MethodPost,
		Path:          "/api/v1/executions",
		Summary:       "Submit a new execution",
		DefaultStatus: 201,
	}, submitExecution(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-execution",
		Method:      http.MethodGet,
		Path:        "/api/v1/executions/{execution_id}",
		Summary:     "Get execution status and latest attempt",
	}, getExecution(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-execution-logs",
		Method:      http.MethodGet,
		Path:        "/api/v1/executions/{execution_id}/logs",
		Summary:     "Get latest execution attempt log output",
	}, getExecutionLogs(svc))

	// Billing proxy — frontend calls these; we enforce org_id from JWT
	// and forward to the billing-service on loopback.
	huma.Register(api, huma.Operation{
		OperationID: "get-billing-balance",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/balance",
		Summary:     "Get org credit balance",
	}, getBillingBalance(billing))

	huma.Register(api, huma.Operation{
		OperationID: "list-billing-subscriptions",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/subscriptions",
		Summary:     "List org subscriptions",
	}, listBillingSubscriptions(billing))

	huma.Register(api, huma.Operation{
		OperationID: "list-billing-grants",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/grants",
		Summary:     "List org credit grants",
	}, listBillingGrants(billing))

	huma.Register(api, huma.Operation{
		OperationID:   "create-billing-checkout",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/checkout",
		Summary:       "Create Stripe checkout session for credit purchase",
		DefaultStatus: 200,
	}, createBillingCheckout(billing))

	huma.Register(api, huma.Operation{
		OperationID:   "create-billing-subscription",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/subscribe",
		Summary:       "Create Stripe subscription checkout",
		DefaultStatus: 200,
	}, createBillingSubscription(billing))
}

type SubmitExecutionInput struct {
	Body jobs.SubmitRequest
}

type SubmitExecutionOutput struct {
	Body struct {
		ExecutionID string `json:"execution_id"`
		AttemptID   string `json:"attempt_id"`
		Status      string `json:"status"`
	}
}

type ExecutionIDPath struct {
	ExecutionID string `path:"execution_id" doc:"Execution UUID"`
}

type GetExecutionOutput struct {
	Body jobs.ExecutionRecord
}

type GetExecutionLogsOutput struct {
	Body struct {
		ExecutionID string `json:"execution_id"`
		AttemptID   string `json:"attempt_id"`
		Logs        string `json:"logs"`
	}
}

type EmptyInput struct{}

type BalanceOutput struct {
	Body billingclient.BalanceOutputBody
}

type SubscriptionsOutput struct {
	Body billingclient.SubscriptionsOutputBody
}

type GrantsInput struct {
	ProductID string `query:"product_id,omitempty" doc:"Filter by product"`
	Active    bool   `query:"active,omitempty" doc:"Only active grants"`
}

type GrantsOutput struct {
	Body billingclient.GrantsOutputBody
}

type CheckoutInput struct {
	Body struct {
		ProductID   string `json:"product_id" required:"true" maxLength:"255" doc:"Product to purchase credits for"`
		AmountCents int64  `json:"amount_cents" required:"true" minimum:"1" doc:"Amount in cents"`
		SuccessURL  string `json:"success_url" required:"true" maxLength:"2048"`
		CancelURL   string `json:"cancel_url" required:"true" maxLength:"2048"`
	}
}

type URLOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

type SubscribeInput struct {
	Body struct {
		PlanID     string `json:"plan_id" required:"true" maxLength:"255" doc:"Plan to subscribe to"`
		Cadence    string `json:"cadence,omitempty" enum:"monthly,annual" doc:"Billing cadence (default monthly)"`
		SuccessURL string `json:"success_url" required:"true" maxLength:"2048"`
		CancelURL  string `json:"cancel_url" required:"true" maxLength:"2048"`
	}
}

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, huma.Error401Unauthorized("missing identity")
	}
	return identity, nil
}

func requireOrgID(ctx context.Context) (uint64, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return 0, err
	}
	orgID, err := strconv.ParseUint(identity.OrgID, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid org_id in token: " + identity.OrgID)
	}
	return orgID, nil
}

func submitExecution(svc *jobs.Service) func(context.Context, *SubmitExecutionInput) (*SubmitExecutionOutput, error) {
	return func(ctx context.Context, input *SubmitExecutionInput) (*SubmitExecutionOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}

		orgID, err := strconv.ParseUint(identity.OrgID, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org_id in token: " + identity.OrgID)
		}

		executionID, attemptID, err := svc.Submit(ctx, orgID, identity.Subject, input.Body)
		if err != nil {
			switch {
			case errors.Is(err, jobs.ErrQuotaExceeded):
				return nil, huma.Error429TooManyRequests("quota exceeded")
			case errors.Is(err, billingclient.ErrPaymentRequired):
				return nil, huma.Error402PaymentRequired("insufficient balance")
			default:
				return nil, huma.Error500InternalServerError("submit execution", err)
			}
		}

		out := &SubmitExecutionOutput{}
		out.Body.ExecutionID = executionID.String()
		out.Body.AttemptID = attemptID.String()
		out.Body.Status = jobs.StateReserved
		return out, nil
	}
}

func getExecution(svc *jobs.Service) func(context.Context, *ExecutionIDPath) (*GetExecutionOutput, error) {
	return func(ctx context.Context, input *ExecutionIDPath) (*GetExecutionOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(input.ExecutionID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid execution_id: " + err.Error())
		}

		execution, err := svc.GetExecution(ctx, orgID, executionID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, huma.Error404NotFound("execution not found")
			}
			return nil, huma.Error500InternalServerError("get execution", err)
		}

		out := &GetExecutionOutput{}
		out.Body = *execution
		return out, nil
	}
}

func getExecutionLogs(svc *jobs.Service) func(context.Context, *ExecutionIDPath) (*GetExecutionLogsOutput, error) {
	return func(ctx context.Context, input *ExecutionIDPath) (*GetExecutionLogsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(input.ExecutionID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid execution_id: " + err.Error())
		}

		attemptID, logs, err := svc.GetExecutionLogs(ctx, orgID, executionID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, huma.Error404NotFound("execution not found")
			}
			return nil, huma.Error500InternalServerError("get execution logs", err)
		}

		out := &GetExecutionLogsOutput{}
		out.Body.ExecutionID = executionID.String()
		out.Body.AttemptID = attemptID.String()
		out.Body.Logs = logs
		return out, nil
	}
}

func getBillingBalance(billing *billingclient.ServiceClient) func(context.Context, *EmptyInput) (*BalanceOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*BalanceOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := billing.Generated().GetOrgBalanceWithResponse(ctx, int64(orgID))
		if err != nil {
			return nil, huma.Error502BadGateway("billing service unreachable")
		}
		if resp.JSON200 == nil {
			return nil, huma.Error502BadGateway("billing: " + resp.Status())
		}
		return &BalanceOutput{Body: *resp.JSON200}, nil
	}
}

func listBillingSubscriptions(billing *billingclient.ServiceClient) func(context.Context, *EmptyInput) (*SubscriptionsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*SubscriptionsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := billing.Generated().ListSubscriptionsWithResponse(ctx, int64(orgID))
		if err != nil {
			return nil, huma.Error502BadGateway("billing service unreachable")
		}
		if resp.JSON200 == nil {
			return nil, huma.Error502BadGateway("billing: " + resp.Status())
		}
		return &SubscriptionsOutput{Body: *resp.JSON200}, nil
	}
}

func listBillingGrants(billing *billingclient.ServiceClient) func(context.Context, *GrantsInput) (*GrantsOutput, error) {
	return func(ctx context.Context, input *GrantsInput) (*GrantsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		params := &billingclient.ListGrantsParams{}
		if input.ProductID != "" {
			params.ProductId = &input.ProductID
		}
		if input.Active {
			params.Active = &input.Active
		}
		resp, err := billing.Generated().ListGrantsWithResponse(ctx, int64(orgID), params)
		if err != nil {
			return nil, huma.Error502BadGateway("billing service unreachable")
		}
		if resp.JSON200 == nil {
			return nil, huma.Error502BadGateway("billing: " + resp.Status())
		}
		return &GrantsOutput{Body: *resp.JSON200}, nil
	}
}

func createBillingCheckout(billing *billingclient.ServiceClient) func(context.Context, *CheckoutInput) (*URLOutput, error) {
	return func(ctx context.Context, input *CheckoutInput) (*URLOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := billing.Generated().CreateCheckoutWithResponse(ctx, billingclient.CreateCheckoutJSONRequestBody{
			OrgId:       int64(orgID),
			ProductId:   input.Body.ProductID,
			AmountCents: input.Body.AmountCents,
			SuccessUrl:  input.Body.SuccessURL,
			CancelUrl:   input.Body.CancelURL,
		})
		if err != nil {
			return nil, huma.Error502BadGateway("billing service unreachable")
		}
		if resp.JSON200 == nil {
			return nil, huma.Error502BadGateway("billing: " + resp.Status())
		}
		out := &URLOutput{}
		out.Body.URL = resp.JSON200.Url
		return out, nil
	}
}

func createBillingSubscription(billing *billingclient.ServiceClient) func(context.Context, *SubscribeInput) (*URLOutput, error) {
	return func(ctx context.Context, input *SubscribeInput) (*URLOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		body := billingclient.CreateSubscriptionJSONRequestBody{
			OrgId:      int64(orgID),
			PlanId:     input.Body.PlanID,
			SuccessUrl: input.Body.SuccessURL,
			CancelUrl:  input.Body.CancelURL,
		}
		if input.Body.Cadence != "" {
			cadence := billingclient.SubscriptionInputBodyCadence(input.Body.Cadence)
			body.Cadence = &cadence
		}
		resp, err := billing.Generated().CreateSubscriptionWithResponse(ctx, body)
		if err != nil {
			return nil, huma.Error502BadGateway("billing service unreachable")
		}
		if resp.JSON200 == nil {
			return nil, huma.Error502BadGateway("billing: " + resp.Status())
		}
		out := &URLOutput{}
		out.Body.URL = resp.JSON200.Url
		return out, nil
	}
}

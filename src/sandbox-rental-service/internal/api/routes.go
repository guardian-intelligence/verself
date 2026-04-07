// Package api registers sandbox-rental-service HTTP routes on a Huma API.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

// RegisterRoutes wires all sandbox-rental-service endpoints onto the Huma API.
func RegisterRoutes(api huma.API, svc *jobs.Service, billing *billingclient.ServiceClient) {
	huma.Register(api, huma.Operation{
		OperationID:   "submit-job",
		Method:        http.MethodPost,
		Path:          "/api/v1/jobs",
		Summary:       "Submit a new sandbox job",
		DefaultStatus: 201,
	}, submitJob(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-job",
		Method:      http.MethodGet,
		Path:        "/api/v1/jobs/{job_id}",
		Summary:     "Get job status and result",
	}, getJob(svc))

	huma.Register(api, huma.Operation{
		OperationID: "get-job-logs",
		Method:      http.MethodGet,
		Path:        "/api/v1/jobs/{job_id}/logs",
		Summary:     "Get job log output",
	}, getJobLogs(svc))

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

// --- typed inputs/outputs ---

type SubmitJobInput struct {
	Body struct {
		RepoURL    string `json:"repo_url" required:"true" doc:"GitHub HTTPS URL of the repository"`
		RunCommand string `json:"run_command,omitempty" doc:"Command to execute (default: echo hello)"`
	}
}

type SubmitJobOutput struct {
	Body struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
}

type JobIDPath struct {
	JobID string `path:"job_id" doc:"Job UUID"`
}

type GetJobOutput struct {
	Body jobs.JobRecord
}

type GetJobLogsOutput struct {
	Body struct {
		JobID string `json:"job_id"`
		Logs  string `json:"logs"`
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

// --- handler factories ---

func requireOrgID(ctx context.Context) (int64, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return 0, huma.Error401Unauthorized("missing identity")
	}
	orgID, err := strconv.ParseInt(identity.OrgID, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid org_id in token: " + identity.OrgID)
	}
	return orgID, nil
}

func submitJob(svc *jobs.Service) func(context.Context, *SubmitJobInput) (*SubmitJobOutput, error) {
	return func(ctx context.Context, input *SubmitJobInput) (*SubmitJobOutput, error) {
		identity := auth.FromContext(ctx)
		if identity == nil {
			return nil, huma.Error401Unauthorized("missing identity")
		}

		repoURL := strings.TrimSpace(input.Body.RepoURL)
		if repoURL == "" {
			return nil, huma.Error400BadRequest("repo_url is required")
		}
		if !strings.HasPrefix(repoURL, "https://") {
			return nil, huma.Error400BadRequest("repo_url must be an HTTPS URL")
		}

		orgID, err := strconv.ParseUint(identity.OrgID, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid org_id in token: " + identity.OrgID)
		}

		jobID, err := svc.Submit(ctx, orgID, identity.Subject, repoURL, input.Body.RunCommand)
		if err != nil {
			if errors.Is(err, jobs.ErrQuotaExceeded) {
				return nil, huma.Error429TooManyRequests("quota exceeded")
			}
			if errors.Is(err, billingclient.ErrPaymentRequired) {
				return nil, huma.Error402PaymentRequired("insufficient balance")
			}
			return nil, huma.Error500InternalServerError("submit job", err)
		}

		out := &SubmitJobOutput{}
		out.Body.JobID = jobID.String()
		out.Body.Status = "running"
		return out, nil
	}
}

func getJob(svc *jobs.Service) func(context.Context, *JobIDPath) (*GetJobOutput, error) {
	return func(ctx context.Context, input *JobIDPath) (*GetJobOutput, error) {
		jobID, err := uuid.Parse(input.JobID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid job_id: " + err.Error())
		}

		job, err := svc.GetJob(ctx, jobID)
		if err != nil {
			return nil, huma.Error404NotFound("job not found")
		}

		out := &GetJobOutput{}
		out.Body = *job
		return out, nil
	}
}

func getJobLogs(svc *jobs.Service) func(context.Context, *JobIDPath) (*GetJobLogsOutput, error) {
	return func(ctx context.Context, input *JobIDPath) (*GetJobLogsOutput, error) {
		jobID, err := uuid.Parse(input.JobID)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid job_id: " + err.Error())
		}

		logs, err := svc.GetJobLogs(ctx, jobID)
		if err != nil {
			return nil, huma.Error500InternalServerError("get job logs", err)
		}

		out := &GetJobLogsOutput{}
		out.Body.JobID = jobID.String()
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
		resp, err := billing.Generated().GetOrgBalanceWithResponse(ctx, orgID)
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
		resp, err := billing.Generated().ListSubscriptionsWithResponse(ctx, orgID)
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
		resp, err := billing.Generated().ListGrantsWithResponse(ctx, orgID, params)
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
			OrgId:       orgID,
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
			OrgId:      orgID,
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

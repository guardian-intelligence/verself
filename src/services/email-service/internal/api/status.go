package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/email-service/internal/app"
	"github.com/verself/email-service/internal/contractapi"
	emailsync "github.com/verself/email-service/internal/sync"
)

type emailServiceHealthOutput struct {
	Body emailServiceHealth
}

type emailServiceHealth struct {
	Status string `json:"status"`
}

func registerPublicRoutes(api huma.API, svc provider) {
	huma.Register(api, huma.Operation{
		OperationID: "email-healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Email service liveness probe",
	}, healthz())

	huma.Register(api, huma.Operation{
		OperationID: "email-readyz",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Email service readiness probe",
	}, readyz(svc))
}

func healthz() func(context.Context, *emailServiceEmptyInput) (*emailServiceHealthOutput, error) {
	return func(context.Context, *emailServiceEmptyInput) (*emailServiceHealthOutput, error) {
		out := &emailServiceHealthOutput{}
		out.Body = emailServiceHealth{Status: "ok"}
		return out, nil
	}
}

func readyz(svc provider) func(context.Context, *emailServiceEmptyInput) (*emailServiceHealthOutput, error) {
	return func(ctx context.Context, _ *emailServiceEmptyInput) (*emailServiceHealthOutput, error) {
		if err := svc.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("email service not ready: " + err.Error())
		}
		out := &emailServiceHealthOutput{}
		out.Body = emailServiceHealth{Status: "ok"}
		return out, nil
	}
}

func serviceStatus(status app.ServiceStatus) contractapi.MailboxSyncStatusView {
	return contractapi.MailboxSyncStatusView{
		StartedAt:       formatContractTime(status.StartedAt),
		StalwartBaseURL: contractapi.EmailServiceURL(status.StalwartBaseURL),
		PublicBaseURL:   contractapi.EmailServiceURL(status.PublicBaseURL),
		SenderProvider:  contractapi.EmailProviderName(status.SenderProvider),
		MailboxSync: contractapi.MailboxSyncWorkerStatusView{
			Running:         status.MailboxSync.Running,
			LastDiscoveryAt: optionalContractTime(status.MailboxSync.LastDiscoveryAt),
			LastError:       optionalStatusError(status.MailboxSync.LastError),
			Accounts:        mailboxSyncAccounts(status.MailboxSync.Accounts),
		},
	}
}

func mailboxSyncAccounts(accounts map[string]emailsync.AccountStatus) contractapi.MailboxSyncAccounts {
	if len(accounts) == 0 {
		return contractapi.MailboxSyncAccounts{}
	}
	out := make(contractapi.MailboxSyncAccounts, len(accounts))
	for key, account := range accounts {
		out[key] = contractapi.MailboxSyncAccountStatusView{
			AccountID:       contractapi.AccountID(account.AccountID),
			Running:         account.Running,
			Connected:       account.Connected,
			LastSyncAt:      optionalContractTime(account.LastSyncAt),
			LastEventAt:     optionalContractTime(account.LastEventAt),
			LastConnectedAt: optionalContractTime(account.LastConnectedAt),
			LastError:       optionalStatusError(account.LastError),
		}
	}
	return out
}

func optionalContractTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	out := formatContractTime(*value)
	return &out
}

func formatContractTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func optionalStatusError(value string) *contractapi.MailboxStatusError {
	if value == "" {
		return nil
	}
	out := contractapi.MailboxStatusError(value)
	return &out
}

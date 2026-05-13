package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/mailbox-service/internal/app"
	"github.com/verself/mailbox-service/internal/contractapi"
	mailboxsync "github.com/verself/mailbox-service/internal/sync"
)

type mailboxServiceHealthOutput struct {
	Body mailboxServiceHealth
}

type mailboxServiceHealth struct {
	Status string `json:"status"`
}

func registerPublicRoutes(api huma.API, svc provider) {
	huma.Register(api, huma.Operation{
		OperationID: "mailbox-healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Mailbox service liveness probe",
	}, healthz())

	huma.Register(api, huma.Operation{
		OperationID: "mailbox-readyz",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Mailbox service readiness probe",
	}, readyz(svc))
}

func healthz() func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
	return func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
		out := &mailboxServiceHealthOutput{}
		out.Body = mailboxServiceHealth{Status: "ok"}
		return out, nil
	}
}

func readyz(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
	return func(ctx context.Context, _ *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
		if err := svc.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("mailbox service not ready: " + err.Error())
		}
		out := &mailboxServiceHealthOutput{}
		out.Body = mailboxServiceHealth{Status: "ok"}
		return out, nil
	}
}

func serviceStatus(status app.ServiceStatus) contractapi.MailboxSyncStatusView {
	return contractapi.MailboxSyncStatusView{
		StartedAt:       formatContractTime(status.StartedAt),
		StalwartBaseURL: contractapi.MailboxServiceURL(status.StalwartBaseURL),
		PublicBaseURL:   contractapi.MailboxServiceURL(status.PublicBaseURL),
		Forwarder: contractapi.MailboxForwarderStatusView{
			Enabled:                 status.Forwarder.Enabled,
			Running:                 status.Forwarder.Running,
			Mailbox:                 contractapi.ForwarderMailbox(status.Forwarder.Mailbox),
			ForwardTargetConfigured: status.Forwarder.ForwardTargetConfigured,
			LastError:               optionalStatusError(status.Forwarder.LastError),
			LastSyncAt:              optionalContractTime(status.Forwarder.LastSyncAt),
			LastForwardedAt:         optionalContractTime(status.Forwarder.LastForwardedAt),
			LastForwardedEmailID:    optionalForwardedEmailID(status.Forwarder.LastForwardedEmailID),
		},
		MailboxSync: contractapi.MailboxSyncWorkerStatusView{
			Running:         status.MailboxSync.Running,
			LastDiscoveryAt: optionalContractTime(status.MailboxSync.LastDiscoveryAt),
			LastError:       optionalStatusError(status.MailboxSync.LastError),
			Accounts:        mailboxSyncAccounts(status.MailboxSync.Accounts),
		},
	}
}

func mailboxSyncAccounts(accounts map[string]mailboxsync.AccountStatus) contractapi.MailboxSyncAccounts {
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

func optionalForwardedEmailID(value string) *contractapi.ForwardedEmailID {
	if value == "" {
		return nil
	}
	out := contractapi.ForwardedEmailID(value)
	return &out
}

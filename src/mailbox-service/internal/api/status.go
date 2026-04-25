package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/apiwire"
	"github.com/verself/mailbox-service/internal/app"
	mailboxsync "github.com/verself/mailbox-service/internal/sync"
)

type mailboxServiceHealthOutput struct {
	Body apiwire.MailboxHealth
}

type mailboxServiceStatusOutput struct {
	Body apiwire.MailboxServiceStatusResponse
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

	huma.Register(api, huma.Operation{
		OperationID: "mailbox-status",
		Method:      http.MethodGet,
		Path:        "/internal/mailbox/v1/status",
		Summary:     "Mailbox service internal status",
	}, status(svc))
}

func healthz() func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
	return func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
		out := &mailboxServiceHealthOutput{}
		out.Body = apiwire.MailboxHealth{Status: "ok"}
		return out, nil
	}
}

func readyz(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
	return func(ctx context.Context, _ *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
		if err := svc.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("mailbox service not ready: " + err.Error())
		}
		out := &mailboxServiceHealthOutput{}
		out.Body = apiwire.MailboxHealth{Status: "ok"}
		return out, nil
	}
}

func status(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceStatusOutput, error) {
	return func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceStatusOutput, error) {
		out := &mailboxServiceStatusOutput{}
		out.Body.Status = serviceStatus(svc.Status())
		return out, nil
	}
}

func serviceStatus(status app.ServiceStatus) apiwire.MailboxServiceStatus {
	return apiwire.MailboxServiceStatus{
		StartedAt:       status.StartedAt,
		StalwartBaseURL: status.StalwartBaseURL,
		PublicBaseURL:   status.PublicBaseURL,
		Forwarder: apiwire.MailboxForwarder{
			Enabled:                 status.Forwarder.Enabled,
			Running:                 status.Forwarder.Running,
			Mailbox:                 status.Forwarder.Mailbox,
			ForwardTargetConfigured: status.Forwarder.ForwardTargetConfigured,
			LastError:               status.Forwarder.LastError,
			LastSyncAt:              status.Forwarder.LastSyncAt,
			LastForwardedAt:         status.Forwarder.LastForwardedAt,
			LastForwardedEmailID:    status.Forwarder.LastForwardedEmailID,
		},
		MailboxSync: apiwire.MailboxSync{
			Running:         status.MailboxSync.Running,
			LastDiscoveryAt: status.MailboxSync.LastDiscoveryAt,
			LastError:       status.MailboxSync.LastError,
			Accounts:        mailboxSyncAccounts(status.MailboxSync.Accounts),
		},
	}
}

func mailboxSyncAccounts(accounts map[string]mailboxsync.AccountStatus) map[string]apiwire.MailboxSyncAccountStatus {
	if len(accounts) == 0 {
		return map[string]apiwire.MailboxSyncAccountStatus{}
	}
	out := make(map[string]apiwire.MailboxSyncAccountStatus, len(accounts))
	for key, account := range accounts {
		out[key] = apiwire.MailboxSyncAccountStatus{
			AccountID:       account.AccountID,
			Running:         account.Running,
			Connected:       account.Connected,
			LastSyncAt:      account.LastSyncAt,
			LastEventAt:     account.LastEventAt,
			LastConnectedAt: account.LastConnectedAt,
			LastError:       account.LastError,
		}
	}
	return out
}

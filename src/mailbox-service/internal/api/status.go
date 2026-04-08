package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type mailboxServiceHealthOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

type mailboxServiceStatusOutput struct {
	Body struct {
		Status any `json:"status"`
	}
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
		out.Body.Status = "ok"
		return out, nil
	}
}

func readyz(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
	return func(ctx context.Context, _ *mailboxServiceEmptyInput) (*mailboxServiceHealthOutput, error) {
		if err := svc.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("mailbox service not ready: " + err.Error())
		}
		out := &mailboxServiceHealthOutput{}
		out.Body.Status = "ok"
		return out, nil
	}
}

func status(svc provider) func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceStatusOutput, error) {
	return func(context.Context, *mailboxServiceEmptyInput) (*mailboxServiceStatusOutput, error) {
		out := &mailboxServiceStatusOutput{}
		out.Body.Status = svc.Status()
		return out, nil
	}
}

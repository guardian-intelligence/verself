package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/mailbox-service/internal/app"
)

type provider interface {
	Ready(context.Context) error
	Status() app.ServiceStatus
}

type emptyInput struct{}

type healthOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

type statusOutput struct {
	Body app.ServiceStatus
}

func NewAPI(mux *http.ServeMux, version, listenAddr string, svc provider) huma.API {
	config := huma.DefaultConfig("Mailbox Service", version)
	config.OpenAPI.Servers = []*huma.Server{
		{URL: "http://" + listenAddr},
	}

	api := humago.New(mux, config)
	registerRoutes(api, svc)
	return api
}

func registerRoutes(api huma.API, svc provider) {
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

func healthz() func(context.Context, *emptyInput) (*healthOutput, error) {
	return func(context.Context, *emptyInput) (*healthOutput, error) {
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	}
}

func readyz(svc provider) func(context.Context, *emptyInput) (*healthOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*healthOutput, error) {
		if err := svc.Ready(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("stalwart jmap session unavailable: " + err.Error())
		}
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	}
}

func status(svc provider) func(context.Context, *emptyInput) (*statusOutput, error) {
	return func(context.Context, *emptyInput) (*statusOutput, error) {
		out := &statusOutput{}
		out.Body = svc.Status()
		return out, nil
	}
}

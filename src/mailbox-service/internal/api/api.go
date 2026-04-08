package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/mailbox-service/internal/app"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
)

type provider interface {
	Ready(context.Context) error
	Status() app.ServiceStatus
	ResolveBoundAccount(context.Context, string) (string, error)
	GetBoundAccount(context.Context, string) (mailstore.Account, error)
	SetEmailSeen(context.Context, string, string, bool) error
	SetEmailFlagged(context.Context, string, string, bool) error
	MoveEmail(context.Context, string, string, string) error
	TrashEmail(context.Context, string, string) error
	FetchEmailBody(context.Context, string, string) (mailstore.EmailBody, error)
}

type mailboxServiceEmptyInput struct{}

func NewAPI(mux *http.ServeMux, version, listenAddr string, svc provider) (huma.API, http.Handler) {
	publicConfig := huma.DefaultConfig("Mailbox Service", version)
	publicConfig.OpenAPI.Servers = []*huma.Server{{URL: "http://" + listenAddr}}
	publicAPI := humago.New(mux, publicConfig)
	registerPublicRoutes(publicAPI, svc)

	privateMux := http.NewServeMux()
	privateConfig := huma.DefaultConfig("Mailbox Service", version)
	privateConfig.OpenAPI.Servers = []*huma.Server{{URL: "http://" + listenAddr}}
	privateAPI := humago.New(privateMux, privateConfig)
	registerMailRoutes(privateAPI, svc)

	return publicAPI, privateMux
}

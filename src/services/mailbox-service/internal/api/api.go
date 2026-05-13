package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/mailbox-service/internal/app"
	"github.com/verself/mailbox-service/internal/mailstore"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type provider interface {
	Ready(context.Context) error
	Status() app.ServiceStatus
	ResolveBoundAccount(context.Context, string) (string, error)
	GetBoundAccount(context.Context, string) (mailstore.Account, error)
	ListMailboxes(context.Context, string) ([]mailstore.Mailbox, error)
	SetEmailSeen(context.Context, string, string, bool) error
	SetEmailFlagged(context.Context, string, string, bool) error
	MoveEmail(context.Context, string, string, string) error
	TrashEmail(context.Context, string, string) error
	FetchEmailBody(context.Context, string, string) (mailstore.EmailBody, error)
}

type mailboxServiceEmptyInput struct{}

func NewAPI(mux *http.ServeMux, version, listenAddr string, svc provider, authorizers ...runtimeiam.OperationAuthorizer) (huma.API, http.Handler) {
	var authorizer runtimeiam.OperationAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	publicConfig := dto.DefaultHumaConfig("Mailbox Service", version)
	publicConfig.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	publicAPI := humago.New(mux, publicConfig)
	registerPublicRoutes(publicAPI, svc)
	dto.ApplyOpenAPIWireDefaults(publicAPI)

	privateMux := http.NewServeMux()
	privateConfig := dto.DefaultHumaConfig("Mailbox Service", version)
	privateConfig.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	privateAPI := humago.New(privateMux, privateConfig)
	applyPublicAPISecurityScheme(privateAPI)
	registerMailRoutes(privateAPI, svc, authorizer)
	dto.ApplyOpenAPIWireDefaults(privateAPI)

	return publicAPI, privateMux
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	privateConfig := dto.DefaultHumaConfig("Mailbox Service", version)
	privateConfig.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	privateAPI := humago.New(http.NewServeMux(), privateConfig)
	applyPublicAPISecurityScheme(privateAPI)
	registerMailRoutes(privateAPI, nil, nil)
	dto.ApplyOpenAPIWireDefaults(privateAPI)
	return privateAPI.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	privateConfig := dto.DefaultHumaConfig("Mailbox Service", version)
	privateConfig.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	privateAPI := humago.New(http.NewServeMux(), privateConfig)
	applyPublicAPISecurityScheme(privateAPI)
	registerMailRoutes(privateAPI, nil, nil)
	dto.ApplyOpenAPIWireDefaults(privateAPI)
	return privateAPI.OpenAPI().YAML()
}

func applyPublicAPISecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Zitadel OIDC access token for a mailbox-bound human subject.",
	}
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

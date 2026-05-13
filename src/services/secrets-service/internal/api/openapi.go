package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/secrets-service/internal/secrets"
	"github.com/verself/service-runtime/humaapi"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func NewAPI(mux *http.ServeMux, version, serverURL string, svc *secrets.Service, installationID string, authorizers ...runtimeiam.OperationAuthorizer) huma.API {
	config := humaapi.DefaultConfig("Verself Secrets Service API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	var authorizer runtimeiam.OperationAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	RegisterRoutes(api, svc, authorizer, installationID)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

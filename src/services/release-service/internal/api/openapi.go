package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/release-service/internal/release"
	"github.com/verself/service-runtime/humaapi"
)

func NewInternalAPI(mux *http.ServeMux, version string, serverURL string, svc *release.Service, installationID string) huma.API {
	config := humaapi.DefaultConfig("Release Internal API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalSecurityScheme(api)
	RegisterInternalRoutes(api, svc, installationID)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func applyInternalSecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["mutualTLS"] = &huma.SecurityScheme{
		Type:        "mutualTLS",
		Description: "SPIFFE X.509-SVID mutual TLS on the release-service internal listener.",
	}
}

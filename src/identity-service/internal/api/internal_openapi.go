package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/apiwire"
	"github.com/verself/identity-service/internal/identity"
)

func NewInternalAPI(mux *http.ServeMux, version, serverURL string, svc *identity.Service) huma.API {
	if version == "" {
		version = "1.0.0"
	}
	config := huma.DefaultConfig("Verself Identity Service Internal API", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalAPISecuritySchemes(api)
	RegisterInternalRoutes(api, svc)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, "https://"+listenAddr, nil)
	return api.OpenAPI().DowngradeYAML()
}

func InternalOpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, "https://"+listenAddr, nil)
	return api.OpenAPI().YAML()
}

func applyInternalAPISecuritySchemes(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["mutualTLS"] = &huma.SecurityScheme{
		Type:        "mutualTLS",
		Description: "SPIFFE X.509-SVID mutual TLS on the identity-service internal listener.",
	}
	openapi.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Forwarded Zitadel human bearer token scoped to the identity-service API audience.",
	}
}

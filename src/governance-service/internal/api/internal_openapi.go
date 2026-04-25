package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gopkg.in/yaml.v3"

	"github.com/verself/apiwire"
	"github.com/verself/governance-service/internal/governance"
)

func NewInternalAPI(mux *http.ServeMux, version, serverURL string, svc *governance.Service) huma.API {
	config := huma.DefaultConfig("Verself Governance Service Internal API", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalAPISecurityScheme(api)
	RegisterInternalRoutes(api, svc)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIYAML(format string) ([]byte, error) {
	mux := http.NewServeMux()
	svc := &governance.Service{}
	api := NewInternalAPI(mux, "dev", "https://127.0.0.1:4254", svc)
	switch format {
	case "3.0":
		return OpenAPIDowngradeYAML(api.OpenAPI())
	default:
		return yaml.Marshal(api.OpenAPI())
	}
}

func applyInternalAPISecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["mutualTLS"] = &huma.SecurityScheme{
		Type:        "mutualTLS",
		Description: "SPIFFE X.509-SVID mutual TLS on the governance-service internal listener.",
	}
}

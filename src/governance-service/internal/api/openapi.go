package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gopkg.in/yaml.v3"

	"github.com/verself/apiwire"
	"github.com/verself/governance-service/internal/governance"
)

func NewAPI(mux *http.ServeMux, version, serverURL string, svc *governance.Service) huma.API {
	config := huma.DefaultConfig("Verself Governance Service API", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, svc)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIYAML(format string) ([]byte, error) {
	mux := http.NewServeMux()
	svc := &governance.Service{}
	api := NewAPI(mux, "dev", "https://governance.api.verself.sh", svc)
	switch format {
	case "3.0":
		return OpenAPIDowngradeYAML(api.OpenAPI())
	default:
		return yaml.Marshal(api.OpenAPI())
	}
}

func OpenAPIDowngradeYAML(openapi *huma.OpenAPI) ([]byte, error) {
	clone := *openapi
	clone.OpenAPI = "3.0.3"
	return yaml.Marshal(&clone)
}

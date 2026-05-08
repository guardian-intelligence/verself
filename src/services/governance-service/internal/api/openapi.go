package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gopkg.in/yaml.v3"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/governance-service/internal/governance"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func NewAPI(mux *http.ServeMux, version, serverURL string, svc *governance.Service, authorizers ...runtimeiam.OperationAuthorizer) huma.API {
	config := huma.DefaultConfig("Verself Governance Service API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	var authorizer runtimeiam.OperationAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	RegisterRoutes(api, svc, authorizer)
	dto.ApplyOpenAPIWireDefaults(api)
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

package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gopkg.in/yaml.v3"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/secrets-service/internal/secrets"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func NewAPI(mux *http.ServeMux, version, serverURL string, svc *secrets.Service, installationID string, authorizers ...runtimeiam.OperationAuthorizer) huma.API {
	config := dto.DefaultHumaConfig("Verself Secrets Service API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	var authorizer runtimeiam.OperationAuthorizer
	if len(authorizers) > 0 {
		authorizer = authorizers[0]
	}
	RegisterRoutes(api, svc, authorizer, installationID)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIYAML(format string) ([]byte, error) {
	mux := http.NewServeMux()
	svc := &secrets.Service{}
	api := NewAPI(mux, "dev", "https://secrets.api.verself.sh", svc, "inst_openapi")
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

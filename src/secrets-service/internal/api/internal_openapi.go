package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gopkg.in/yaml.v3"

	"github.com/forge-metal/apiwire"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/secrets-service/internal/secrets"
)

type resolveInjectionOpenAPIInput struct {
	Body injectionResolveRequest
}

type resolveInjectionOpenAPIOutput struct {
	Body injectionResolveResponse
}

func NewInternalAPI(mux *http.ServeMux, version, serverURL string, svc *secrets.Service) huma.API {
	config := huma.DefaultConfig("Forge Metal Secrets Service Internal API", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalAPISecurityScheme(api)
	registerInternalOpenAPIRoutes(api, svc)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIYAML(format string) ([]byte, error) {
	mux := http.NewServeMux()
	svc := &secrets.Service{}
	api := NewInternalAPI(mux, "dev", "https://127.0.0.1:4253", svc)
	switch format {
	case "3.0":
		return OpenAPIDowngradeYAML(api.OpenAPI())
	default:
		return yaml.Marshal(api.OpenAPI())
	}
}

func registerInternalOpenAPIRoutes(api huma.API, svc *secrets.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "resolve-injection",
		Method:      http.MethodPost,
		Path:        "/internal/v1/injections/resolve",
		Summary:     "Resolve sandbox secret injection",
		Description: "SPIFFE-mTLS internal endpoint for sandbox-rental-service to resolve execution secret injections.",
		Security:    []map[string][]string{{"mutualTLS": {}}},
	}, resolveInjectionOpenAPIRoute(svc))
}

func resolveInjectionOpenAPIRoute(svc *secrets.Service) func(context.Context, *resolveInjectionOpenAPIInput) (*resolveInjectionOpenAPIOutput, error) {
	return func(ctx context.Context, input *resolveInjectionOpenAPIInput) (*resolveInjectionOpenAPIOutput, error) {
		if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
			return nil, unauthorized(ctx, "missing-workload-identity", "missing SPIFFE peer identity")
		}
		response, err := resolveInjection(ctx, svc, input.Body)
		if err != nil {
			return nil, mapError(ctx, err)
		}
		return &resolveInjectionOpenAPIOutput{Body: response}, nil
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
		Description: "SPIFFE X.509-SVID mutual TLS on the secrets-service internal listener.",
	}
}

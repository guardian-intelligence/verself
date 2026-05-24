package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/github-integration-service/internal/githubintegration"
	"github.com/verself/service-runtime/humaapi"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type Config struct {
	Version    string
	ListenAddr string
	Service    *githubintegration.Service
	Authorizer runtimeiam.OperationAuthorizer
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "0.1.0"
	}
	config := humaapi.DefaultConfig("Verself GitHub Integration API", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(cfg.ListenAddr)}}
	}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, cfg.Service, cfg.Authorizer)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
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
		Description:  "Zitadel OIDC access token scoped to the Verself product API audience.",
	}
}

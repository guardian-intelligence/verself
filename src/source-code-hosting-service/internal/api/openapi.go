package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/apiwire"
)

func NewAPI(mux *http.ServeMux, version, listenAddr string, cfg Config) huma.API {
	config := huma.DefaultConfig("Source Code Hosting Service", version)
	if listenAddr != "" {
		config.OpenAPI.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applySecuritySchemes(api)
	RegisterRoutes(api, cfg)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, Config{})
	return api.OpenAPI().YAML()
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, Config{})
	return api.OpenAPI().DowngradeYAML()
}

func NewInternalAPI(mux *http.ServeMux, version, listenAddr string, cfg Config) huma.API {
	config := huma.DefaultConfig("Source Code Hosting Service Internal API", version)
	if listenAddr != "" {
		config.OpenAPI.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applySecuritySchemes(api)
	RegisterInternalRoutes(api, cfg)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func NewInternalAPIYAML(version, listenAddr string, downgrade bool) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, listenAddr, Config{})
	if downgrade {
		return api.OpenAPI().DowngradeYAML()
	}
	return api.OpenAPI().YAML()
}

func applySecuritySchemes(api huma.API) {
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
		Description:  "Zitadel OIDC access token for a human subject.",
	}
	openapi.Components.SecuritySchemes["mutualTLS"] = &huma.SecurityScheme{
		Type:        "mutualTLS",
		Description: "SPIFFE mTLS between Forge Metal workloads.",
	}
}

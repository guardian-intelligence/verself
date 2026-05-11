package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/domain-transfer-objects"
)

func NewAPI(mux *http.ServeMux, version, listenAddr string, cfg Config) huma.API {
	config := huma.DefaultConfig("Source Code Hosting Service", version)
	if listenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applySecuritySchemes(api, "bearerAuth")
	RegisterRoutes(api, cfg)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, Config{InstallationID: "inst_openapi"})
	return api.OpenAPI().YAML()
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, Config{InstallationID: "inst_openapi"})
	return api.OpenAPI().DowngradeYAML()
}

func NewInternalAPI(mux *http.ServeMux, version, listenAddr string, cfg Config) huma.API {
	config := huma.DefaultConfig("Source Code Hosting Service Internal API", version)
	if listenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applySecuritySchemes(api, "mutualTLS")
	RegisterInternalRoutes(api, cfg)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func NewInternalAPIYAML(version, listenAddr string, downgrade bool) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, listenAddr, Config{InstallationID: "inst_openapi"})
	if downgrade {
		return api.OpenAPI().DowngradeYAML()
	}
	return api.OpenAPI().YAML()
}

func applySecuritySchemes(api huma.API, scheme string) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	switch scheme {
	case "bearerAuth":
		openapi.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "Zitadel OIDC access token for a human subject.",
		}
	case "mutualTLS":
		openapi.Components.SecuritySchemes["mutualTLS"] = &huma.SecurityScheme{
			Type:        "mutualTLS",
			Description: "SPIFFE mTLS between Verself workloads.",
		}
	default:
		panic("unknown Source OpenAPI security scheme " + scheme)
	}
}

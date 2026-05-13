package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/service-runtime/humaapi"
)

func NewAPI(mux *http.ServeMux, version, listenAddr string, cfg Config) huma.API {
	config := humaapi.DefaultConfig("Source Code Hosting Service", version)
	if listenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applySecuritySchemes(api, "bearerAuth")
	RegisterRoutes(api, cfg)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func NewInternalAPI(mux *http.ServeMux, version, listenAddr string, cfg Config) huma.API {
	config := humaapi.DefaultConfig("Source Code Hosting Service Internal API", version)
	if listenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applySecuritySchemes(api, "mutualTLS")
	RegisterInternalRoutes(api, cfg)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
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

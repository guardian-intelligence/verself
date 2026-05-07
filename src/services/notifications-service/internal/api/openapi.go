package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/notifications-service/internal/notifications"
)

type Config struct {
	Version    string
	ListenAddr string
	Service    *notifications.Service
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	config := huma.DefaultConfig("Notifications Service", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(cfg.ListenAddr)}}
	}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, cfg.Service)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func NewInternalAPI(mux *http.ServeMux, version, listenAddr string, cfg InternalConfig) huma.API {
	if version == "" {
		version = "1.0.0"
	}
	config := huma.DefaultConfig("Notifications Service Internal API", version)
	if listenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	}
	api := humago.New(mux, config)
	applyInternalAPISecurityScheme(api)
	RegisterInternalRoutes(api, cfg)
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
	api := NewAPI(http.NewServeMux(), Config{Version: version, ListenAddr: listenAddr})
	return api.OpenAPI().YAML()
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: version, ListenAddr: listenAddr})
	return api.OpenAPI().DowngradeYAML()
}

func InternalOpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, listenAddr, InternalConfig{Service: &notifications.Service{}})
	return api.OpenAPI().YAML()
}

func InternalOpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, listenAddr, InternalConfig{Service: &notifications.Service{}})
	return api.OpenAPI().DowngradeYAML()
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
		Description:  "Zitadel OIDC access token for a human subject.",
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
		Description: "SPIFFE X.509-SVID mutual TLS on the notifications-service internal listener.",
	}
}

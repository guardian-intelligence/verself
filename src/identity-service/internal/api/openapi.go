package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/identity-service/internal/identity"
)

type Config struct {
	Version    string
	ListenAddr string
	Service    *identity.Service
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	config := huma.DefaultConfig("Identity Service", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: "http://" + cfg.ListenAddr}}
	}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, cfg.Service)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: version, ListenAddr: listenAddr})
	return api.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: version, ListenAddr: listenAddr})
	return api.OpenAPI().YAML()
}

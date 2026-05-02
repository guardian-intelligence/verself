package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/object-storage-service/internal/objectstorage"
)

type Config struct {
	Version    string
	ListenAddr string
	Service    *objectstorage.Service
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	config := huma.DefaultConfig("Object Storage Service", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: "https://" + cfg.ListenAddr}}
	}
	api := humago.New(mux, config)
	RegisterAdminRoutes(api, cfg.Service)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: version, ListenAddr: listenAddr})
	return api.OpenAPI().YAML()
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: version, ListenAddr: listenAddr})
	return api.OpenAPI().DowngradeYAML()
}

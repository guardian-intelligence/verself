package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/apiwire"
	billingclient "github.com/forge-metal/billing-service/client"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func NewAPI(mux *http.ServeMux, version, listenAddr string, svc *jobs.Service, billing *billingclient.ServiceClient) huma.API {
	config := huma.DefaultConfig("Sandbox Rental Service", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: "http://" + listenAddr}}
	api := humago.New(mux, config)
	RegisterRoutes(api, svc, billing)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, nil, nil)
	return api.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, nil, nil)
	return api.OpenAPI().YAML()
}

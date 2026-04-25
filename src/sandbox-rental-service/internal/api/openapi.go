package api

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/apiwire"
	billingclient "github.com/forge-metal/billing-service/client"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
	"github.com/forge-metal/sandbox-rental-service/internal/recurring"
)

type PublicAPIConfig struct {
	BillingReturnOrigins []string
	PublicBaseURL        string
}

func NewAPI(mux *http.ServeMux, version, listenAddr string, svc *jobs.Service, recurringSvc *recurring.Service, billing *billingclient.ClientWithResponses, publicConfig PublicAPIConfig) huma.API {
	config := huma.DefaultConfig("Sandbox Rental Service", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	api := humago.New(mux, config)
	applyPublicAPISecurityScheme(api)
	RegisterRoutes(api, svc, recurringSvc, billing, publicConfig)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func NewInternalAPI(mux *http.ServeMux, version, listenAddr string, svc *jobs.Service) huma.API {
	config := huma.DefaultConfig("Sandbox Rental Service Internal API", version)
	config.OpenAPI.Servers = []*huma.Server{{URL: serverURL(listenAddr)}}
	api := humago.New(mux, config)
	applyInternalAPISecurityScheme(api)
	RegisterInternalRoutes(api, svc)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func OpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, nil, nil, nil, PublicAPIConfig{})
	return api.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewAPI(http.NewServeMux(), version, listenAddr, nil, nil, nil, PublicAPIConfig{})
	return api.OpenAPI().YAML()
}

func InternalOpenAPIDowngradeYAML(version, listenAddr string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, listenAddr, &jobs.Service{})
	return api.OpenAPI().DowngradeYAML()
}

func InternalOpenAPIYAML(version, listenAddr string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, listenAddr, &jobs.Service{})
	return api.OpenAPI().YAML()
}

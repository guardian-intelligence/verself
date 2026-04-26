package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/apiwire"
	"github.com/verself/projects-service/internal/projects"
)

func NewInternalAPI(mux *http.ServeMux, version, serverURL string, svc *projects.Service) huma.API {
	config := huma.DefaultConfig("Projects Service Internal API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalSecurityScheme(api)
	RegisterInternalRoutes(api, svc)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIYAML(version, serverURL string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, serverURL, &projects.Service{})
	return api.OpenAPI().YAML()
}

func InternalOpenAPIDowngradeYAML(version, serverURL string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, serverURL, &projects.Service{})
	return api.OpenAPI().DowngradeYAML()
}

func applyInternalSecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["mutualTLS"] = &huma.SecurityScheme{
		Type:        "mutualTLS",
		Description: "SPIFFE X.509-SVID mutual TLS on the projects-service internal listener.",
	}
}

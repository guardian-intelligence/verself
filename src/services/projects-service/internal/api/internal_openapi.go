package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/projects-service/internal/projects"
	"github.com/verself/service-runtime/humaapi"
)

func NewInternalAPI(mux *http.ServeMux, version, serverURL string, svc *projects.Service, installationID string) huma.API {
	config := humaapi.DefaultConfig("Projects Internal API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalSecurityScheme(api)
	registerProjectOperations(api, svc, apiProjectionInternal, nil, installationID)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIYAML(version, serverURL string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, serverURL, &projects.Service{}, "inst_openapi")
	return api.OpenAPI().YAML()
}

func InternalOpenAPIDowngradeYAML(version, serverURL string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, serverURL, &projects.Service{}, "inst_openapi")
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
		Description: "SPIFFE X.509-SVID mutual TLS on the projects-service service listener.",
	}
}

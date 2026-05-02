package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/profile-service/internal/profile"
)

func NewInternalAPI(mux *http.ServeMux, version, serverURL string, svc *profile.Service) huma.API {
	config := huma.DefaultConfig("Profile Service Internal API", version)
	config.Servers = []*huma.Server{{URL: serverURL}}
	api := humago.New(mux, config)
	applyInternalAPISecurityScheme(api)
	RegisterInternalRoutes(api, svc)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIYAML(version, serverURL string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, serverURL, &profile.Service{})
	return api.OpenAPI().YAML()
}

func InternalOpenAPIDowngradeYAML(version, serverURL string) ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), version, serverURL, &profile.Service{})
	return api.OpenAPI().DowngradeYAML()
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
		Description: "SPIFFE X.509-SVID mutual TLS on the profile-service internal listener.",
	}
}

package humaapi

import "github.com/danielgtaylor/huma/v2"

func DefaultConfig(title, version string) huma.Config {
	config := huma.DefaultConfig(title, version)
	// Huma's default create hook injects a `$schema` member into response bodies.
	config.CreateHooks = nil
	return config
}

func ApplyOpenAPIWireDefaults(api huma.API) {
	oapi := api.OpenAPI()
	if oapi == nil || oapi.Components == nil || oapi.Components.Schemas == nil {
		return
	}
	errorModel := oapi.Components.Schemas.Map()["ErrorModel"]
	if errorModel == nil || errorModel.Properties == nil {
		return
	}
	status := errorModel.Properties["status"]
	if status == nil {
		return
	}
	max := float64(599)
	status.Maximum = &max
}

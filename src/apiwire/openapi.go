package apiwire

import "github.com/danielgtaylor/huma/v2"

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

package endpoints

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "endpoints" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	payload := map[string]any{}
	for _, component := range components {
		endpoints, err := projection.NestedFields(component, "endpoints")
		if err != nil {
			return err
		}
		renderedEndpoints := map[string]any{}
		for _, endpoint := range endpoints {
			rendered, err := projection.EndpointWithAddresses(endpoint.Value)
			if err != nil {
				return err
			}
			renderedEndpoints[endpoint.Name] = rendered
		}
		probes := component.Value["probes"]
		if probes == nil {
			// schema marks #Component.probes optional; renderer materialises
			// the empty struct so existing consumers keep seeing `probes: {}`.
			probes = map[string]any{}
		}
		payload[component.Name] = map[string]any{
			"host":       component.Value["host"],
			"endpoints":  renderedEndpoints,
			"interfaces": component.Value["interfaces"],
			"probes":     probes,
		}
	}
	return projection.WriteYAML(out, "endpoints", map[string]any{
		"topology_endpoints": payload,
	})
}

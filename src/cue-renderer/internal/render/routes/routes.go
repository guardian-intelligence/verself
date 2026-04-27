package routes

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "routes" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	gateways, err := projection.TopologyMap(loaded, "gateways")
	if err != nil {
		return err
	}
	routes, err := projection.TopologySlice(loaded, "routes")
	if err != nil {
		return err
	}
	return projection.WriteYAML(out, "routes", map[string]any{
		"topology_gateways": gateways,
		"topology_routes":   routes,
	})
}

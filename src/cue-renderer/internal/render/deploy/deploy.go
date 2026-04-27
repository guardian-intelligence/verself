package deploy

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "deploy" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	artifacts, err := projection.DeployArtifacts(loaded)
	if err != nil {
		return err
	}
	edges, err := projection.TopologySlice(loaded, "edges")
	if err != nil {
		return err
	}
	return projection.WriteYAML(out, "deploy", map[string]any{
		"topology_deploy": map[string]any{
			"artifacts": artifacts,
			"edges":     edges,
		},
	})
}

package smoketest

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "smoke_test" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	return projection.WriteYAML(out, "smoke_test", map[string]any{
		"topology_smoke_test": map[string]any{
			"spans": loaded.SmokeTests.Spans,
		},
	})
}

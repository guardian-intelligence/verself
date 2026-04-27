package runtime

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "runtime" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	runtimes := map[string]any{}
	for _, component := range components {
		runtimes[component.Name] = map[string]any{
			"kind":     component.Value["kind"],
			"artifact": component.Value["artifact"],
			"runtime":  component.Value["runtime"],
		}
	}
	processes, err := projection.Processes(loaded)
	if err != nil {
		return err
	}
	return projection.WriteYAML(out, "runtime", map[string]any{
		"topology_runtime":   runtimes,
		"topology_processes": processes,
	})
}

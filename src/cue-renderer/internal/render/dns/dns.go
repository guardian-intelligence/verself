package dns

import (
	"context"
	"fmt"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "dns" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	routes, err := projection.TopologySlice(loaded, "routes")
	if err != nil {
		return err
	}
	var records []map[string]any
	for _, item := range routes {
		route, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("route: expected map, got %T", item)
		}
		kind, err := projection.String(route, "route", "kind")
		if err != nil {
			return err
		}
		host, err := projection.String(route, "route", "host")
		if err != nil {
			return err
		}
		if kind == "guest_host_route" || host == "10.255.0.1" {
			continue
		}
		records = append(records, map[string]any{
			"zone":   route["zone"],
			"record": host,
			"kind":   kind,
		})
	}
	return projection.WriteYAML(out, "dns", map[string]any{
		"topology_dns_records": records,
	})
}

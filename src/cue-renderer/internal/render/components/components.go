package components

import (
	"context"
	"fmt"
	"sort"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "components" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	rulesets, err := nftablesRulesets(loaded)
	if err != nil {
		return err
	}

	var rendered []map[string]any
	for _, component := range components {
		kind, err := projection.String(component.Value, component.Name, "kind")
		if err != nil {
			return err
		}
		workload, ok := component.Value["workload"].(map[string]any)
		if !ok {
			workload = map[string]any{}
		}
		order, err := optionalInt(workload, component.Name+".workload", "order")
		if err != nil {
			return err
		}
		workloadProjection, err := componentWorkloadProjection(component.Name, workload, order)
		if err != nil {
			return err
		}

		componentRulesets := rulesets[component.Name]
		if componentRulesets == nil {
			componentRulesets = []map[string]any{}
		}
		postgres, err := postgresProjection(component.Name, component.Value)
		if err != nil {
			return err
		}
		item := map[string]any{
			"name":              component.Name,
			"kind":              kind,
			"order":             order,
			"postgres":          postgres,
			"identities":        mapValue(component.Value, "identities"),
			"workload":          workloadProjection,
			"deployment":        deploymentProjection(mapValue(component.Value, "deployment")),
			"nftables_rulesets": componentRulesets,
		}
		rendered = append(rendered, item)
	}

	sort.Slice(rendered, func(i, j int) bool {
		leftOrder, _ := rendered[i]["order"].(int64)
		rightOrder, _ := rendered[j]["order"].(int64)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return rendered[i]["name"].(string) < rendered[j]["name"].(string)
	})

	return projection.WriteYAML(out, "components", map[string]any{
		"topology_components": rendered,
	})
}

// deploymentProjection materialises the supervisor knob for downstream
// substrate configuration. Only `supervisor` flows to the Ansible component
// fact bundle; the rest of the #Deployment knobs land in the rendered Nomad
// job spec. The default ("systemd") is what every component without an
// explicit opt-in receives.
func deploymentProjection(deployment map[string]any) map[string]any {
	supervisor := stringValue(deployment, "supervisor")
	if supervisor == "" {
		supervisor = "systemd"
	}
	return map[string]any{"supervisor": supervisor}
}

func optionalInt(parent map[string]any, path, key string) (int64, error) {
	if _, ok := parent[key]; !ok {
		return 0, nil
	}
	return projection.Int(parent, path, key)
}

func componentWorkloadProjection(name string, workload map[string]any, order int64) (map[string]any, error) {
	out := map[string]any{
		"order":            order,
		"directories":      sliceValue(workload, "directories"),
		"secret_refs":      sliceValue(workload, "secret_refs"),
		"auth":             authValue(workload),
		"bootstrap":        sliceValue(workload, "bootstrap"),
		"bootstrap_config": mapValue(workload, "bootstrap_config"),
		"units":            []map[string]any{},
	}
	if clickhouse, ok := workload["clickhouse"]; ok {
		out["clickhouse"] = clickhouse
	}

	// Non-service components (resources, gateways) don't carry a units
	// list. Treat missing as empty so the projection stays uniform
	// and downstream consumers can blanket-iterate `topology_components`.
	if _, has := workload["units"]; !has {
		return out, nil
	}
	rawUnits, err := projection.Slice(workload, name+".workload", "units")
	if err != nil {
		return nil, err
	}
	units := make([]map[string]any, 0, len(rawUnits))
	for i, raw := range rawUnits {
		unit, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.workload.units[%d]: expected map, got %T", name, i, raw)
		}
		units = append(units, unitProjection(unit))
	}
	out["units"] = units
	return out, nil
}

func unitProjection(unit map[string]any) map[string]any {
	out := map[string]any{
		"name":                 unit["name"],
		"user":                 unit["user"],
		"group":                unit["group"],
		"home":                 stringValue(unit, "home"),
		"create_home":          boolValue(unit, "create_home"),
		"supplementary_groups": sliceValue(unit, "supplementary_groups"),
		"load_credentials":     sliceValue(unit, "load_credentials"),
	}
	if uid, ok := unit["uid"]; ok {
		out["uid"] = uid
	}
	return out
}

func postgresProjection(name string, component map[string]any) (map[string]any, error) {
	postgres, err := projection.Map(component, name, "postgres")
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"database":         stringValue(postgres, "database"),
		"owner":            stringValue(postgres, "owner"),
		"connection_limit": postgres["connection_limit"],
		"password_ref":     mapValue(postgres, "password_ref"),
	}
	return out, nil
}

func mapValue(parent map[string]any, key string) map[string]any {
	value, ok := parent[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return value
}

func authValue(parent map[string]any) map[string]any {
	value, ok := parent["auth"].(map[string]any)
	if !ok {
		return map[string]any{"kind": "none"}
	}
	return value
}

func sliceValue(parent map[string]any, key string) []any {
	value, ok := parent[key].([]any)
	if !ok {
		return []any{}
	}
	return value
}

func boolValue(parent map[string]any, key string) bool {
	value, ok := parent[key].(bool)
	return ok && value
}

func stringValue(parent map[string]any, key string) string {
	value, ok := parent[key].(string)
	if !ok {
		return ""
	}
	return value
}

func nftablesRulesets(loaded load.Loaded) (map[string][]map[string]any, error) {
	raw, err := projection.TopologyMap(loaded, "nftables.rulesets")
	if err != nil {
		return nil, err
	}
	out := map[string][]map[string]any{}
	for _, name := range projection.SortedKeys(raw) {
		ruleset, ok := raw[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("topology.nftables.rulesets.%s: expected map, got %T", name, raw[name])
		}
		component, err := projection.OptionalString(ruleset, "topology.nftables.rulesets."+name, "component")
		if err != nil {
			return nil, err
		}
		if component == "" {
			continue
		}
		target, err := projection.String(ruleset, "topology.nftables.rulesets."+name, "target")
		if err != nil {
			return nil, err
		}
		table, err := projection.String(ruleset, "topology.nftables.rulesets."+name, "table")
		if err != nil {
			return nil, err
		}
		out[component] = append(out[component], map[string]any{
			"name":   name,
			"target": target,
			"table":  table,
		})
	}
	for component := range out {
		sort.Slice(out[component], func(i, j int) bool {
			return out[component][i]["name"].(string) < out[component][j]["name"].(string)
		})
	}
	return out, nil
}

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
		converge, ok := component.Value["converge"].(map[string]any)
		if !ok {
			converge = map[string]any{}
		}
		deployTag, err := projection.OptionalString(converge, component.Name+".converge", "deploy_tag")
		if err != nil {
			return err
		}
		if deployTag == "" {
			deployTag = component.Name
		}
		order, err := optionalInt(converge, component.Name+".converge", "order")
		if err != nil {
			return err
		}
		convergeProjection, err := componentConvergeProjection(component.Name, converge, deployTag, order)
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
			"deploy_tag":        deployTag,
			"order":             order,
			"postgres":          postgres,
			"identities":        mapValue(component.Value, "identities"),
			"converge":          convergeProjection,
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
// Ansible. Only `supervisor` flows to converge_component.yml; the rest of
// the #Deployment knobs land in the rendered Nomad job spec, not in the
// per-component fact bundle. The default ("systemd") is what every
// component without an explicit opt-in receives.
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

func componentConvergeProjection(name string, converge map[string]any, deployTag string, order int64) (map[string]any, error) {
	out := map[string]any{
		"enabled":          boolValue(converge, "enabled"),
		"deploy_tag":       deployTag,
		"order":            order,
		"directories":      sliceValue(converge, "directories"),
		"secret_refs":      sliceValue(converge, "secret_refs"),
		"auth":             authValue(converge),
		"bootstrap":        sliceValue(converge, "bootstrap"),
		"bootstrap_config": mapValue(converge, "bootstrap_config"),
		"units":            []map[string]any{},
	}
	if clickhouse, ok := converge["clickhouse"]; ok {
		out["clickhouse"] = clickhouse
	}

	// Non-service components (resources, gateways) don't carry a units
	// list. Treat missing as empty so the projection stays uniform
	// and downstream consumers can blanket-iterate `topology_components`.
	if _, has := converge["units"]; !has {
		return out, nil
	}
	rawUnits, err := projection.Slice(converge, name+".converge", "units")
	if err != nil {
		return nil, err
	}
	units := make([]map[string]any, 0, len(rawUnits))
	for i, raw := range rawUnits {
		unit, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.converge.units[%d]: expected map, got %T", name, i, raw)
		}
		units = append(units, unitProjection(unit))
	}
	out["units"] = units
	return out, nil
}

func unitProjection(unit map[string]any) map[string]any {
	out := map[string]any{
		"name":                 unit["name"],
		"exec":                 unit["exec"],
		"user":                 unit["user"],
		"group":                unit["group"],
		"home":                 stringValue(unit, "home"),
		"create_home":          boolValue(unit, "create_home"),
		"supplementary_groups": sliceValue(unit, "supplementary_groups"),
		"load_credentials":     sliceValue(unit, "load_credentials"),
		"readiness":            sliceValue(unit, "readiness"),
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

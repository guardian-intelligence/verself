package nftables

import (
	"fmt"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render/projection"
	"github.com/verself/cue-renderer/schema"
)

type endpointRef struct {
	Component string
	Endpoint  string
}

func renderRulesets(loaded load.Loaded) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range sortedKeys(loaded.Topology.Nftables.Rulesets) {
		ruleset := loaded.Topology.Nftables.Rulesets[name]
		body, err := renderRuleset(loaded.Topology.Components, name, ruleset)
		if err != nil {
			return nil, err
		}
		path := strings.TrimPrefix(ruleset.Target, "/")
		if _, exists := out[path]; exists {
			return nil, fmt.Errorf("duplicate nftables target %s", ruleset.Target)
		}
		out[path] = body
	}
	return out, nil
}

func renderRuleset(components map[string]schema.Component, name string, ruleset schema.NftablesRuleset) ([]byte, error) {
	var b strings.Builder
	b.WriteString(projection.Header)
	fmt.Fprintf(&b, "# nftables ruleset %s.\n", name)
	writeTableStart(&b, ruleset.Table)

	if len(ruleset.Input) > 0 {
		if err := writeInputRules(&b, components, name, ruleset.Input); err != nil {
			return nil, err
		}
	}

	// gengotypes emits NftablesOutputChain as a struct even when CUE
	// declared the field optional. Detect "absent" by Final being its
	// zero string — every present output chain unifies with the
	// `final: "drop" | "none" | *"drop"` constraint.
	if ruleset.Output.Final != "" {
		if err := writeOutputChain(&b, components, name, ruleset.Output); err != nil {
			return nil, err
		}
	}

	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeInputRules(b *strings.Builder, components map[string]schema.Component, rulesetName string, rules []any) error {
	b.WriteString(`    chain input {
        type filter hook input priority filter; policy accept;

`)
	for i, raw := range rules {
		// Each input rule in CUE is `#NftablesInputRule`, but
		// `[...#NftablesInputRule] | *[]` collapses to []any in
		// gengotypes — so we type-assert at the leaves.
		rule, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("topology.nftables.rulesets.%s.input[%d]: expected map, got %T", rulesetName, i, raw)
		}
		path := fmt.Sprintf("topology.nftables.rulesets.%s.input[%d]", rulesetName, i)
		kind, err := projection.String(rule, path, "kind")
		if err != nil {
			return err
		}
		switch kind {
		case "drop_non_loopback":
			refs, err := endpointRefs(rule, path)
			if err != nil {
				return err
			}
			ports, err := endpointPorts(components, refs)
			if err != nil {
				return err
			}
			fmt.Fprintf(b, "        tcp dport %s iifname != \"lo\" drop\n", portSet(ports))
		case "accept_iifname_endpoints":
			iifname, err := projection.String(rule, path, "iifname")
			if err != nil {
				return err
			}
			refs, err := endpointRefs(rule, path)
			if err != nil {
				return err
			}
			ports, err := endpointPorts(components, refs)
			if err != nil {
				return err
			}
			fmt.Fprintf(b, "        iifname %q tcp dport %s accept\n", iifname, portSet(ports))
		default:
			return fmt.Errorf("%s.kind: unsupported %q", path, kind)
		}
	}
	b.WriteString("    }\n\n")
	return nil
}

func writeOutputChain(b *strings.Builder, components map[string]schema.Component, rulesetName string, output schema.NftablesOutputChain) error {
	b.WriteString(`    chain output {
        type filter hook output priority filter; policy accept;

`)
	if output.Established {
		b.WriteString("        ct state established,related accept\n")
	}
	if output.User != nil {
		fmt.Fprintf(b, "        meta skuid != %s accept\n", formatSkuid(output.User))
	}
	for i, rule := range output.Rules {
		if err := writeOutputRule(b, components, rulesetName, i, rule); err != nil {
			return err
		}
	}
	if output.Final == "drop" {
		b.WriteString("\n        drop\n")
	}
	b.WriteString("    }\n")
	return nil
}

// writeOutputRule renders one element of an output chain. The CUE
// `#NftablesOutputRule` is a disjunction over kinds; gengotypes types
// the slice element as a bare `map[string]any` so the renderer
// discriminates on `kind` and reaches for the rule's own fields.
func writeOutputRule(b *strings.Builder, components map[string]schema.Component, rulesetName string, index int, rule schema.NftablesOutputRule) error {
	path := fmt.Sprintf("topology.nftables.rulesets.%s.output.rules[%d]", rulesetName, index)
	kind, err := projection.String(rule, path, "kind")
	if err != nil {
		return err
	}
	switch kind {
	case "accept_loopback_all":
		b.WriteString("        oifname \"lo\" accept\n")
	case "accept_loopback_endpoints":
		refs, err := endpointRefs(rule, path)
		if err != nil {
			return err
		}
		ports, err := endpointPorts(components, refs)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "        oifname \"lo\" tcp dport %s", portSet(ports))
		if skuid, ok := rule["skuid"]; ok {
			fmt.Fprintf(b, " meta skuid %s", formatSkuid(skuid))
		}
		b.WriteString(" accept\n")
	case "drop_loopback_endpoints":
		refs, err := endpointRefs(rule, path)
		if err != nil {
			return err
		}
		ports, err := endpointPorts(components, refs)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "        oifname \"lo\" tcp dport %s drop\n", portSet(ports))
	case "accept_port":
		protocol, err := projection.String(rule, path, "protocol")
		if err != nil {
			return err
		}
		port, err := projection.Int(rule, path, "port")
		if err != nil {
			return err
		}
		if oifname, ok := rule["oifname"]; ok {
			oif, ok := oifname.(string)
			if !ok {
				return fmt.Errorf("%s.oifname: expected string, got %T", path, oifname)
			}
			fmt.Fprintf(b, "        oifname %q ", oif)
		} else {
			b.WriteString("        ")
		}
		fmt.Fprintf(b, "%s dport %d accept\n", protocol, port)
	case "drop_ip_daddr_set":
		family, err := projection.String(rule, path, "family")
		if err != nil {
			return err
		}
		addrs, err := projection.StringList(rule, path, "addrs")
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "        %s daddr { %s } drop\n", family, strings.Join(addrs, ", "))
	case "accept_non_tcp_udp":
		b.WriteString("        meta l4proto != tcp meta l4proto != udp accept\n")
	default:
		return fmt.Errorf("%s.kind: unsupported %q", path, kind)
	}
	return nil
}

func endpointPorts(components map[string]schema.Component, refs []endpointRef) ([]int, error) {
	ports := make([]int, 0, len(refs))
	for _, ref := range refs {
		component, ok := components[ref.Component]
		if !ok {
			return nil, fmt.Errorf("topology.components.%s: missing", ref.Component)
		}
		endpoint, ok := component.Endpoints[ref.Endpoint]
		if !ok {
			return nil, fmt.Errorf("topology.components.%s.endpoints.%s: missing", ref.Component, ref.Endpoint)
		}
		ports = append(ports, int(endpoint.Port))
	}
	return ports, nil
}

func endpointRefs(rule map[string]any, path string) ([]endpointRef, error) {
	raw, err := projection.Slice(rule, path, "endpoints")
	if err != nil {
		return nil, err
	}
	out := make([]endpointRef, 0, len(raw))
	for i, item := range raw {
		ref, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.endpoints[%d]: expected map, got %T", path, i, item)
		}
		component, err := projection.String(ref, fmt.Sprintf("%s.endpoints[%d]", path, i), "component")
		if err != nil {
			return nil, err
		}
		endpoint, err := projection.String(ref, fmt.Sprintf("%s.endpoints[%d]", path, i), "endpoint")
		if err != nil {
			return nil, err
		}
		out = append(out, endpointRef{Component: component, Endpoint: endpoint})
	}
	return out, nil
}

func writeTableStart(b *strings.Builder, table string) {
	fmt.Fprintf(b, "table inet %s\n", table)
	fmt.Fprintf(b, "delete table inet %s\n\n", table)
	fmt.Fprintf(b, "table inet %s {\n", table)
}

// formatSkuid renders an `meta skuid` operand. CUE types it as the
// disjunction `(string & =~"...") | (int & >=0)`; gengotypes emits
// schema.NftablesSkuid as `any` because of the disjunction, so the
// switch lives here once and the renderer never type-asserts in line.
func formatSkuid(value schema.NftablesSkuid) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%d", int64(v))
	default:
		return fmt.Sprintf("%v", v)
	}
}

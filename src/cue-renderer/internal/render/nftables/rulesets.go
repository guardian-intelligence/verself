package nftables

import (
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type endpointRef struct {
	Component string
	Endpoint  string
}

func renderRulesets(loaded load.Loaded) (map[string][]byte, error) {
	idx, err := buildTopologyIndex(loaded)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	names := make([]string, 0, len(loaded.Nftables.Rulesets))
	for name := range loaded.Nftables.Rulesets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ruleset := loaded.Nftables.Rulesets[name]
		target, err := projection.String(ruleset, "topology.nftables.rulesets."+name, "target")
		if err != nil {
			return nil, err
		}
		table, err := projection.String(ruleset, "topology.nftables.rulesets."+name, "table")
		if err != nil {
			return nil, err
		}
		body, err := renderRuleset(idx, name, table, ruleset)
		if err != nil {
			return nil, err
		}
		path := strings.TrimPrefix(target, "/")
		if _, exists := out[path]; exists {
			return nil, fmt.Errorf("duplicate nftables target %s", target)
		}
		out[path] = body
	}
	return out, nil
}

func renderRuleset(idx topologyIndex, name, table string, ruleset map[string]any) ([]byte, error) {
	var b strings.Builder
	b.WriteString(generatedHeader)
	fmt.Fprintf(&b, "# nftables ruleset %s.\n", name)
	writeTableStart(&b, table)

	if rawInput, ok := ruleset["input"]; ok {
		inputRules, ok := rawInput.([]any)
		if !ok {
			return nil, fmt.Errorf("topology.nftables.rulesets.%s.input: expected list, got %T", name, rawInput)
		}
		if len(inputRules) > 0 {
			if err := writeInputRules(&b, idx, name, inputRules); err != nil {
				return nil, err
			}
		}
	}

	if rawOutput, ok := ruleset["output"]; ok {
		output, ok := rawOutput.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("topology.nftables.rulesets.%s.output: expected map, got %T", name, rawOutput)
		}
		if err := writeOutputChain(&b, idx, name, output); err != nil {
			return nil, err
		}
	}

	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func writeInputRules(b *strings.Builder, idx topologyIndex, rulesetName string, rules []any) error {
	b.WriteString(`    chain input {
        type filter hook input priority filter; policy accept;

`)
	for i, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("topology.nftables.rulesets.%s.input[%d]: expected map, got %T", rulesetName, i, raw)
		}
		kind, err := projection.String(rule, fmt.Sprintf("topology.nftables.rulesets.%s.input[%d]", rulesetName, i), "kind")
		if err != nil {
			return err
		}
		switch kind {
		case "drop_non_loopback":
			refs, err := endpointRefs(rule, fmt.Sprintf("topology.nftables.rulesets.%s.input[%d]", rulesetName, i))
			if err != nil {
				return err
			}
			ports, err := idx.endpointPorts(refs)
			if err != nil {
				return err
			}
			fmt.Fprintf(b, "        tcp dport %s iifname != \"lo\" drop\n", portSet(ports))
		default:
			return fmt.Errorf("topology.nftables.rulesets.%s.input[%d].kind: unsupported %q", rulesetName, i, kind)
		}
	}
	b.WriteString("    }\n\n")
	return nil
}

func writeOutputChain(b *strings.Builder, idx topologyIndex, rulesetName string, output map[string]any) error {
	established, err := boolValue(output, "topology.nftables.rulesets."+rulesetName+".output", "established")
	if err != nil {
		return err
	}
	final, err := projection.String(output, "topology.nftables.rulesets."+rulesetName+".output", "final")
	if err != nil {
		return err
	}
	rules, err := projection.Slice(output, "topology.nftables.rulesets."+rulesetName+".output", "rules")
	if err != nil {
		return err
	}

	b.WriteString(`    chain output {
        type filter hook output priority filter; policy accept;

`)
	if established {
		b.WriteString("        ct state established,related accept\n")
	}
	if user, ok := output["user"]; ok {
		fmt.Fprintf(b, "        meta skuid != %s accept\n", formatSkuid(user))
	}
	for i, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("topology.nftables.rulesets.%s.output.rules[%d]: expected map, got %T", rulesetName, i, raw)
		}
		if err := writeOutputRule(b, idx, rulesetName, i, rule); err != nil {
			return err
		}
	}
	if final == "drop" {
		b.WriteString("\n        drop\n")
	}
	b.WriteString("    }\n")
	return nil
}

func writeOutputRule(b *strings.Builder, idx topologyIndex, rulesetName string, index int, rule map[string]any) error {
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
		ports, err := idx.endpointPorts(refs)
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
		ports, err := idx.endpointPorts(refs)
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
		addrs, err := stringSlice(rule, path, "addrs")
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

type topologyIndex struct {
	components map[string]projection.NamedMap
}

func buildTopologyIndex(loaded load.Loaded) (topologyIndex, error) {
	components, err := projection.Components(loaded)
	if err != nil {
		return topologyIndex{}, err
	}
	idx := topologyIndex{components: map[string]projection.NamedMap{}}
	for _, component := range components {
		idx.components[component.Name] = component
	}
	return idx, nil
}

func (idx topologyIndex) endpointPorts(refs []endpointRef) ([]int, error) {
	ports := make([]int, 0, len(refs))
	for _, ref := range refs {
		component, ok := idx.components[ref.Component]
		if !ok {
			return nil, fmt.Errorf("topology.components.%s: missing", ref.Component)
		}
		endpoints, err := projection.Map(component.Value, ref.Component, "endpoints")
		if err != nil {
			return nil, err
		}
		endpoint, err := projection.Map(endpoints, ref.Component+".endpoints", ref.Endpoint)
		if err != nil {
			return nil, err
		}
		port, err := projection.Int(endpoint, ref.Component+".endpoints."+ref.Endpoint, "port")
		if err != nil {
			return nil, err
		}
		ports = append(ports, int(port))
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

func formatSkuid(value any) string {
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

func stringSlice(parent map[string]any, path, key string) ([]string, error) {
	raw, err := projection.Slice(parent, path, key)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for i, item := range raw {
		value, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s[%d]: expected string, got %T", path, key, i, item)
		}
		out = append(out, value)
	}
	return out, nil
}

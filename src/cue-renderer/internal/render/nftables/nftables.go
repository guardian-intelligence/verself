package nftables

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
	"github.com/verself/cue-renderer/schema"
)

type Renderer struct{}

func (Renderer) Name() string { return "nftables" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	if err := out.WriteFile(ConfPath, renderMainConfig()); err != nil {
		return err
	}

	hostFirewall, err := renderHostChain(loaded)
	if err != nil {
		return err
	}
	if err := out.WriteFile(HostFirewallPath, hostFirewall); err != nil {
		return err
	}

	if err := out.WriteFile(FirewallTargetPath, renderFirewallTarget()); err != nil {
		return err
	}

	files, err := renderRulesets(loaded)
	if err != nil {
		return err
	}
	for target, data := range files {
		if err := out.WriteFile(RulesetPath(target), data); err != nil {
			return err
		}
	}
	return nil
}

func renderMainConfig() []byte {
	return []byte(projection.Header + `#!/usr/sbin/nft -f
# Load all rulesets from /etc/nftables.d/.
#
# No "flush ruleset": each .nft file does its own atomic table replace, and
# provider rules such as Latitude.sh metadata DNAT are left untouched.
include "/etc/nftables.d/*.nft"
`)
}

func renderHostChain(loaded load.Loaded) ([]byte, error) {
	chain := loaded.Topology.Nftables.Host
	if chain.Table == "" {
		return nil, fmt.Errorf("topology.nftables.host: missing host firewall declaration")
	}

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString("#!/usr/sbin/nft -f\n")
	b.WriteString("# Host firewall: default-deny ingress with allowlisted services.\n\n")
	fmt.Fprintf(&b, "table inet %s\n", chain.Table)
	fmt.Fprintf(&b, "delete table inet %s\n\n", chain.Table)
	fmt.Fprintf(&b, "table inet %s {\n", chain.Table)
	b.WriteString("    chain input {\n")
	fmt.Fprintf(&b, "        type filter hook input priority filter; policy %s;\n", chain.Policy)

	if chain.Input.AcceptEstablishedRelated {
		b.WriteString("        ct state established,related accept\n")
	}
	if chain.Input.DropInvalid {
		b.WriteString("        ct state invalid drop\n")
	}

	for i, rule := range chain.Input.Rules {
		if err := writeHostInputRule(&b, loaded.Topology.Components, i, rule); err != nil {
			return nil, err
		}
	}

	b.WriteString("    }\n}\n")
	return []byte(b.String()), nil
}

func writeHostInputRule(b *strings.Builder, components map[string]schema.Component, index int, rule schema.NftablesHostInputRule) error {
	path := fmt.Sprintf("topology.nftables.host.input.rules[%d]", index)
	kind, err := projection.String(rule, path, "kind")
	if err != nil {
		return err
	}
	switch kind {
	case "accept_iifname":
		iifname, err := projection.String(rule, path, "iifname")
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "        iifname %q accept\n", iifname)
	case "accept_guest_iifname_endpoints":
		iifname, err := projection.String(rule, path, "iifname")
		if err != nil {
			return err
		}
		saddr, err := projection.String(rule, path, "saddr")
		if err != nil {
			return err
		}
		daddr, err := projection.String(rule, path, "daddr")
		if err != nil {
			return err
		}
		protocol, err := projection.String(rule, path, "protocol")
		if err != nil {
			return err
		}
		ports, err := portsFromRule(components, rule, path)
		if err != nil {
			return err
		}
		if len(ports) == 0 {
			return fmt.Errorf("%s: no endpoints resolved to ports", path)
		}
		fmt.Fprintf(b, "        iifname %q ip saddr %s ip daddr %s %s dport %s accept\n", iifname, saddr, daddr, protocol, portSet(ports))
	case "accept_protocol_family":
		family, err := projection.String(rule, path, "family")
		if err != nil {
			return err
		}
		switch family {
		case "icmp":
			b.WriteString("        ip protocol icmp accept\n")
		case "icmpv6":
			b.WriteString("        ip6 nexthdr icmpv6 accept\n")
		default:
			return fmt.Errorf("%s.family: unsupported %q", path, family)
		}
	case "accept_port_set":
		protocol, err := projection.String(rule, path, "protocol")
		if err != nil {
			return err
		}
		ports, err := portsFromRule(components, rule, path)
		if err != nil {
			return err
		}
		if len(ports) == 0 {
			return nil
		}
		fmt.Fprintf(b, "        %s dport %s accept\n", protocol, portSet(ports))
	case "accept_rate_limited_port":
		protocol, err := projection.String(rule, path, "protocol")
		if err != nil {
			return err
		}
		port, err := projection.Int(rule, path, "port")
		if err != nil {
			return err
		}
		meter, err := projection.String(rule, path, "meter")
		if err != nil {
			return err
		}
		rate, err := projection.String(rule, path, "rate")
		if err != nil {
			return err
		}
		burst, err := projection.Int(rule, path, "burst")
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "        %s dport %d ct state new meter %s { ip saddr limit rate %s burst %d packets } accept\n", protocol, port, meter, rate, burst)
	default:
		return fmt.Errorf("%s.kind: unsupported %q", path, kind)
	}
	return nil
}

// portsFromRule resolves the rule's `ports` literal list and `endpoints`
// component/endpoint refs into a sorted, deduplicated []int. Ports and
// endpoints are both optional; an "accept_port_set" rule may carry either
// or both, mirroring how config.nftables.public_tcp_ports merges with
// component endpoints in CUE.
func portsFromRule(components map[string]schema.Component, rule schema.NftablesHostInputRule, path string) ([]int, error) {
	seen := map[int]bool{}
	if raw, ok := rule["ports"]; ok && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("%s.ports: expected list, got %T", path, raw)
		}
		for i, item := range list {
			p, err := asInt(item)
			if err != nil {
				return nil, fmt.Errorf("%s.ports[%d]: %w", path, i, err)
			}
			seen[p] = true
		}
	}
	if _, ok := rule["endpoints"]; ok {
		refs, err := endpointRefs(rule, path)
		if err != nil {
			return nil, err
		}
		ports, err := endpointPorts(components, refs)
		if err != nil {
			return nil, err
		}
		for _, p := range ports {
			seen[p] = true
		}
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out, nil
}

func asInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("expected integer, got %v", v)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

func renderFirewallTarget() []byte {
	return []byte(projection.Header + `# verself-firewall.target is the host firewall readiness point.
#
# Network-facing verself services declare After= and Requires= on this target so
# they cannot bind before nftables has installed the default-deny policy.

[Unit]
Description=verself host firewall ready
Requires=nftables.service
After=nftables.service

[Install]
WantedBy=multi-user.target
`)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func portSet(ports []int) string {
	if len(ports) == 1 {
		return fmt.Sprintf("%d", ports[0])
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, fmt.Sprintf("%d", port))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

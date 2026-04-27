package nftables

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "nftables" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	if err := out.WriteFile(ConfPath, renderMainConfig()); err != nil {
		return err
	}

	hostFirewall, err := renderHostFirewall(loaded)
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

type hostFirewallConfig struct {
	TrustedInterfaces []string
	PublicTCPPorts    []int
	PublicUDPPorts    []int
	GuestTCPPorts     []int
	GuestCIDR         string
	GuestHostIP       string
	SSHPublic         bool
	SSHRate           string
	SSHBurst          int
}

func renderHostFirewall(loaded load.Loaded) ([]byte, error) {
	cfg, err := hostFirewallConfigFromLoaded(loaded)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString(`#!/usr/sbin/nft -f
# Host firewall: default-deny ingress with allowlisted services.

table inet verself_host
delete table inet verself_host

table inet verself_host {
    chain input {
        type filter hook input priority filter; policy drop;

        # Existing connections.
        ct state established,related accept
        ct state invalid drop

        # Trusted interfaces: loopback and WireGuard meshes.
`)
	for _, iface := range cfg.TrustedInterfaces {
		fmt.Fprintf(&b, "        iifname %q accept\n", iface)
	}

	if len(cfg.GuestTCPPorts) > 0 {
		b.WriteString(`
        # Firecracker guests may reach selected services on the host-only service plane.
`)
		fmt.Fprintf(&b, "        iifname \"fc-tap-*\" ip saddr %s ip daddr %s tcp dport %s accept\n", cfg.GuestCIDR, cfg.GuestHostIP, portSet(cfg.GuestTCPPorts))
	}

	b.WriteString(`
        # ICMP / ICMPv6 (ping, PMTUD, neighbor discovery).
        ip protocol icmp accept
        ip6 nexthdr icmpv6 accept
`)

	if len(cfg.PublicTCPPorts) > 0 {
		b.WriteString(`
        # Public TCP services.
`)
		fmt.Fprintf(&b, "        tcp dport %s accept\n", portSet(cfg.PublicTCPPorts))
	}

	if len(cfg.PublicUDPPorts) > 0 {
		b.WriteString(`
        # Public UDP services.
`)
		fmt.Fprintf(&b, "        udp dport %s accept\n", portSet(cfg.PublicUDPPorts))
	}

	if cfg.SSHPublic {
		b.WriteString(`
        # SSH from the public internet with per-source-IP rate limiting.
`)
		fmt.Fprintf(&b, "        tcp dport 22 ct state new meter ssh_rate { ip saddr limit rate %s burst %d packets } accept\n", cfg.SSHRate, cfg.SSHBurst)
	}

	b.WriteString(`
        # Everything else hits policy drop.
    }
}
`)
	return []byte(b.String()), nil
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

func hostFirewallConfigFromLoaded(loaded load.Loaded) (hostFirewallConfig, error) {
	nft := loaded.Config.Nftables
	publicTCP := map[int]bool{}
	for i, raw := range nft.PublicTCPPorts {
		// PublicTCPPorts is `[...#Port] | *[80, 443]` in CUE; gengotypes
		// degrades a list-with-default to []any, so we reach for the
		// element type at the leaves rather than re-modelling the schema.
		port, ok := raw.(int64)
		if !ok {
			return hostFirewallConfig{}, fmt.Errorf("config.nftables.public_tcp_ports[%d]: expected int, got %T", i, raw)
		}
		publicTCP[int(port)] = true
	}

	publicUDP := map[int]bool{}
	trustedInterfaces := map[string]bool{"lo": true}
	for _, name := range sortedKeys(loaded.Config.Wireguard.Tunnels) {
		tunnel := loaded.Config.Wireguard.Tunnels[name]
		trustedInterfaces[tunnel.Interface] = true
		publicUDP[int(tunnel.Port)] = true
	}

	guestTCP := map[int]bool{}
	var guestHostIP string
	for _, name := range sortedKeys(loaded.Topology.Components) {
		component := loaded.Topology.Components[name]
		if name == "firecracker_host_service" {
			guestHostIP = component.Host
		}
		for _, endpoint := range component.Endpoints {
			switch endpoint.Exposure {
			case "public":
				if endpoint.Protocol == "statsd" {
					publicUDP[int(endpoint.Port)] = true
				} else {
					publicTCP[int(endpoint.Port)] = true
				}
			case "guest_host":
				if endpoint.Protocol == "statsd" {
					continue
				}
				guestTCP[int(endpoint.Port)] = true
			}
		}
	}
	if guestHostIP == "" {
		return hostFirewallConfig{}, fmt.Errorf("firecracker_host_service.host is required for guest host firewall rules")
	}

	return hostFirewallConfig{
		TrustedInterfaces: sortedStringSet(trustedInterfaces),
		PublicTCPPorts:    sortedIntSet(publicTCP),
		PublicUDPPorts:    sortedIntSet(publicUDP),
		GuestTCPPorts:     sortedIntSet(guestTCP),
		GuestCIDR:         loaded.Config.Firecracker.GuestPoolCIDR,
		GuestHostIP:       guestHostIP,
		SSHPublic:         nft.Ssh.Public,
		SSHRate:           nft.Ssh.Rate,
		SSHBurst:          int(nft.Ssh.Burst),
	}, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStringSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	if len(out) > 0 && out[0] != "lo" {
		for i, item := range out {
			if item == "lo" {
				copy(out[1:i+1], out[0:i])
				out[0] = "lo"
				break
			}
		}
	}
	return out
}

func sortedIntSet(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Ints(out)
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

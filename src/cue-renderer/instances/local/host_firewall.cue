package topology

import (
	"list"
	s "github.com/verself/cue-renderer/schema"
)

// Host firewall: the default-deny ingress chain that fronts every public
// service on the bare-metal node. All policy lives here so the renderer
// can stay a pure projection of typed rules into nftables syntax.
topology: nftables: host: s.#NftablesHostChain & {
	target: "/etc/nftables.d/host-firewall.nft"
	table:  "verself_host"
	policy: "drop"

	input: {
		accept_established_related: true
		drop_invalid:               true

		// WireGuard tunnel interfaces are trusted; loopback is always trusted
		// and must come first so an emergency `iifname "lo" accept` can never
		// be shadowed by a later rule.
		_trusted_wg_iifnames: list.Sort([
			for _, t in config.wireguard.tunnels {t.interface}
		], list.Ascending)

		// Endpoints reachable from Firecracker guest TAPs to the host service
		// plane. statsd is collected into the public UDP set instead.
		_guest_endpoints: [
			for cname, c in topology.components
			for ename, e in c.endpoints
			if e.exposure == "guest_host"
			if e.protocol != "statsd" {
				component: cname
				endpoint:  ename
			},
		]

		// Public-internet TCP endpoints. Caddy's 80/443 sockets are not
		// modeled as component endpoints; they live in
		// config.nftables.public_tcp_ports and are merged here.
		_public_tcp_endpoints: [
			for cname, c in topology.components
			for ename, e in c.endpoints
			if e.exposure == "public"
			if e.protocol != "statsd" {
				component: cname
				endpoint:  ename
			},
		]

		// Public-internet UDP. WireGuard listen ports + any public statsd
		// endpoints (none today, but the policy is "statsd is UDP").
		_wg_listen_ports: [for _, t in config.wireguard.tunnels {t.port}]
		_public_udp_endpoints: [
			for cname, c in topology.components
			for ename, e in c.endpoints
			if e.exposure == "public"
			if e.protocol == "statsd" {
				component: cname
				endpoint:  ename
			},
		]

		rules: list.Concat([
			[{kind: "accept_iifname", iifname: "lo"}],
			[for iface in _trusted_wg_iifnames {
				kind:    "accept_iifname"
				iifname: iface
			}],
			[for _ in [1] if len(_guest_endpoints) > 0 {
				kind:      "accept_guest_iifname_endpoints"
				iifname:   "fc-tap-*"
				saddr:     config.firecracker.guest_pool_cidr
				daddr:     topology.components.firecracker_host_service.host
				protocol:  "tcp"
				endpoints: _guest_endpoints
			}],
			[
				{kind: "accept_protocol_family", family: "icmp"},
				{kind: "accept_protocol_family", family: "icmpv6"},
			],
			[{
				kind:      "accept_port_set"
				protocol:  "tcp"
				ports:     config.nftables.public_tcp_ports
				endpoints: _public_tcp_endpoints
			}],
			[{
				kind:      "accept_port_set"
				protocol:  "udp"
				ports:     _wg_listen_ports
				endpoints: _public_udp_endpoints
			}],
			[for _ in [1] if config.nftables.ssh.public {
				kind:     "accept_rate_limited_port"
				protocol: "tcp"
				port:     22
				meter:    "ssh_rate"
				rate:     config.nftables.ssh.rate
				burst:    config.nftables.ssh.burst
			}],
		])
	}
}

package topology

import (
	"list"
	s "verself.sh/cue-renderer/schema"
)

// Host firewall: the default-deny ingress chain that fronts every public
// service on the bare-metal node. All policy lives here so the renderer
// can stay a pure projection of typed rules into nftables syntax.
// Firecracker guest networking: forward + NAT chains that gate
// every Firecracker guest's egress and block guest-to-guest lateral
// movement. The uplink interface is resolved by the firecracker
// Ansible role at deploy time and substituted into the rendered
// file via the __VERSELF_UPLINK__ placeholder before the nftables
// reload.
topology: nftables: firecracker: s.#NftablesFirecrackerChain & {
	target:     "/etc/nftables.d/firecracker.nft"
	table:      "verself_firecracker"
	guest_cidr: config.firecracker.guest_pool_cidr
	forward: [
		// Block guest-to-guest lateral movement first so the rest of
		// the chain only sees egress / return-traffic packets.
		{kind: "guest_to_guest_drop"},
		// Block outbound SMTP (port 25) from guests with a
		// rate-limited log + unconditional drop. nftables `limit` is
		// a *match*, not a log modifier — combining it with the drop
		// on one rule would let packets through past the rate cap.
		{
			kind:       "rate_limited_log_then_drop"
			protocol:   "tcp"
			port:       25
			log_prefix: "fc-smtp-block: "
			rate:       "10/minute"
		},
		// Guest outbound to internet via uplink.
		{kind: "guest_egress"},
		// Stateful return traffic from internet to guests.
		{kind: "return_traffic"},
		// Default-deny inbound to the guest pool.
		{kind: "catch_all_drop"},
	]
}

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

package topology

import (
	"list"
	s "guardianintelligence.org/forge-metal/topology/schema"
)

network: s.#Network & {
	wireguard: {
		interface: "wg0"
		port:      51820
		network:   "10.0.0.0/16"
	}

	firecracker: {
		guest_pool_cidr: "172.16.0.0/16"
		host_service_ip: topology.services.firecracker_host_service.host
	}

	nftables: {
		public_tcp_ports: [25, 80, 443]
		public_udp_ports: [network.wireguard.port]
		trusted_interfaces: ["lo", network.wireguard.interface]

		firecracker_guest_cidr:      network.firecracker.guest_pool_cidr
		firecracker_host_service_ip: network.firecracker.host_service_ip
		firecracker_guest_tcp_ports: [
			topology.services.verdaccio.port,
			topology.services.firecracker_host_service.http_port,
		]

		ssh_public:                true
		ssh_rate:                  "3/minute"
		ssh_burst:                 5
		remove_legacy_tables:      true
		legacy_table_name_pattern: "^forge_metal_"
	}
}

_uniquePublicTCPPorts:           true & list.UniqueItems(network.nftables.public_tcp_ports)
_uniquePublicUDPPorts:           true & list.UniqueItems(network.nftables.public_udp_ports)
_uniqueTrustedInterfaces:        true & list.UniqueItems(network.nftables.trusted_interfaces)
_uniqueFirecrackerGuestTCPPorts: true & list.UniqueItems(network.nftables.firecracker_guest_tcp_ports)

ansible: {
	wireguard_interface: network.wireguard.interface
	wireguard_port:      network.wireguard.port
	wireguard_network:   network.wireguard.network

	firecracker_guest_pool_cidr: network.firecracker.guest_pool_cidr
	firecracker_host_service_ip: network.firecracker.host_service_ip

	nftables_public_tcp_ports:            network.nftables.public_tcp_ports
	nftables_public_udp_ports:            network.nftables.public_udp_ports
	nftables_trusted_interfaces:          network.nftables.trusted_interfaces
	nftables_firecracker_guest_cidr:      network.nftables.firecracker_guest_cidr
	nftables_firecracker_host_service_ip: network.nftables.firecracker_host_service_ip
	nftables_firecracker_guest_tcp_ports: network.nftables.firecracker_guest_tcp_ports
	nftables_ssh_public:                  network.nftables.ssh_public
	nftables_ssh_rate:                    network.nftables.ssh_rate
	nftables_ssh_burst:                   network.nftables.ssh_burst
	nftables_remove_legacy_tables:        network.nftables.remove_legacy_tables
	nftables_legacy_table_name_pattern:   network.nftables.legacy_table_name_pattern
}

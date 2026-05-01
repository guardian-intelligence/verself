package topology

import s "verself.sh/cue-renderer/schema"

// config is the operator-facing, non-secret instance configuration for the
// single-node deployment. Topology describes components and relationships;
// this file describes the values an operator expects to tune directly.
//
// The file has two layers:
//
//   1. Typed sections (wireguard, postgres, nftables, firecracker, spire)
//      that Go renderers consume directly via loaded.Config.<Section>.
//      Schema validates them at evaluation time.
//
//   2. ansible_vars: the explicit Ansible-vars surface. Every key here
//      becomes a top-level group_vars/all entry. Cross-references into
//      typed sections use CUE values, not Jinja strings, so a typo is a
//      CUE evaluation error instead of an Ansible runtime surprise.
config: s.#InstanceConfig & {
	wireguard: {
		tunnels: {
			worker: {
				interface:      "wg0"
				port:           51820
				network:        "10.0.0.0/16"
				address:        "10.0.0.1"
				address_prefix: 16
				peers: []
			}
			ops: {
				interface:      "wg-ops"
				port:           51821
				network:        "10.66.66.0/24"
				address:        "10.66.66.1"
				address_prefix: 24
				peers: [
					{
						public_key:  "AoVgh4aWFK5Gi7HBdqIzTea37aa5SaemU4Pyk92Nglc="
						allowed_ips: "10.66.66.2/32"
					},
				]
			}
		}
		host_groups: {
			workers: ["worker"]
			infra: ["worker", "ops"]
		}
	}

	postgres: {
		max_connections:                300
		superuser_reserved_connections: 10
	}

	nftables: {
		// Caddy owns the public HTTP/TLS sockets; direct public component
		// endpoints such as SMTP are added from topology by the renderer.
		public_tcp_ports: [80, 443]
		// Public :22 is closed. SSH is reachable only via wg-ops; the
		// sshd ListenAddress is bound to 10.66.66.1, and the wg-ops
		// trusted-iifname rule in the host-firewall chain accepts that
		// traffic. Operators authenticate with OpenBao-issued
		// certificates only — see ssh_ca above. The rate/burst values
		// are kept as historical record but unused while public=false.
		ssh: {
			public: false
			rate:   "3/minute"
			burst:  5
		}
	}

	firecracker: {
		guest_pool_cidr: "172.16.0.0/16"
		// Composable image zvols seeded by vm-orchestrator-cli at deploy
		// time. The list is consumed by the firecracker Ansible role to
		// template the vm-orchestrator-seed oneshot unit; entries are
		// ordered substrate → platform_toolchain → customer_uploaded so
		// dependents land after their bases. Sizes are bytes because the
		// gRPC field is uint64 and the daemon validates >0. See
		// <guest_rootfs_direction> in AGENTS.md for the substrate/
		// toolchain split this catalog is built for.
		images: [
			{
				ref:         "substrate"
				tier:        "substrate"
				size_bytes:  2147483648 // 2 GiB; substrate.ext4 itself is ~500 MiB, the zvol headroom covers package additions without re-volsizing every clone.
				strategy:    "dd_from_file"
				source_path: "/var/lib/verself/guest-images/substrate.ext4"
			},
			{
				ref:         "gh-actions-runner"
				tier:        "platform_toolchain"
				size_bytes:  1073741824 // 1 GiB; the toolchain ext4 itself is ~700 MiB.
				strategy:    "dd_from_file"
				source_path: "/var/lib/verself/guest-images/toolchains/gh-actions-runner.ext4"
			},
			{
				ref:         "forgejo-runner"
				tier:        "platform_toolchain"
				size_bytes:  268435456 // 256 MiB; the toolchain ext4 itself is ~37 MiB.
				strategy:    "dd_from_file"
				source_path: "/var/lib/verself/guest-images/toolchains/forgejo-runner.ext4"
			},
			{
				ref:              "sticky-empty"
				tier:             "platform_toolchain"
				size_bytes:       8589934592 // 8 GiB
				strategy:         "mkfs_ext4"
				filesystem_label: "stickydisk"
			},
		]
	}

	spire: {
		trust_domain:                 "spiffe.\(config.ansible_vars.verself_domain)"
		server_bind_address:          "127.0.0.1"
		server_socket_path:           "/run/spire-server/private/api.sock"
		agent_socket_path:            "/run/spire-agent/sockets/agent.sock"
		workload_group:               "spire_workload"
		agent_id_path:                "/node/single-node"
		bundle_endpoint_bind_address: "127.0.0.1"
	}

	ssh_ca: {
		ca_name:         "verself-ssh-ca"
		mount:           "ssh-ca"
		default_user:    "ubuntu"
		ca_pubkey_path:  "/etc/ssh/verself-ssh-ca.pub"
		principals_file: "/etc/ssh/principals/ubuntu"

		oidc: {
			discovery_url: "https://auth.\(config.ansible_vars.verself_domain)"
			project_name:  "verself-ssh-ca"
			// Three callback ports so a stuck listener on 8250 doesn't
			// require operator intervention; bao tries them in order.
			allowed_redirect_uris: [
				"http://localhost:8250/oidc/callback",
				"http://localhost:8251/oidc/callback",
				"http://localhost:8252/oidc/callback",
			]
			operator_project_role: "platform-operator"
		}

		principals: {
			operator: {
				name: "operator"
				role: "operator"
				// 1h matches a normal interactive work block; longer than
				// 15min so a deploy or a debug session doesn't re-auth
				// through Zitadel mid-flight, shorter than the breakglass
				// window so a leaked cert can't outlive a shift. The
				// Vault token TTL matches, so one OIDC login covers up to
				// 1h of cert re-signing.
				max_ttl_seconds:      3600
				source_address_cidrs: ["10.66.66.0/24"]
				permit_pty:           true
			}
			breakglass: {
				name:                 "breakglass"
				role:                 "breakglass"
				max_ttl_seconds:      86400 // 24 hours
				source_address_cidrs: ["10.66.66.0/24", "0.0.0.0/0"]
				permit_pty:           true
			}
			canary: {
				name:                 "canary"
				role:                 "automation"
				max_ttl_seconds:      60
				source_address_cidrs: ["127.0.0.1/32", "10.66.66.0/24"]
				force_command:        "/bin/true"
				permit_pty:           false
			}
		}
	}

	let nomadArtifactHost = "artifacts.internal.\(config.ansible_vars.verself_domain)"

	artifacts: {
		nomad: {
			kind: "garage_s3_private_origin"
			storage: {
				provider:   "garage"
				bucket:     "nomad-artifacts"
				key_prefix: "sha256"
				region:     "garage"
			}
			origin: {
				scheme:         "https"
				hostname:       nomadArtifactHost
				port:           9443
				placement:      "node_local"
				resolution:     "per_node_hosts_file"
				listen_host:    "127.0.0.1"
				public_dns:     false
				public_ingress: false
				tls: {
					server_name:    nomadArtifactHost
					ca_bundle_path: "/etc/verself/pki/nomad-artifacts-ca.pem"
				}
			}
			nomad_getter: {
				protocol:           "s3"
				source_prefix:      "s3::https://\(nomadArtifactHost):9443/nomad-artifacts"
				checksum_algorithm: "sha256"
				options: {
					region: "garage"
				}
				credentials: {
					source:                "host_environment"
					environment_file:      "/etc/nomad/nomad-artifacts.env"
					access_key_id_env:     "AWS_ACCESS_KEY_ID"
					secret_access_key_env: "AWS_SECRET_ACCESS_KEY"
				}
			}
			publisher: {
				credentials: {
					source:                "controller_environment"
					environment_file:      "/etc/garage/nomad-artifacts/publisher.env"
					access_key_id_env:     "VERSELF_NOMAD_ARTIFACTS_AWS_ACCESS_KEY_ID"
					secret_access_key_env: "VERSELF_NOMAD_ARTIFACTS_AWS_SECRET_ACCESS_KEY"
				}
			}
		}
	}

	ansible_vars: {
		verself_version: "0.1.0"
		verself_bin:     "/opt/verself/profile/bin"

		// Public domains, organization labels, and per-site sender
		// addresses are split into site.cue. Both files contribute to
		// `config.ansible_vars` via CUE unification.

		// Object-storage UIDs are also CUE-side identifiers used by
		// firewall and service runtime facts; reference them via
		// config.ansible_vars rather than redeclaring per call site.
		object_storage_service_uid: 960
		object_storage_admin_uid:   961

		// Wireguard projects as a single nested var because the Ansible
		// role iterates over the structured tunnels/peers.
		topology_wireguard: config.wireguard

		// SSH CA projects through to the openbao + ssh_ca Ansible roles.
		// Both consume the principal catalog directly: openbao to mint one
		// OpenBao SSH role per principal, ssh_ca to render the on-host
		// principals file and the sshd_config drop-in. Single source of
		// truth, schema-validated.
		topology_ssh_ca: config.ssh_ca

		// Firecracker image-seeding inputs consumed by the firecracker
		// role's vm-orchestrator-seed oneshot unit.
		firecracker_guest_pool_cidr: config.firecracker.guest_pool_cidr
		firecracker_seed_images:     config.firecracker.images

		// OpenBao tenancy configuration consumed by the openbao Ansible
		// role and the secrets-service relying-party scaffolding.
		openbao_spiffe_jwt_mount:              "spiffe-jwt"
		openbao_workload_audience:             "openbao"
		openbao_tenancy_credstore_dir:         "/etc/credstore/openbao"
		openbao_tenancy_tls_dir:               "/etc/openbao/tls"
		openbao_tenancy_spiffe_jwt_mount:      "spiffe-jwt"
		openbao_tenancy_rebootstrap_stage_dir: "{{ openbao_tenancy_credstore_dir }}/rebootstrap"
		openbao_tenancy_oidc_discovery_url:    "https://auth.{{ verself_domain }}"
		openbao_tenancy_bound_issuer:          "https://auth.{{ verself_domain }}"
		openbao_tenancy_secrets_project_name:  "secrets-service"
		openbao_tenancy_token_ttl:             "15m"
		openbao_tenancy_token_max_ttl:         "1h"
		openbao_tenancy_layout:                "auto"
		openbao_tenancy_skip_jwks_validation:  false
		openbao_tenancy_kv_mount_prefix:       "kv"
		openbao_tenancy_transit_mount_prefix:  "transit"
		openbao_tenancy_jwt_mount_prefix:      "jwt"

		// Temporal namespace + web vars consumed by the temporal role.
		temporal_sandbox_namespace:   "sandbox-rental-service"
		temporal_billing_namespace:   "billing-service"
		temporal_namespace_retention: "24h"
		temporal_bootstrap_namespaces: ["{{ temporal_sandbox_namespace }}", "{{ temporal_billing_namespace }}"]
		temporal_namespace_role_bindings: [
			{spiffe_id: "{{ spire_sandbox_rental_id }}", namespace: "{{ temporal_sandbox_namespace }}", role: "admin"},
			{spiffe_id: "{{ spire_billing_service_id }}", namespace: "{{ temporal_billing_namespace }}", role: "admin"},
		]
	}
}

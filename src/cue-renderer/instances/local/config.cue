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
		ssh: {
			public: true
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
				ref:         "golden"
				tier:        "substrate"
				size_bytes:  8589934592 // 8 GiB
				strategy:    "dd_from_file"
				source_path: "/var/lib/verself/guest-artifacts/rootfs.ext4"
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

	ansible_vars: {
		verself_version: "0.1.0"
		verself_bin:     "/opt/verself/profile/bin"

		// Domains are emitted as Ansible vars because Caddy, Zitadel,
		// Resend, seed-system, and browser-facing services all consume
		// the same public names. Inner Jinja templates resolve at Ansible
		// runtime against these top-level vars.
		verself_domain:  "verself.sh"
		platform_domain: "{{ verself_domain }}"
		company_domain:  "guardianintelligence.org"

		console_subdomain: "console"
		console_domain:    "{{ console_subdomain }}.{{ verself_domain }}"

		billing_service_subdomain: "billing.api"
		billing_service_domain:    "{{ billing_service_subdomain }}.{{ verself_domain }}"

		sandbox_rental_service_subdomain: "sandbox.api"
		sandbox_rental_service_domain:    "{{ sandbox_rental_service_subdomain }}.{{ verself_domain }}"

		identity_service_subdomain: "identity.api"
		identity_service_domain:    "{{ identity_service_subdomain }}.{{ verself_domain }}"

		profile_service_subdomain: "profile.api"
		profile_service_domain:    "{{ profile_service_subdomain }}.{{ verself_domain }}"

		notifications_service_subdomain: "notifications.api"
		notifications_service_domain:    "{{ notifications_service_subdomain }}.{{ verself_domain }}"

		projects_service_subdomain: "projects.api"
		projects_service_domain:    "{{ projects_service_subdomain }}.{{ verself_domain }}"

		source_code_hosting_service_subdomain: "source.api"
		source_code_hosting_service_domain:    "{{ source_code_hosting_service_subdomain }}.{{ verself_domain }}"

		governance_service_subdomain: "governance.api"
		governance_service_domain:    "{{ governance_service_subdomain }}.{{ verself_domain }}"

		secrets_service_subdomain: "secrets.api"
		secrets_service_domain:    "{{ secrets_service_subdomain }}.{{ verself_domain }}"

		mailbox_service_subdomain: "mail.api"
		mailbox_service_domain:    "{{ mailbox_service_subdomain }}.{{ verself_domain }}"

		forgejo_subdomain: "git"
		forgejo_domain:    "{{ forgejo_subdomain }}.{{ verself_domain }}"

		zitadel_subdomain: "auth"
		zitadel_domain:    "{{ zitadel_subdomain }}.{{ verself_domain }}"

		resend_subdomain:      "notify"
		resend_domain:         "{{ resend_subdomain }}.{{ verself_domain }}"
		resend_sender_address: "noreply@{{ resend_domain }}"
		resend_sender_name:    "verself"

		stalwart_subdomain: "mail"
		stalwart_domain:    "{{ stalwart_subdomain }}.{{ verself_domain }}"

		// Object-storage UIDs are also CUE-side identifiers used by
		// firewall and convergence rules; reference them via
		// config.ansible_vars rather than redeclaring per call site.
		object_storage_service_uid: 960
		object_storage_admin_uid:   961

		// Wireguard projects as a single nested var because the Ansible
		// role iterates over the structured tunnels/peers.
		topology_wireguard: config.wireguard

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
		openbao_tenancy_platform_org_name:     "Guardian Intelligence LLC"

		// Temporal namespace + web vars consumed by the temporal role.
		temporal_sandbox_namespace:   "sandbox-rental-service"
		temporal_billing_namespace:   "billing-service"
		temporal_namespace_retention: "24h"
		temporal_bootstrap_namespaces: ["{{ temporal_sandbox_namespace }}", "{{ temporal_billing_namespace }}"]
		temporal_namespace_role_bindings: [
			{spiffe_id: "{{ spire_sandbox_rental_id }}", namespace: "{{ temporal_sandbox_namespace }}", role: "admin"},
			{spiffe_id: "{{ spire_billing_service_id }}", namespace: "{{ temporal_billing_namespace }}", role: "admin"},
		]
		temporal_web_subdomain:             "temporal"
		temporal_web_domain:                "{{ temporal_web_subdomain }}.{{ verself_domain }}"
		temporal_web_default_namespace:     "{{ temporal_sandbox_namespace }}"
		temporal_web_disable_write_actions: true
		temporal_web_oidc_scopes: ["openid", "profile", "email", "offline_access"]
		temporal_web_oidc_max_session_duration:     "8h"
		temporal_web_auth_project_name:             "temporal-web"
		temporal_web_auth_app_name:                 "temporal-web"
		temporal_web_oidc_redirect_uri:             "https://{{ temporal_web_domain }}/auth/sso/callback"
		temporal_web_oidc_post_logout_redirect_uri: "https://{{ temporal_web_domain }}/"

		// seed-system identity + billing fixtures used by the seed-system
		// playbook. Deliberately authored as flat keys because Ansible
		// templates them via `{{ seed_system_* }}` directly.
		seed_system_zitadel_base_url: "http://{{ topology_endpoints.zitadel.endpoints.http.address }}"
		seed_system_zitadel_host:     "auth.{{ verself_domain }}"
		seed_system_openbao_tls_dir:  "/etc/openbao/tls"
		seed_system_password_overrides: {}
		seed_system_credstore_dir:         "/etc/credstore/seed-system"
		seed_system_platform_org_name:     "Guardian Intelligence LLC"
		seed_system_platform_org_slug:     "guardian-platform"
		seed_system_acme_org_name:         "Acme Corp"
		seed_system_acme_org_slug:         "acme-corp"
		seed_system_sandbox_project_name:  "sandbox-rental"
		seed_system_identity_project_name: "identity-service"
		seed_system_secrets_project_name:  "secrets-service"
		seed_system_forgejo_project_name:  "forgejo"
		seed_system_mailbox_project_name:  "mailbox-service"
		seed_system_users: {
			ceo: {key:             "ceo", org_key:             "platform", email: "ceo@{{ verself_domain }}", username:        "ceo", first_name:        "CEO", last_name:        "Operator", password_credstore_path: "{{ seed_system_credstore_dir }}/ceo-password"}
			platform_agent: {key:  "platform_agent", org_key:  "platform", email: "agent@{{ verself_domain }}", username:      "agent", first_name:      "Platform", last_name:    "Agent", password_credstore_path: "{{ seed_system_credstore_dir }}/platform-agent-password"}
			acme_admin: {key:      "acme_admin", org_key:      "acme", email:     "acme-admin@{{ verself_domain }}", username: "acme-admin", first_name: "Acme", last_name:        "Admin", password_credstore_path: "{{ seed_system_credstore_dir }}/acme-admin-password"}
			acme_user: {key:       "acme_user", org_key:       "acme", email:     "acme-user@{{ verself_domain }}", username:  "acme-user", first_name:  "Acme", last_name:        "User", password_credstore_path:  "{{ seed_system_credstore_dir }}/acme-user-password"}
		}
		seed_system_machine_users: {
			platform_admin: {key: "platform_admin", org_key: "platform", username: "assume-platform-admin", name: "Assume Platform Admin", secret_credstore_path: "{{ seed_system_credstore_dir }}/assume-platform-admin-client-secret"}
			acme_admin: {key:     "acme_admin", org_key:     "acme", username:     "assume-acme-admin", name:     "Assume Acme Admin", secret_credstore_path:     "{{ seed_system_credstore_dir }}/assume-acme-admin-client-secret"}
			acme_member: {key:    "acme_member", org_key:    "acme", username:     "assume-acme-member", name:    "Assume Acme Member", secret_credstore_path:    "{{ seed_system_credstore_dir }}/assume-acme-member-client-secret"}
		}
		seed_system_product_id:           "sandbox"
		seed_system_product_display_name: "Sandbox"
		seed_system_meter_unit:           "sku_ms"
		seed_system_billing_model:        "metered"
		seed_system_plan_id:              "sandbox-default"
		seed_system_plan_display_name:    "Sandbox PAYG"
		seed_system_free_tier_buckets: {compute: 10000000, memory: 5000000, execution_root_storage: 2000000}
		seed_system_plan_entitlements: {}
		seed_system_contract_tiers: [
			{plan_id: "sandbox-hobby", display_name: "Hobby", tier: "hobby", currency: "usd", cadence: "monthly", unit_amount_cents: 500, entitlements: {compute: 30000000, memory: 15000000, execution_root_storage: 5000000}},
			{plan_id: "sandbox-pro", display_name:   "Pro", tier:   "pro", currency:   "usd", cadence:   "monthly", unit_amount_cents: 2000, entitlements: {compute: 120000000, memory: 60000000, execution_root_storage: 20000000}},
		]
		seed_system_platform_target_prepaid_units: 500000000000
		seed_system_customer_target_prepaid_units: 500000000000
		seed_system_expires_after:                 "8760h"
		seed_system_acme_sku_scoped_grants: [{sku_id: "sandbox_execution_root_storage_premium_nvme_gib_ms", units: 50000000, source: "promo"}]
		seed_system_helper_local_path:                 "/tmp/billing-seed-{{ inventory_hostname }}"
		seed_system_helper_remote_path:                "/opt/verself/staging/billing-seed-{{ inventory_hostname }}"
		seed_system_stripe_catalog_helper_local_path:  "/tmp/billing-stripe-catalog-{{ inventory_hostname }}"
		seed_system_stripe_catalog_helper_remote_path: "/opt/verself/staging/billing-stripe-catalog-{{ inventory_hostname }}"
		seed_system_stalwart_disabled_permissions: ["email-send", "jmap-sieve-script-set", "jmap-sieve-script-validate", "sieve-put-script", "sieve-set-active"]
	}
}

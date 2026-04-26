package topology

import (
	"list"

	cat "guardianintelligence.org/forge-metal/topology/catalog"
	s "guardianintelligence.org/forge-metal/topology/schema"
)

#ServiceProbes: {
	healthz: {path: "/healthz"}
	readyz: {path: "/readyz"}
	...
}

#PublicGoService: {
	kind: "service"
	host: "127.0.0.1"
	artifact: {
		kind: "go_binary"
		...
	}
	endpoints: {
		public_http: {
			protocol: "http"
			exposure: "loopback"
			...
		}
		...
	}
	interfaces: {
		public_api: {
			kind:        "huma_api"
			endpoint:    "public_http"
			path_prefix: "/api/v1"
			auth:        "zitadel_jwt"
			probes:      #ServiceProbes
			...
		}
		...
	}
	probes: #ServiceProbes
	...
}

#InternalGoAPI: {
	endpoints: {
		internal_https: {
			protocol: "https"
			exposure: "loopback"
			...
		}
		...
	}
	interfaces: {
		internal_api: {
			kind:        "huma_api"
			endpoint:    "internal_https"
			path_prefix: "/internal"
			auth:        "spiffe_mtls"
			probes:      #ServiceProbes
			...
		}
		...
	}
	...
}

#Frontend: {
	kind: "frontend"
	host: "127.0.0.1"
	artifact: {
		kind: "node_app"
		...
	}
	endpoints: {
		http: {
			protocol: "http"
			exposure: "loopback"
			...
		}
		...
	}
	interfaces: {
		frontend: {
			kind:     "frontend_http"
			endpoint: "http"
			auth:     "none"
			probes:   #ServiceProbes
			...
		}
		...
	}
	probes: #ServiceProbes
	...
}

#DefaultSPIFFEIdentity: {
	runtime: {
		user:  string & !=""
		group: string & !=""
		...
	}
	identities: default: {
		path:        string & =~"^/"
		ansible_var: string & !=""
		entry_id:    string & !=""
		user:        runtime.user
		group:       runtime.group
		restart_units: [...string] | *[runtime.systemd]
		...
	}
	...
}

_config: {
	verself_version: "0.1.0"
	verself_bin:     "/opt/verself/profile/bin"

	domains: {
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

		resend_subdomain:      "notify"
		resend_domain:         "{{ resend_subdomain }}.{{ verself_domain }}"
		resend_sender_address: "noreply@{{ resend_domain }}"
		resend_sender_name:    "verself"

		stalwart_subdomain: "mail"
		stalwart_domain:    "{{ stalwart_subdomain }}.{{ verself_domain }}"
	}

	openbao: {
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
		openbao_tenancy_platform_org_name:     "verself"
	}

	wireguard: {
		wireguard_interface: "wg0"
		wireguard_port:      51820
		wireguard_network:   "10.0.0.0/16"
		wireguard_address:   "10.0.0.1"
	}

	object_storage: {
		object_storage_service_uid: 960
		object_storage_admin_uid:   961
	}

	retired_product_runtimes: [
		{unit: "letters.service", user: "letters", group: "letters", paths: ["/etc/letters", "/opt/letters", "/var/lib/letters"]},
		{unit: "webmail.service", user: "webmail", group: "webmail", paths: ["/etc/webmail", "/opt/webmail", "/var/lib/webmail"]},
	]

	postgres: {
		max_connections:                300
		superuser_reserved_connections: 10
	}

	temporal: {
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
	}

	seed_system: {
		seed_system_zitadel_base_url: "http://{{ topology_endpoints.zitadel.endpoints.http.address }}"
		seed_system_zitadel_host:     "auth.{{ verself_domain }}"
		seed_system_openbao_tls_dir:  "/etc/openbao/tls"
		seed_system_password_overrides: {}
		seed_system_credstore_dir:         "/etc/credstore/seed-system"
		seed_system_platform_org_name:     "verself"
		seed_system_acme_org_name:         "Acme Corp"
		seed_system_sandbox_project_name:  "sandbox-rental"
		seed_system_identity_project_name: "identity-service"
		seed_system_secrets_project_name:  "secrets-service"
		seed_system_forgejo_project_name:  "forgejo"
		seed_system_mailbox_project_name:  "mailbox-service"
		seed_system_users: {
			ceo: {key: "ceo", org_key: "platform", email: "ceo@{{ verself_domain }}", username: "ceo", first_name: "CEO", last_name: "Operator", password_credstore_path: "{{ seed_system_credstore_dir }}/ceo-password"}
			platform_agent: {key: "platform_agent", org_key: "platform", email: "agent@{{ verself_domain }}", username: "agent", first_name: "Platform", last_name: "Agent", password_credstore_path: "{{ seed_system_credstore_dir }}/platform-agent-password"}
			acme_admin: {key: "acme_admin", org_key: "acme", email: "acme-admin@{{ verself_domain }}", username: "acme-admin", first_name: "Acme", last_name: "Admin", password_credstore_path: "{{ seed_system_credstore_dir }}/acme-admin-password"}
			acme_user: {key: "acme_user", org_key: "acme", email: "acme-user@{{ verself_domain }}", username: "acme-user", first_name: "Acme", last_name: "User", password_credstore_path: "{{ seed_system_credstore_dir }}/acme-user-password"}
		}
		seed_system_machine_users: {
			platform_admin: {key: "platform_admin", org_key: "platform", username: "assume-platform-admin", name: "Assume Platform Admin", secret_credstore_path: "{{ seed_system_credstore_dir }}/assume-platform-admin-client-secret"}
			acme_admin: {key: "acme_admin", org_key: "acme", username: "assume-acme-admin", name: "Assume Acme Admin", secret_credstore_path: "{{ seed_system_credstore_dir }}/assume-acme-admin-client-secret"}
			acme_member: {key: "acme_member", org_key: "acme", username: "assume-acme-member", name: "Assume Acme Member", secret_credstore_path: "{{ seed_system_credstore_dir }}/assume-acme-member-client-secret"}
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
			{plan_id: "sandbox-pro", display_name: "Pro", tier: "pro", currency: "usd", cadence: "monthly", unit_amount_cents: 2000, entitlements: {compute: 120000000, memory: 60000000, execution_root_storage: 20000000}},
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

topology: s.#Topology & {
	gateways: {
		public_caddy: {
			kind: "caddy"
			host: "0.0.0.0"
		}
		direct_smtp: {
			kind: "direct"
			host: "0.0.0.0"
		}
		firecracker_host: {
			kind: "firecracker_host"
			host: "10.255.0.1"
		}
	}

	components: {
		clickhouse: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {
				systemd: "clickhouse-server"
				user:    "clickhouse"
				group:   "clickhouse"
			}
			identities: {
				server: {ansible_var: "spire_clickhouse_server_id", path: "/svc/clickhouse-server", user: "clickhouse", group: "clickhouse", entry_id: "verself-clickhouse-server", restart_units: ["clickhouse-server-spiffe-helper"]}
				operator: {ansible_var: "spire_clickhouse_operator_id", path: "/svc/clickhouse-operator", user: "clickhouse_operator", group: "clickhouse_operator", entry_id: "verself-clickhouse-operator", restart_units: ["clickhouse-operator-spiffe-helper"]}
			}
			artifact: {kind: "upstream_binary", output: "clickhouse-server", role: "clickhouse"}
			endpoints: native_tls: {
				protocol: "clickhouse_native"
				port:     9440
				exposure: "loopback"
			}
			interfaces: native: {
				kind:     "resource_protocol"
				endpoint: "native_tls"
				auth:     "spiffe_mtls"
			}
		}
		otelcol: #DefaultSPIFFEIdentity & {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "otelcol", user: "otelcol", group: "otelcol"}
			identities: default: {ansible_var: "spire_otelcol_id", path: "/svc/otelcol", entry_id: "verself-otelcol", restart_units: ["otelcol-clickhouse-spiffe-helper", "otelcol"]}
			artifact: {kind: "upstream_binary", output: "otelcol", role: "otelcol"}
			endpoints: {
				otlp_grpc: {
					protocol: "grpc"
					port:     4317
					exposure: "loopback"
				}
				otlp_http: {
					protocol: "http"
					port:     4318
					exposure: "loopback"
				}
				statsd: {
					protocol: "statsd"
					port:     8125
					exposure: "loopback"
				}
			}
			interfaces: {
				otlp_grpc: {kind: "resource_protocol", endpoint: "otlp_grpc", auth: "none"}
				otlp_http: {kind: "resource_protocol", endpoint: "otlp_http", auth: "none"}
				statsd: {kind: "metrics", endpoint: "statsd", auth: "none"}
			}
		}
		forgejo: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "forgejo", user: "forgejo", group: "forgejo"}
			artifact: {kind: "static_binary", output: "forgejo", role: "forgejo"}
			endpoints: http: {
				protocol: "http"
				port:     3000
				exposure: "loopback"
			}
			interfaces: forgejo_http: {
				kind:     "protocol"
				endpoint: "http"
				auth:     "operator"
				probes:   #ServiceProbes
			}
		}
		grafana: #DefaultSPIFFEIdentity & {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "grafana", user: "grafana", group: "grafana"}
			identities: default: {ansible_var: "spire_grafana_id", path: "/svc/grafana", entry_id: "verself-grafana", restart_units: ["grafana-clickhouse-spiffe-helper", "grafana"]}
			artifact: {kind: "upstream_binary", output: "grafana", role: "grafana"}
			endpoints: http: {
				protocol: "http"
				port:     4300
				exposure: "loopback"
			}
			interfaces: operator_ui: {kind: "frontend_http", endpoint: "http", auth: "operator"}
			postgres: {database: "grafana", owner: "grafana", connection_limit: 10}
		}
		temporal_web: #DefaultSPIFFEIdentity & {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "temporal-web", user: "temporal_web", group: "temporal_web"}
			identities: default: {ansible_var: "spire_temporal_web_id", path: "/svc/temporal-web", entry_id: "verself-temporal-web"}
			artifact: {kind: "go_binary", package: "./src/temporal-platform/cmd/verself-temporal-web", output: "verself-temporal-web", role: "temporal"}
			endpoints: http: {
				protocol: "http"
				port:     4301
				exposure: "loopback"
			}
			interfaces: operator_ui: {kind: "frontend_http", endpoint: "http", auth: "operator", probes: #ServiceProbes}
		}
		verdaccio: {
			kind:        "resource"
			host:        "127.0.0.1"
			listen_host: "0.0.0.0"
			runtime: {systemd: "verdaccio", user: "verdaccio", group: "verdaccio"}
			artifact: {kind: "upstream_binary", output: "verdaccio", role: "verdaccio"}
			endpoints: http: {
				protocol:    "http"
				listen_host: "0.0.0.0"
				port:        4873
				exposure:    "guest_host"
			}
			interfaces: registry: {kind: "resource_protocol", endpoint: "http", auth: "none"}
		}
		tigerbeetle: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "tigerbeetle", user: "tigerbeetle", group: "tigerbeetle"}
			artifact: {kind: "static_binary", output: "tigerbeetle", role: "tigerbeetle"}
			endpoints: client: {
				protocol: "tcp"
				port:     3320
				exposure: "loopback"
			}
			interfaces: ledger: {kind: "resource_protocol", endpoint: "client", auth: "none"}
		}
		postgresql: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "postgresql", user: "postgres", group: "postgres"}
			artifact: {kind: "upstream_binary", output: "postgres", role: "postgresql"}
			endpoints: postgres: {
				protocol: "tcp"
				port:     5432
				exposure: "loopback"
			}
			interfaces: sql: {kind: "resource_protocol", endpoint: "postgres", auth: "operator"}
		}
		nats: #DefaultSPIFFEIdentity & {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "nats", user: "nats", group: "nats"}
			identities: default: {ansible_var: "spire_nats_id", path: "/svc/nats", entry_id: "verself-nats", restart_units: ["nats-spiffe-helper", "nats"]}
			artifact: {kind: "static_binary", output: "nats-server", role: "nats"}
			endpoints: {
				client: {
					protocol: "tcp"
					port:     4222
					exposure: "loopback"
				}
				monitoring: {
					protocol: "http"
					port:     8222
					exposure: "loopback"
				}
			}
			interfaces: {
				client: {kind: "resource_protocol", endpoint: "client", auth: "spiffe_mtls"}
				monitoring: {kind: "metrics", endpoint: "monitoring", auth: "operator"}
			}
		}
		temporal: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "temporal-server", user: "temporal_server", group: "temporal_server"}
			identities: server: {ansible_var: "spire_temporal_server_id", path: "/svc/temporal-server", user: "temporal_server", group: "temporal_server", entry_id: "verself-temporal-server", restart_units: ["temporal-server"]}
			artifact: {kind: "go_binary", package: "./src/temporal-platform/cmd/verself-temporal-server", output: "verself-temporal-server", role: "temporal"}
			temporal: {
				frontend: {grpc_port: 7233, http_port: 7243, membership_port: 6933}
				internal_frontend: {grpc_port: 7236, http_port: 7246, membership_port: 6936}
				history: {grpc_port: 7234, membership_port: 6934}
				matching: {grpc_port: 7235, membership_port: 6935}
				worker: {grpc_port: 7239, membership_port: 6939}
				diagnostics: {metrics_port: 9001, pprof_port: 7936}
			}
			endpoints: {
				frontend_grpc: {protocol: "grpc", port: 7233, exposure: "loopback"}
				frontend_http: {protocol: "http", port: 7243, exposure: "loopback"}
				internal_frontend_grpc: {protocol: "grpc", port: 7236, exposure: "loopback"}
				internal_frontend_http: {protocol: "http", port: 7246, exposure: "loopback"}
				history_grpc: {protocol: "grpc", port: 7234, exposure: "loopback"}
				matching_grpc: {protocol: "grpc", port: 7235, exposure: "loopback"}
				frontend_membership: {protocol: "tcp", port: 6933, exposure: "loopback"}
				internal_membership: {protocol: "tcp", port: 6936, exposure: "loopback"}
				history_membership: {protocol: "tcp", port: 6934, exposure: "loopback"}
				matching_membership: {protocol: "tcp", port: 6935, exposure: "loopback"}
				worker_membership: {protocol: "tcp", port: 6939, exposure: "loopback"}
				metrics: {protocol: "http", port: 9001, exposure: "loopback"}
				pprof: {protocol: "http", port: 7936, exposure: "loopback"}
			}
			interfaces: {
				frontend: {kind: "resource_protocol", endpoint: "frontend_grpc", auth: "none"}
				metrics: {kind: "metrics", endpoint: "metrics", auth: "operator"}
			}
			postgres: {database: "temporal", owner: "temporal", connection_limit: 80}
		}
		billing: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/billing-service/cmd/billing-service", output: "billing-service", role: "billing_service"}
			runtime: {systemd: "billing-service", user: "billing", group: "billing"}
			identities: default: {ansible_var: "spire_billing_service_id", path: "/svc/billing-service", entry_id: "verself-billing-service"}
			endpoints: {
				public_http: port:    4242
				internal_https: port: 4255
			}
			postgres: {database: "billing", owner: "billing", connection_limit: 30}
		}
		sandbox_rental: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/sandbox-rental-service/cmd/sandbox-rental-service", output: "sandbox-rental-service", role: "sandbox_rental_service"}
			runtime: {systemd: "sandbox-rental-service", user: "sandbox_rental", group: "sandbox_rental"}
			identities: default: {ansible_var: "spire_sandbox_rental_id", path: "/svc/sandbox-rental-service", entry_id: "verself-sandbox-rental-service"}
			endpoints: {
				public_http: port:    4243
				internal_https: port: 4263
			}
			postgres: {database: "sandbox_rental", owner: "sandbox_rental", connection_limit: 30}
		}
		identity_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/identity-service/cmd/identity-service", output: "identity-service", role: "identity_service"}
			runtime: {systemd: "identity-service", user: "identity_service", group: "identity_service"}
			identities: default: {ansible_var: "spire_identity_service_id", path: "/svc/identity-service", entry_id: "verself-identity-service"}
			endpoints: {
				public_http: port:    4248
				internal_https: port: 4241
			}
			postgres: {database: "identity_service", owner: "identity_service", connection_limit: 10}
		}
		governance_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/governance-service/cmd/governance-service", output: "governance-service", role: "governance_service"}
			runtime: {systemd: "governance-service", user: "governance_service", group: "governance_service"}
			identities: default: {ansible_var: "spire_governance_service_id", path: "/svc/governance-service", entry_id: "verself-governance-service"}
			endpoints: {
				public_http: port:    4250
				internal_https: port: 4254
			}
			postgres: {database: "governance_service", owner: "governance_service", connection_limit: 15}
		}
		secrets_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/secrets-service/cmd/secrets-service", output: "secrets-service", role: "secrets_service"}
			runtime: {systemd: "secrets-service", user: "secrets_service", group: "secrets_service"}
			identities: default: {ansible_var: "spire_secrets_service_id", path: "/svc/secrets-service", entry_id: "verself-secrets-service"}
			endpoints: {
				public_http: port:    4251
				internal_https: port: 4253
			}
			postgres: {database: "secrets_service", owner: "secrets_service"}
		}
		profile_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/profile-service/cmd/profile-service", output: "profile-service", role: "profile_service"}
			runtime: {systemd: "profile-service", user: "profile_service", group: "profile_service"}
			identities: default: {ansible_var: "spire_profile_service_id", path: "/svc/profile-service", entry_id: "verself-profile-service"}
			endpoints: {
				public_http: port:    4258
				internal_https: port: 4259
			}
			postgres: {database: "profile_service", owner: "profile_service", connection_limit: 10}
		}
		notifications_service: #PublicGoService & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/notifications-service/cmd/notifications-service", output: "notifications-service", role: "notifications_service"}
			runtime: {systemd: "notifications-service", user: "notifications_service", group: "notifications_service"}
			identities: default: {ansible_var: "spire_notifications_service_id", path: "/svc/notifications-service", entry_id: "verself-notifications-service"}
			endpoints: public_http: port: 4260
			postgres: {database: "notifications_service", owner: "notifications_service", connection_limit: 10}
		}
		projects_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/projects-service/cmd/projects-service", output: "projects-service", role: "projects_service"}
			runtime: {systemd: "projects-service", user: "projects_service", group: "projects_service"}
			identities: default: {ansible_var: "spire_projects_service_id", path: "/svc/projects-service", entry_id: "verself-projects-service"}
			endpoints: {
				public_http: port:    4264
				internal_https: port: 4265
			}
			postgres: {database: "projects_service", owner: "projects_service", connection_limit: 10}
		}
		source_code_hosting_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/source-code-hosting-service/cmd/source-code-hosting-service", output: "source-code-hosting-service", role: "source_code_hosting_service"}
			runtime: {systemd: "source-code-hosting-service", user: "source_code_hosting_service", group: "source_code_hosting_service"}
			identities: default: {ansible_var: "spire_source_code_hosting_service_id", path: "/svc/source-code-hosting-service", entry_id: "verself-source-code-hosting-service"}
			endpoints: {
				public_http: port:    4261
				internal_https: port: 4262
			}
			interfaces: git_smart_http: {
				kind:        "protocol"
				endpoint:    "public_http"
				path_prefix: "/"
				auth:        "zitadel_jwt"
			}
			postgres: {database: "source_code_hosting_service", owner: "source_code_hosting_service", connection_limit: 10}
		}
		console: #Frontend & {
			artifact: {package: "src/viteplus-monorepo/apps/console", output: "console", role: "console"}
			runtime: {systemd: "console", user: "console", group: "console"}
			endpoints: http: port: 4244
			postgres: {database: "frontend_auth", owner: "frontend_auth", connection_limit: 15}
		}
		electric: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "electric", user: "electric", group: "electric"}
			artifact: {kind: "upstream_binary", output: "electric", role: "electric"}
			endpoints: http: {protocol: "http", port: 3010, exposure: "loopback"}
			interfaces: shape_api: {kind: "resource_protocol", endpoint: "http", auth: "shared_secret"}
			electric: {
				instance:         "default"
				pg_role:          "electric"
				pg_conn_limit:    25
				source_database:  "sandbox_rental"
				writer_role:      "sandbox_rental"
				publication_name: "electric_publication_default"
				publication_tables: ["executions", "execution_logs"]
				storage_dir:    "/var/lib/electric"
				credstore_dir:  "/etc/credstore/electric"
				nftables_table: "verself_electric"
				nftables_file:  "/etc/nftables.d/electric.nft"
				db_pool_size:   15
			}
		}
		zitadel: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "zitadel", user: "zitadel", group: "zitadel"}
			artifact: {kind: "static_binary", output: "zitadel", role: "zitadel"}
			endpoints: http: {protocol: "http", port: 8085, exposure: "loopback"}
			interfaces: oidc: {kind: "protocol", endpoint: "http", auth: "none", probes: #ServiceProbes}
			postgres: {database: "zitadel", owner: "zitadel", connection_limit: 15}
		}
		openbao: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "openbao", user: "openbao", group: "openbao"}
			artifact: {kind: "static_binary", output: "bao", role: "openbao"}
			endpoints: {
				api: {protocol: "https", port: 8200, exposure: "loopback"}
				cluster: {protocol: "tcp", port: 8201, exposure: "loopback"}
			}
			interfaces: api: {kind: "resource_protocol", endpoint: "api", auth: "spiffe_mtls"}
		}
		spire_server: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "spire-server", user: "spire", group: "spire"}
			artifact: {kind: "static_binary", output: "spire-server", role: "spire"}
			endpoints: api: {protocol: "tcp", port: 8081, exposure: "loopback"}
			interfaces: api: {kind: "resource_protocol", endpoint: "api", auth: "operator"}
		}
		spire_bundle_endpoint: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "spire-server", user: "spire", group: "spire"}
			artifact: {kind: "static_binary", output: "spire-server", role: "spire"}
			endpoints: bundle: {protocol: "http", port: 8082, exposure: "loopback"}
			interfaces: bundle: {kind: "resource_protocol", endpoint: "bundle", auth: "none"}
		}
		stalwart: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "stalwart", user: "stalwart", group: "stalwart"}
			artifact: {kind: "static_binary", output: "stalwart-mail", role: "stalwart"}
			endpoints: {
				smtp: {protocol: "smtp", listen_host: "0.0.0.0", port: 25, exposure: "public"}
				http: {protocol: "http", port: 8090, exposure: "loopback"}
			}
			interfaces: {
				smtp: {kind: "protocol", endpoint: "smtp", auth: "none"}
				jmap: {kind: "protocol", endpoint: "http", auth: "zitadel_jwt"}
				admin: {kind: "admin_api", endpoint: "http", auth: "operator"}
			}
			postgres: {database: "stalwart", owner: "stalwart", connection_limit: 10}
		}
		mailbox_service: #PublicGoService & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/mailbox-service/cmd/mailbox-service", output: "mailbox-service", role: "mailbox_service"}
			runtime: {systemd: "mailbox-service", user: "mailbox_service", group: "mailbox_service"}
			identities: default: {ansible_var: "spire_mailbox_service_id", path: "/svc/mailbox-service", entry_id: "verself-mailbox-service"}
			endpoints: public_http: port: 4246
			postgres: {database: "mailbox_service", owner: "mailbox_service", connection_limit: 10}
		}
		object_storage_service: #PublicGoService & {
			artifact: {package: "./src/object-storage-service/cmd/object-storage-service", output: "object-storage-service", role: "object_storage_service"}
			runtime: {systemd: "object-storage-service", user: "object_storage_service", group: "object_storage_service"}
			identities: {
				service: {ansible_var: "spire_object_storage_service_id", path: "/svc/object-storage-service", user: "object_storage_service", group: "object_storage_service", uid_policy: {kind: "fixed", value: _config.object_storage.object_storage_service_uid}, entry_id: "verself-object-storage-service", restart_units: ["object-storage-admin", "object-storage-service"]}
				admin: {ansible_var: "spire_object_storage_admin_id", path: "/svc/object-storage-admin", user: "object_storage_admin", group: "object_storage_admin", uid_policy: {kind: "fixed", value: _config.object_storage.object_storage_admin_uid}, entry_id: "verself-object-storage-admin", restart_units: ["object-storage-admin", "object-storage-service"]}
			}
			endpoints: {
				public_http: port: 4256
				admin_http: {
					protocol: "https"
					port:     4257
					exposure: "loopback"
				}
			}
			interfaces: admin_api: {kind: "admin_api", endpoint: "admin_http", auth: "spiffe_mtls", probes: #ServiceProbes}
			postgres: {database: "object_storage_service", owner: "object_storage_service", connection_limit: 10}
		}
		garage: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "garage", user: "garage", group: "garage"}
			artifact: {kind: "static_binary", output: "garage", role: "garage"}
			garage: {
				instances: {
					count: 3
					port_plan: {
						stride:     10
						s3_base:    3900
						rpc_base:   3901
						admin_base: 3903
					}
				}
				nodes: [
					for i in list.Range(0, instances.count, 1) {
						instance:   i
						s3_port:    instances.port_plan.s3_base + i*instances.port_plan.stride
						rpc_port:   instances.port_plan.rpc_base + i*instances.port_plan.stride
						admin_port: instances.port_plan.admin_base + i*instances.port_plan.stride
					},
				]
			}
			endpoints: {
				for node in garage.nodes {
					"s3_\(node.instance)": {protocol: "http", port: node.s3_port, exposure: "loopback"}
					"rpc_\(node.instance)": {protocol: "tcp", port: node.rpc_port, exposure: "loopback"}
					"admin_\(node.instance)": {protocol: "http", port: node.admin_port, exposure: "loopback"}
				}
			}
			interfaces: {
				s3: {kind: "resource_protocol", endpoint: "s3_0", auth: "spiffe_mtls"}
				admin: {kind: "admin_api", endpoint: "admin_0", auth: "operator"}
			}
		}
		electric_mail: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "electric-mail", user: "electric", group: "electric"}
			artifact: {kind: "upstream_binary", output: "electric", role: "electric"}
			endpoints: http: {protocol: "http", port: 3011, exposure: "loopback"}
			interfaces: shape_api: {kind: "resource_protocol", endpoint: "http", auth: "shared_secret"}
			electric: {
				instance:         "mail"
				pg_role:          "electric_mail"
				pg_conn_limit:    25
				source_database:  "mailbox_service"
				writer_role:      "mailbox_service"
				publication_name: "electric_publication_mail"
				publication_tables: ["mailbox_accounts", "mailboxes", "emails", "email_mailboxes", "email_bodies", "threads"]
				storage_dir:    "/var/lib/electric-mail"
				credstore_dir:  "/etc/credstore/electric-mail"
				nftables_table: "verself_electric_mail"
				nftables_file:  "/etc/nftables.d/electric-mail.nft"
				db_pool_size:   10
				extra_systemd_after: ["mailbox-service.service"]
			}
		}
		electric_notifications: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "electric-notifications", user: "electric", group: "electric"}
			artifact: {kind: "upstream_binary", output: "electric", role: "electric"}
			endpoints: http: {protocol: "http", port: 3012, exposure: "loopback"}
			interfaces: shape_api: {kind: "resource_protocol", endpoint: "http", auth: "shared_secret"}
			electric: {
				instance:         "notifications"
				pg_role:          "electric_notifications"
				pg_conn_limit:    15
				source_database:  "notifications_service"
				writer_role:      "notifications_service"
				publication_name: "electric_publication_notifications"
				publication_tables: ["notification_inbox_state"]
				storage_dir:    "/var/lib/electric-notifications"
				credstore_dir:  "/etc/credstore/electric-notifications"
				nftables_table: "verself_electric_notifications"
				nftables_file:  "/etc/nftables.d/electric-notifications.nft"
				db_pool_size:   8
				extra_systemd_after: ["notifications-service.service"]
			}
		}
		platform: #Frontend & {
			artifact: {package: "src/viteplus-monorepo/apps/platform", output: "platform", role: "platform"}
			runtime: {systemd: "platform", user: "platform", group: "platform"}
			endpoints: http: port: 4249
		}
		company: #Frontend & {
			artifact: {package: "src/viteplus-monorepo/apps/company", output: "company", role: "company"}
			runtime: {systemd: "company", user: "company", group: "company"}
			endpoints: http: port: 4252
		}
		firecracker_host_service: {
			kind: "privileged_daemon"
			host: "10.255.0.1"
			runtime: {systemd: "vm-orchestrator", user: "root", group: "root"}
			artifact: {kind: "go_binary", package: "./src/vm-orchestrator/cmd/vm-orchestrator", output: "vm-orchestrator", role: "firecracker"}
			endpoints: host_http: {
				protocol: "http"
				host:     "10.255.0.1"
				port:     18080
				exposure: "guest_host"
			}
			interfaces: guest_host_api: {kind: "privileged_daemon_api", endpoint: "host_http", auth: "none"}
		}
	}

	routes: [
		{kind: "browser_origin", gateway: "public_caddy", host: "@", to: {component: "platform", interface: "frontend"}, waf: "detection", browser_cors: "same_origin"},
		{kind: "browser_origin", gateway: "public_caddy", zone: "company", host: "@", to: {component: "company", interface: "frontend"}, waf: "detection", browser_cors: "same_origin"},
		{kind: "browser_origin", gateway: "public_caddy", host: "console", to: {component: "console", interface: "frontend"}, waf: "detection", browser_cors: "same_origin"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "billing.api", path_prefix: "/api/v1", to: {component: "billing", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "sandbox.api", path_prefix: "/api/v1", to: {component: "sandbox_rental", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "identity.api", path_prefix: "/api/v1", to: {component: "identity_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "profile.api", path_prefix: "/api/v1", to: {component: "profile_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 16384, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "notifications.api", path_prefix: "/api/v1", to: {component: "notifications_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 16384, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "projects.api", path_prefix: "/api/v1", to: {component: "projects_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 65536, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "source.api", path_prefix: "/api/v1", to: {component: "source_code_hosting_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "governance.api", path_prefix: "/api/v1", to: {component: "governance_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "secrets.api", path_prefix: "/api/v1", to: {component: "secrets_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "public_api_origin", gateway: "public_caddy", host: "mail.api", path_prefix: "/api/v1", to: {component: "mailbox_service", interface: "public_api"}, waf: "blocking", max_body_bytes: 1048576, browser_cors: "none"},
		{kind: "operator_origin", gateway: "public_caddy", host: "dashboard", to: {component: "grafana", interface: "operator_ui"}, waf: "detection", browser_cors: "not_browser_reachable"},
		{kind: "operator_origin", gateway: "public_caddy", host: "temporal", to: {component: "temporal_web", interface: "operator_ui"}, waf: "detection", browser_cors: "not_browser_reachable"},
		{kind: "protocol_origin", gateway: "public_caddy", host: "git", to: {component: "source_code_hosting_service", interface: "git_smart_http"}, waf: "detection", max_body_bytes: 1048576},
		{kind: "protocol_origin", gateway: "public_caddy", host: "auth", to: {component: "zitadel", interface: "oidc"}, waf: "detection"},
		{kind: "protocol_origin", gateway: "public_caddy", host: "mail", to: {component: "stalwart", interface: "jmap"}, waf: "detection"},
		{kind: "protocol_origin", gateway: "direct_smtp", host: "mail", to: {component: "stalwart", interface: "smtp"}, waf: "off"},
		{kind: "guest_host_route", gateway: "firecracker_host", host: "10.255.0.1", paths: ["/internal/sandbox/v1/github-runner-jit", "/internal/sandbox/v1/runner-bootstrap", "/internal/sandbox/v1/stickydisk/*", "/internal/sandbox/v1/github-checkout/bundle"], to: {component: "sandbox_rental", interface: "public_api"}, waf: "off"},
		{kind: "guest_host_route", gateway: "firecracker_host", host: "10.255.0.1", paths: ["/api/actions", "/api/actions/*", "/{owner}/{repo}.git/info/refs", "/{owner}/{repo}.git/git-upload-pack"], to: {component: "forgejo", interface: "forgejo_http"}, waf: "off"},
	]

	edges: [
		{from: "console", to: {component: "billing", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "console", to: {component: "sandbox_rental", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "console", to: {component: "identity_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "console", to: {component: "profile_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "console", to: {component: "notifications_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "console", to: {component: "projects_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "console", to: {component: "source_code_hosting_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "sandbox_rental", to: {component: "billing", interface: "internal_api"}, auth: "spiffe_mtls", purpose: "metering_and_entitlements"},
		{from: "sandbox_rental", to: {component: "governance_service", interface: "internal_api"}, auth: "spiffe_mtls", purpose: "audit"},
		{from: "sandbox_rental", to: {component: "secrets_service", interface: "internal_api"}, auth: "spiffe_mtls", purpose: "secret_injection"},
		{from: "sandbox_rental", to: {component: "firecracker_host_service", interface: "guest_host_api"}, auth: "none", purpose: "guest_bootstrap"},
		{from: "secrets_service", to: {component: "openbao", interface: "api"}, auth: "spiffe_mtls", purpose: "secrets_resource_plane"},
		{from: "source_code_hosting_service", to: {component: "projects_service", interface: "internal_api"}, auth: "spiffe_mtls", purpose: "project_resolution"},
		{from: "source_code_hosting_service", to: {component: "identity_service", interface: "internal_api"}, auth: "spiffe_mtls", purpose: "organization_resolution"},
		{from: "object_storage_service", to: {component: "garage", interface: "s3"}, auth: "spiffe_mtls", purpose: "object_data_plane"},
	]

	policies: {
		frontend_csp: {kind: "csp", values: {connect_src: "self"}}
		public_api_body_limit: {kind: "body_limit", values: {default_bytes: 1048576}}
	}

	evidence: [
		{name: "topology_cue_fmt", kind: "span", service: "topology-compiler", span_name: "topology.cue.fmt_check", attributes: {}},
		{name: "topology_graph_validate", kind: "span", service: "topology-compiler", span_name: "topology.graph.validate", attributes: {}},
		{name: "topology_artifact_freshness", kind: "span", service: "topology-compiler", span_name: "topology.generated.freshness_check", attributes: {}},
	]
}

_interfaceEndpointChecks: [
	for componentName, componentValue in topology.components
	for interfaceName, interfaceValue in componentValue.interfaces {
		component: componentName
		interface: interfaceName
		endpoint:  componentValue.endpoints[interfaceValue.endpoint]
	},
]

_routeTargetChecks: [
	for route in topology.routes {
		gateway:   topology.gateways[route.gateway]
		component: topology.components[route.to.component]
		interface: topology.components[route.to.component].interfaces[route.to.interface]
	},
]

_edgeTargetChecks: [
	for edge in topology.edges {
		from:      topology.components[edge.from]
		component: topology.components[edge.to.component]
		interface: topology.components[edge.to.component].interfaces[edge.to.interface]
	},
]

_publicAPIRouteChecks: [
	for route in topology.routes
	if route.kind == "public_api_origin" {
		kind:         topology.components[route.to.component].interfaces[route.to.interface].kind & "huma_api"
		path_prefix:  route.path_prefix & "/api/v1"
		browser_cors: route.browser_cors & "none"
	},
]

_resourceExposureChecks: [
	for componentName, componentValue in topology.components
	if componentValue.kind == "resource"
	for endpointName, endpointValue in componentValue.endpoints
	if endpointValue.exposure == "public" {
		component: componentName
		endpoint:  endpointName
		_route:    true & false
	},
]

_endpointEntries: [
	for componentName, componentValue in topology.components
	for endpointName, endpointValue in componentValue.endpoints {
		component: componentName
		endpoint:  endpointName
		port:      endpointValue.port
		protocol:  endpointValue.protocol
		exposure:  endpointValue.exposure
	},
]

_ports: [for entry in _endpointEntries {entry.port}]
_uniquePorts: true & list.UniqueItems(_ports)

_controlPlaneComponents: [
	"billing",
	"company",
	"console",
	"governance_service",
	"identity_service",
	"mailbox_service",
	"notifications_service",
	"object_storage_service",
	"platform",
	"profile_service",
	"projects_service",
	"sandbox_rental",
	"secrets_service",
	"source_code_hosting_service",
]

_controlPlanePortChecks: [
	for name in _controlPlaneComponents
	for _, endpointValue in topology.components[name].endpoints {
		port: endpointValue.port & >=4240 & <=4269
	},
]

_workloadIdentities: [
	for componentName, componentValue in topology.components
	for identityName, identityValue in componentValue.identities {
		key:                   "\(componentName).\(identityName)"
		component:             "\(componentName)"
		name:                  "\(identityName)"
		ansible_var:           identityValue.ansible_var
		entry_id:              identityValue.entry_id
		spiffe_id:             "spiffe://{{ spire_trust_domain }}\(identityValue.path)"
		user:                  identityValue.user
		group:                 identityValue.group
		uid_policy:            identityValue.uid_policy
		selector:              identityValue.selector
		x509_svid_ttl_seconds: identityValue.x509_svid_ttl_seconds
		restart_units:         identityValue.restart_units
	},
]

_electricComponents: [
	for componentName, componentValue in topology.components
	if componentValue.electric != _|_ {
		component:    "\(componentName)"
		service_name: componentValue.runtime.systemd
		port:         componentValue.endpoints[componentValue.interfaces.shape_api.endpoint].port
		sync:         componentValue.electric
	},
]

_postgresRoleConnectionLimits: {
	for _, componentValue in topology.components
	if componentValue.postgres.owner != "" && componentValue.postgres.connection_limit > 0 {
		"\(componentValue.postgres.owner)": componentValue.postgres.connection_limit
	}
	for _, componentValue in _electricComponents {
		"\(componentValue.sync.pg_role)": componentValue.sync.pg_conn_limit
	}
}

outputs: {
	runtime: {
		topology_runtime: {
			for componentName, componentValue in topology.components {
				"\(componentName)": {
					kind:     componentValue.kind
					artifact: componentValue.artifact
					runtime:  componentValue.runtime
				}
			}
		}
	}
	endpoints: {
		topology_endpoints: {
			for componentName, componentValue in topology.components {
				"\(componentName)": {
					host: componentValue.host
					endpoints: {
						for endpointName, endpointValue in componentValue.endpoints {
							"\(endpointName)": endpointValue & {
								address: "\(endpointValue.host):\(endpointValue.port)"
								if endpointValue.listen_host == "" {
									bind_address: "\(endpointValue.host):\(endpointValue.port)"
								}
								if endpointValue.listen_host != "" {
									bind_address: "\(endpointValue.listen_host):\(endpointValue.port)"
								}
							}
						}
					}
					interfaces: componentValue.interfaces
					probes:     componentValue.probes
				}
			}
		}
	}
	clusters: {
		topology_clusters: {
			garage: {
				host:  topology.components.garage.host
				nodes: topology.components.garage.garage.nodes
			}
			temporal: topology.components.temporal.temporal
		}
	}
	routes: {
		topology_gateways: topology.gateways
		topology_routes:   topology.routes
	}
	dns: {
		topology_dns_records: [
			for route in topology.routes
			if route.kind != "guest_host_route" && route.host != "10.255.0.1" {
				zone:   route.zone
				record: route.host
				kind:   route.kind
			},
		]
	}
	nftables: {
		topology_nftables: {
			endpoints: _endpointEntries
			edges:     topology.edges
		}
	}
	spire: {
		spire_trust_domain:                 "spiffe.{{ verself_domain }}"
		spire_server_bind_address:          "127.0.0.1"
		spire_server_bind_port:             "{{ topology_endpoints.spire_server.endpoints.api.port }}"
		spire_server_socket_path:           "/run/spire-server/private/api.sock"
		spire_agent_socket_path:            "/run/spire-agent/sockets/agent.sock"
		spire_workload_group:               "spire_workload"
		spire_agent_id:                     "spiffe://{{ spire_trust_domain }}/node/single-node"
		spire_bundle_endpoint_bind_address: "127.0.0.1"
		spire_bundle_endpoint_bind_port:    "{{ topology_endpoints.spire_bundle_endpoint.endpoints.bundle.port }}"
		spire_jwt_bundle_endpoint_url:      "https://{{ spire_bundle_endpoint_bind_address }}:{{ spire_bundle_endpoint_bind_port }}"
		spire_jwt_issuer_url:               "{{ spire_jwt_bundle_endpoint_url }}"
		for _, identity in _workloadIdentities
		if identity.ansible_var != "" {
			"\(identity.ansible_var)": identity.spiffe_id
		}
		topology_spire: {
			identities: [
				for identity in _workloadIdentities {
					key:                   identity.key
					component:             identity.component
					entry_id:              identity.entry_id
					spiffe_id:             identity.spiffe_id
					user:                  identity.user
					group:                 identity.group
					uid_policy:            identity.uid_policy
					selector:              identity.selector
					x509_svid_ttl_seconds: identity.x509_svid_ttl_seconds
					restart_units:         identity.restart_units
				},
			]
			edges: [
				for edge in topology.edges
				if edge.auth == "spiffe_mtls" {
					from: edge.from
					to:   edge.to
				},
			]
		}
	}
	postgres: {
		postgresql_max_connections:                _config.postgres.max_connections
		postgresql_superuser_reserved_connections: _config.postgres.superuser_reserved_connections
		postgresql_role_connection_limits:         _postgresRoleConnectionLimits
		topology_postgres: {
			databases: [
				for componentName, componentValue in topology.components
				if componentValue.postgres.database != "" {
					component: componentName
					database:  componentValue.postgres.database
					owner:     componentValue.postgres.owner
				},
			]
		}
	}
	deploy: {
		topology_deploy: {
			artifacts: [
				for componentName, componentValue in topology.components
				if componentValue.artifact.kind != "none" {
					component: componentName
					artifact:  componentValue.artifact
				},
			]
			edges: topology.edges
		}
	}
	ops: {
		verself_version: _config.verself_version
		verself_bin:     _config.verself_bin
		for key, value in _config.domains {
			"\(key)": value
		}
		for key, value in _config.openbao {
			"\(key)": value
		}
		for key, value in _config.wireguard {
			"\(key)": value
		}
		for key, value in _config.object_storage {
			"\(key)": value
		}
		retired_product_runtimes: _config.retired_product_runtimes
		topology_electric_instances: {
			for _, componentValue in _electricComponents {
				"\(componentValue.sync.instance)": {
					electric_instance:              componentValue.sync.instance
					electric_service_name:          componentValue.service_name
					electric_service_port:          componentValue.port
					electric_pg_role:               componentValue.sync.pg_role
					electric_pg_conn_limit:         componentValue.sync.pg_conn_limit
					electric_db:                    componentValue.sync.source_database
					electric_writer_role:           componentValue.sync.writer_role
					electric_publication_name:      componentValue.sync.publication_name
					electric_publication_tables:    componentValue.sync.publication_tables
					electric_storage_dir:           componentValue.sync.storage_dir
					electric_credstore_dir:         componentValue.sync.credstore_dir
					electric_nftables_table:        componentValue.sync.nftables_table
					electric_nftables_file:         componentValue.sync.nftables_file
					electric_db_pool_size:          componentValue.sync.db_pool_size
					electric_replication_stream_id: componentValue.sync.replication_stream_id
					electric_extra_systemd_after:   componentValue.sync.extra_systemd_after
				}
			}
		}
		for key, value in _config.temporal {
			"\(key)": value
		}
		for key, value in _config.seed_system {
			"\(key)": value
		}
	}
	proof: {
		topology_proof: {
			evidence: topology.evidence
		}
	}
	catalog: {
		topology_versions:       cat.versions
		topology_server_tools:   cat.serverTools
		topology_dev_tools:      cat.devTools
		topology_guest_versions: cat.guestVersions
	}
}

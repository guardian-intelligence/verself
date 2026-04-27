package topology

import s "verself.sh/cue-renderer/schema"

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

	// WireGuard tunnel topology. The host can carry multiple independent tunnels;
	// `tunnels` holds the per-interface config and `host_groups` selects which
	// tunnels each Ansible play attaches to. The worker mesh (wg0) keeps the
	// historical 10.0.0.0/16 plane between platform nodes; the operator mesh
	// (wg-ops) is a private path for reaching internal substrate (bazel-remote
	// today; openbao UI / verdaccio admin in the future) without ever fronting
	// those listeners on the public internet.
	wireguard: {
		tunnels: {
			worker: {
				interface:      "wg0"
				port:           51820
				network:        "10.0.0.0/16"
				address:        "10.0.0.1"
				address_prefix: 16
				peers: [...{public_key: string, allowed_ips: string}] | *[]
			}
			ops: {
				interface:      "wg-ops"
				port:           51821
				network:        "10.66.66.0/24"
				address:        "10.66.66.1"
				address_prefix: 24
				// Operator laptops join by generating a keypair locally with `wg
				// genkey | wg pubkey` and submitting their public key here. Server
				// has no `endpoint` for these peers because operators sit behind
				// NAT and initiate the handshake themselves.
				peers: [
					{
						// Controller / founder laptop. Bringing up wg-ops on the
						// laptop unlocks bazel-remote at grpc://10.66.66.1:9092
						// for both `make deploy` (so deploy_profile pushes warm
						// cache) and ad-hoc `bazelisk build --config=remote`.
						public_key:  "AoVgh4aWFK5Gi7HBdqIzTea37aa5SaemU4Pyk92Nglc="
						allowed_ips: "10.66.66.2/32"
					},
				]
			}
		}
		// Which tunnels each Ansible host group runs. Single-node deployments
		// hit both groups so workers gets the worker tunnel and infra adds
		// wg-ops on top.
		host_groups: {
			workers: ["worker"]
			infra: ["worker", "ops"]
		}
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
			artifact: {kind: "go_binary", package: "./src/temporal-platform/cmd/verself-temporal-web", output: "verself-temporal-web", role: "temporal", bazel_label: "//src/temporal-platform/cmd/verself-temporal-web:verself-temporal-web"}
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
			artifact: {kind: "go_binary", package: "./src/temporal-platform/cmd/verself-temporal-server", output: "verself-temporal-server", role: "temporal", bazel_label: "//src/temporal-platform/cmd/verself-temporal-server:verself-temporal-server"}
			tools: {
				bootstrap: {kind: "go_binary", package: "./src/temporal-platform/cmd/temporal-bootstrap", output: "temporal-bootstrap", role: "temporal", bazel_label: "//src/temporal-platform/cmd/temporal-bootstrap:temporal-bootstrap"}
				schema: {kind: "go_binary", package: "./src/temporal-platform/cmd/temporal-schema", output: "temporal-schema", role: "temporal", bazel_label: "//src/temporal-platform/cmd/temporal-schema:temporal-schema"}
			}
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
			artifact: {package: "./src/billing-service/cmd/billing-service", output: "billing-service", role: "billing_service", bazel_label: "//src/billing-service/cmd/billing-service:billing-service"}
			runtime: {systemd: "billing-service", user: "billing", group: "billing"}
			identities: default: {ansible_var: "spire_billing_service_id", path: "/svc/billing-service", entry_id: "verself-billing-service"}
			endpoints: {
				public_http: port:    4242
				internal_https: port: 4255
			}
			postgres: {database: "billing", owner: "billing", connection_limit: 30}
		}
		sandbox_rental: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/sandbox-rental-service/cmd/sandbox-rental-service", output: "sandbox-rental-service", role: "sandbox_rental_service", bazel_label: "//src/sandbox-rental-service/cmd/sandbox-rental-service:sandbox-rental-service"}
			runtime: {systemd: "sandbox-rental-service", user: "sandbox_rental", group: "sandbox_rental"}
			identities: default: {ansible_var: "spire_sandbox_rental_id", path: "/svc/sandbox-rental-service", entry_id: "verself-sandbox-rental-service"}
			processes: recurring_worker: {
				systemd: "sandbox-rental-recurring-worker"
				user:    "sandbox_rental"
				group:   "sandbox_rental"
				artifact: {kind: "go_binary", package: "./src/sandbox-rental-service/cmd/sandbox-rental-recurring-worker", output: "sandbox-rental-recurring-worker", role: "sandbox_rental_service", bazel_label: "//src/sandbox-rental-service/cmd/sandbox-rental-recurring-worker:sandbox-rental-recurring-worker"}
				identities: ["default"]
				after: ["verself-firewall.target", "network.target", "postgresql.service", "temporal-server.service", "spire-agent.service", "otelcol.service", "source-code-hosting-service.service"]
				wants: ["postgresql.service", "temporal-server.service", "spire-agent.service", "otelcol.service", "source-code-hosting-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				requires_spiffe_sock: true
			}
			endpoints: {
				public_http: port:    4243
				internal_https: port: 4263
			}
			postgres: {database: "sandbox_rental", owner: "sandbox_rental", connection_limit: 30}
		}
		identity_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/identity-service/cmd/identity-service", output: "identity-service", role: "identity_service", bazel_label: "//src/identity-service/cmd/identity-service:identity-service"}
			runtime: {systemd: "identity-service", user: "identity_service", group: "identity_service"}
			identities: default: {ansible_var: "spire_identity_service_id", path: "/svc/identity-service", entry_id: "verself-identity-service"}
			endpoints: {
				public_http: port:    4248
				internal_https: port: 4241
			}
			postgres: {database: "identity_service", owner: "identity_service", connection_limit: 10}
		}
		governance_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/governance-service/cmd/governance-service", output: "governance-service", role: "governance_service", bazel_label: "//src/governance-service/cmd/governance-service:governance-service"}
			runtime: {systemd: "governance-service", user: "governance_service", group: "governance_service"}
			identities: default: {ansible_var: "spire_governance_service_id", path: "/svc/governance-service", entry_id: "verself-governance-service"}
			endpoints: {
				public_http: port:    4250
				internal_https: port: 4254
			}
			postgres: {database: "governance_service", owner: "governance_service", connection_limit: 15}
		}
		secrets_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/secrets-service/cmd/secrets-service", output: "secrets-service", role: "secrets_service", bazel_label: "//src/secrets-service/cmd/secrets-service:secrets-service"}
			runtime: {systemd: "secrets-service", user: "secrets_service", group: "secrets_service"}
			identities: default: {ansible_var: "spire_secrets_service_id", path: "/svc/secrets-service", entry_id: "verself-secrets-service"}
			endpoints: {
				public_http: port:    4251
				internal_https: port: 4253
			}
			postgres: {database: "secrets_service", owner: "secrets_service"}
		}
		profile_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/profile-service/cmd/profile-service", output: "profile-service", role: "profile_service", bazel_label: "//src/profile-service/cmd/profile-service:profile-service"}
			runtime: {systemd: "profile-service", user: "profile_service", group: "profile_service"}
			identities: default: {ansible_var: "spire_profile_service_id", path: "/svc/profile-service", entry_id: "verself-profile-service"}
			endpoints: {
				public_http: port:    4258
				internal_https: port: 4259
			}
			postgres: {database: "profile_service", owner: "profile_service", connection_limit: 10}
		}
		notifications_service: #PublicGoService & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/notifications-service/cmd/notifications-service", output: "notifications-service", role: "notifications_service", bazel_label: "//src/notifications-service/cmd/notifications-service:notifications-service"}
			runtime: {systemd: "notifications-service", user: "notifications_service", group: "notifications_service"}
			identities: default: {ansible_var: "spire_notifications_service_id", path: "/svc/notifications-service", entry_id: "verself-notifications-service"}
			endpoints: public_http: port: 4260
			postgres: {database: "notifications_service", owner: "notifications_service", connection_limit: 10}
		}
		projects_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/projects-service/cmd/projects-service", output: "projects-service", role: "projects_service", bazel_label: "//src/projects-service/cmd/projects-service:projects-service"}
			runtime: {systemd: "projects-service", user: "projects_service", group: "projects_service"}
			identities: default: {ansible_var: "spire_projects_service_id", path: "/svc/projects-service", entry_id: "verself-projects-service"}
			endpoints: {
				public_http: port:    4264
				internal_https: port: 4265
			}
			postgres: {database: "projects_service", owner: "projects_service", connection_limit: 10}
		}
		source_code_hosting_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/source-code-hosting-service/cmd/source-code-hosting-service", output: "source-code-hosting-service", role: "source_code_hosting_service", bazel_label: "//src/source-code-hosting-service/cmd/source-code-hosting-service:source-code-hosting-service"}
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
				publication_tables: ["executions", "execution_logs", "runner_provider_repositories"]
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
		bazel_remote: {
			kind: "resource"
			host: "127.0.0.1"
			// listen_host 0.0.0.0 so the daemon is reachable on the wg-ops mesh as
			// well as on loopback; nftables enforces the actual access policy.
			listen_host: "0.0.0.0"
			runtime: {systemd: "bazel-remote", user: "bazel_remote", group: "bazel_remote"}
			artifact: {kind: "static_binary", output: "bazel-remote", role: "bazel_remote"}
			endpoints: {
				grpc: {protocol: "grpc", listen_host: "0.0.0.0", port: 9092, exposure: "wireguard"}
				http: {protocol: "http", listen_host: "0.0.0.0", port: 8080, exposure: "wireguard"}
			}
			interfaces: {
				cas: {kind: "resource_protocol", endpoint: "grpc", auth: "operator"}
				status: {kind: "admin_api", endpoint: "http", auth: "operator"}
			}
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
			artifact: {package: "./src/mailbox-service/cmd/mailbox-service", output: "mailbox-service", role: "mailbox_service", bazel_label: "//src/mailbox-service/cmd/mailbox-service:mailbox-service"}
			runtime: {systemd: "mailbox-service", user: "mailbox_service", group: "mailbox_service"}
			identities: default: {ansible_var: "spire_mailbox_service_id", path: "/svc/mailbox-service", entry_id: "verself-mailbox-service"}
			endpoints: public_http: port: 4246
			postgres: {database: "mailbox_service", owner: "mailbox_service", connection_limit: 10}
		}
		object_storage_service: #PublicGoService & {
			artifact: {package: "./src/object-storage-service/cmd/object-storage-service", output: "object-storage-service", role: "object_storage_service", bazel_label: "//src/object-storage-service/cmd/object-storage-service:object-storage-service"}
			runtime: {systemd: "object-storage-service", user: "object_storage_service", group: "object_storage_service"}
			identities: {
				service: {ansible_var: "spire_object_storage_service_id", path: "/svc/object-storage-service", user: "object_storage_service", group: "object_storage_service", uid_policy: {kind: "fixed", value: _config.object_storage.object_storage_service_uid}, entry_id: "verself-object-storage-service", restart_units: ["object-storage-admin", "object-storage-service"]}
				admin: {ansible_var: "spire_object_storage_admin_id", path: "/svc/object-storage-admin", user: "object_storage_admin", group: "object_storage_admin", uid_policy: {kind: "fixed", value: _config.object_storage.object_storage_admin_uid}, entry_id: "verself-object-storage-admin", restart_units: ["object-storage-admin", "object-storage-service"]}
			}
			tools: secret_sync: {kind: "go_binary", package: "./src/object-storage-service/cmd/object-storage-secret-sync", output: "object-storage-secret-sync", role: "object_storage_service", bazel_label: "//src/object-storage-service/cmd/object-storage-secret-sync:object-storage-secret-sync"}
			processes: admin: {
				systemd: "object-storage-admin"
				user:    "object_storage_admin"
				group:   "object_storage_admin"
				artifact: {kind: "go_binary", package: "./src/object-storage-service/cmd/object-storage-service", output: "object-storage-service", role: "object_storage_service", bazel_label: "//src/object-storage-service/cmd/object-storage-service:object-storage-service"}
				endpoints: ["admin_http"]
				identities: ["service", "admin"]
				after: ["verself-firewall.target", "network.target", "postgresql.service", "governance-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
				wants: ["postgresql.service", "governance-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
				supplementary_groups: ["object_storage_service", "{{ spire_workload_group }}"]
				requires_spiffe_sock: true
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
					{instance: 0, s3_port: 3900, rpc_port: 3901, admin_port: 3903},
					{instance: 1, s3_port: 3910, rpc_port: 3911, admin_port: 3913},
					{instance: 2, s3_port: 3920, rpc_port: 3921, admin_port: 3923},
				]
			}
			endpoints: {
				s3_0: {protocol: "http", port: 3900, exposure: "loopback"}
				rpc_0: {protocol: "tcp", port: 3901, exposure: "loopback"}
				admin_0: {protocol: "http", port: 3903, exposure: "loopback"}
				s3_1: {protocol: "http", port: 3910, exposure: "loopback"}
				rpc_1: {protocol: "tcp", port: 3911, exposure: "loopback"}
				admin_1: {protocol: "http", port: 3913, exposure: "loopback"}
				s3_2: {protocol: "http", port: 3920, exposure: "loopback"}
				rpc_2: {protocol: "tcp", port: 3921, exposure: "loopback"}
				admin_2: {protocol: "http", port: 3923, exposure: "loopback"}
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
			artifact: {kind: "go_binary", package: "./src/vm-orchestrator/cmd/vm-orchestrator", output: "vm-orchestrator", role: "firecracker", bazel_label: "//src/vm-orchestrator/cmd/vm-orchestrator:vm-orchestrator"}
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
		{name: "cue_renderer_run", kind: "span", service: "cue-renderer", span_name: "cue_renderer.run", attributes: {}},
		{name: "topology_graph_export", kind: "span", service: "cue-renderer", span_name: "topology.cue.export_graph", attributes: {}},
		{name: "topology_clusters_freshness", kind: "span", service: "cue-renderer", span_name: "topology.generated.freshness_check", attributes: {"topology.artifact": "clusters"}},
	]
}

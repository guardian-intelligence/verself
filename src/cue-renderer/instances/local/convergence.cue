package topology

topology: components: {
	billing: {
		converge: {
			enabled:    true
			deploy_tag: "billing_service"
			order:      80
			directories: [{path: "/etc/credstore/billing", owner: "root", group: "billing", mode: "0750"}]
			secret_refs: [
				{name: "stripe-test-publishable-key", path: "/etc/credstore/billing/stripe-test-publishable-key", owner: "root", group: "billing", mode: "0640", source: {kind: "ansible_var", ansible_var: "stripe_publishable_key"}},
				{name: "stripe-test-webhook-endpoint-id", path: "/etc/credstore/billing/stripe-test-webhook-endpoint-id", owner: "root", group: "billing", mode: "0640", source: {kind: "ansible_var", ansible_var: "stripe_test_webhook_endpoint_id"}},
			]
			clickhouse: {
				user:            "billing_service"
				spiffe_identity: "default"
				grants: [
					{action: "INSERT", table: "verself.billing_events"},
					{action: "INSERT", table: "verself.metering"},
				]
			}
			auth: {kind: "owned_project", project_name: "billing", project_role_assertion: false, project_role_check: false}
			systemd: units: [{
				name:        "billing-service"
				description: "Billing Service"
				user:        "billing"
				group:       "billing"
				exec:        "{{ verself_bin }}/billing-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "spire-agent.service", "secrets-service.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "spire-agent.service", "secrets-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"}]
				environment: {
					BILLING_CH_ADDRESS:                   "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
					BILLING_CH_USER:                      "billing_service"
					BILLING_TB_ADDRESS:                   "{{ topology_endpoints.tigerbeetle.endpoints.client.address }}"
					BILLING_TB_CLUSTER_ID:                "0"
					BILLING_PG_DSN:                       "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
					BILLING_LISTEN_ADDR:                  "{{ topology_endpoints.billing.endpoints.public_http.address }}"
					BILLING_INTERNAL_LISTEN_ADDR:         "{{ topology_endpoints.billing.endpoints.internal_https.address }}"
					BILLING_SECRETS_URL:                  "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
					BILLING_AUTH_ISSUER_URL:              "https://auth.{{ verself_domain }}"
					BILLING_AUTH_AUDIENCE:                "{{ component_auth_audience }}"
					BILLING_PG_MAX_CONNS:                 "12"
					BILLING_PG_MIN_CONNS:                 "1"
					BILLING_PG_CONN_MAX_LIFETIME_SECONDS: "1800"
					BILLING_PG_CONN_MAX_IDLE_SECONDS:     "300"
					SPIFFE_ENDPOINT_SOCKET:               "unix://{{ spire_agent_socket_path }}"
				}
				hardening: {
					private_devices:     false
					private_tmp:         false
					protect_clock:       false
					protect_kernel_logs: false
					restrict_realtime:   false
					restrict_suid_sgid:  false
					restrict_namespaces: true
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
			bootstrap: ["billing_stripe_webhook"]
		}
	}
	sandbox_rental: {
		converge: {
			enabled:    true
			deploy_tag: "sandbox_rental_service"
			order:      90
			directories: [
				{path: "/etc/credstore/sandbox-rental", owner: "root", group: "sandbox_rental", mode: "0750"},
				{path: "/var/lib/verself/sandbox-rental/github-checkout", owner: "sandbox_rental", group: "sandbox_rental", mode: "0700"},
			]
			secret_refs: [
				{name: "forgejo-webhook-secret", path: "/etc/credstore/sandbox-rental/forgejo-webhook-secret", owner: "root", group: "sandbox_rental", mode: "0640", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
				{name: "forgejo-bootstrap-secret", path: "/etc/credstore/sandbox-rental/forgejo-bootstrap-secret", owner: "root", group: "sandbox_rental", mode: "0640", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
			]
			clickhouse: {
				user:            "sandbox_rental"
				spiffe_identity: "default"
				grants: [
					{action: "INSERT", table: "verself.job_logs"},
					{action: "INSERT", table: "verself.job_events"},
					{action: "SELECT", table: "verself.job_logs"},
					{action: "SELECT", table: "verself.job_events"},
					{action: "SELECT", table: "verself.job_cache_events"},
				]
			}
			auth: {
				kind:                   "owned_project"
				project_name:           "sandbox-rental"
				project_role_assertion: true
				project_role_check:     true
				roles: [
					{key: "owner", display_name: "Owner", group: "sandbox"},
					{key: "admin", display_name: "Admin", group: "sandbox"},
					{key: "member", display_name: "Member", group: "sandbox"},
				]
			}
			bootstrap_config: sandbox_github_app: {
				enabled:      true
				app_id:       "3370540"
				slug:         "verself-ci"
				client_id:    "Iv23liDpxGOmBSQwSJ5i"
				api_base_url: "https://api.github.com"
				web_base_url: "https://github.com"
			}
			systemd: units: [
				{
					name:        "sandbox-rental-service"
					description: "Sandbox Rental Service"
					user:        "sandbox_rental"
					group:       "sandbox_rental"
					exec:        "{{ verself_bin }}/sandbox-rental-service"
					after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "spire-agent.service", "secrets-service.service", "source-code-hosting-service.service"]
					wants: ["postgresql.service", "clickhouse-server.service", "spire-agent.service", "secrets-service.service", "source-code-hosting-service.service"]
					supplementary_groups: ["vm-clients", "{{ spire_workload_group }}"]
					bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
					load_credentials: [
						{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"},
						{name: "forgejo-token", path: "/etc/credstore/forgejo/automation-token"},
						{name: "forgejo-webhook-secret", path: "/etc/credstore/sandbox-rental/forgejo-webhook-secret"},
						{name: "forgejo-bootstrap-secret", path: "/etc/credstore/sandbox-rental/forgejo-bootstrap-secret"},
					]
					environment: {
						SANDBOX_LISTEN_ADDR:                   "{{ topology_endpoints.sandbox_rental.endpoints.public_http.address }}"
						SANDBOX_INTERNAL_LISTEN_ADDR:          "{{ topology_endpoints.sandbox_rental.endpoints.internal_https.address }}"
						SANDBOX_PG_DSN:                        "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
						SANDBOX_GOVERNANCE_AUDIT_URL:          "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
						SANDBOX_SECRETS_URL:                   "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
						SANDBOX_SOURCE_INTERNAL_URL:           "https://{{ topology_endpoints.source_code_hosting_service.endpoints.internal_https.address }}"
						SANDBOX_TEMPORAL_FRONTEND_ADDRESS:     "{{ topology_endpoints.temporal.endpoints.frontend_grpc.address }}"
						SANDBOX_TEMPORAL_NAMESPACE:            "sandbox-rental-service"
						SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING: "sandbox-rental-service.recurring-vm"
						SANDBOX_CH_ADDRESS:                    "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
						SANDBOX_CH_USER:                       "sandbox_rental"
						SANDBOX_BILLING_URL:                   "https://{{ topology_endpoints.billing.endpoints.internal_https.address }}"
						SANDBOX_BILLING_RETURN_ORIGINS:        "https://{{ console_domain }}"
						SANDBOX_PUBLIC_BASE_URL:               "https://{{ sandbox_rental_service_domain }}"
						SANDBOX_AUTH_ISSUER_URL:               "https://auth.{{ verself_domain }}"
						SANDBOX_AUTH_AUDIENCE:                 "{{ component_auth_audience }}"
						SANDBOX_VM_ORCHESTRATOR_SOCKET:        "/run/vm-orchestrator/api.sock"
						SANDBOX_PG_MAX_CONNS:                  "16"
						SANDBOX_PG_MIN_CONNS:                  "1"
						SANDBOX_PG_CONN_MAX_LIFETIME_SECONDS:  "1800"
						SANDBOX_PG_CONN_MAX_IDLE_SECONDS:      "300"
						SANDBOX_EXECUTION_MAX_WORKERS:         "4"
						SANDBOX_GITHUB_APP_ENABLED:            "\(bootstrap_config.sandbox_github_app.enabled)"
						SANDBOX_GITHUB_APP_ID:                 bootstrap_config.sandbox_github_app.app_id
						SANDBOX_GITHUB_APP_SLUG:               bootstrap_config.sandbox_github_app.slug
						SANDBOX_GITHUB_APP_CLIENT_ID:          bootstrap_config.sandbox_github_app.client_id
						SANDBOX_GITHUB_API_BASE_URL:           bootstrap_config.sandbox_github_app.api_base_url
						SANDBOX_GITHUB_WEB_BASE_URL:           bootstrap_config.sandbox_github_app.web_base_url
						SANDBOX_GITHUB_RUNNER_GROUP_ID:        "1"
						SANDBOX_GITHUB_CHECKOUT_CACHE_DIR:     "/var/lib/verself/sandbox-rental/github-checkout"
						SANDBOX_FORGEJO_API_BASE_URL:          "http://{{ topology_endpoints.forgejo.endpoints.http.address }}"
						SANDBOX_FORGEJO_RUNNER_BASE_URL:       "http://{{ topology_endpoints.firecracker_host_service.endpoints.host_http.address }}"
						SANDBOX_FORGEJO_WEBHOOK_BASE_URL:      "https://{{ sandbox_rental_service_domain }}"
						SPIFFE_ENDPOINT_SOCKET:                "unix://{{ spire_agent_socket_path }}"
					}
					hardening: {protect_system: "full"}
					readiness: [
						{kind: "tcp", endpoint: "public_http"},
						{kind: "tcp", endpoint: "internal_https"},
					]
				},
				{
					name:        "sandbox-rental-recurring-worker"
					description: "Sandbox Rental Recurring Worker"
					user:        "sandbox_rental"
					group:       "sandbox_rental"
					exec:        "{{ verself_bin }}/sandbox-rental-recurring-worker"
					after:       components.sandbox_rental.processes.recurring_worker.after
					wants:       components.sandbox_rental.processes.recurring_worker.wants
					supplementary_groups: ["{{ spire_workload_group }}"]
					environment: {
						SANDBOX_PG_DSN:                        "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
						SANDBOX_SOURCE_INTERNAL_URL:           "https://{{ topology_endpoints.source_code_hosting_service.endpoints.internal_https.address }}"
						SANDBOX_PG_MAX_CONNS:                  "4"
						SANDBOX_PG_MIN_CONNS:                  "1"
						SANDBOX_PG_CONN_MAX_LIFETIME_SECONDS:  "1800"
						SANDBOX_PG_CONN_MAX_IDLE_SECONDS:      "300"
						SANDBOX_TEMPORAL_FRONTEND_ADDRESS:     "{{ topology_endpoints.temporal.endpoints.frontend_grpc.address }}"
						SANDBOX_TEMPORAL_NAMESPACE:            "sandbox-rental-service"
						SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING: "sandbox-rental-service.recurring-vm"
						SPIFFE_ENDPOINT_SOCKET:                "unix://{{ spire_agent_socket_path }}"
					}
					hardening: {protect_system: "full"}
				},
			]
			bootstrap: ["sandbox_vm_socket_acl", "sandbox_github_app"]
		}
	}
	identity_service: {
		converge: {
			enabled:    true
			deploy_tag: "identity_service"
			order:      10
			directories: [{path: "/etc/credstore/identity-service", owner: "root", group: "identity_service", mode: "0750"}]
			clickhouse: {
				user:            "identity_service"
				spiffe_identity: "default"
				grants: [
					{action: "INSERT", table: "verself.domain_update_ledger"},
					{action: "SELECT", table: "verself.domain_update_ledger"},
				]
			}
			auth: {
				kind:                   "owned_project"
				project_name:           "identity-service"
				project_role_assertion: true
				project_role_check:     true
				roles: [
					{key: "owner", display_name: "Owner", group: "identity"},
					{key: "admin", display_name: "Admin", group: "identity"},
					{key: "member", display_name: "Member", group: "identity"},
				]
			}
			systemd: units: [{
				name:        "identity-service"
				description: "Identity Service"
				user:        "identity_service"
				group:       "identity_service"
				exec:        "{{ verself_bin }}/identity-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [
					{name: "zitadel-admin-token", path: "/etc/credstore/identity-service/zitadel-admin-token"},
					{name: "zitadel-action-signing-key", path: "/etc/credstore/identity-service/zitadel-action-signing-key"},
					{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"},
				]
				environment: {
					IDENTITY_LISTEN_ADDR:          "{{ topology_endpoints.identity_service.endpoints.public_http.address }}"
					IDENTITY_INTERNAL_LISTEN_ADDR: "{{ topology_endpoints.identity_service.endpoints.internal_https.address }}"
					IDENTITY_PG_DSN:               "postgres://identity_service@/identity_service?host=/var/run/postgresql&sslmode=disable"
					IDENTITY_CH_ADDRESS:           "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
					IDENTITY_CH_USER:              "identity_service"
					IDENTITY_GOVERNANCE_AUDIT_URL: "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
					IDENTITY_AUTH_ISSUER_URL:      "https://auth.{{ verself_domain }}"
					IDENTITY_AUTH_AUDIENCE:        "{{ component_auth_audience }}"
					IDENTITY_ZITADEL_BASE_URL:     "http://{{ topology_endpoints.zitadel.endpoints.http.address }}"
					IDENTITY_ZITADEL_HOST:         "auth.{{ verself_domain }}"
					SPIFFE_ENDPOINT_SOCKET:        "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:             "identity-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:   "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				readiness: [
					{kind: "http", endpoint: "public_http", path: "/readyz"},
					{kind: "tcp", endpoint: "internal_https"},
				]
			}]
			bootstrap: ["identity_zitadel_actions"]
		}
	}
	governance_service: {
		converge: {
			enabled:    true
			deploy_tag: "governance_service"
			order:      20
			directories: [
				{path: "/etc/credstore/governance-service", owner: "root", group: "governance_service", mode: "0750"},
				{path: "/var/lib/governance-service", owner: "governance_service", group: "governance_service", mode: "0750"},
				{path: "/var/lib/governance-service/exports", owner: "governance_service", group: "governance_service", mode: "0750"},
			]
			secret_refs: [
				{name: "pg-password", path: "/etc/credstore/governance-service/pg-password", owner: "root", group: "governance_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
				{name: "audit-hmac-key", path: "/etc/credstore/governance-service/audit-hmac-key", owner: "root", group: "governance_service", mode: "0640", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
			]
			clickhouse: {
				user:            "governance_service"
				spiffe_identity: "default"
				grants: [
					{action: "INSERT", table: "verself.audit_events"},
					{action: "SELECT", table: "verself.audit_events"},
				]
			}
			auth: {kind: "identity_project_audience", project_name: "identity-service"}
			systemd: units: [{
				name:        "governance-service"
				description: "Governance Service"
				user:        "governance_service"
				group:       "governance_service"
				exec:        "{{ verself_bin }}/governance-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [
					{name: "audit-hmac-key", path: "/etc/credstore/governance-service/audit-hmac-key"},
					{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"},
				]
				environment: {
					GOVERNANCE_LISTEN_ADDR:          "{{ topology_endpoints.governance_service.endpoints.public_http.address }}"
					GOVERNANCE_INTERNAL_LISTEN_ADDR: "{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
					GOVERNANCE_PG_DSN:               "postgres://governance_service@/governance_service?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_IDENTITY_PG_DSN:      "postgres://identity_service@/identity_service?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_BILLING_PG_DSN:       "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_SANDBOX_PG_DSN:       "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_CH_ADDRESS:           "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
					GOVERNANCE_CH_USER:              "governance_service"
					GOVERNANCE_AUTH_ISSUER_URL:      "https://auth.{{ verself_domain }}"
					GOVERNANCE_AUTH_AUDIENCE:        "{{ component_auth_audience }}"
					GOVERNANCE_EXPORT_DIR:           "/var/lib/governance-service/exports"
					SPIFFE_ENDPOINT_SOCKET:          "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:               "governance-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:     "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				hardening: read_write_paths: ["/var/lib/governance-service"]
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
		}
	}
	secrets_service: {
		converge: {
			enabled:    true
			deploy_tag: "secrets_service"
			order:      60
			directories: [{path: "/etc/credstore/secrets-service", owner: "root", group: "secrets_service", mode: "0750"}]
			auth: {
				kind:                   "owned_project"
				project_name:           "secrets-service"
				project_role_assertion: true
				project_role_check:     true
				roles: [
					{key: "owner", display_name: "Owner", group: "secrets"},
					{key: "admin", display_name: "Admin", group: "secrets"},
					{key: "member", display_name: "Member", group: "secrets"},
				]
			}
			systemd: units: [{
				name:        "secrets-service"
				description: "Secrets Service"
				user:        "secrets_service"
				group:       "secrets_service"
				exec:        "{{ verself_bin }}/secrets-service"
				after: ["verself-firewall.target", "network.target", "openbao.service", "zitadel.service", "governance-service.service", "spire-agent.service"]
				wants: ["openbao.service", "zitadel.service", "governance-service.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [{name: "openbao-ca-cert", path: "/etc/openbao/tls/cert.pem"}]
				environment: {
					SECRETS_LISTEN_ADDR:               "{{ topology_endpoints.secrets_service.endpoints.public_http.address }}"
					SECRETS_INTERNAL_LISTEN_ADDR:      "{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
					SECRETS_PLATFORM_ORG_ID:           "{{ component_secrets_platform_org_id }}"
					SECRETS_GOVERNANCE_AUDIT_URL:      "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
					SECRETS_AUTH_ISSUER_URL:           "https://auth.{{ verself_domain }}"
					SECRETS_AUTH_AUDIENCE:             "{{ component_auth_audience }}"
					SECRETS_OPENBAO_ADDR:              "https://{{ topology_endpoints.openbao.endpoints.api.address }}"
					SECRETS_OPENBAO_KV_PREFIX:         "kv"
					SECRETS_OPENBAO_TRANSIT_PREFIX:    "transit"
					SECRETS_OPENBAO_JWT_PREFIX:        "jwt"
					SECRETS_OPENBAO_SPIFFE_JWT_PREFIX: "spiffe-jwt"
					SECRETS_OPENBAO_WORKLOAD_AUDIENCE: "openbao"
					SECRETS_BILLING_URL:               "https://{{ topology_endpoints.billing.endpoints.internal_https.address }}"
					SPIFFE_ENDPOINT_SOCKET:            "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:                 "secrets-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:       "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
			bootstrap: ["secrets_platform_org", "openbao_tenancy"]
		}
	}
	profile_service: {
		converge: {
			enabled:    true
			deploy_tag: "profile_service"
			order:      30
			directories: [{path: "/etc/credstore/profile-service", owner: "root", group: "profile_service", mode: "0750"}]
			secret_refs: [{name: "pg-password", path: "/etc/credstore/profile-service/pg-password", owner: "root", group: "profile_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}}]
			auth: {kind: "identity_project_audience", project_name: "identity-service"}
			systemd: units: [{
				name:        "profile-service"
				description: "Profile Service"
				user:        "profile_service"
				group:       "profile_service"
				exec:        "{{ verself_bin }}/profile-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "zitadel.service", "spire-agent.service", "identity-service.service", "governance-service.service"]
				wants: ["postgresql.service", "zitadel.service", "spire-agent.service", "identity-service.service", "governance-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				environment: {
					PROFILE_LISTEN_ADDR:           "{{ topology_endpoints.profile_service.endpoints.public_http.address }}"
					PROFILE_INTERNAL_LISTEN_ADDR:  "{{ topology_endpoints.profile_service.endpoints.internal_https.address }}"
					PROFILE_PG_DSN:                "postgres://profile_service@/profile?host=/var/run/postgresql&sslmode=disable"
					PROFILE_PG_MAX_CONNS:          "8"
					PROFILE_AUTH_ISSUER_URL:       "https://auth.{{ verself_domain }}"
					PROFILE_AUTH_AUDIENCE:         "{{ component_auth_audience }}"
					PROFILE_IDENTITY_INTERNAL_URL: "https://{{ topology_endpoints.identity_service.endpoints.internal_https.address }}"
					PROFILE_GOVERNANCE_AUDIT_URL:  "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
					SPIFFE_ENDPOINT_SOCKET:        "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:             "profile-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:   "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
		}
	}
	notifications_service: {
		converge: {
			enabled:    true
			deploy_tag: "notifications_service"
			order:      40
			directories: [{path: "/etc/credstore/notifications-service", owner: "root", group: "notifications_service", mode: "0750"}]
			secret_refs: [{name: "pg-password", path: "/etc/credstore/notifications-service/pg-password", owner: "root", group: "notifications_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}}]
			clickhouse: {
				user:            "notifications_service"
				spiffe_identity: "default"
				grants: [
					{action: "INSERT", table: "verself.notification_events"},
					{action: "SELECT", table: "verself.notification_events"},
				]
			}
			auth: {kind: "identity_project_audience", project_name: "identity-service"}
			systemd: units: [{
				name:        "notifications-service"
				description: "Notifications Service"
				user:        "notifications_service"
				group:       "notifications_service"
				exec:        "{{ verself_bin }}/notifications-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "nats.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "nats.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"}]
				environment: {
					NOTIFICATIONS_LISTEN_ADDR:                  "{{ topology_endpoints.notifications_service.endpoints.public_http.address }}"
					NOTIFICATIONS_PG_DSN:                       "postgres://notifications_service@/notifications_service?host=/var/run/postgresql&sslmode=disable"
					NOTIFICATIONS_PG_MAX_CONNS:                 "8"
					NOTIFICATIONS_PG_MIN_CONNS:                 "1"
					NOTIFICATIONS_PG_CONN_MAX_LIFETIME_SECONDS: "1800"
					NOTIFICATIONS_PG_CONN_MAX_IDLE_SECONDS:     "300"
					NOTIFICATIONS_CH_ADDRESS:                   "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
					NOTIFICATIONS_CH_USER:                      "notifications_service"
					NOTIFICATIONS_NATS_URL:                     "tls://{{ topology_endpoints.nats.endpoints.client.address }}"
					NOTIFICATIONS_AUTH_ISSUER_URL:              "https://auth.{{ verself_domain }}"
					NOTIFICATIONS_AUTH_AUDIENCE:                "{{ component_auth_audience }}"
					SPIFFE_ENDPOINT_SOCKET:                     "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:                          "notifications-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:                "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
		}
	}
	projects_service: {
		converge: {
			enabled:    true
			deploy_tag: "projects_service"
			order:      50
			directories: [{path: "/etc/credstore/projects-service", owner: "root", group: "projects_service", mode: "0750"}]
			secret_refs: [{name: "pg-password", path: "/etc/credstore/projects-service/pg-password", owner: "root", group: "projects_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}}]
			auth: {kind: "identity_project_audience", project_name: "identity-service"}
			systemd: units: [{
				name:        "projects-service"
				description: "Projects Service"
				user:        "projects_service"
				group:       "projects_service"
				exec:        "{{ verself_bin }}/projects-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				environment: {
					PROJECTS_LISTEN_ADDR:          "{{ topology_endpoints.projects_service.endpoints.public_http.address }}"
					PROJECTS_INTERNAL_LISTEN_ADDR: "{{ topology_endpoints.projects_service.endpoints.internal_https.address }}"
					PROJECTS_PG_DSN:               "postgres://projects_service@/projects_service?host=/var/run/postgresql&sslmode=disable"
					PROJECTS_PG_MAX_CONNS:         "8"
					PROJECTS_AUTH_ISSUER_URL:      "https://auth.{{ verself_domain }}"
					PROJECTS_AUTH_AUDIENCE:        "{{ component_auth_audience }}"
					SPIFFE_ENDPOINT_SOCKET:        "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:             "projects-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:   "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
		}
	}
	source_code_hosting_service: {
		converge: {
			enabled:    true
			deploy_tag: "source_code_hosting_service"
			order:      75
			directories: [{path: "/etc/credstore/source-code-hosting-service", owner: "root", group: "source_code_hosting_service", mode: "0750"}]
			secret_refs: [
				{name: "pg-password", path: "/etc/credstore/source-code-hosting-service/pg-password", owner: "root", group: "source_code_hosting_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
				{name: "webhook-secret", path: "/etc/credstore/source-code-hosting-service/webhook-secret", owner: "root", group: "source_code_hosting_service", mode: "0640", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
			]
			auth: {kind: "identity_project_audience", project_name: "identity-service"}
			systemd: units: [{
				name:        "source-code-hosting-service"
				description: "Source Code Hosting Service"
				user:        "source_code_hosting_service"
				group:       "source_code_hosting_service"
				exec:        "{{ verself_bin }}/source-code-hosting-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "zitadel.service", "spire-agent.service", "forgejo.service", "secrets-service.service", "projects-service.service"]
				wants: ["postgresql.service", "zitadel.service", "spire-agent.service", "forgejo.service", "secrets-service.service", "projects-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [
					{name: "forgejo-token", path: "/etc/credstore/forgejo/automation-token"},
					{name: "webhook-secret", path: "/etc/credstore/source-code-hosting-service/webhook-secret"},
				]
				environment: {
					SOURCE_LISTEN_ADDR:           "{{ topology_endpoints.source_code_hosting_service.endpoints.public_http.address }}"
					SOURCE_INTERNAL_LISTEN_ADDR:  "{{ topology_endpoints.source_code_hosting_service.endpoints.internal_https.address }}"
					SOURCE_PG_DSN:                "postgres://source_code_hosting_service@/source_code_hosting?host=/var/run/postgresql&sslmode=disable"
					SOURCE_PG_MAX_CONNS:          "8"
					SOURCE_AUTH_ISSUER_URL:       "https://auth.{{ verself_domain }}"
					SOURCE_AUTH_AUDIENCE:         "{{ component_auth_audience }}"
					SOURCE_FORGEJO_BASE_URL:      "http://{{ topology_endpoints.forgejo.endpoints.http.address }}"
					SOURCE_FORGEJO_OWNER:         "{{ component_source_forgejo_username }}"
					SOURCE_SANDBOX_INTERNAL_URL:  "https://{{ topology_endpoints.sandbox_rental.endpoints.internal_https.address }}"
					SOURCE_SECRETS_INTERNAL_URL:  "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
					SOURCE_PROJECTS_INTERNAL_URL: "https://{{ topology_endpoints.projects_service.endpoints.internal_https.address }}"
					SOURCE_IDENTITY_INTERNAL_URL: "https://{{ topology_endpoints.identity_service.endpoints.internal_https.address }}"
					SOURCE_PUBLIC_BASE_URL:       "https://{{ forgejo_domain }}"
					SPIFFE_ENDPOINT_SOCKET:       "unix://{{ spire_agent_socket_path }}"
					OTEL_SERVICE_NAME:            "source-code-hosting-service"
					OTEL_EXPORTER_OTLP_ENDPOINT:  "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
			bootstrap: ["source_forgejo_owner"]
		}
	}
	mailbox_service: {
		converge: {
			enabled:    true
			deploy_tag: "mailbox_service"
			order:      100
			directories: [
				{path: "/etc/credstore/mailbox-service", owner: "root", group: "mailbox_service", mode: "0750"},
				{path: "/var/lib/mailbox-service", owner: "mailbox_service", group: "mailbox_service", mode: "0750"},
			]
			secret_refs: [
				{name: "stalwart-ceo-password", path: "/etc/credstore/mailbox-service/stalwart-ceo-password", owner: "root", group: "mailbox_service", mode: "0640", restart_units: ["mailbox-service"], source: {kind: "ansible_var", ansible_var: "stalwart_ceo_password"}},
				{name: "stalwart-agents-password", path: "/etc/credstore/mailbox-service/stalwart-agents-password", owner: "root", group: "mailbox_service", mode: "0640", restart_units: ["mailbox-service"], source: {kind: "ansible_var", ansible_var: "stalwart_agents_password"}},
				{name: "forward-to", path: "/etc/credstore/mailbox-service/forward-to", owner: "root", group: "mailbox_service", mode: "0640", restart_units: ["mailbox-service"], source: {kind: "ansible_var", ansible_var: "stalwart_operator_forward_to"}},
			]
			auth: {
				kind:                   "owned_project"
				project_name:           "mailbox-service"
				project_role_assertion: true
				project_role_check:     true
				roles: [{key: "mailbox_user", display_name: "Mailbox User", group: "mailbox"}]
			}
			systemd: units: [{
				name:        "mailbox-service"
				description: "Mailbox Service"
				user:        "mailbox_service"
				group:       "mailbox_service"
				exec:        "{{ verself_bin }}/mailbox-service"
				after: ["verself-firewall.target", "network.target", "postgresql.service", "stalwart.service", "zitadel.service", "spire-agent.service", "secrets-service.service"]
				wants: ["postgresql.service", "stalwart.service", "zitadel.service", "spire-agent.service", "secrets-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [
					{name: "stalwart-ceo-password", path: "/etc/credstore/mailbox-service/stalwart-ceo-password"},
					{name: "stalwart-agents-password", path: "/etc/credstore/mailbox-service/stalwart-agents-password"},
					{name: "forward-to", path: "/etc/credstore/mailbox-service/forward-to"},
				]
				environment: {
					MAILBOX_SERVICE_LISTEN_ADDR:              "{{ topology_endpoints.mailbox_service.endpoints.public_http.address }}"
					MAILBOX_SERVICE_PG_DSN:                   "postgres://mailbox_service@/mailbox_service?host=/var/run/postgresql&sslmode=disable"
					MAILBOX_SERVICE_STALWART_BASE_URL:        "http://{{ topology_endpoints.stalwart.endpoints.http.address }}"
					MAILBOX_SERVICE_STALWART_PUBLIC_BASE_URL: "https://{{ stalwart_domain }}"
					MAILBOX_SERVICE_STALWART_MAILBOX:         "ceo"
					MAILBOX_SERVICE_STALWART_LOCAL_DOMAIN:    "{{ verself_domain }}"
					MAILBOX_SERVICE_SECRETS_URL:              "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
					MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL:  "2m"
					MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL:  "10m"
					MAILBOX_SERVICE_AUTH_ISSUER_URL:          "https://auth.{{ verself_domain }}"
					MAILBOX_SERVICE_AUTH_AUDIENCE:            "{{ component_auth_audience }}"
					MAILBOX_SERVICE_FORWARDER_FROM_ADDRESS:   "{{ resend_sender_address }}"
					MAILBOX_SERVICE_FORWARDER_FROM_NAME:      "{{ resend_sender_name }}"
					MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL:  "5s"
					MAILBOX_SERVICE_FORWARDER_STATE_PATH:     "/var/lib/mailbox-service/forwarder-state.json"
					SPIFFE_ENDPOINT_SOCKET:                   "unix://{{ spire_agent_socket_path }}"
					OTEL_EXPORTER_OTLP_ENDPOINT:              "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
				}
				restart: "always"
				hardening: {
					read_write_paths: ["/var/lib/mailbox-service"]
					private_devices:     false
					protect_clock:       false
					protect_kernel_logs: false
					restrict_realtime:   false
					restrict_suid_sgid:  false
					restrict_namespaces: true
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
			bootstrap: ["mailbox_state"]
		}
	}
	object_storage_service: {
		converge: {
			enabled:    true
			deploy_tag: "object_storage_service"
			order:      70
			directories: [
				{path: "/etc/credstore/object-storage-service", owner: "root", group: "object_storage_service", mode: "0750"},
				{path: "/etc/object-storage-service", owner: "root", group: "object_storage_service", mode: "0750"},
				{path: "/etc/object-storage-service/tls", owner: "root", group: "object_storage_service", mode: "0750"},
				{path: "/etc/verself", owner: "root", group: "root", mode: "0755"},
				{path: "/etc/verself/local-cas", owner: "root", group: "root", mode: "0755"},
				{path: "/var/lib/object-storage-service", owner: "object_storage_service", group: "object_storage_service", mode: "0750"},
				{path: "/var/lib/object-storage-admin", owner: "object_storage_admin", group: "object_storage_admin", mode: "0750"},
			]
			secret_refs: [
				{name: "pg-password", path: "/etc/credstore/object-storage-service/pg-password", owner: "root", group: "object_storage_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}},
				{name: "credential-kek", path: "/etc/credstore/object-storage-service/credential-kek", owner: "root", group: "object_storage_service", mode: "0640", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "hexdigits"}}},
			]
			clickhouse: {
				user:            "object_storage_service"
				spiffe_identity: "service"
				grants: [
					{action: "INSERT", table: "verself.object_access_events"},
					{action: "SELECT", table: "verself.object_access_events"},
				]
			}
			systemd: units: [
				{
					name:        "object-storage-service"
					description: "Object Storage Service"
					user:        "object_storage_service"
					group:       "object_storage_service"
					uid:         config.object_storage.object_storage_service_uid
					home:        "/var/lib/object-storage-service"
					exec:        "{{ verself_bin }}/object-storage-service"
					after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "secrets-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
					wants: ["postgresql.service", "clickhouse-server.service", "secrets-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
					supplementary_groups: ["{{ spire_workload_group }}"]
					load_credentials: [
						{name: "credential-kek", path: "/etc/credstore/object-storage-service/credential-kek"},
						{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"},
						{name: "s3-tls-cert", path: "/etc/object-storage-service/tls/server-cert.pem"},
						{name: "s3-tls-key", path: "/etc/object-storage-service/tls/server-key.pem"},
					]
					environment: {
						OBJECT_STORAGE_ROLE:           "s3"
						OBJECT_STORAGE_S3_LISTEN_ADDR: "{{ topology_endpoints.object_storage_service.endpoints.public_http.address }}"
						OBJECT_STORAGE_PG_DSN:         "postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"
						OBJECT_STORAGE_CH_ADDRESS:     "{{ topology_endpoints.clickhouse.endpoints.native_tls.address }}"
						OBJECT_STORAGE_CH_USER:        "object_storage_service"
						OBJECT_STORAGE_SECRETS_URL:    "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
						OBJECT_STORAGE_GARAGE_S3_URLS: "{{ component_object_storage_garage_s3_urls }}"
						OBJECT_STORAGE_GARAGE_REGION:  "garage"
						SPIFFE_ENDPOINT_SOCKET:        "unix://{{ spire_agent_socket_path }}"
						OTEL_EXPORTER_OTLP_ENDPOINT:   "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
					}
					hardening: {
						read_write_paths: ["/var/lib/object-storage-service"]
						private_devices:     false
						protect_clock:       false
						protect_kernel_logs: false
						restrict_realtime:   false
						restrict_suid_sgid:  false
						restrict_namespaces: true
					}
					readiness: [{kind: "http", endpoint: "public_http", path: "/healthz", scheme: "https", ca_path: "/etc/verself/local-cas/object-storage-s3-ca.pem"}]
				},
				{
					name:        "object-storage-admin"
					description: "Object Storage Admin"
					user:        "object_storage_admin"
					group:       "object_storage_admin"
					uid:         config.object_storage.object_storage_admin_uid
					home:        "/var/lib/object-storage-admin"
					exec:        "{{ verself_bin }}/object-storage-service"
					after: ["verself-firewall.target", "network.target", "postgresql.service", "governance-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
					wants: ["postgresql.service", "governance-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
					supplementary_groups: ["object_storage_service", "{{ spire_workload_group }}"]
					load_credentials: [
						{name: "credential-kek", path: "/etc/credstore/object-storage-service/credential-kek"},
						{name: "garage-admin-token", path: "/etc/garage/admin-token"},
						{name: "garage-proxy-access-key-id", path: "/etc/credstore/object-storage-service/garage-proxy-access-key-id"},
					]
					environment: {
						OBJECT_STORAGE_ROLE:                 "admin"
						OBJECT_STORAGE_ADMIN_LISTEN_ADDR:    "{{ topology_endpoints.object_storage_service.endpoints.admin_http.address }}"
						OBJECT_STORAGE_PG_DSN:               "postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"
						OBJECT_STORAGE_GARAGE_ADMIN_URLS:    "{{ component_object_storage_garage_admin_urls }}"
						OBJECT_STORAGE_GARAGE_REGION:        "garage"
						OBJECT_STORAGE_GOVERNANCE_AUDIT_URL: "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
						SPIFFE_ENDPOINT_SOCKET:              "unix://{{ spire_agent_socket_path }}"
						OTEL_EXPORTER_OTLP_ENDPOINT:         "http://{{ topology_endpoints.otelcol.endpoints.otlp_grpc.address }}"
					}
					hardening: {
						read_write_paths: ["/var/lib/object-storage-admin"]
						private_devices:     false
						protect_clock:       false
						protect_kernel_logs: false
						restrict_realtime:   false
						restrict_suid_sgid:  false
						restrict_namespaces: true
					}
					readiness: [{kind: "tcp", endpoint: "admin_http"}]
				},
			]
			bootstrap: ["object_storage_tls", "object_storage_garage_proxy"]
		}
	}
}

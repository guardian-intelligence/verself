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
			units: [{
				name:        "billing-service"
				description: "Billing Service"
				user:        "billing"
				group:       "billing"
				exec:        "{{ verself_bin }}/billing-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "spire-agent.service", "secrets-service.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "spire-agent.service", "secrets-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"}]
				environment: {
					BILLING_TB_ADDRESS:    "{{ topology_endpoints.tigerbeetle.endpoints.client.address }}"
					BILLING_TB_CLUSTER_ID: "0"
					BILLING_SECRETS_URL:   "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
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
			bootstrap: [{
				name:   "billing_stripe_webhook"
				class:  "external_provider"
				reason: "Reconciles the Stripe webhook endpoint and runtime provider secret outside the generic service shape."
			}]
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
			units: [
				{
					name:        "sandbox-rental-service"
					description: "Sandbox Rental Service"
					user:        "sandbox_rental"
					group:       "sandbox_rental"
					exec:        "{{ verself_bin }}/sandbox-rental-service"
					requires_spiffe_sock: true
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
						SANDBOX_GOVERNANCE_AUDIT_URL:          "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
						SANDBOX_SECRETS_URL:                   "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
						SANDBOX_SOURCE_INTERNAL_URL:           "https://{{ topology_endpoints.source_code_hosting_service.endpoints.internal_https.address }}"
						SANDBOX_TEMPORAL_FRONTEND_ADDRESS:     "{{ topology_endpoints.temporal.endpoints.frontend_grpc.address }}"
						SANDBOX_TEMPORAL_NAMESPACE:            "sandbox-rental-service"
						SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING: "sandbox-rental-service.recurring-vm"
						SANDBOX_BILLING_URL:                   "https://{{ topology_endpoints.billing.endpoints.internal_https.address }}"
						SANDBOX_BILLING_RETURN_ORIGINS:        "https://{{ verself_domain }}"
						SANDBOX_PUBLIC_BASE_URL:               "https://{{ sandbox_rental_service_domain }}"
						SANDBOX_VM_ORCHESTRATOR_SOCKET:        "/run/vm-orchestrator/api.sock"
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
					requires_spiffe_sock: true
					after:       components.sandbox_rental.processes.recurring_worker.after
					wants:       components.sandbox_rental.processes.recurring_worker.wants
					supplementary_groups: ["{{ spire_workload_group }}"]
					environment: {
						SANDBOX_SOURCE_INTERNAL_URL:           "https://{{ topology_endpoints.source_code_hosting_service.endpoints.internal_https.address }}"
						SANDBOX_TEMPORAL_FRONTEND_ADDRESS:     "{{ topology_endpoints.temporal.endpoints.frontend_grpc.address }}"
						SANDBOX_TEMPORAL_NAMESPACE:            "sandbox-rental-service"
						SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING: "sandbox-rental-service.recurring-vm"
						VERSELF_PG_MAX_CONNS:                  "4"
						VERSELF_PG_MIN_CONNS:                  "1"
						VERSELF_PG_CONN_MAX_LIFETIME_SECONDS:  "1800"
						VERSELF_PG_CONN_MAX_IDLE_SECONDS:      "300"
					}
					hardening: {protect_system: "full"}
				},
			]
			bootstrap: [{
				name:   "sandbox_vm_socket_acl"
				class:  "security_audit"
				reason: "Asserts the vm-clients privileged group remains limited to the sandbox rental runtime."
			}]
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
			units: [{
				name:        "identity-service"
				description: "Identity Service"
				user:        "identity_service"
				group:       "identity_service"
				exec:        "{{ verself_bin }}/identity-service"
				requires_spiffe_sock: true
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
					IDENTITY_GOVERNANCE_AUDIT_URL: "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
					IDENTITY_ZITADEL_BASE_URL:     "http://{{ topology_endpoints.zitadel.endpoints.http.address }}"
					IDENTITY_ZITADEL_HOST:         "auth.{{ verself_domain }}"
				}
				readiness: [
					{kind: "http", endpoint: "public_http", path: "/readyz"},
					{kind: "tcp", endpoint: "internal_https"},
				]
			}]
			bootstrap: [{
				name:   "identity_zitadel_actions"
				class:  "identity_provider"
				reason: "Installs Zitadel Actions V2 targets and executions that are identity-provider control-plane state."
			}]
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
			units: [{
				name:        "governance-service"
				description: "Governance Service"
				user:        "governance_service"
				group:       "governance_service"
				exec:        "{{ verself_bin }}/governance-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [
					{name: "audit-hmac-key", path: "/etc/credstore/governance-service/audit-hmac-key"},
					{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"},
				]
				environment: {
					GOVERNANCE_IDENTITY_PG_DSN: "postgres://identity_service@/identity_service?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_BILLING_PG_DSN:  "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_SANDBOX_PG_DSN:  "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
					GOVERNANCE_EXPORT_DIR:      "/var/lib/governance-service/exports"
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
			units: [{
				name:        "secrets-service"
				description: "Secrets Service"
				user:        "secrets_service"
				group:       "secrets_service"
				exec:        "{{ verself_bin }}/secrets-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "openbao.service", "zitadel.service", "governance-service.service", "spire-agent.service"]
				wants: ["openbao.service", "zitadel.service", "governance-service.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [{name: "openbao-ca-cert", path: "/etc/openbao/tls/cert.pem"}]
				environment: {
					SECRETS_PLATFORM_ORG_ID:           "{{ component_secrets_platform_org_id }}"
					SECRETS_GOVERNANCE_AUDIT_URL:      "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
					SECRETS_OPENBAO_ADDR:              "https://{{ topology_endpoints.openbao.endpoints.api.address }}"
					SECRETS_OPENBAO_KV_PREFIX:         "kv"
					SECRETS_OPENBAO_TRANSIT_PREFIX:    "transit"
					SECRETS_OPENBAO_JWT_PREFIX:        "jwt"
					SECRETS_OPENBAO_SPIFFE_JWT_PREFIX: "spiffe-jwt"
					SECRETS_OPENBAO_WORKLOAD_AUDIENCE: "openbao"
					SECRETS_BILLING_URL:               "https://{{ topology_endpoints.billing.endpoints.internal_https.address }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
			bootstrap: [
				{
					name:   "secrets_platform_org"
					class:  "identity_lookup"
					reason: "Resolves the platform organization ID from Zitadel until identity object lookups become a generic projection."
				},
				{
					name:   "openbao_tenancy"
					class:  "secret_backend"
					reason: "Reconciles OpenBao mounts, auth backends, policies, and platform runtime secrets."
				},
			]
		}
	}
	profile_service: {
		// Nomad-supervised: converge_component.yml branches on
		// component.deployment.supervisor and submits the rendered job
		// spec via tasks/component/nomad_submit.yml instead of installing
		// a systemd unit. The systemd block below is kept as the source
		// of truth for env vars / dependency wiring; both renderers read
		// from it.
		deployment: supervisor: "nomad"
		converge: {
			enabled:    true
			deploy_tag: "profile_service"
			order:      30
			directories: [{path: "/etc/credstore/profile-service", owner: "root", group: "profile_service", mode: "0750"}]
			secret_refs: [{name: "pg-password", path: "/etc/credstore/profile-service/pg-password", owner: "root", group: "profile_service", mode: "0640", expose_as: "postgres_password", source: {kind: "generated", generate: {kind: "password", length: 64, chars: "ascii_letters,digits"}}}]
			auth: {kind: "identity_project_audience", project_name: "identity-service"}
			units: [{
				name:        "profile-service"
				description: "Profile Service"
				user:        "profile_service"
				group:       "profile_service"
				exec:        "{{ verself_bin }}/profile-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "postgresql.service", "zitadel.service", "spire-agent.service", "identity-service.service", "governance-service.service"]
				wants: ["postgresql.service", "zitadel.service", "spire-agent.service", "identity-service.service", "governance-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				// auth-discovery /etc/hosts override is satisfied by
				// merging the entries into the host's /etc/hosts via the
				// base role; raw_exec inherits that, no per-task mount
				// namespace required.
				environment: {
					PROFILE_IDENTITY_INTERNAL_URL: "https://{{ topology_endpoints.identity_service.endpoints.internal_https.address }}"
					PROFILE_GOVERNANCE_AUDIT_URL:  "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
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
			units: [{
				name:        "notifications-service"
				description: "Notifications Service"
				user:        "notifications_service"
				group:       "notifications_service"
				exec:        "{{ verself_bin }}/notifications-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "postgresql.service", "clickhouse-server.service", "nats.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "clickhouse-server.service", "nats.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [{name: "clickhouse-ca-cert", path: "/etc/clickhouse-server/tls/server-ca.pem"}]
				environment: {
					NOTIFICATIONS_NATS_URL: "tls://{{ topology_endpoints.nats.endpoints.client.address }}"
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
			units: [{
				name:        "projects-service"
				description: "Projects Service"
				user:        "projects_service"
				group:       "projects_service"
				exec:        "{{ verself_bin }}/projects-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "postgresql.service", "zitadel.service", "spire-agent.service"]
				wants: ["postgresql.service", "zitadel.service", "spire-agent.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
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
			units: [{
				name:        "source-code-hosting-service"
				description: "Source Code Hosting Service"
				user:        "source_code_hosting_service"
				group:       "source_code_hosting_service"
				exec:        "{{ verself_bin }}/source-code-hosting-service"
				requires_spiffe_sock: true
				after: ["verself-firewall.target", "network.target", "postgresql.service", "zitadel.service", "spire-agent.service", "forgejo.service", "secrets-service.service", "projects-service.service"]
				wants: ["postgresql.service", "zitadel.service", "spire-agent.service", "forgejo.service", "secrets-service.service", "projects-service.service"]
				supplementary_groups: ["{{ spire_workload_group }}"]
				bind_read_only_paths: ["/etc/verself/auth-discovery-hosts:/etc/hosts"]
				load_credentials: [
					{name: "forgejo-token", path: "/etc/credstore/forgejo/automation-token"},
					{name: "webhook-secret", path: "/etc/credstore/source-code-hosting-service/webhook-secret"},
				]
				environment: {
					SOURCE_FORGEJO_BASE_URL:      "http://{{ topology_endpoints.forgejo.endpoints.http.address }}"
					SOURCE_FORGEJO_OWNER:         "forgejo-automation"
					SOURCE_SANDBOX_INTERNAL_URL:  "https://{{ topology_endpoints.sandbox_rental.endpoints.internal_https.address }}"
					SOURCE_SECRETS_INTERNAL_URL:  "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
					SOURCE_PROJECTS_INTERNAL_URL: "https://{{ topology_endpoints.projects_service.endpoints.internal_https.address }}"
					SOURCE_IDENTITY_INTERNAL_URL: "https://{{ topology_endpoints.identity_service.endpoints.internal_https.address }}"
					SOURCE_PUBLIC_BASE_URL:       "https://{{ forgejo_domain }}"
				}
				readiness: [{kind: "http", endpoint: "public_http", path: "/readyz"}]
			}]
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
			units: [{
				name:        "mailbox-service"
				description: "Mailbox Service"
				user:        "mailbox_service"
				group:       "mailbox_service"
				exec:        "{{ verself_bin }}/mailbox-service"
				requires_spiffe_sock: true
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
					MAILBOX_SERVICE_STALWART_BASE_URL:        "http://{{ topology_endpoints.stalwart.endpoints.http.address }}"
					MAILBOX_SERVICE_STALWART_PUBLIC_BASE_URL: "https://{{ stalwart_domain }}"
					MAILBOX_SERVICE_STALWART_MAILBOX:         "ceo"
					MAILBOX_SERVICE_STALWART_LOCAL_DOMAIN:    "{{ verself_domain }}"
					MAILBOX_SERVICE_SECRETS_URL:              "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
					MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL:  "2m"
					MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL:  "10m"
					MAILBOX_SERVICE_FORWARDER_FROM_ADDRESS:   "{{ resend_sender_address }}"
					MAILBOX_SERVICE_FORWARDER_FROM_NAME:      "{{ resend_sender_name }}"
					MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL:  "5s"
					MAILBOX_SERVICE_FORWARDER_STATE_PATH:     "/var/lib/mailbox-service/forwarder-state.json"
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
			units: [
				{
					name:        "object-storage-service"
					description: "Object Storage Service"
					user:        "object_storage_service"
					group:       "object_storage_service"
					uid:         config.ansible_vars.object_storage_service_uid
					home:        "/var/lib/object-storage-service"
					exec:        "{{ verself_bin }}/object-storage-service"
					requires_spiffe_sock: true
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
						OBJECT_STORAGE_SECRETS_URL:    "https://{{ topology_endpoints.secrets_service.endpoints.internal_https.address }}"
						OBJECT_STORAGE_GARAGE_S3_URLS: "{{ component_object_storage_garage_s3_urls }}"
						OBJECT_STORAGE_GARAGE_REGION:  "garage"
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
					uid:         config.ansible_vars.object_storage_admin_uid
					home:        "/var/lib/object-storage-admin"
					exec:        "{{ verself_bin }}/object-storage-service"
					requires_spiffe_sock: true
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
						OBJECT_STORAGE_GARAGE_ADMIN_URLS:    "{{ component_object_storage_garage_admin_urls }}"
						OBJECT_STORAGE_GARAGE_REGION:        "garage"
						OBJECT_STORAGE_GOVERNANCE_AUDIT_URL: "https://{{ topology_endpoints.governance_service.endpoints.internal_https.address }}"
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
			bootstrap: [
				{
					name:   "object_storage_tls"
					class:  "storage_provider"
					reason: "Creates the local S3 TLS CA, server certificate, and operator CA bundle for Garage proxy traffic."
				},
				{
					name:   "object_storage_garage_proxy"
					class:  "storage_provider"
					reason: "Creates Garage proxy credentials and syncs the runtime proxy secret through secrets-service."
				},
			]
		}
	}
}

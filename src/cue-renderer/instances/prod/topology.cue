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
		restart_units: [...string] | *[]
		...
	}
	...
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
		verdaccio: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "verdaccio", user: "verdaccio", group: "verdaccio"}
			artifact: {kind: "upstream_binary", output: "verdaccio", role: "verdaccio"}
			endpoints: http: {
				protocol: "http"
				// Firecracker guests reach the npm mirror through the host service
				// plane; nftables restricts this socket to guest TAP ingress.
				listen_host:            "0.0.0.0"
				wildcard_listen_reason: "Firecracker guests reach Verdaccio through the host service plane; nftables restricts this socket to guest TAP ingress."
				port:                   4873
				exposure:               "guest_host"
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
			runtime: {user: "billing", group: "billing"}
			identities: default: {ansible_var: "spire_billing_service_id", path: "/svc/billing-service", entry_id: "verself-billing-service"}
			endpoints: {
				public_http: port:    4242
				internal_https: port: 4255
			}
			postgres: {
				database:         "billing"
				owner:            "billing"
				connection_limit: 30
				pool: max_conns: 12
				password_ref: {kind: "ansible_var", name: "postgresql_billing_password"}
			}
		}
		sandbox_rental: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/sandbox-rental-service/cmd/sandbox-rental-service", output: "sandbox-rental-service", role: "sandbox_rental_service", bazel_label: "//src/sandbox-rental-service/cmd/sandbox-rental-service:sandbox-rental-service"}
			runtime: {user: "sandbox_rental", group: "sandbox_rental"}
			identities: default: {ansible_var: "spire_sandbox_rental_id", path: "/svc/sandbox-rental-service", entry_id: "verself-sandbox-rental-service"}
			processes: recurring_worker: {
				unit:  "sandbox-rental-recurring-worker"
				user:  "sandbox_rental"
				group: "sandbox_rental"
				artifact: {kind: "go_binary", package: "./src/sandbox-rental-service/cmd/sandbox-rental-recurring-worker", output: "sandbox-rental-recurring-worker", role: "sandbox_rental_service", bazel_label: "//src/sandbox-rental-service/cmd/sandbox-rental-recurring-worker:sandbox-rental-recurring-worker"}
				identities: ["default"]
				after: ["verself-firewall.target", "network.target", "postgresql.service", "temporal-server.service", "spire-agent.service", "otelcol.service", "source-code-hosting-service.service"]
				wants: ["postgresql.service", "temporal-server.service", "spire-agent.service", "otelcol.service", "source-code-hosting-service.service"]
				supplementary_groups: [config.spire.workload_group]
				requires_spiffe_sock: true
			}
			endpoints: {
				public_http: port:    4243
				internal_https: port: 4263
			}
			postgres: {
				database:         "sandbox_rental"
				owner:            "sandbox_rental"
				connection_limit: 30
				pool: max_conns: 16
				password_ref: {kind: "ansible_var", name: "postgresql_sandbox_rental_password"}
			}
		}
		identity_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/identity-service/cmd/identity-service", output: "identity-service", role: "identity_service", bazel_label: "//src/identity-service/cmd/identity-service:identity-service"}
			runtime: {user: "identity_service", group: "identity_service"}
			identities: default: {ansible_var: "spire_identity_service_id", path: "/svc/identity-service", entry_id: "verself-identity-service"}
			endpoints: {
				public_http: port:    4248
				internal_https: port: 4241
			}
			postgres: {
				database:         "identity_service"
				owner:            "identity_service"
				connection_limit: 10
				password_ref: {kind: "ansible_var", name: "postgresql_identity_service_password"}
			}
		}
		governance_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/governance-service/cmd/governance-service", output: "governance-service", role: "governance_service", bazel_label: "//src/governance-service/cmd/governance-service:governance-service"}
			runtime: {user: "governance_service", group: "governance_service"}
			identities: default: {ansible_var: "spire_governance_service_id", path: "/svc/governance-service", entry_id: "verself-governance-service"}
			endpoints: {
				public_http: port:    4250
				internal_https: port: 4254
			}
			postgres: {
				database:         "governance_service"
				owner:            "governance_service"
				connection_limit: 15
				password_ref: {kind: "secret_ref", expose_as: "postgres_password"}
			}
		}
		secrets_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/secrets-service/cmd/secrets-service", output: "secrets-service", role: "secrets_service", bazel_label: "//src/secrets-service/cmd/secrets-service:secrets-service"}
			runtime: {user: "secrets_service", group: "secrets_service"}
			identities: default: {ansible_var: "spire_secrets_service_id", path: "/svc/secrets-service", entry_id: "verself-secrets-service"}
			endpoints: {
				public_http: port:    4251
				internal_https: port: 4253
			}
			postgres: {}
		}
		profile_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/profile-service/cmd/profile-service", output: "profile-service", role: "profile_service", bazel_label: "//src/profile-service/cmd/profile-service:profile-service"}
			runtime: {user: "profile_service", group: "profile_service"}
			identities: default: {ansible_var: "spire_profile_service_id", path: "/svc/profile-service", entry_id: "verself-profile-service"}
			endpoints: {
				public_http: port:    4258
				internal_https: port: 4259
			}
			postgres: {
				database:         "profile"
				owner:            "profile_service"
				connection_limit: 10
				password_ref: {kind: "secret_ref", expose_as: "postgres_password"}
			}
		}
		notifications_service: #PublicGoService & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/notifications-service/cmd/notifications-service", output: "notifications-service", role: "notifications_service", bazel_label: "//src/notifications-service/cmd/notifications-service:notifications-service"}
			runtime: {user: "notifications_service", group: "notifications_service"}
			identities: default: {ansible_var: "spire_notifications_service_id", path: "/svc/notifications-service", entry_id: "verself-notifications-service"}
			endpoints: public_http: port: 4260
			postgres: {
				database:         "notifications_service"
				owner:            "notifications_service"
				connection_limit: 10
				password_ref: {kind: "secret_ref", expose_as: "postgres_password"}
			}
		}
		projects_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/projects-service/cmd/projects-service", output: "projects-service", role: "projects_service", bazel_label: "//src/projects-service/cmd/projects-service:projects-service"}
			runtime: {user: "projects_service", group: "projects_service"}
			identities: default: {ansible_var: "spire_projects_service_id", path: "/svc/projects-service", entry_id: "verself-projects-service"}
			endpoints: {
				public_http: port:    4264
				internal_https: port: 4265
			}
			postgres: {
				database:         "projects_service"
				owner:            "projects_service"
				connection_limit: 10
				password_ref: {kind: "secret_ref", expose_as: "postgres_password"}
			}
		}
		source_code_hosting_service: #PublicGoService & #InternalGoAPI & #DefaultSPIFFEIdentity & {
			artifact: {package: "./src/source-code-hosting-service/cmd/source-code-hosting-service", output: "source-code-hosting-service", role: "source_code_hosting_service", bazel_label: "//src/source-code-hosting-service/cmd/source-code-hosting-service:source-code-hosting-service"}
			runtime: {user: "source_code_hosting_service", group: "source_code_hosting_service"}
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
			postgres: {
				database:         "source_code_hosting"
				owner:            "source_code_hosting_service"
				connection_limit: 10
				password_ref: {kind: "secret_ref", expose_as: "postgres_password"}
			}
		}
		verself_web: #Frontend & {
			artifact: {package: "src/viteplus-monorepo/apps/verself-web", output: "verself-web", role: "verself_web", bazel_label: "//src/viteplus-monorepo/apps/verself-web:node_app_nomad_artifact"}
			runtime: {user: "verself-web", group: "verself-web"}
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
			runtime: {systemd: "bazel-remote", user: "bazel_remote", group: "bazel_remote"}
			artifact: {kind: "static_binary", output: "bazel-remote", role: "bazel_remote"}
			endpoints: {
				grpc: {
					protocol: "grpc"
					// bazel-remote serves both loopback and wg-ops clients; nftables
					// keeps 9092 off the public interface.
					listen_host:            "0.0.0.0"
					wildcard_listen_reason: "bazel-remote serves both loopback and wg-ops clients; nftables keeps the gRPC port off the public interface."
					port:                   9092
					exposure:               "wireguard"
				}
				http: {
					protocol: "http"
					// bazel-remote serves both loopback and wg-ops clients; nftables
					// keeps 8080 off the public interface.
					listen_host:            "0.0.0.0"
					wildcard_listen_reason: "bazel-remote serves both loopback and wg-ops clients; nftables keeps the HTTP port off the public interface."
					port:                   8080
					exposure:               "wireguard"
				}
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
		nomad: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "nomad", user: "root", group: "root"}
			artifact: {kind: "static_binary", output: "nomad", role: "nomad"}
			endpoints: http: {protocol: "http", port: 4646, exposure: "loopback"}
			interfaces: api: {kind: "admin_api", endpoint: "http", auth: "operator"}
			// nomad-deploy is the controller-side wrapper around the
			// Nomad API. It publishes Bazel-built artifacts, submits the
			// rendered job spec, and waits for the deployment result.
			tools: deploy: {kind: "go_binary", package: "./src/cue-renderer/cmd/nomad-deploy", output: "nomad-deploy", role: "nomad", bazel_label: "//src/cue-renderer/cmd/nomad-deploy:nomad-deploy"}
		}
		nomad_artifacts: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "caddy", user: "caddy", group: "caddy"}
			artifact: {kind: "static_binary", output: "caddy", role: "caddy"}
			// Nomad reserves 4646-4648 for HTTP, RPC, and Serf.
			endpoints: http: {protocol: "http", port: 14647, exposure: "loopback"}
			interfaces: http: {kind: "resource_protocol", endpoint: "http", auth: "none"}
		}
		stalwart: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "stalwart", user: "stalwart", group: "stalwart"}
			artifact: {kind: "static_binary", output: "stalwart-mail", role: "stalwart"}
			endpoints: {
				smtp: {
					protocol: "smtp"
					// Public SMTP must bind externally; nftables and Stalwart own the
					// exposed TCP/25 contract.
					listen_host:            "0.0.0.0"
					wildcard_listen_reason: "SMTP accepts public internet mail on TCP/25; nftables exposes that port intentionally."
					port:                   25
					exposure:               "public"
				}
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
			runtime: {user: "mailbox_service", group: "mailbox_service"}
			identities: default: {ansible_var: "spire_mailbox_service_id", path: "/svc/mailbox-service", entry_id: "verself-mailbox-service"}
			endpoints: public_http: port: 4246
			postgres: {
				database:         "mailbox_service"
				owner:            "mailbox_service"
				connection_limit: 10
				password_ref: {kind: "ansible_var", name: "postgresql_mailbox_service_password"}
			}
		}
		object_storage_service: #PublicGoService & {
			artifact: {package: "./src/object-storage-service/cmd/object-storage-service", output: "object-storage-service", role: "object_storage_service", bazel_label: "//src/object-storage-service/cmd/object-storage-service:object-storage-service"}
			runtime: {user: "object_storage_service", group: "object_storage_service"}
			identities: {
				service: {ansible_var: "spire_object_storage_service_id", path: "/svc/object-storage-service", user: "object_storage_service", group: "object_storage_service", uid_policy: {kind: "fixed", value: config.ansible_vars.object_storage_service_uid}, entry_id: "verself-object-storage-service", restart_units: []}
				admin: {ansible_var: "spire_object_storage_admin_id", path: "/svc/object-storage-admin", user: "object_storage_admin", group: "object_storage_admin", uid_policy: {kind: "fixed", value: config.ansible_vars.object_storage_admin_uid}, entry_id: "verself-object-storage-admin", restart_units: []}
			}
			tools: secret_sync: {kind: "go_binary", package: "./src/object-storage-service/cmd/object-storage-secret-sync", output: "object-storage-secret-sync", role: "object_storage_service", bazel_label: "//src/object-storage-service/cmd/object-storage-secret-sync:object-storage-secret-sync"}
			processes: admin: {
				unit:  "object-storage-admin"
				user:  "object_storage_admin"
				group: "object_storage_admin"
				artifact: {kind: "go_binary", package: "./src/object-storage-service/cmd/object-storage-service", output: "object-storage-service", role: "object_storage_service", bazel_label: "//src/object-storage-service/cmd/object-storage-service:object-storage-service"}
				endpoints: ["admin_http"]
				identities: ["service", "admin"]
				after: ["verself-firewall.target", "network.target", "postgresql.service", "governance-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
				wants: ["postgresql.service", "governance-service.service", "garage@0.service", "garage@1.service", "garage@2.service", "spire-agent.service"]
				supplementary_groups: ["object_storage_service", config.spire.workload_group]
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
			postgres: {
				database:         "object_storage_service"
				owner:            "object_storage_service"
				connection_limit: 10
				pool: max_conns: 12
				password_ref: {kind: "secret_ref", expose_as: "postgres_password"}
			}
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
			}
		}
		company: #Frontend & {
			artifact: {package: "src/viteplus-monorepo/apps/company", output: "company", role: "company", bazel_label: "//src/viteplus-monorepo/apps/company:node_app_nomad_artifact"}
			runtime: {user: "company", group: "company"}
			endpoints: http: port: 4252
		}
		firecracker_host_service: {
			kind: "privileged_daemon"
			host: "10.255.0.1"
			runtime: {systemd: "vm-orchestrator", user: "root", group: "root"}
			artifact: {kind: "go_binary", package: "./src/vm-orchestrator/cmd/vm-orchestrator", output: "vm-orchestrator", role: "firecracker", bazel_label: "//src/vm-orchestrator/cmd/vm-orchestrator:vm-orchestrator"}
			tools: cli: {kind: "go_binary", package: "./src/vm-orchestrator/cmd/vm-orchestrator-cli", output: "vm-orchestrator-cli", role: "firecracker", bazel_label: "//src/vm-orchestrator/cmd/vm-orchestrator-cli:vm-orchestrator-cli"}
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
		{kind: "browser_origin", gateway: "public_caddy", host: "@", to: {component: "verself_web", interface: "frontend"}, waf: "detection", browser_cors: "same_origin"},
		{kind: "browser_origin", gateway: "public_caddy", zone: "company", host: "@", to: {component: "company", interface: "frontend"}, waf: "detection", browser_cors: "same_origin"},
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
		{kind: "protocol_origin", gateway: "public_caddy", host: "git", to: {component: "source_code_hosting_service", interface: "git_smart_http"}, waf: "detection", max_body_bytes: 1048576},
		{kind: "protocol_origin", gateway: "public_caddy", host: "auth", to: {component: "zitadel", interface: "oidc"}, waf: "detection"},
		{kind: "protocol_origin", gateway: "public_caddy", host: "mail", to: {component: "stalwart", interface: "jmap"}, waf: "detection"},
		{kind: "protocol_origin", gateway: "direct_smtp", host: "mail", to: {component: "stalwart", interface: "smtp"}, waf: "off"},
		{kind: "guest_host_route", gateway: "firecracker_host", host: "10.255.0.1", paths: ["/internal/sandbox/v1/github-runner-jit", "/internal/sandbox/v1/runner-bootstrap", "/internal/sandbox/v1/stickydisk/*", "/internal/sandbox/v1/github-checkout/bundle"], to: {component: "sandbox_rental", interface: "public_api"}, waf: "off"},
		{kind: "guest_host_route", gateway: "firecracker_host", host: "10.255.0.1", paths: ["/api/actions", "/api/actions/*", "/{owner}/{repo}.git/info/refs", "/{owner}/{repo}.git/git-upload-pack"], to: {component: "forgejo", interface: "forgejo_http"}, waf: "off"},
	]

	edges: [
		{from: "verself_web", to: {component: "billing", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "verself_web", to: {component: "sandbox_rental", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "verself_web", to: {component: "identity_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "verself_web", to: {component: "profile_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "verself_web", to: {component: "notifications_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "verself_web", to: {component: "projects_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
		{from: "verself_web", to: {component: "source_code_hosting_service", interface: "public_api"}, auth: "zitadel_jwt", purpose: "server_functions"},
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
}

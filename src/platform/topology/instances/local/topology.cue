package topology

import (
	"list"

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
		otelcol: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "otelcol"}
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
		grafana: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "grafana", user: "grafana", group: "grafana"}
			artifact: {kind: "upstream_binary", output: "grafana", role: "grafana"}
			endpoints: http: {
				protocol: "http"
				port:     4300
				exposure: "loopback"
			}
			interfaces: operator_ui: {kind: "frontend_http", endpoint: "http", auth: "operator"}
		}
		temporal_web: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "temporal-web", user: "temporal", group: "temporal"}
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
		nats: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "nats", user: "nats", group: "nats"}
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
			runtime: {systemd: "temporal-server", user: "temporal", group: "temporal"}
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
			postgres: {database: "temporal", owner: "temporal"}
		}
		billing: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/billing-service/cmd/billing-service", output: "billing-service", role: "billing_service"}
			runtime: {systemd: "billing-service", user: "billing", group: "billing"}
			endpoints: {
				public_http: port:    4242
				internal_https: port: 4255
			}
			postgres: {database: "billing", owner: "billing"}
		}
		sandbox_rental: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/sandbox-rental-service/cmd/sandbox-rental-service", output: "sandbox-rental-service", role: "sandbox_rental_service"}
			runtime: {systemd: "sandbox-rental-service", user: "sandbox_rental", group: "sandbox_rental"}
			endpoints: {
				public_http: port:    4243
				internal_https: port: 4263
			}
			postgres: {database: "sandbox_rental", owner: "sandbox_rental"}
		}
		identity_service: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/identity-service/cmd/identity-service", output: "identity-service", role: "identity_service"}
			runtime: {systemd: "identity-service", user: "identity_service", group: "identity_service"}
			endpoints: {
				public_http: port:    4248
				internal_https: port: 4241
			}
			postgres: {database: "identity_service", owner: "identity_service"}
		}
		governance_service: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/governance-service/cmd/governance-service", output: "governance-service", role: "governance_service"}
			runtime: {systemd: "governance-service", user: "governance_service", group: "governance_service"}
			endpoints: {
				public_http: port:    4250
				internal_https: port: 4254
			}
			postgres: {database: "governance_service", owner: "governance_service"}
		}
		secrets_service: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/secrets-service/cmd/secrets-service", output: "secrets-service", role: "secrets_service"}
			runtime: {systemd: "secrets-service", user: "secrets_service", group: "secrets_service"}
			endpoints: {
				public_http: port:    4251
				internal_https: port: 4253
			}
			postgres: {database: "secrets_service", owner: "secrets_service"}
		}
		profile_service: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/profile-service/cmd/profile-service", output: "profile-service", role: "profile_service"}
			runtime: {systemd: "profile-service", user: "profile_service", group: "profile_service"}
			endpoints: {
				public_http: port:    4258
				internal_https: port: 4259
			}
			postgres: {database: "profile_service", owner: "profile_service"}
		}
		notifications_service: #PublicGoService & {
			artifact: {package: "./src/notifications-service/cmd/notifications-service", output: "notifications-service", role: "notifications_service"}
			runtime: {systemd: "notifications-service", user: "notifications_service", group: "notifications_service"}
			endpoints: public_http: port: 4260
			postgres: {database: "notifications_service", owner: "notifications_service"}
		}
		projects_service: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/projects-service/cmd/projects-service", output: "projects-service", role: "projects_service"}
			runtime: {systemd: "projects-service", user: "projects_service", group: "projects_service"}
			endpoints: {
				public_http: port:    4264
				internal_https: port: 4265
			}
			postgres: {database: "projects_service", owner: "projects_service"}
		}
		source_code_hosting_service: #PublicGoService & #InternalGoAPI & {
			artifact: {package: "./src/source-code-hosting-service/cmd/source-code-hosting-service", output: "source-code-hosting-service", role: "source_code_hosting_service"}
			runtime: {systemd: "source-code-hosting-service", user: "source_code_hosting_service", group: "source_code_hosting_service"}
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
			postgres: {database: "source_code_hosting_service", owner: "source_code_hosting_service"}
		}
		console: #Frontend & {
			artifact: {package: "src/viteplus-monorepo/apps/console", output: "console", role: "console"}
			runtime: {systemd: "console", user: "console", group: "console"}
			endpoints: http: port: 4244
			postgres: {database: "frontend_auth", owner: "frontend_auth"}
		}
		electric: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "electric", user: "electric", group: "electric"}
			artifact: {kind: "upstream_binary", output: "electric", role: "electric"}
			endpoints: http: {protocol: "http", port: 3010, exposure: "loopback"}
			interfaces: shape_api: {kind: "resource_protocol", endpoint: "http", auth: "shared_secret"}
		}
		zitadel: {
			kind: "protocol_backend"
			host: "127.0.0.1"
			runtime: {systemd: "zitadel", user: "zitadel", group: "zitadel"}
			artifact: {kind: "static_binary", output: "zitadel", role: "zitadel"}
			endpoints: http: {protocol: "http", port: 8085, exposure: "loopback"}
			interfaces: oidc: {kind: "protocol", endpoint: "http", auth: "none", probes: #ServiceProbes}
			postgres: {database: "zitadel", owner: "zitadel"}
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
			postgres: {database: "stalwart", owner: "stalwart"}
		}
		mailbox_service: #PublicGoService & {
			artifact: {package: "./src/mailbox-service/cmd/mailbox-service", output: "mailbox-service", role: "mailbox_service"}
			runtime: {systemd: "mailbox-service", user: "mailbox_service", group: "mailbox_service"}
			endpoints: public_http: port: 4246
			postgres: {database: "mailbox_service", owner: "mailbox_service"}
		}
		object_storage_service: #PublicGoService & {
			artifact: {package: "./src/object-storage-service/cmd/object-storage-service", output: "object-storage-service", role: "object_storage_service"}
			runtime: {systemd: "object-storage-service", user: "object_storage_service", group: "object_storage_service"}
			endpoints: {
				public_http: port: 4256
				admin_http: {
					protocol: "https"
					port:     4257
					exposure: "loopback"
				}
			}
			interfaces: admin_api: {kind: "admin_api", endpoint: "admin_http", auth: "spiffe_mtls", probes: #ServiceProbes}
			postgres: {database: "object_storage_service", owner: "object_storage_service"}
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
		}
		electric_notifications: {
			kind: "resource"
			host: "127.0.0.1"
			runtime: {systemd: "electric-notifications", user: "electric", group: "electric"}
			artifact: {kind: "upstream_binary", output: "electric", role: "electric"}
			endpoints: http: {protocol: "http", port: 3012, exposure: "loopback"}
			interfaces: shape_api: {kind: "resource_protocol", endpoint: "http", auth: "shared_secret"}
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
		{kind: "browser_origin", gateway: "public_caddy", host: "company_domain", to: {component: "company", interface: "frontend"}, waf: "detection", browser_cors: "same_origin"},
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
		{kind: "guest_host_route", gateway: "firecracker_host", host: "10.255.0.1", paths: ["/api/actions", "/api/actions/*"], to: {component: "forgejo", interface: "forgejo_http"}, waf: "off"},
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
				host:    route.host
				kind:    route.kind
				gateway: route.gateway
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
		topology_spire: {
			identities: [
				for componentName, componentValue in topology.components
				if componentValue.runtime.spiffe_id != "" {
					component: componentName
					spiffe_id: componentValue.runtime.spiffe_id
					user:      componentValue.runtime.user
					group:     componentValue.runtime.group
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
	proof: {
		topology_proof: {
			evidence: topology.evidence
		}
	}
}

package topology

topology: {
	_sandbox_private_clone_ipv4: ["0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4"]
	_sandbox_private_clone_ipv6: ["::/128", "::1/128", "::ffff:0:0/96", "64:ff9b::/96", "100::/64", "2001::/23", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8"]

	nftables: rulesets: {
		billing: {
			target:    "/etc/nftables.d/billing.nft"
			table:     "verself_billing"
			component: "billing"
			output: {
				user: topology.components.billing.runtime.user
				rules: [
					{kind: "accept_loopback_all"},
					{kind: "accept_port", protocol: "tcp", port: 443},
					{kind: "accept_port", protocol: "tcp", port: 53},
					{kind: "accept_port", protocol: "udp", port: 53},
				]
			}
		}
		company: {
			target:    "/etc/nftables.d/company.nft"
			table:     "verself_company"
			component: "company"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "company", endpoint: "http"}]}]
		}
		console: {
			target:    "/etc/nftables.d/console.nft"
			table:     "verself_console"
			component: "console"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "console", endpoint: "http"}]}]
		}
		electric: {
			target:    topology.components.electric.electric.nftables_file
			table:     topology.components.electric.electric.nftables_table
			component: "electric"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "electric", endpoint: "http"}]}]
		}
		electric_mail: {
			target:    topology.components.electric_mail.electric.nftables_file
			table:     topology.components.electric_mail.electric.nftables_table
			component: "electric_mail"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "electric_mail", endpoint: "http"}]}]
		}
		electric_notifications: {
			target:    topology.components.electric_notifications.electric.nftables_file
			table:     topology.components.electric_notifications.electric.nftables_table
			component: "electric_notifications"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "electric_notifications", endpoint: "http"}]}]
		}
		garage: {
			target:    "/etc/nftables.d/garage.nft"
			table:     "verself_garage"
			component: "garage"
			input: [{kind: "drop_non_loopback", endpoints: [
				{component: "garage", endpoint: "s3_0"},
				{component: "garage", endpoint: "rpc_0"},
				{component: "garage", endpoint: "admin_0"},
				{component: "garage", endpoint: "s3_1"},
				{component: "garage", endpoint: "rpc_1"},
				{component: "garage", endpoint: "admin_1"},
				{component: "garage", endpoint: "s3_2"},
				{component: "garage", endpoint: "rpc_2"},
				{component: "garage", endpoint: "admin_2"},
			]}]
			output: {
				established: true
				final:       "none"
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "garage", endpoint: "admin_0"}, {component: "garage", endpoint: "admin_1"}, {component: "garage", endpoint: "admin_2"}], skuid: 0},
					{kind: "accept_loopback_endpoints", endpoints: [{component: "garage", endpoint: "admin_0"}, {component: "garage", endpoint: "admin_1"}, {component: "garage", endpoint: "admin_2"}], skuid: config.object_storage.object_storage_admin_uid},
					{kind: "drop_loopback_endpoints", endpoints: [{component: "garage", endpoint: "admin_0"}, {component: "garage", endpoint: "admin_1"}, {component: "garage", endpoint: "admin_2"}]},
					{kind: "accept_loopback_endpoints", endpoints: [{component: "garage", endpoint: "s3_0"}, {component: "garage", endpoint: "s3_1"}, {component: "garage", endpoint: "s3_2"}], skuid: config.object_storage.object_storage_service_uid},
					{kind: "accept_loopback_endpoints", endpoints: [{component: "garage", endpoint: "s3_0"}, {component: "garage", endpoint: "s3_1"}, {component: "garage", endpoint: "s3_2"}], skuid: config.object_storage.object_storage_admin_uid},
					{kind: "drop_loopback_endpoints", endpoints: [{component: "garage", endpoint: "s3_0"}, {component: "garage", endpoint: "s3_1"}, {component: "garage", endpoint: "s3_2"}]},
					{kind: "accept_loopback_endpoints", endpoints: [{component: "garage", endpoint: "rpc_0"}, {component: "garage", endpoint: "rpc_1"}, {component: "garage", endpoint: "rpc_2"}], skuid: topology.components.garage.runtime.user},
					{kind: "drop_loopback_endpoints", endpoints: [{component: "garage", endpoint: "rpc_0"}, {component: "garage", endpoint: "rpc_1"}, {component: "garage", endpoint: "rpc_2"}]},
				]
			}
		}
		governance_service: {
			target:    "/etc/nftables.d/governance-service.nft"
			table:     "verself_governance_service"
			component: "governance_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "governance_service", endpoint: "public_http"}]}]
			output: {
				user: topology.components.governance_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "clickhouse", endpoint: "native_tls"}, {component: "openbao", endpoint: "api"}, {component: "profile_service", endpoint: "internal_https"}, {component: "zitadel", endpoint: "http"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		identity_service: {
			target:    "/etc/nftables.d/identity-service.nft"
			table:     "verself_identity_service"
			component: "identity_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "identity_service", endpoint: "public_http"}, {component: "identity_service", endpoint: "internal_https"}]}]
			output: {
				user: topology.components.identity_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "clickhouse", endpoint: "native_tls"}, {component: "zitadel", endpoint: "http"}, {component: "governance_service", endpoint: "internal_https"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		mailbox_service: {
			target:    "/etc/nftables.d/mailbox-service.nft"
			table:     "verself_mailbox_service"
			component: "mailbox_service"
			output: {
				user: topology.components.mailbox_service.runtime.user
				rules: [
					{kind: "accept_loopback_all"},
					{kind: "accept_port", protocol: "tcp", port: 443},
					{kind: "accept_port", protocol: "tcp", port: 53},
					{kind: "accept_port", protocol: "udp", port: 53},
				]
			}
		}
		nats: {
			target:    "/etc/nftables.d/nats.nft"
			table:     "verself_nats"
			component: "nats"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "nats", endpoint: "client"}, {component: "nats", endpoint: "monitoring"}]}]
			output: {
				user: topology.components.nats.runtime.user
				rules: [{kind: "accept_non_tcp_udp"}]
			}
		}
		notifications_service: {
			target:    "/etc/nftables.d/notifications-service.nft"
			table:     "verself_notifications_service"
			component: "notifications_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "notifications_service", endpoint: "public_http"}]}]
			output: {
				user: topology.components.notifications_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "clickhouse", endpoint: "native_tls"}, {component: "nats", endpoint: "client"}, {component: "zitadel", endpoint: "http"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		object_storage_admin: {
			target:    "/etc/nftables.d/object-storage-admin.nft"
			table:     "verself_object_storage_admin"
			component: "object_storage_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "object_storage_service", endpoint: "admin_http"}]}]
			output: {
				user: "object_storage_admin"
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "object_storage_service", endpoint: "public_http"}, {component: "object_storage_service", endpoint: "admin_http"}, {component: "postgresql", endpoint: "postgres"}, {component: "governance_service", endpoint: "internal_https"}, {component: "secrets_service", endpoint: "internal_https"}, {component: "otelcol", endpoint: "otlp_grpc"}, {component: "garage", endpoint: "s3_0"}, {component: "garage", endpoint: "s3_1"}, {component: "garage", endpoint: "s3_2"}, {component: "garage", endpoint: "admin_0"}, {component: "garage", endpoint: "admin_1"}, {component: "garage", endpoint: "admin_2"}]},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		object_storage_service: {
			target:    "/etc/nftables.d/object-storage-service.nft"
			table:     "verself_object_storage_service"
			component: "object_storage_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "object_storage_service", endpoint: "public_http"}]}]
			output: {
				user: topology.components.object_storage_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "object_storage_service", endpoint: "public_http"}, {component: "postgresql", endpoint: "postgres"}, {component: "clickhouse", endpoint: "native_tls"}, {component: "secrets_service", endpoint: "internal_https"}, {component: "otelcol", endpoint: "otlp_grpc"}, {component: "garage", endpoint: "s3_0"}, {component: "garage", endpoint: "s3_1"}, {component: "garage", endpoint: "s3_2"}]},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		openbao: {
			target:    "/etc/nftables.d/openbao.nft"
			table:     "verself_openbao"
			component: "openbao"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "openbao", endpoint: "api"}, {component: "openbao", endpoint: "cluster"}]}]
			output: {
				user: topology.components.openbao.runtime.user
				rules: [
					{kind: "accept_non_tcp_udp"},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_loopback_endpoints", endpoints: [{component: "spire_bundle_endpoint", endpoint: "bundle"}]},
				]
			}
		}
		platform: {
			target:    "/etc/nftables.d/platform.nft"
			table:     "verself_platform"
			component: "platform"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "platform", endpoint: "http"}]}]
		}
		profile_service: {
			target:    "/etc/nftables.d/profile-service.nft"
			table:     "verself_profile_service"
			component: "profile_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "profile_service", endpoint: "public_http"}, {component: "profile_service", endpoint: "internal_https"}]}]
			output: {
				user: topology.components.profile_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "identity_service", endpoint: "internal_https"}, {component: "governance_service", endpoint: "internal_https"}, {component: "zitadel", endpoint: "http"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		projects_service: {
			target:    "/etc/nftables.d/projects-service.nft"
			table:     "verself_projects_service"
			component: "projects_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "projects_service", endpoint: "public_http"}, {component: "projects_service", endpoint: "internal_https"}]}]
			output: {
				user: topology.components.projects_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "zitadel", endpoint: "http"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		sandbox_rental: {
			target:    "/etc/nftables.d/sandbox-rental.nft"
			table:     "verself_sandbox_rental"
			component: "sandbox_rental"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "sandbox_rental", endpoint: "public_http"}, {component: "sandbox_rental", endpoint: "internal_https"}]}]
			output: {
				user: topology.components.sandbox_rental.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "clickhouse", endpoint: "native_tls"}, {component: "billing", endpoint: "internal_https"}, {component: "openbao", endpoint: "api"}, {component: "zitadel", endpoint: "http"}, {component: "governance_service", endpoint: "internal_https"}, {component: "secrets_service", endpoint: "internal_https"}, {component: "source_code_hosting_service", endpoint: "internal_https"}, {component: "forgejo", endpoint: "http"}, {component: "temporal", endpoint: "frontend_grpc"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_port", protocol: "tcp", port: 53, oifname: "lo"},
					{kind: "accept_port", protocol: "udp", port: 53, oifname: "lo"},
					{kind: "drop_ip_daddr_set", family: "ip", addrs: _sandbox_private_clone_ipv4},
					{kind: "drop_ip_daddr_set", family: "ip6", addrs: _sandbox_private_clone_ipv6},
					{kind: "accept_port", protocol: "tcp", port: 53},
					{kind: "accept_port", protocol: "udp", port: 53},
					{kind: "accept_port", protocol: "tcp", port: 443},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		secrets_service: {
			target:    "/etc/nftables.d/secrets-service.nft"
			table:     "verself_secrets_service"
			component: "secrets_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "secrets_service", endpoint: "public_http"}, {component: "secrets_service", endpoint: "internal_https"}]}]
			output: {
				user: topology.components.secrets_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "zitadel", endpoint: "http"}, {component: "openbao", endpoint: "api"}, {component: "billing", endpoint: "internal_https"}, {component: "governance_service", endpoint: "internal_https"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		source_code_hosting_service: {
			target:    "/etc/nftables.d/source-code-hosting-service.nft"
			table:     "verself_source_code_hosting_service"
			component: "source_code_hosting_service"
			input: [{kind: "drop_non_loopback", endpoints: [{component: "source_code_hosting_service", endpoint: "public_http"}, {component: "source_code_hosting_service", endpoint: "internal_https"}]}]
			output: {
				user: topology.components.source_code_hosting_service.runtime.user
				rules: [
					{kind: "accept_loopback_endpoints", endpoints: [{component: "postgresql", endpoint: "postgres"}, {component: "forgejo", endpoint: "http"}, {component: "sandbox_rental", endpoint: "internal_https"}, {component: "secrets_service", endpoint: "internal_https"}, {component: "projects_service", endpoint: "internal_https"}, {component: "identity_service", endpoint: "internal_https"}, {component: "zitadel", endpoint: "http"}, {component: "otelcol", endpoint: "otlp_grpc"}]},
					{kind: "accept_port", protocol: "tcp", port: 443, oifname: "lo"},
					{kind: "accept_non_tcp_udp"},
				]
			}
		}
		stalwart: {
			target:    "/etc/nftables.d/stalwart.nft"
			table:     "verself_stalwart"
			component: "stalwart"
			output: {
				user: topology.components.stalwart.runtime.user
				rules: [
					{kind: "accept_loopback_all"},
					{kind: "accept_port", protocol: "tcp", port: 53},
					{kind: "accept_port", protocol: "udp", port: 53},
				]
			}
		}
	}
}

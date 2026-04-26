package topology

import (
	"list"
	s "guardianintelligence.org/forge-metal/topology/schema"
)

artifacts: s.#Artifacts & {
	go_binaries: [
		{
			name:            "billing-service"
			package:         "./src/billing-service/cmd/billing-service"
			output_path:     "src/billing-service/billing-service"
			cgo_enabled:     "1"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart billing-service"]
			deploy_tags: ["billing_service"]
		},
		{
			name:            "tb-inspect"
			package:         "./src/billing-service/cmd/tb-inspect"
			output_path:     "src/billing-service/tb-inspect"
			cgo_enabled:     "1"
			version_ldflags: false
			restart_handlers: []
			deploy_tags: ["billing_service"]
		},
		{
			name:            "sandbox-rental-service"
			package:         "./src/sandbox-rental-service/cmd/sandbox-rental-service"
			output_path:     "src/sandbox-rental-service/sandbox-rental-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart sandbox-rental-service"]
			deploy_tags: ["sandbox_rental_service"]
		},
		{
			name:            "sandbox-rental-recurring-worker"
			package:         "./src/sandbox-rental-service/cmd/sandbox-rental-recurring-worker"
			output_path:     "src/sandbox-rental-service/sandbox-rental-recurring-worker"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart sandbox-rental-recurring-worker"]
			deploy_tags: ["sandbox_rental_service"]
		},
		{
			name:            "mailbox-service"
			package:         "./src/mailbox-service/cmd/mailbox-service"
			output_path:     "src/mailbox-service/mailbox-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart mailbox-service"]
			deploy_tags: ["mailbox_service"]
		},
		{
			name:            "object-storage-service"
			package:         "./src/object-storage-service/cmd/object-storage-service"
			output_path:     "src/object-storage-service/object-storage-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart object-storage-admin", "deploy-profile restart object-storage-service"]
			deploy_tags: ["object_storage_service"]
		},
		{
			name:            "object-storage-secret-sync"
			package:         "./src/object-storage-service/cmd/object-storage-secret-sync"
			output_path:     "src/object-storage-service/object-storage-secret-sync"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: []
			deploy_tags: ["object_storage_service"]
		},
		{
			name:            "identity-service"
			package:         "./src/identity-service/cmd/identity-service"
			output_path:     "src/identity-service/identity-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart identity-service"]
			deploy_tags: ["identity_service"]
		},
		{
			name:            "governance-service"
			package:         "./src/governance-service/cmd/governance-service"
			output_path:     "src/governance-service/governance-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart governance-service"]
			deploy_tags: ["governance_service"]
		},
		{
			name:            "profile-service"
			package:         "./src/profile-service/cmd/profile-service"
			output_path:     "src/profile-service/profile-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart profile-service"]
			deploy_tags: ["profile_service"]
		},
		{
			name:            "notifications-service"
			package:         "./src/notifications-service/cmd/notifications-service"
			output_path:     "src/notifications-service/notifications-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart notifications-service"]
			deploy_tags: ["notifications_service"]
		},
		{
			name:            "projects-service"
			package:         "./src/projects-service/cmd/projects-service"
			output_path:     "src/projects-service/projects-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart projects-service"]
			deploy_tags: ["projects_service"]
		},
		{
			name:            "secrets-service"
			package:         "./src/secrets-service/cmd/secrets-service"
			output_path:     "src/secrets-service/secrets-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart secrets-service"]
			deploy_tags: ["secrets_service"]
		},
		{
			name:            "source-code-hosting-service"
			package:         "./src/source-code-hosting-service/cmd/source-code-hosting-service"
			output_path:     "src/source-code-hosting-service/source-code-hosting-service"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart source-code-hosting-service"]
			deploy_tags: ["source_code_hosting_service"]
		},
		{
			name:            "verself-temporal-server"
			package:         "./src/temporal-platform/cmd/verself-temporal-server"
			output_path:     "src/temporal-platform/verself-temporal-server"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart temporal-server"]
			deploy_tags: ["temporal"]
		},
		{
			name:            "verself-temporal-web"
			package:         "./src/temporal-platform/cmd/verself-temporal-web"
			output_path:     "src/temporal-platform/verself-temporal-web"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: ["deploy-profile restart temporal-web"]
			deploy_tags: ["temporal"]
		},
		{
			name:            "temporal-bootstrap"
			package:         "./src/temporal-platform/cmd/temporal-bootstrap"
			output_path:     "src/temporal-platform/temporal-bootstrap"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: []
			deploy_tags: ["temporal"]
		},
		{
			name:            "temporal-schema"
			package:         "./src/temporal-platform/cmd/temporal-schema"
			output_path:     "src/temporal-platform/temporal-schema"
			cgo_enabled:     "0"
			version_ldflags: true
			restart_handlers: []
			deploy_tags: ["temporal"]
		},
	]

	server_tools: {
		clickhouse: {
			version:        "26.3.2.3"
			format:         "tarball"
			url:            "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-26.3.2.3-amd64.tgz"
			sha256:         "2b0ccdc84bc3cc624408a8a490181c6eed6b8df4e090f9b4ed7e647e46093278"
			extract_binary: "clickhouse-common-static-26.3.2.3/usr/bin/clickhouse"
			symlinks: ["clickhouse-server", "clickhouse-client", "clickhouse-local", "clickhouse-keeper", "clickhouse-benchmark"]
			deploy_tags: ["clickhouse"]
		}
		tigerbeetle: {
			version:        "0.16.78"
			format:         "zip"
			url:            "https://github.com/tigerbeetle/tigerbeetle/releases/download/0.16.78/tigerbeetle-x86_64-linux.zip"
			sha256:         "d32d7ce6aefd76559eff93efc17e74585243581059d47d988155458e4aaa2beb"
			extract_binary: "tigerbeetle"
			deploy_tags: ["tigerbeetle", "billing_service"]
		}
		zitadel: {
			version:        "4.13.1"
			format:         "tarball"
			url:            "https://github.com/zitadel/zitadel/releases/download/v4.13.1/zitadel-linux-amd64.tar.gz"
			sha256:         "fe1f5231e5dcbdca63ae77adab0d2241daafeb9712e7d6cded3713e9ef50f1cb"
			extract_binary: "zitadel-linux-amd64/zitadel"
			deploy_tags: ["zitadel"]
		}
		openbao: {
			version:        "2.5.2"
			format:         "deb"
			url:            "https://github.com/openbao/openbao/releases/download/v2.5.2/openbao_2.5.2_linux_amd64.deb"
			sha256:         "5b915011ba8fa8137bd3309830aded0250b8fce42706bd6dcd2b91ac5560cde7"
			extract_binary: "usr/bin/bao"
			deploy_tags: ["openbao", "openbao_tenancy"]
		}
		spire: {
			version:     "1.14.5"
			format:      "tarball"
			url:         "https://github.com/spiffe/spire/releases/download/v1.14.5/spire-1.14.5-linux-amd64-musl.tar.gz"
			sha256:      "cacab9ff32b7a24714edcf4328c4ce27bddd38496b443e8750395883c72a3bfb"
			extract_dir: "spire-1.14.5"
			binaries: ["spire-server", "spire-agent"]
			deploy_tags: ["spire", "clickhouse", "otelcol", "billing_service", "identity_service", "governance_service", "profile_service", "notifications_service", "projects_service", "nats", "secrets_service", "sandbox_rental_service", "source_code_hosting_service", "mailbox_service", "object_storage_service", "temporal", "grafana"]
		}
		spiffe_helper: {
			version:        "0.11.0"
			format:         "tarball"
			url:            "https://github.com/spiffe/spiffe-helper/releases/download/v0.11.0/spiffe-helper_v0.11.0_Linux-x86_64.tar.gz"
			sha256:         "7fba909574320d6a656e2e7d7f0657890fefad08a2abecd86c7bafe62d6d9134"
			extract_binary: "spiffe-helper"
			deploy_tags: ["clickhouse", "otelcol", "billing_service", "identity_service", "governance_service", "profile_service", "notifications_service", "projects_service", "nats", "secrets_service", "sandbox_rental_service", "source_code_hosting_service", "mailbox_service", "object_storage_service", "temporal", "grafana"]
		}
		nats_server: {
			version:        "2.12.7"
			format:         "tarball"
			url:            "https://github.com/nats-io/nats-server/releases/download/v2.12.7/nats-server-v2.12.7-linux-amd64.tar.gz"
			sha256:         "570d2d627db111e679cc1e6bc57ba78f373ed1769acd8dc9c21c8f62d15b3c52"
			extract_binary: "nats-server-v2.12.7-linux-amd64/nats-server"
			deploy_tags: ["nats", "notifications_service"]
		}
		garage: {
			version:     "v2.3.0"
			format:      "binary"
			url:         "https://garagehq.deuxfleurs.fr/_releases/v2.3.0/x86_64-unknown-linux-musl/garage"
			sha256:      "f98d317942bb341151a2775162016bb50cf86b865d0108de03eb5db16e2120cd"
			binary_name: "garage"
			deploy_tags: ["garage", "object_storage_service"]
		}
		forgejo: {
			version:     "15.0.0"
			format:      "binary"
			url:         "https://codeberg.org/forgejo/forgejo/releases/download/v15.0.0/forgejo-15.0.0-linux-amd64"
			sha256:      "3919f10a7845f3b71bacc2c7a3bfa2cd71aed58a0b8be6ab5e95f2e150b4ded7"
			binary_name: "forgejo"
			deploy_tags: ["forgejo"]
		}
		otelcol_contrib: {
			version:        "0.149.0"
			format:         "tarball"
			url:            "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v0.149.0/otelcol-contrib_0.149.0_linux_amd64.tar.gz"
			sha256:         "4acb57355e9388f257b28de8c18422ff43e52eb329052bd54ebecde000dcbb47"
			extract_binary: "otelcol-contrib"
			deploy_tags: ["otelcol", "temporal", "projects_service", "source_code_hosting_service"]
		}
		temporal: {
			version: "1.30.4"
			format:  "tarball"
			url:     "https://github.com/temporalio/temporal/releases/download/v1.30.4/temporal_1.30.4_linux_amd64.tar.gz"
			sha256:  "83f900fe8f9fd23c0e6369041355d2edbd768a91667dd6bc22a98d8316632177"
			binaries: ["temporal-server", "temporal-sql-tool", "tdbg"]
			deploy_tags: ["temporal"]
		}
		grafana: {
			version:          "12.4.2"
			format:           "tarball"
			url:              "https://dl.grafana.com/oss/release/grafana-12.4.2.linux-amd64.tar.gz"
			sha256:           "f240b200a803bf64592fae645331750c2681df5f07496743d714db12393a591f"
			extract_dir:      "grafana-v12.4.2"
			install_dir:      "/opt/verself/grafana"
			binary_name:      "grafana"
			strip_components: 1
			deploy_tags: ["grafana"]
		}
		grafana_clickhouse_datasource: {
			version:    "4.14.1"
			format:     "zip"
			url:        "https://github.com/grafana/clickhouse-datasource/releases/download/v4.14.1/grafana-clickhouse-datasource-4.14.1.linux_amd64.zip"
			sha256:     "11f569287a607043a9c60c4abca493784cc187b6bc4298270e5ef764719a22f1"
			plugin_dir: "grafana-clickhouse-datasource"
			deploy_tags: ["grafana"]
		}
		containerd: {
			version:        "2.2.2"
			format:         "tarball"
			url:            "https://github.com/containerd/containerd/releases/download/v2.2.2/containerd-static-2.2.2-linux-amd64.tar.gz"
			sha256:         "5db46232ce716f85bf1e71497a9038c87e63030574bf03f9d09557802188ad27"
			extract_binary: "bin/containerd"
			deploy_tags: ["containerd", "firecracker"]
		}
		nodejs: {
			version:          "22.22.2"
			format:           "tarball_xz"
			url:              "https://nodejs.org/dist/v22.22.2/node-v22.22.2-linux-x64.tar.xz"
			sha256:           "88fd1ce767091fd8d4a99fdb2356e98c819f93f3b1f8663853a2dee9b438068a"
			strip_components: 1
			install_dir:      "/opt/verself/nodejs"
			bin_symlinks: ["node", "npm", "npx", "corepack"]
			deploy_tags: ["nodejs", "console", "company", "platform", "verdaccio"]
		}
		stalwart: {
			version:        "0.15.5"
			format:         "tarball"
			url:            "https://github.com/stalwartlabs/stalwart/releases/download/v0.15.5/stalwart-x86_64-unknown-linux-musl.tar.gz"
			sha256:         "b2042dbcf0a110a4a756a5288de013649fd1f7ee84fa002bb3d2e6ec1e5f1f0b"
			extract_binary: "stalwart"
			deploy_tags: ["stalwart"]
		}
		stalwart_cli: {
			version:        "0.15.5"
			format:         "tarball"
			url:            "https://github.com/stalwartlabs/stalwart/releases/download/v0.15.5/stalwart-cli-x86_64-unknown-linux-musl.tar.gz"
			sha256:         "8fbe74206bed46974c272623e184b11d6c8362eb606dddc155fdb6ae9df7b3e9"
			extract_binary: "stalwart-cli"
			deploy_tags: ["stalwart", "mailbox_service"]
		}
		caddy: {
			version: "2.11.2"
			format:  "xcaddy_build"
			plugins: ["github.com/corazawaf/coraza-caddy/v2@v2.4.0"]
			xcaddy_version: "0.4.5"
			deploy_tags: ["caddy"]
		}
	}
}

_goBinaryNames: [for binary in artifacts.go_binaries {binary.name}]
_goBinaryOutputPaths: [for binary in artifacts.go_binaries {binary.output_path}]

_uniqueGoBinaryNames:       true & list.UniqueItems(_goBinaryNames)
_uniqueGoBinaryOutputPaths: true & list.UniqueItems(_goBinaryOutputPaths)

ansible: {
	deploy_profile_go_binaries:       artifacts.go_binaries
	deploy_profile_server_tools_pins: artifacts.server_tools
}

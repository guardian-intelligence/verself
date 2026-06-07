package gamma

import guardian "guardianintelligence.org/guardian-specification/cue/guardian/v1alpha1"

guardian.#Document

// Transitional harvested graph. Do not add non-site-specific config here.
// The replacement gamma overlay should be small: substrate access, public origins,
// and per-site secret refs/import authority only.

let openbaoOperatorAPublicKeyBase64 = "mDMEaiOJ+RYJKwYBBAHaRw8BAQdAAYbADEfH17sDLX0SI7iAC9bcdPH8aotQRTtD8Zsmyf60TEdhbW1hIE9wZW5CYW8gb3BlcmF0b3ItYSA8b3BlcmF0b3ItYS5nYW1tYS1vcGVuYmFvQGd1YXJkaWFuaW50ZWxsaWdlbmNlLm9yZz6IkwQTFgoAOxYhBEZBawrVEmfloiCCL5b5vLYzeQToBQJqI4n5AhsDBQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEJb5vLYzeQToxU4BAPkVtbeOQmuDizUjl/gJO5SHezdDHWfyzoyUtackFBm5AP9TVpFLbOb0aFgw1LQxjdYLAZQkh0NlizTtFU1fGKgkDbg4BGojifkSCisGAQQBl1UBBQEBB0BuQaidG8ObtxFzAEwIrKZAChiP7TTpV1Xx/UJIDsZ6NwMBCAeIeAQYFgoAIBYhBEZBawrVEmfloiCCL5b5vLYzeQToBQJqI4n5AhsMAAoJEJb5vLYzeQTo3JoA/0v35/RUCPblBuwSC7sdSnpUHBzfjyYr0roc+eJHHSltAQDXnVUtg/O/B2mibJt87LtT1hijA/Oiox4/D5fj/XdABA=="
let openbaoOperatorBPublicKeyBase64 = "mDMEaiOJ+RYJKwYBBAHaRw8BAQdA6bubGkccdirSzvgIIgVVpt4Fj3NcYhs/9wqeCPmfqz20TEdhbW1hIE9wZW5CYW8gb3BlcmF0b3ItYiA8b3BlcmF0b3ItYi5nYW1tYS1vcGVuYmFvQGd1YXJkaWFuaW50ZWxsaWdlbmNlLm9yZz6IkwQTFgoAOxYhBM5fgQ69yZaRlgZuICsvVrVb81xIBQJqI4n5AhsDBQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJECsvVrVb81xIdjUBAMmkwKz1aZ6M6p/hy8VCERUaifleNguTvL0MoFOMt9KLAQDS4geMPEWp7Ot7m5vMZN9jMIDnLE7LE9ExT725kJXIArg4BGojifkSCisGAQQBl1UBBQEBB0BBzz6cUUJWeUMbPmL8AjY9bqGX+s6/NMwpisBbXXnvCwMBCAeIeAQYFgoAIBYhBM5fgQ69yZaRlgZuICsvVrVb81xIBQJqI4n5AhsMAAoJECsvVrVb81xIP6ABAKa68/nvx5mGA6Ukz05sJhQqcLMNMUzfFu50NfJS6DrvAPwMAkX/AKJKJv0kn/YLG/WS3PTjNNRhwZtef9lTQc9kDA=="
let openbaoOperatorCPublicKeyBase64 = "mDMEaiOJ+RYJKwYBBAHaRw8BAQdAk8b33//YRFh+tnQTHdF8YBoRbdQlK0f9GTjc8uK3dcC0TEdhbW1hIE9wZW5CYW8gb3BlcmF0b3ItYyA8b3BlcmF0b3ItYy5nYW1tYS1vcGVuYmFvQGd1YXJkaWFuaW50ZWxsaWdlbmNlLm9yZz6IkwQTFgoAOxYhBBxAfMN++1GYi1EqmeT897zG400NBQJqI4n5AhsDBQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEOT897zG400NCVkA/iGDVuMteClIXDhC75Z5mQdD9xS3/k0g1zt1DihRPK2uAP0bmmC8TZObdVBNn7mQ8TxzMwisK0CDmtwJHp8yQ/H+A7g4BGojifkSCisGAQQBl1UBBQEBB0C8tq2q7d+EeFOMdkpP4c9xyGukm9M6FlFLV59zgiD0UQMBCAeIeAQYFgoAIBYhBBxAfMN++1GYi1EqmeT897zG400NBQJqI4n5AhsMAAoJEOT897zG400NydsA/0oeRRJ3ZAoy7bMXsBXd3wgaUA4w8R+JgSH5RwbErvfXAQC8Ckpse31YZ3CF6d+PgXOiLeafNr8+B1hQU3+5ATGdAQ=="

entrypoint: {
	apiVersion: "guardian.guardianintelligence.org/v1alpha1"
	kind:       "FlyProcedure"
	name:       "gamma"
}

resources: [
	{
		apiVersion: "guardian.guardianintelligence.org/v1alpha1"
		kind:       "FlyProcedure"
		metadata: name: "gamma"
		spec: {
			substrateRef: {
				apiVersion: "substrate.guardianintelligence.org/v1alpha1"
				kind:       "Substrate"
				name:       "gamma-primary"
			}
			preflight: ansible: playbook: "src/guardian-specification/ansible/preflight.yml"
			nomad: {
				address:   "http://127.0.0.1:4646"
				namespace: "default"
				jobs: [
					{
						name: "cloudflare-integration-recovery"
						path: "src/integrations/cloudflare/control-plane/nomad.hcl"
						varsPath: "bazel-bin/src/integrations/cloudflare/control-plane/nomad.vars.hcl"
					},
					{
						name: "postgresql"
						path: "src/infrastructure-components/postgresql/nomad.hcl"
						varsPath: "bazel-bin/src/infrastructure-components/postgresql/nomad.vars.hcl"
					},
				]
			}
		}
	},
	{
		apiVersion: "substrate.guardianintelligence.org/v1alpha1"
		kind:       "Substrate"
		metadata: name: "gamma-primary"
		spec: {

			remote: {
				repoRoot: "/home/ubuntu/.local/state/guardian/repo"
				ssh: [
					"ssh",
					"-T",
					"-o", "BatchMode=yes",
					"-o", "StrictHostKeyChecking=yes",
					"-o", "UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts",
					"-o", "ConnectTimeout=10",
					"ubuntu@206.223.228.87",
				]
			}
		}
	},
	{
		apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
		kind:       "AccountAuthority"
		metadata: name: "cloudflare-account-admin"
		spec: {}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "cloudflare.account-admin"
		spec: {
			path:   "kv-controller/data/integrations/cloudflare/account-admin"
			key:    "api_token"
			source: "operatorImport"
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "cloudflare.r2.object-storage-admin"
		spec: {
			path:   "kv-controller/data/integrations/cloudflare/r2/capabilities/object-storage-admin"
			key:    "access_key_id"
			source: "producedBy"
			producerRef: {
				apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
				kind:       "CloudflareControlPlane"
				name:       "gamma-cloudflare"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "cloudflare.r2.recovery"
		spec: {
			path:   "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery"
			key:    "access_key_id"
			source: "producedBy"
			producerRef: {
				apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
				kind:       "CloudflareControlPlane"
				name:       "gamma-cloudflare"
			}
		}
	},
	{
		apiVersion: "networking.guardianintelligence.org/v1alpha1"
		kind:       "PublicOrigin"
		metadata: name: "product"
		spec: url:      "https://gamma.verself.sh"
	},
	{
		apiVersion: "networking.guardianintelligence.org/v1alpha1"
		kind:       "PublicOrigin"
		metadata: name: "company"
		spec: url:      "https://gamma.guardianintelligence.org"
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "OpenBaoCluster"
		metadata: name: "openbao"
		spec: {
			address:          "https://127.0.0.1:8200"
			caCert:           "/etc/verself/openbao/ca.pem"
			runtimeRoot:      "/var/lib/openbao/runtime"
			dataDir:          "/var/lib/openbao/raft"
			configPath:       "/etc/openbao/openbao.hcl"
			reportPath:       "/run/verself/recovery/openbao/report.json"
			initMaterialPath: "/run/verself/recovery/openbao/init-material.json"
			seal: shamir: {
				keyShares:    3
				keyThreshold: 2
				pgpRecipientRefs: [
					{
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "PGPRecipient"
						name:       "operator-a"
					},
					{
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "PGPRecipient"
						name:       "operator-b"
					},
					{
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "PGPRecipient"
						name:       "operator-c"
					},
				]
			}
			snapshots: {
				restore: sourceRef: {
					apiVersion: "objectstorage.guardianintelligence.org/v1alpha1"
					kind:       "ObjectStorageService"
					name:       "object-storage"
				}
				save: {
					snapshotPath: "/run/verself/recovery/openbao/openbao.snap"
					manifestPath: "/run/verself/recovery/openbao/openbao.manifest.json"
					destinationRef: {
						apiVersion: "objectstorage.guardianintelligence.org/v1alpha1"
						kind:       "ObjectStorageService"
						name:       "object-storage"
					}
				}
			}
		}
	},
	{
		apiVersion: "nftables.guardianintelligence.org/v1alpha1"
		kind:       "NftablesFirewall"
		metadata: name: "nftables"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar"
			runtimeRoot:     "/opt/verself/nftables"
			configPath:      "/etc/nftables.conf"
			rulesDir:        "/etc/nftables.d"
			manageSystemd:   true
			systemd: {
				serviceUnitPath:    "/etc/systemd/system/verself-nftables.service"
				firewallTargetPath: "/etc/systemd/system/verself-firewall.target"
			}
		}
	},
	{
		apiVersion: "spire.guardianintelligence.org/v1alpha1"
		kind:       "SPIRECluster"
		metadata: name: "spire"
		spec: {
			runtimeArtifact:          "bazel-bin/src/infrastructure-components/spire/spire-runtime.tar"
			identityRegistryArtifact: "bazel-bin/src/infrastructure-components/spire/identity_registry.spire_identity_registry.json"
			runtimeRoot:              "/var/lib/spire/runtime"
			serverConfigPath:         "/etc/spire/server.conf"
			agentConfigPath:          "/etc/spire/agent.conf"
			serverDataDir:            "/var/lib/spire/server"
			agentDataDir:             "/var/lib/spire/agent"
			serverSocketPath:         "/run/spire-server/private/api.sock"
			agentSocketPath:          "/run/spire-agent/sockets/agent.sock"
			joinTokenPath:            "/run/verself/recovery/spire/join-token"
			reportPath:               "/run/verself/recovery/spire/report.json"
			trustDomain:              "gamma.verself.sh"
			agentSpiffeID:            "spiffe://gamma.verself.sh/agent/gamma-primary"
			serverAddress:            "127.0.0.1"
			serverPort:               8081
			serverUser:               "spire"
			serverGroup:              "spire"
			workloadGroup:            "spire_workload"
			registrarIntervalSeconds: 5
		}
	},
	{
		apiVersion: "nats.guardianintelligence.org/v1alpha1"
		kind:       "NATSCluster"
		metadata: name: "nats"
		spec: {
			runtimeArtifact:  "bazel-bin/src/infrastructure-components/nats/nats-runtime.tar"
			runtimeRoot:      "/var/lib/nats/runtime"
			configPath:       "/etc/nats/nats-server.conf"
			helperConfigPath: "/etc/nats/nats-spiffe-helper.conf"
			dataDir:          "/var/lib/nats"
			jetStreamDir:     "/var/lib/nats/jetstream"
			pidPath:          "/var/lib/nats/nats.pid"
			reportPath:       "/run/verself/recovery/nats/report.json"
			user:             "nats"
			group:            "nats"
			workloadGroup:    "spire_workload"
			serverName:       "verself-nats"
			host:             "127.0.0.1"
			clientPort:       4222
			monitoringPort:   8222
			jetstream: {
				maxMemStore:  "256Mb"
				maxFileStore: "4Gb"
			}
			spiffe: {
				agentSocket: "/run/spire-agent/sockets/agent.sock"
				certDir:     "/var/lib/nats/spiffe"
			}
			authorization: users: [
				{
					spiffeID: "spiffe://gamma.verself.sh/svc/notifications-service"
					publishAllow: ["events.>", "$JS.API.>", "$JS.ACK.>"]
					subscribeAllow: ["_INBOX.>"]
				},
			]
		}
	},
	{
		apiVersion: "nomadobserver.guardianintelligence.org/v1alpha1"
		kind:       "NomadObserver"
		metadata: name: "nomad-observer"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer.tar"
			runtimeRoot:     "/var/lib/nomad-observer/runtime"
			dataDir:         "/var/lib/nomad-observer"
			graphPath:       "/run/verself/recovery/nomad-observer/document.json"
			reportPath:      "/run/verself/recovery/nomad-observer/report.json"
			user:            "nomad_observer"
			group:           "nomad_observer"
			supplementaryGroups: ["spire_workload"]
			nomad: {
				address:   "http://127.0.0.1:4646"
				namespace: "default"
			}
			otel: exporterEndpoint:         "http://127.0.0.1:4317"
			fleet: snapshotIntervalSeconds: 30
			clickhouse: {
				address:    "127.0.0.1:9440"
				user:       "nomad_observer"
				caCertPath: "/etc/verself/clickhouse/server-ca.pem"
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "otelcol.guardianintelligence.org/v1alpha1"
		kind:       "OtelCollector"
		metadata: name: "otelcol"
		spec: {
			runtimeArtifact:     "bazel-bin/src/infrastructure-components/otelcol/otelcol-runtime.tar"
			configArtifact:      "bazel-bin/src/infrastructure-components/otelcol/otelcol-config.tar"
			runtimeRoot:         "/var/lib/otelcol/runtime"
			configRoot:          "/etc/otelcol"
			configPath:          "/etc/otelcol/current/config/config.yaml"
			helperConfigPath:    "/etc/otelcol/current/config/clickhouse-spiffe-helper.conf"
			dataDir:             "/var/lib/otelcol"
			storageDir:          "/var/lib/otelcol/storage"
			clickhouseSpiffeDir: "/var/lib/otelcol/clickhouse-spiffe"
			reportPath:          "/run/verself/recovery/otelcol/report.json"
			user:                "otelcol"
			group:               "otelcol"
			supplementaryGroups: ["spire_workload", "adm", "systemd-journal"]
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "PGPRecipient"
		metadata: name:        "operator-a"
		spec: publicKeyBase64: openbaoOperatorAPublicKeyBase64
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "PGPRecipient"
		metadata: name:        "operator-b"
		spec: publicKeyBase64: openbaoOperatorBPublicKeyBase64
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "PGPRecipient"
		metadata: name:        "operator-c"
		spec: publicKeyBase64: openbaoOperatorCPublicKeyBase64
	},
	{
		apiVersion: "postgresql.guardianintelligence.org/v1alpha1"
		kind:       "PostgreSQLCluster"
		metadata: name: "postgresql"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar"
			runtimeRoot:     "/var/lib/postgresql/runtime"
			dataDir:         "/var/lib/postgresql/16/verself"
			configDir:       "/etc/postgresql/verself"
			logDir:          "/var/log/postgresql"
			socketDir:       "/var/run/postgresql"
			reportPath:      "/run/verself/recovery/postgresql/report.json"
			backup: {
				stanza:                        "gamma"
				configPath:                    "/run/verself/recovery/postgresql/pgbackrest.conf"
				spoolDir:                      "/var/spool/pgbackrest"
				logDir:                        "/var/log/pgbackrest"
				archiveTimeout:                "60s"
				processMax:                    2
				retentionFull:                 2
				destructiveRestoreAllowed:     false
				recoveryCredentialOpenBaoPath: "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery"
				cipherPassRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "postgresql.pgbackrest.cipher_pass"
				}
				repository: {
					type:     "s3"
					endpoint: "c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com"
					region:   "auto"
					bucket:   "verself-recovery"
					path:     "/gamma/postgresql"
				}
			}
			listenAddress:                "127.0.0.1"
			port:                         5432
			maxConnections:               300
			superuserReservedConnections: 10
			databases: [
				{
					name:  "object_storage_service"
					owner: "object_storage_service"
				},
				{
					name:  "iam_service"
					owner: "iam_service"
				},
				{
					name:  "deployment_service"
					owner: "deployment_service"
				},
				{
					name:  "distribution_service"
					owner: "distribution_service"
				},
				{
					name:  "profile"
					owner: "profile_service"
				},
				{
					name:  "projects_service"
					owner: "projects_service"
				},
				{
					name:  "source_code_hosting"
					owner: "source_code_hosting_service"
				},
				{
					name:  "governance_service"
					owner: "governance_service"
				},
				{
					name:  "billing"
					owner: "billing"
				},
				{
					name:  "sandbox_rental"
					owner: "sandbox_rental"
				},
				{
					name:  "zitadel"
					owner: "zitadel"
				},
				{
					name:  "spicedb"
					owner: "spicedb"
				},
				{
					name:  "grafana"
					owner: "grafana"
				},
				{
					name:  "stalwart"
					owner: "stalwart"
				},
				{
					name:  "temporal"
					owner: "temporal"
				},
				{
					name:  "temporal_visibility"
					owner: "temporal"
				},
			]
			roles: [
				{
					name:  "otelcol"
					login: true
					memberOf: ["pg_monitor"]
				},
				{
					name:  "iam_service"
					login: true
				},
				{
					name:  "deployment_service"
					login: true
				},
				{
					name:  "distribution_service"
					login: true
				},
				{
					name:  "profile_service"
					login: true
				},
				{
					name:  "projects_service"
					login: true
				},
				{
					name:  "source_code_hosting_service"
					login: true
				},
				{
					name:  "governance_service"
					login: true
				},
				{
					name:  "billing"
					login: true
				},
				{
					name:  "sandbox_rental"
					login: true
				},
				{
					name:  "zitadel"
					login: true
				},
				{
					name:  "spicedb"
					login: true
				},
				{
					name:  "grafana"
					login: true
				},
				{
					name:  "stalwart"
					login: true
				},
				{
					name:  "temporal"
					login: true
				},
			]
			peerMappings: [
				{
					systemUser:   "object_storage_service"
					postgresUser: "object_storage_service"
				},
				{
					systemUser:   "object_storage_admin"
					postgresUser: "object_storage_service"
				},
				{
					systemUser:   "iam_service"
					postgresUser: "iam_service"
				},
				{
					systemUser:   "deployment_service"
					postgresUser: "deployment_service"
				},
				{
					systemUser:   "distribution_service"
					postgresUser: "distribution_service"
				},
				{
					systemUser:   "profile_service"
					postgresUser: "profile_service"
				},
				{
					systemUser:   "projects_service"
					postgresUser: "projects_service"
				},
				{
					systemUser:   "source_code_hosting_service"
					postgresUser: "source_code_hosting_service"
				},
				{
					systemUser:   "governance_service"
					postgresUser: "governance_service"
				},
				{
					systemUser:   "governance_service"
					postgresUser: "iam_service"
				},
				{
					systemUser:   "governance_service"
					postgresUser: "billing"
				},
				{
					systemUser:   "governance_service"
					postgresUser: "sandbox_rental"
				},
				{
					systemUser:   "billing"
					postgresUser: "billing"
				},
				{
					systemUser:   "otelcol"
					postgresUser: "otelcol"
				},
				{
					systemUser:   "zitadel"
					postgresUser: "zitadel"
				},
				{
					systemUser:   "spicedb"
					postgresUser: "spicedb"
				},
				{
					systemUser:   "grafana"
					postgresUser: "grafana"
				},
				{
					systemUser:   "stalwart"
					postgresUser: "stalwart"
				},
				{
					systemUser:   "temporal_server"
					postgresUser: "temporal"
				},
			]
		}
	},
	{
		apiVersion: "clickhouse.guardianintelligence.org/v1alpha1"
		kind:       "ClickHouseCluster"
		metadata: name: "clickhouse"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/clickhouse/clickhouse-runtime.tar"
			runtimeRoot:     "/opt/verself/clickhouse"
			dataDir:         "/var/lib/clickhouse"
			backupDir:       "/var/lib/clickhouse/backups"
			backupDiskName:  "verself_recovery_backups"
			logDir:          "/var/log/clickhouse-server"
			host:            "127.0.0.1"
			port:            9440

			configPath: "/etc/clickhouse-server/config.d/verself.xml"
			tlsDir:     "/etc/clickhouse-server/tls"
			pidPath:    "/run/clickhouse-server/clickhouse-server.pid"

			serverUser:  "clickhouse"
			serverGroup: "clickhouse"

			operatorUser:             "clickhouse_operator"
			operatorGroup:            "clickhouse_operator"
			operatorDatabaseUser:     "clickhouse_operator"
			operatorClientConfigPath: "/etc/clickhouse-client/operator.xml"
			operatorCAPath:           "/etc/clickhouse-client/server-ca.pem"

			spiffe: {
				trustDomain:          "gamma.verself.sh"
				servicePrefix:        "spiffe://gamma.verself.sh/svc"
				agentSocket:          "/run/spire-agent/sockets/agent.sock"
				helperPath:           "/opt/verself/clickhouse/current/bin/spiffe-helper"
				serverID:             "spiffe://gamma.verself.sh/svc/clickhouse-server"
				operatorID:           "spiffe://gamma.verself.sh/svc/clickhouse-operator"
				serverDir:            "/var/lib/clickhouse/spiffe"
				operatorDir:          "/var/lib/clickhouse-operator/spiffe"
				spireWorkloadGroup:   "spire_workload"
				serverHelperConfig:   "/etc/clickhouse-server/server-spiffe-helper.conf"
				operatorHelperConfig: "/etc/clickhouse-client/operator-spiffe-helper.conf"
				bundleReloadState:    "/var/lib/clickhouse/spiffe/.bundle-reload.sha256"
			}

			systemd: {
				serverServicePath:         "/etc/systemd/system/clickhouse-server.service"
				serverHelperServicePath:   "/etc/systemd/system/clickhouse-server-spiffe-helper.service"
				operatorHelperServicePath: "/etc/systemd/system/clickhouse-operator-spiffe-helper.service"
				bundleReloadServicePath:   "/etc/systemd/system/clickhouse-server-spiffe-bundle-reload.service"
				bundleReloadPathUnitPath:  "/etc/systemd/system/clickhouse-server-spiffe-bundle-reload.path"
			}

			migrations: {
				bootstrapSchemaPath: "src/infrastructure-components/clickhouse/migrations/001_initial_schema.up.sql"
				deltaDir:            "src/infrastructure-components/clickhouse/migrations"
				stateDir:            "/opt/verself/migrations"
			}

			clientCAProjections: [
				{
					path:          "/etc/verself/clickhouse/server-ca.pem"
					group:         "root"
					mode:          "0644"
					directoryMode: "0755"
				},
			]

			reportPath: "/run/verself/recovery/clickhouse/report.json"
		}
	},
	{
		apiVersion: "tigerbeetle.guardianintelligence.org/v1alpha1"
		kind:       "TigerBeetleCluster"
		metadata: name: "tigerbeetle"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/tigerbeetle/tigerbeetle-runtime.tar"
			runtimeRoot:     "/var/lib/tigerbeetle/runtime"
			dataFile:        "/var/lib/tigerbeetle/data.tigerbeetle"
			reportPath:      "/run/verself/recovery/tigerbeetle/report.json"
			user:            "tigerbeetle"
			group:           "tigerbeetle"
			clusterID:       0
			replica:         0
			replicaCount:    1
			addresses: ["127.0.0.1:3320"]
			logLevel:      "info"
			statsdAddress: "127.0.0.1:8125"
			experimental:  true
		}
	},
	{
		apiVersion: "zot.guardianintelligence.org/v1alpha1"
		kind:       "ZotRegistry"
		metadata: name: "zot"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/zot/zot-runtime.tar"
			runtimeRoot:     "/var/lib/zot/runtime"
			configPath:      "/etc/zot/config.json"
			storageDir:      "/var/lib/zot/storage"
			htpasswdPath:    "/etc/zot/htpasswd"
			reportPath:      "/run/verself/recovery/zot/report.json"
			user:            "zot"
			group:           "zot"
			host:            "127.0.0.1"
			port:            5080
			realm:           "verself-artifacts"
			logLevel:        "info"
			publisherUser:   "artifact-publisher"
		}
	},
	{
		apiVersion: "verdaccio.guardianintelligence.org/v1alpha1"
		kind:       "VerdaccioRegistry"
		metadata: name: "verdaccio"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/verdaccio/verdaccio-runtime.tar"
			runtimeRoot:     "/var/lib/verdaccio/runtime"
			configPath:      "/etc/verdaccio/config.yaml"
			storageDir:      "/var/lib/verdaccio/storage"
			htpasswdPath:    "/var/lib/verdaccio/htpasswd"
			reportPath:      "/run/verself/recovery/verdaccio/report.json"
			user:            "verdaccio"
			group:           "verdaccio"
			host:            "127.0.0.1"
			port:            4873
			maxBodySize:     "100mb"
			uplink: {
				name:      "npmjs"
				url:       "https://registry.npmjs.org/"
				cache:     true
				maxAge:    "30m"
				strictSSL: true
			}
			packageFilter: minAgeDays: 3
			log: level:                "http"
		}
	},
	{
		apiVersion: "spicedb.guardianintelligence.org/v1alpha1"
		kind:       "SpiceDBCluster"
		metadata: name: "spicedb"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/spicedb/spicedb-runtime.tar"
			runtimeRoot:     "/var/lib/spicedb/runtime"
			homeDir:         "/var/lib/spicedb"
			reportPath:      "/run/verself/recovery/spicedb/report.json"
			user:            "spicedb"
			group:           "spicedb"
			datastore: {
				engine:           "postgres"
				connURI:          "postgres://spicedb@/spicedb?host=/var/run/postgresql&sslmode=disable&application_name=spicedb"
				readPoolMaxOpen:  8
				readPoolMinOpen:  1
				writePoolMaxOpen: 4
				writePoolMinOpen: 1
			}
			grpc: {
				host: "127.0.0.1"
				presharedKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.spicedb.grpc_preshared_key"
				}
			}
			metrics: host: "127.0.0.1"
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
			}
		}
	},
	{
		apiVersion: "grafana.guardianintelligence.org/v1alpha1"
		kind:       "GrafanaInstance"
		metadata: name: "grafana"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/grafana/grafana-runtime.tar"
			runtimeRoot:     "/var/lib/grafana/runtime"
			configPath:      "/etc/grafana/grafana.ini"
			dataDir:         "/var/lib/grafana"
			logDir:          "/var/log/grafana"
			reportPath:      "/run/verself/recovery/grafana/report.json"
			user:            "grafana"
			group:           "grafana"
			server: {
				httpAddr: "127.0.0.1"
				httpPort: 4300
				domain:   "dashboard.gamma.verself.sh"
				rootURL:  "https://dashboard.gamma.verself.sh/"
			}
			database: {
				host:    "/var/run/postgresql"
				name:    "grafana"
				user:    "grafana"
				sslMode: "disable"
			}
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
				adminPasswordRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "grafana.admin_password"
				}
				secretKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "grafana.secret_key"
				}
			}
			authJWT: {
				enabled:       true
				headerName:    "X-Pomerium-Jwt-Assertion"
				emailClaim:    "email"
				usernameClaim: "email"
				jwkSetURL:     "https://dashboard.gamma.verself.sh/.well-known/pomerium/jwks.json"
				cacheTTL:      "60m"
				autoSignUp:    true
			}
		}
	},
	{
		apiVersion: "forgejo.guardianintelligence.org/v1alpha1"
		kind:       "ForgejoInstance"
		metadata: name: "forgejo"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/forgejo/forgejo-runtime.tar"
			runtimeRoot:     "/var/lib/forgejo/runtime"
			configPath:      "/etc/forgejo/app.ini"
			workDir:         "/var/lib/forgejo"
			dataDir:         "/var/lib/forgejo/data"
			logDir:          "/var/lib/forgejo/log"
			repositoriesDir: "/var/lib/forgejo/repositories"
			reportPath:      "/run/verself/recovery/forgejo/report.json"
			user:            "forgejo"
			group:           "forgejo"
			server: {
				httpAddr: "127.0.0.1"
				httpPort: 3000
				domain:   "git.gamma.verself.sh"
				rootURL:  "https://git.gamma.verself.sh/"
			}
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
				secretKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "forgejo.secret_key"
				}
				internalTokenRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "forgejo.internal_token"
				}
				lfsJWTSecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "forgejo.lfs_jwt_secret"
				}
				oauthJWTSecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "forgejo.oauth_jwt_secret"
				}
				automationTokenRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "source-code-hosting-service.forgejo.automation_token"
				}
			}
		}
	},
	{
		apiVersion: "stalwart.guardianintelligence.org/v1alpha1"
		kind:       "StalwartMailServer"
		metadata: name: "stalwart"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/stalwart/stalwart-runtime.tar"
			runtimeRoot:     "/var/lib/stalwart/runtime"
			configPath:      "/etc/stalwart/config.toml"
			dataDir:         "/var/lib/stalwart"
			reportPath:      "/run/verself/recovery/stalwart/report.json"
			user:            "stalwart"
			group:           "stalwart"
			server: {
				hostname:     "mail.gamma.verself.sh"
				baseURL:      "https://mail.gamma.verself.sh"
				httpAddr:     "127.0.0.1"
				httpPort:     8090
				smtpAddr:     "127.0.0.1"
				smtpPort:     25
				otlpEndpoint: "http://127.0.0.1:4317"
			}
			database: {
				host: "/var/run/postgresql"
				name: "stalwart"
				user: "stalwart"
			}
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
				adminPasswordRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "stalwart.admin_password"
				}
			}
		}
	},
	{
		apiVersion: "electric.guardianintelligence.org/v1alpha1"
		kind:       "ElectricDeployment"
		metadata: name: "electric"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/electric/electric-runtime.tar"
			runtimeRoot:     "/var/lib/electric/runtime"
			image:           "docker.io/electricsql/electric:1.5.0@sha256:f311edc272e227ddaea593c5205a02c3d1e5969c2db0f7655a039a5e24abb176"
			containerd: {
				socketPath: "/run/electric-containerd/containerd.sock"
				stateDir:   "/run/electric-containerd/state"
				rootDir:    "/var/lib/electric-containerd/root"
			}
			postgres: {
				runtimeRoot: "/var/lib/postgresql/runtime"
				host:        "/var/run/postgresql"
				port:        5432
			}
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
			}
			instances: [
				{
					name:                "electric"
					serviceName:         "electric"
					storageDir:          "/var/lib/electric"
					database:            "sandbox_rental"
					databaseUser:        "electric"
					connectionLimit:     25
					replicationStreamID: "default"
					dbPoolSize:          15
					pgPasswordRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "electric.pg.password"
					}
					apiSecretRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "electric.api_secret"
					}
				},
				{
					name:                "electric-notifications"
					serviceName:         "electric-notifications"
					storageDir:          "/var/lib/electric-notifications"
					database:            "notifications_service"
					databaseUser:        "electric_notifications"
					connectionLimit:     15
					replicationStreamID: "notifications"
					dbPoolSize:          8
					pgPasswordRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "electric-notifications.pg.password"
					}
					apiSecretRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "electric-notifications.api_secret"
					}
				},
				{
					name:                "electric-iam"
					serviceName:         "electric-iam"
					storageDir:          "/var/lib/electric-iam"
					database:            "iam_service"
					databaseUser:        "electric_iam"
					connectionLimit:     15
					replicationStreamID: "iam"
					dbPoolSize:          8
					pgPasswordRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "electric-iam.pg.password"
					}
					apiSecretRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "electric-iam.api_secret"
					}
				},
			]
		}
	},
	{
		apiVersion: "temporal.guardianintelligence.org/v1alpha1"
		kind:       "TemporalPlatform"
		metadata: name: "temporal"
		spec: {
			runtimeArtifact: "bazel-bin/src/infrastructure-components/temporal-platform/temporal-runtime.tar"
			runtimeRoot:     "/var/lib/temporal/runtime"
			stateDir:        "/var/lib/temporal"
			authConfigPath:  "/etc/temporal/auth.json"
			reportPath:      "/run/verself/recovery/temporal/report.json"
			user:            "temporal_server"
			group:           "temporal_server"
			workloadGroup:   "spire_workload"
			persistence: {
				user:               "temporal"
				socketDir:          "/var/run/postgresql"
				defaultDatabase:    "temporal"
				visibilityDatabase: "temporal_visibility"
				defaultMaxConns:    20
				visibilityMaxConns: 10
			}
			authorization: {
				systemAdminIDs: ["spiffe://gamma.verself.sh/svc/temporal-server"]
				namespaceRoles: [
					{
						spiffeID:  "spiffe://gamma.verself.sh/svc/sandbox-rental-service"
						namespace: "sandbox-rental-service"
						role:      "admin"
					},
					{
						spiffeID:  "spiffe://gamma.verself.sh/svc/billing-service"
						namespace: "billing-service"
						role:      "admin"
					},
					{
						spiffeID:  "spiffe://gamma.verself.sh/svc/distribution-service"
						namespace: "distribution-service"
						role:      "admin"
					},
				]
			}
			bootstrap: namespaces: [
				{
					name:      "sandbox-rental-service"
					retention: "24h"
				},
				{
					name:      "billing-service"
					retention: "24h"
				},
				{
					name:      "distribution-service"
					retention: "24h"
				},
			]
		}
	},
	{
		apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
		kind:       "CloudflareControlPlane"
		metadata: name: "gamma-cloudflare"
		spec: {
			site:                    "gamma"
			accountID:               "c3eaeffaadf7d4847684d4775c16d598"
			accountAdminOpenBaoPath: "kv-controller/data/integrations/cloudflare/account-admin"
			targetIPv4:              "206.223.228.87"
			openBao: {
				address:    "https://127.0.0.1:8200"
				tokenFile:  "${NOMAD_SECRETS_DIR}/vault_token"
				caCertFile: "/etc/verself/openbao/ca.pem"
			}
			dns: {
				zones: [
					{
						name:   "product"
						zone:   "verself.sh"
						domain: "gamma.verself.sh"
					},
					{
						name:   "company"
						zone:   "guardianintelligence.org"
						domain: "gamma.guardianintelligence.org"
					},
				]
				records: [
					{zone: "product", record: "@", ttl: 1, proxied: false},
					{zone: "product", record: "billing.api", ttl: 1, proxied: false},
					{zone: "product", record: "deployments.api", ttl: 1, proxied: false},
					{zone: "product", record: "distribution.api", ttl: 1, proxied: false},
					{zone: "product", record: "oci", ttl: 1, proxied: false},
					{zone: "product", record: "sandbox.api", ttl: 1, proxied: false},
					{zone: "product", record: "iam.api", ttl: 1, proxied: false},
					{zone: "product", record: "profile.api", ttl: 1, proxied: false},
					{zone: "product", record: "notifications.api", ttl: 1, proxied: false},
					{zone: "product", record: "projects.api", ttl: 1, proxied: false},
					{zone: "product", record: "source.api", ttl: 1, proxied: false},
					{zone: "product", record: "governance.api", ttl: 1, proxied: false},
					{zone: "product", record: "github.api", ttl: 1, proxied: false},
					{zone: "product", record: "secrets.api", ttl: 1, proxied: false},
					{zone: "product", record: "email.api", ttl: 1, proxied: false},
					{zone: "product", record: "dashboard", ttl: 1, proxied: false},
					{zone: "product", record: "access", ttl: 1, proxied: false},
					{zone: "product", record: "git", ttl: 1, proxied: false},
					{zone: "product", record: "mail", ttl: 1, proxied: false},
					{zone: "product", record: "npm", ttl: 1, proxied: false},
					{zone: "company", record: "@", ttl: 1, proxied: false},
				]
			}
			tls: {
				outputDir: "/etc/haproxy/certs"
				acme: {
					directoryURL:       "https://acme-v02.api.letsencrypt.org/directory"
					contactEmail:       "agents@guardianintelligence.org"
					dnsPropagationWait: "2m"
					renewBefore:        "720h"
				}
				certificates: [
					{
						name: "gamma.verself.sh"
						domains: [
							"gamma.verself.sh",
							"*.gamma.verself.sh",
							"*.api.gamma.verself.sh",
						]
					},
					{
						name: "gamma.guardianintelligence.org"
						domains: ["gamma.guardianintelligence.org"]
					},
				]
			}
			objectStorage: {
				bucket:         "verself-deployment-artifacts"
				recoveryBucket: "verself-recovery"
				childTokenTTL:  "168h"
				runtimeSecrets: {
					adminAccessKeyID:     "object-storage-service.r2.admin_access_key_id"
					adminSecretAccessKey: "object-storage-service.r2.admin_secret_access_key"
					proxyAccessKeyID:     "object-storage-service.r2.proxy_access_key_id"
					proxySecretAccessKey: "object-storage-service.r2.proxy_secret_access_key"
				}
			}
		}
	},
	{
		apiVersion: "haproxy.guardianintelligence.org/v1alpha1"
		kind:       "HAProxyGateway"
		metadata: name: "public-edge"
		spec: {
			runtimeRoot:   "/var/lib/haproxy/runtime/current"
			configPath:    "/etc/haproxy/haproxy.cfg"
			upstreamsPath: "/etc/haproxy/nomad-upstreams.cfg"
			origins: [
				{
					apiVersion: "networking.guardianintelligence.org/v1alpha1"
					kind:       "PublicOrigin"
					name:       "product"
				},
				{
					apiVersion: "networking.guardianintelligence.org/v1alpha1"
					kind:       "PublicOrigin"
					name:       "company"
				},
			]
			certificates: [
				{
					name: "gamma-product"
					dnsNames: [
						"gamma.verself.sh",
						"api.gamma.verself.sh",
						"billing.api.gamma.verself.sh",
						"deployments.api.gamma.verself.sh",
						"distribution.api.gamma.verself.sh",
						"iam.api.gamma.verself.sh",
						"oci.gamma.verself.sh",
						"profile.api.gamma.verself.sh",
						"projects.api.gamma.verself.sh",
						"source.api.gamma.verself.sh",
						"governance.api.gamma.verself.sh",
					]
					pemPath: "/etc/haproxy/certs/gamma.verself.sh.pem"
				},
				{
					name: "gamma-company"
					dnsNames: ["gamma.guardianintelligence.org"]
					pemPath: "/etc/haproxy/certs/gamma.guardianintelligence.org.pem"
				},
			]
			routes: [
				{
					name: "product-auth"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "gamma.verself.sh"
					backend:  "be_route_product_auth_zitadel_oidc"
					paths: [
						"/.well-known/openid-configuration",
					]
					pathPrefixes: [
						"/oauth/v2/",
						"/oidc/v1/",
						"/ui/",
						"/assets/",
					]
				},
				{
					name: "product-apex"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "gamma.verself.sh"
					backend:  "be_route_product_apex_verself_web_frontend"
				},
				{
					name: "company-apex"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "company"
					}
					hostname: "gamma.guardianintelligence.org"
					backend:  "be_route_company_apex_company_frontend"
				},
				{
					name: "billing-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "billing.api.gamma.verself.sh"
					backend:  "be_route_product_billing_api_billing_public_api"
				},
				{
					name: "deployment-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "deployments.api.gamma.verself.sh"
					backend:  "be_route_product_deployments_api_deployment_service_public_api"
				},
				{
					name: "distribution-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "distribution.api.gamma.verself.sh"
					backend:  "be_route_product_distribution_api_distribution_service_public_api"
				},
				{
					name: "oci-registry"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "oci.gamma.verself.sh"
					backend:  "be_route_product_distribution_api_distribution_service_public_api"
				},
				{
					name: "iam-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "iam.api.gamma.verself.sh"
					backend:  "be_route_product_iam_api_iam_service_public_api"
				},
				{
					name: "profile-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "profile.api.gamma.verself.sh"
					backend:  "be_route_product_profile_api_profile_service_public_api"
				},
				{
					name: "projects-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "projects.api.gamma.verself.sh"
					backend:  "be_route_product_projects_api_projects_service_public_api"
				},
				{
					name: "source-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "source.api.gamma.verself.sh"
					backend:  "be_route_product_source_api_source_code_hosting_service_public_api"
				},
				{
					name: "governance-api"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "governance.api.gamma.verself.sh"
					backend:  "be_route_product_governance_api_governance_service_public_api"
				},
				{
					name: "dashboard"
					originRef: {
						apiVersion: "networking.guardianintelligence.org/v1alpha1"
						kind:       "PublicOrigin"
						name:       "product"
					}
					hostname: "dashboard.gamma.verself.sh"
					backend:  "be_route_product_dashboard_grafana_operator_ui"
				},
			]
			readiness: paths: ["/.well-known/guardian/ready"]
		}
	},
	{
		apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
		kind:       "ZitadelCluster"
		metadata: name: "zitadel"
		spec: {
			externalDomain:  "gamma.verself.sh"
			runtimeArtifact: "bazel-bin/src/infrastructure-components/zitadel/zitadel-runtime.tar"
			runtimeRoot:     "/var/lib/zitadel/runtime"
			configFile:      "src/infrastructure-components/zitadel/config/config.yaml"
			setupStepsFile:  "src/infrastructure-components/zitadel/config/setup-steps.yaml"
			adminPATPath:    "/run/verself/recovery/zitadel/zitadel-admin/admin.pat"
			user:            "zitadel"
			group:           "zitadel"
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
				masterkeySecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "zitadel.masterkey"
				}
				adminPasswordRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "zitadel.admin_password"
				}
				adminPATSecretRefs: [
					{
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "auth-control-plane.zitadel.admin_token"
					},
					{
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "iam-service.zitadel.admin_token"
					},
				]
				smtpPasswordRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "zitadel.smtp.password"
				}
			}
			instance: {
				name:    "verself"
				orgName: "Guardian Intelligence LLC"
				human: {
					userName:  "anveio"
					firstName: "Anveio"
					lastName:  "Platform"
					email:     "anveio@gamma.verself.sh"
				}
				machine: {
					userName: "verself-admin"
					name:     "verself admin"
				}
			}
			smtp: {
				host:     "smtp.resend.com:465"
				user:     "resend"
				from:     "anveio@gamma.verself.sh"
				fromName: "verself"
			}
		}
	},
	{
		apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
		kind:       "ZitadelAuthControlPlane"
		metadata: name: "auth-control-plane"
		spec: {
			zitadelBaseURL:   "http://127.0.0.1:8085"
			zitadelHost:      "gamma.verself.sh"
			verselfDomain:    "gamma.verself.sh"
			iamServiceDomain: "iam.api.gamma.verself.sh"
			projectName:      "verself-api"
			browserAppName:   "verself-web"
			cliAppName:       "verself-cli"
			claimsTargetName: "verself-product-token-claims"
			claimsActionPath: "/internal/zitadel/actions/product-token-claims"
			openBao: {
				address: "https://127.0.0.1:8200"
				caCert:  "/etc/verself/openbao/ca.pem"
				adminPATSecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "auth-control-plane.zitadel.admin_token"
				}
				githubLoginClientID: ""
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "zitadel.masterkey"
		spec: {
			path:   "kv-runtime/data/secret/org/zitadel.masterkey"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "alphanumeric"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "zitadel.admin_password"
		spec: {
			path:   "kv-runtime/data/secret/org/zitadel.admin_password"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "alphanumeric"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "zitadel.smtp.password"
		spec: {
			path:   "kv-runtime/data/secret/org/zitadel.smtp.password"
			key:    "value"
			source: "operatorImport"
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "auth-control-plane.zitadel.admin_token"
		spec: {
			path:   "kv-runtime/data/secret/org/auth-control-plane.zitadel.admin_token"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelCluster"
				name:       "zitadel"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.admin_token"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.admin_token"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelCluster"
				name:       "zitadel"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "auth-control-plane.github_login.oauth_client_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/auth-control-plane.github_login.oauth_client_secret"
			key:    "value"
			source: "operatorImport"
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "object-storage-service.credential_kek"
		spec: {
			path:   "kv-runtime/data/secret/org/object-storage-service.credential_kek"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "hex"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "postgresql.pgbackrest.cipher_pass"
		spec: {
			path:   "kv-runtime/data/secret/org/postgresql.pgbackrest.cipher_pass"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.email_identity.hmac_key"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.email_identity.hmac_key"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    64
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.spicedb.grpc_preshared_key"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.spicedb.grpc_preshared_key"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    64
				encoding: "alphanumeric"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "grafana.admin_password"
		spec: {
			path:   "kv-runtime/data/secret/org/grafana.admin_password"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "grafana.secret_key"
		spec: {
			path:   "kv-runtime/data/secret/org/grafana.secret_key"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    48
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "forgejo.secret_key"
		spec: {
			path:   "kv-runtime/data/secret/org/forgejo.secret_key"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "forgejo.internal_token"
		spec: {
			path:   "kv-runtime/data/secret/org/forgejo.internal_token"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    48
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "forgejo.lfs_jwt_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/forgejo.lfs_jwt_secret"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "forgejo.oauth_jwt_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/forgejo.oauth_jwt_secret"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "source-code-hosting-service.forgejo.automation_token"
		spec: {
			path:   "kv-runtime/data/secret/org/source-code-hosting-service.forgejo.automation_token"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "forgejo.guardianintelligence.org/v1alpha1"
				kind:       "ForgejoInstance"
				name:       "forgejo"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "source-code-hosting-service.forgejo.webhook_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/source-code-hosting-service.forgejo.webhook_secret"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "governance-service.api_activity.hmac_key"
		spec: {
			path:   "kv-runtime/data/secret/org/governance-service.api_activity.hmac_key"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "hex"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "billing-service.stripe.secret_key"
		spec: {
			path:   "kv-runtime/data/secret/org/billing-service.stripe.secret_key"
			key:    "value"
			source: "operatorImport"
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "billing-service.stripe.webhook_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/billing-service.stripe.webhook_secret"
			key:    "value"
			source: "operatorImport"
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "stalwart.admin_password"
		spec: {
			path:   "kv-runtime/data/secret/org/stalwart.admin_password"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "electric.pg.password"
		spec: {
			path:   "kv-runtime/data/secret/org/electric.pg.password"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "electric.api_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/electric.api_secret"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "electric-notifications.pg.password"
		spec: {
			path:   "kv-runtime/data/secret/org/electric-notifications.pg.password"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "electric-notifications.api_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/electric-notifications.api_secret"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "electric-iam.pg.password"
		spec: {
			path:   "kv-runtime/data/secret/org/electric-iam.pg.password"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "electric-iam.api_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/electric-iam.api_secret"
			key:    "value"
			source: "generated"
			generate: {
				bytes:    32
				encoding: "base64url"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "object-storage-service.r2.admin_access_key_id"
		spec: {
			path:   "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
				kind:       "CloudflareControlPlane"
				name:       "gamma-cloudflare"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "object-storage-service.r2.admin_secret_access_key"
		spec: {
			path:   "kv-runtime/data/secret/org/object-storage-service.r2.admin_secret_access_key"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
				kind:       "CloudflareControlPlane"
				name:       "gamma-cloudflare"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "object-storage-service.r2.proxy_access_key_id"
		spec: {
			path:   "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
				kind:       "CloudflareControlPlane"
				name:       "gamma-cloudflare"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "object-storage-service.r2.proxy_secret_access_key"
		spec: {
			path:   "kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
				kind:       "CloudflareControlPlane"
				name:       "gamma-cloudflare"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.auth_audience"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_app_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_app_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_client_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_client_secret"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_secret"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_project_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_project_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_cli_app_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_cli_app_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_cli_client_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_cli_client_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.oidc_cli_project_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.oidc_cli_project_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.action_signing_key"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.action_signing_key"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "openbao.guardianintelligence.org/v1alpha1"
		kind:       "SecretPath"
		metadata: name: "iam-service.zitadel.github_login_idp_id"
		spec: {
			path:   "kv-runtime/data/secret/org/iam-service.zitadel.github_login_idp_id"
			key:    "value"
			source: "producedBy"
			producerRef: {
				apiVersion: "zitadel.guardianintelligence.org/v1alpha1"
				kind:       "ZitadelAuthControlPlane"
				name:       "auth-control-plane"
			}
		}
	},
	{
		apiVersion: "iam.guardianintelligence.org/v1alpha1"
		kind:       "IAMService"
		metadata: name: "iam-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			auth: {
				issuerURL:                   "https://gamma.verself.sh"
				browserAuthPublicBaseURL:    "https://gamma.verself.sh"
				pwnedPasswordsRangeEndpoint: "https://api.pwnedpasswords.com"
			}
			zitadel: host: "gamma.verself.sh"
			postgres: {
				dsn:      "postgres://iam_service@/iam_service?host=/var/run/postgresql&sslmode=disable"
				maxConns: 8
			}
			clickhouse: {
				address:    "127.0.0.1:9440"
				user:       "iam_service"
				caCertPath: "/etc/verself/clickhouse/server-ca.pem"
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
			credentials: {
				zitadelActionSigningKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.action_signing_key"
				}
				browserOIDCClientIDRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.oidc_client_id"
				}
				browserOIDCClientSecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.oidc_client_secret"
				}
				authAudienceRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.auth_audience"
				}
				zitadelAdminTokenRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.admin_token"
				}
				spiceDBPresharedKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.spicedb.grpc_preshared_key"
				}
				emailIdentityHMACKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.email_identity.hmac_key"
				}
			}
		}
	},
	{
		apiVersion: "deployment.guardianintelligence.org/v1alpha1"
		kind:       "DeploymentService"
		metadata: name: "deployment-service"
		spec: {
			site:          "gamma"
			publicBaseURL: "https://deployments.api.gamma.verself.sh"
			sourceRepo: {
				root:        "/var/lib/deployment-service/repo"
				url:         "https://github.com/guardian-intelligence/verself.git"
				initTimeout: "2m"
			}
			nomad: {
				address: "http://127.0.0.1:4646"
				jobPaths: [
					"src/integrations/cloudflare/control-plane/nomad.hcl",
				]
			}
			objectStorage: address: "https://object-storage-admin-internal-https"
			postgres: {
				dsn:      "postgres://deployment_service@/deployment_service?host=/var/run/postgresql&sslmode=disable"
				maxConns: 4
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
			admissionConcurrency: 4
			recoverySSHReady:     true
			githubOIDC: {
				allowedRepositories: "guardian-intelligence/verself"
				allowedRefs:         "refs/heads/main"
				allowedWorkflowRefs: "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main"
			}
		}
	},
	{
		apiVersion: "distribution.guardianintelligence.org/v1alpha1"
		kind:       "DistributionService"
		metadata: name: "distribution-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audience:  "376309723453988548"
			}
			postgres: {
				dsn:      "postgres://distribution_service@/distribution_service?host=/var/run/postgresql&sslmode=disable"
				maxConns: 8
			}
			clickhouse: {
				address:    "127.0.0.1:9440"
				user:       "distribution_service"
				caCertPath: "/etc/verself/clickhouse/server-ca.pem"
			}
			attestation: {
				trustedBuilders: ["spiffe://gamma.verself.sh/svc/release-builder"]
				trustedSigners: ["https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"]
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "secrets.guardianintelligence.org/v1alpha1"
		kind:       "SecretsService"
		metadata: name: "secrets-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			environment:    "single-node"
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audience:  "376309723453988548"
			}
			openbao: {
				address:          "https://127.0.0.1:8200"
				caCertPath:       "/etc/verself/openbao/ca.pem"
				kvPrefix:         "kv"
				transitPrefix:    "transit"
				jwtPrefix:        "jwt"
				spiffeJWTPrefix:  "spiffe-jwt"
				workloadAudience: "openbao"
			}
			runtimeSecretNamespace: "runtime"
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "profile.guardianintelligence.org/v1alpha1"
		kind:       "ProfileService"
		metadata: name: "profile-service"
		spec: {
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audience:  "376309723453988548"
			}
			postgres: {
				dsn:      "postgres://profile_service@/profile?host=/var/run/postgresql&sslmode=disable"
				maxConns: 8
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "projects.guardianintelligence.org/v1alpha1"
		kind:       "ProjectsService"
		metadata: name: "projects-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audienceRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.auth_audience"
				}
			}
			postgres: {
				dsn:      "postgres://projects_service@/projects_service?host=/var/run/postgresql&sslmode=disable"
				maxConns: 8
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "source.guardianintelligence.org/v1alpha1"
		kind:       "SourceCodeHostingService"
		metadata: name: "source-code-hosting-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			publicBaseURL:  "https://git.gamma.verself.sh"
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audienceRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.auth_audience"
				}
			}
			forgejo: {
				baseURL: "http://127.0.0.1:3000"
				owner:   "forgejo-automation"
				automationTokenRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "source-code-hosting-service.forgejo.automation_token"
				}
				webhookSecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "source-code-hosting-service.forgejo.webhook_secret"
				}
			}
			postgres: {
				dsn:      "postgres://source_code_hosting_service@/source_code_hosting?host=/var/run/postgresql&sslmode=disable"
				maxConns: 8
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "governance.guardianintelligence.org/v1alpha1"
		kind:       "GovernanceService"
		metadata: name: "governance-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			environment:    "single-node"
			publicBaseURL:  "https://governance.api.gamma.verself.sh"
			writer: instanceID: "gamma-primary"
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audienceRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.auth_audience"
				}
			}
			apiActivity: {
				hmacKeyID: "governance-service.v1"
				hmacKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "governance-service.api_activity.hmac_key"
				}
			}
			postgres: {
				dsn:      "postgres://governance_service@/governance_service?host=/var/run/postgresql&sslmode=disable"
				maxConns: 8
				identity: {
					dsn:      "postgres://iam_service@/iam_service?host=/var/run/postgresql&sslmode=disable"
					maxConns: 4
				}
				billing: {
					dsn:      "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
					maxConns: 4
				}
				sandbox: {
					dsn:      "postgres://sandbox_rental@/sandbox_rental?host=/var/run/postgresql&sslmode=disable"
					maxConns: 4
				}
			}
			clickhouse: {
				address:    "127.0.0.1:9440"
				user:       "governance_service"
				caCertPath: "/etc/verself/clickhouse/server-ca.pem"
			}
			exports: {
				dir:      "/var/lib/governance-service/exports"
				ttlHours: 168
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "billing.guardianintelligence.org/v1alpha1"
		kind:       "BillingService"
		metadata: name: "billing-service"
		spec: {
			installationID: "inst_gamma_01JZ0000000000000000000000"
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audienceRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.auth_audience"
				}
			}
			postgres: {
				dsn:                    "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
				maxConns:               12
				minConns:               1
				maxConnLifetimeSeconds: 1800
				maxConnIdleSeconds:     300
			}
			clickhouse: {
				address:    "127.0.0.1:9440"
				user:       "billing_service"
				caCertPath: "/etc/verself/clickhouse/server-ca.pem"
			}
			tigerbeetle: {
				addresses: ["127.0.0.1:3320"]
				clusterID: 0
			}
			billing: {
				returnOrigins: ["https://gamma.verself.sh"]
			}
			stripe: {
				secretKeyRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "billing-service.stripe.secret_key"
				}
				webhookSecretRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "billing-service.stripe.webhook_secret"
				}
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
	{
		apiVersion: "objectstorage.guardianintelligence.org/v1alpha1"
		kind:       "ObjectStorageService"
		metadata: name: "object-storage"
		spec: {
			site: "gamma"
			credentials: {
				credentialKEKRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "object-storage-service.credential_kek"
				}
			}
			provider: cloudflareR2: {
				endpoint: "https://c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com"
				region:   "auto"
				authorityRef: {
					apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
					kind:       "AccountAuthority"
					name:       "cloudflare-account-admin"
				}
				credentials: {
					adminAccessKeyIDRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "object-storage-service.r2.admin_access_key_id"
					}
					adminSecretAccessKeyRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "object-storage-service.r2.admin_secret_access_key"
					}
					proxyAccessKeyIDRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "object-storage-service.r2.proxy_access_key_id"
					}
					proxySecretAccessKeyRef: {
						apiVersion: "openbao.guardianintelligence.org/v1alpha1"
						kind:       "SecretPath"
						name:       "object-storage-service.r2.proxy_secret_access_key"
					}
				}
			}
			buckets: [
				{
					name:         "deployment-artifacts"
					providerName: "verself-deployment-artifacts"
					purpose:      "deploymentArtifacts"
				},
				{
					name:         "recovery"
					providerName: "verself-recovery"
					purpose:      "recovery"
				},
			]
			auth: {
				issuerURL: "https://gamma.verself.sh"
				audienceRef: {
					apiVersion: "openbao.guardianintelligence.org/v1alpha1"
					kind:       "SecretPath"
					name:       "iam-service.zitadel.auth_audience"
				}
			}
			postgres: dsn: "postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"
			clickhouse: {
				address:    "127.0.0.1:9440"
				user:       "object_storage_service"
				caCertPath: "/etc/verself/clickhouse/server-ca.pem"
			}
			spiffe: endpointSocket: "unix:///run/spire-agent/sockets/agent.sock"
		}
	},
]

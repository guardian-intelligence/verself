package gamma

import guardian "guardianintelligence.org/guardian-specification/cue/guardian/v1alpha1"

guardian.#Document

let uploadBundlePath = ".guardian/board/upload.tar.gz"
let uploadDigestPath = ".guardian/board/upload.sha256"
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
		}
	},
	{
		apiVersion: "substrate.guardianintelligence.org/v1alpha1"
		kind:       "Substrate"
		metadata: name: "gamma-primary"
		spec: {
			access: argv: [
				"ssh",
				"-T",
				"-o", "BatchMode=yes",
				"-o", "StrictHostKeyChecking=yes",
				"-o", "UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts",
				"-o", "ConnectTimeout=10",
				"ubuntu@206.223.228.87",
				"true",
			]
			upload: {
				bundlePath:   uploadBundlePath
				manifestPath: ".guardian/board/upload-manifest.json"
				digestPath:   uploadDigestPath
				run: argv: [
					"sh",
					"-c",
					"""
						set -eu
						remote_dir=/home/ubuntu/.local/state/guardian/uploads/current
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 ubuntu@206.223.228.87 "mkdir -p $remote_dir"
						rsync -a --timeout=60 -e "ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10" -- "\(uploadBundlePath)" "ubuntu@206.223.228.87:$remote_dir/upload.tar.gz"
						""",
				]
				extract: argv: [
					"sh",
					"-c",
					"""
						set -eu
						digest_dir=$(tr ':' '-' < "\(uploadDigestPath)")
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 ubuntu@206.223.228.87 "sh -s -- '$digest_dir'" <<'REMOTE'
						set -eu
						digest_dir=$1
						repo_root=/home/ubuntu/.local/state/guardian/repo
						release="$repo_root/releases/$digest_dir"
						tmp="$repo_root/tmp/$digest_dir.$$"
						archive=/home/ubuntu/.local/state/guardian/uploads/current/upload.tar.gz
						mkdir -p "$repo_root/releases" "$repo_root/tmp"
						trap 'rm -rf "$tmp"' EXIT
						if [ -d "$release" ] && (cd "$release" && sha256sum -c guardian-upload-sha256sums.txt >/dev/null); then
							:
						else
							rm -rf "$tmp"
							mkdir -p "$tmp"
							tar -xzf "$archive" -C "$tmp"
							(cd "$tmp" && sha256sum -c guardian-upload-sha256sums.txt >/dev/null)
							rm -rf "$release"
							mv "$tmp" "$release"
						fi
						ln -sfn "$release" "$repo_root/current.next"
						if [ -d "$repo_root/current" ] && [ ! -L "$repo_root/current" ]; then
							rm -rf "$repo_root/current"
						fi
						mv -Tf "$repo_root/current.next" "$repo_root/current"
						REMOTE
						""",
				]
				verify: argv: [
					"sh",
					"-c",
					"""
						set -eu
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 ubuntu@206.223.228.87 'cd /home/ubuntu/.local/state/guardian/repo/current && sha256sum -c guardian-upload-sha256sums.txt >/dev/null && sha256sum /home/ubuntu/.local/state/guardian/uploads/current/upload.tar.gz'
						""",
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
			caCert:           "/etc/openbao/tls/cert.pem"
			runtimeRoot:      "/var/lib/openbao/runtime"
			dataDir:          "/var/lib/openbao/raft"
			configPath:       "/etc/openbao/openbao.hcl"
			reportPath:       "/run/verself/recovery/openbao/report.json"
			initMaterialPath: "/run/verself/recovery/openbao/init-material.json"
			freshInit: wipeDataDirBeforeStart: true
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
			baseline: {
				reconcile: true
				mounts: [
					{
						path:        "kv-runtime"
						type:        "kv"
						description: "runtime secret material"
						options: version: "2"
					},
					{
						path:        "kv-controller"
						type:        "kv"
						description: "controller secret material"
						options: version: "2"
					},
					{
						path:        "transit"
						type:        "transit"
						description: "controller cryptographic operations"
					},
				]
				policies: [
					{
						name: "cloudflare-integration-recovery-runtime"
						hcl: """
							path "kv-controller/data/integrations/cloudflare/account-admin" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.admin_secret_access_key" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "object-storage-service-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/object-storage-service.credential_kek" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.admin_secret_access_key" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" {
							  capabilities = ["read"]
							}
							"""
					},
					{
						name: "zitadel-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/zitadel.masterkey" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/zitadel.admin_password" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/zitadel.smtp.password" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/auth-control-plane.zitadel.admin_token" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.admin_token" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "auth-control-plane-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/auth-control-plane.zitadel.admin_token" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/auth-control-plane.github_login.oauth_client_secret" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.auth_audience" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_app_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_client_secret" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_project_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_cli_app_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_cli_client_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.oidc_cli_project_id" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.action_signing_key" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/iam-service.zitadel.github_login_idp_id" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "spicedb-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/iam-service.spicedb.grpc_preshared_key" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "grafana-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/grafana.admin_password" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/grafana.secret_key" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
				]
				nomadJWT: {
					path:        "jwt-nomad"
					description: "Verself Nomad workload identity auth"
					jwksURL:     "http://127.0.0.1:4646/.well-known/jwks.json"
					supportedAlgs: ["RS256", "EdDSA"]
					roles: [
						{
							name:     "cloudflare-integration-recovery-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "cloudflare-integration-recovery"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["cloudflare-integration-recovery-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "object-storage-service-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "object-storage-service"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["object-storage-service-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "zitadel-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "zitadel"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["zitadel-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "auth-control-plane-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "auth-control-plane"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["auth-control-plane-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "spicedb-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "spicedb"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["spicedb-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "grafana-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "grafana"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["grafana-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
					]
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
			otel: exporterEndpoint: "http://127.0.0.1:4317"
			capture: {
				workers:         4
				queueSize:       128
				stderrTailBytes: 65536
				stdoutTailBytes: 32768
			}
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
			runtimeArtifact:              "bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar"
			runtimeRoot:                  "/var/lib/postgresql/runtime"
			dataDir:                      "/var/lib/postgresql/16/verself"
			configDir:                    "/etc/postgresql/verself"
			logDir:                       "/var/log/postgresql"
			socketDir:                    "/var/run/postgresql"
			reportPath:                   "/run/verself/recovery/postgresql/report.json"
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
			]
			roles: [
				{
					name:  "otelcol"
					login: true
					memberOf: ["pg_monitor"]
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
			addresses:       ["127.0.0.1:3320"]
			logLevel:        "info"
			statsdAddress:   "127.0.0.1:8125"
			experimental:    true
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
			log: level: "http"
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
				enabled:      true
				headerName:   "X-Pomerium-Jwt-Assertion"
				emailClaim:   "email"
				usernameClaim: "email"
				jwkSetURL:    "https://dashboard.gamma.verself.sh/.well-known/pomerium/jwks.json"
				cacheTTL:     "60m"
				autoSignUp:   true
			}
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
				bucket:        "verself-deployment-artifacts"
				childTokenTTL: "168h"
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
						"deployments.api.gamma.verself.sh",
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
				encoding: "base64url"
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

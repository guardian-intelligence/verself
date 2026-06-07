package gamma

import guardian "guardianintelligence.org/guardian-specification/cue/guardian/v1alpha1"

guardian.#Document

let openbaoOperatorAPublicKeyBase64 = "mDMEaiRhIxYJKwYBBAHaRw8BAQdA8GOif2dDXBWOBg/GHP6LO8LwNw2MfZx5W/oI0Z/gtYG0VUdhbW1hIE9wZW5CYW8gb3BlcmF0b3ItYSA8b3BlcmF0b3ItYS5nYW1tYS1vcGVuYmFvLTIwMjYwNjA2QGd1YXJkaWFuaW50ZWxsaWdlbmNlLm9yZz6ImQQTFgoAQRYhBGNW1vGxjQtSegmSQ20xt8fxFYl3BQJqJGEjAhsBBQkB4TOABQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEG0xt8fxFYl35VUBAOixZYI/dNDzn9VyzHO7bFN0GNArLEcLq6BFEGQ7IjXaAQD/gDiRo3hCwkwZFd8xDhcwmjAueFbz/EE+7nUZBRHwArg4BGokYSMSCisGAQQBl1UBBQEBB0DLYp659TibCfBxFLfSkIhF/xXEQ7W1wp598YBbYSkEJwMBCAeIfgQYFgoAJhYhBGNW1vGxjQtSegmSQ20xt8fxFYl3BQJqJGEjAhsMBQkB4TOAAAoJEG0xt8fxFYl36KkBAPPHsGHCl7iMKj1mv6pOiq3IpmUHiKQuPcxaQJosOywhAP0SClTwfkanxQCoqRQ25xwDgXl0T5G+CTAFTaa4prXVCQ=="
let openbaoOperatorBPublicKeyBase64 = "mDMEaiRhIxYJKwYBBAHaRw8BAQdAjfu3dNTLPT+QgJhV7iGb7JDb3+YeefJudUIQJtjo6Ya0VUdhbW1hIE9wZW5CYW8gb3BlcmF0b3ItYiA8b3BlcmF0b3ItYi5nYW1tYS1vcGVuYmFvLTIwMjYwNjA2QGd1YXJkaWFuaW50ZWxsaWdlbmNlLm9yZz6ImQQTFgoAQRYhBGz35jGr25/6zSNxaqcfyZRqEfguBQJqJGEjAhsBBQkB4TOABQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEKcfyZRqEfguSNgA/R+3WOoRCTGMsd9Xa1OoB020KL2aacC2YIKuaxSzpoj0AQCUWTuBBFajVkoVNoiVuF6Xqamil4n35/SJkCI9axIhDbg4BGokYSMSCisGAQQBl1UBBQEBB0D1I/QMcqzJsawZTvYQi7dXDaDekRHSEoOUNn+Ma4kcKQMBCAeIfgQYFgoAJhYhBGz35jGr25/6zSNxaqcfyZRqEfguBQJqJGEjAhsMBQkB4TOAAAoJEKcfyZRqEfguPKYBAPOJPFkx4dgU4ikm2G5cHp3dK8zUVkvVnkmWwl2mkXGqAQCj2InQlPCc3b7Vo/N3nTv3y77NhQ+nTvgbomBw4xddDQ=="
let openbaoOperatorCPublicKeyBase64 = "mDMEaiRhIxYJKwYBBAHaRw8BAQdAUMZkn5+mJ5B/rStGFsRXl4mO4JnWvMbbAvFv+Vf1K160VUdhbW1hIE9wZW5CYW8gb3BlcmF0b3ItYyA8b3BlcmF0b3ItYy5nYW1tYS1vcGVuYmFvLTIwMjYwNjA2QGd1YXJkaWFuaW50ZWxsaWdlbmNlLm9yZz6ImQQTFgoAQRYhBA8rlTrW1GIuHSCz4J4Q3PJiM5yyBQJqJGEjAhsBBQkB4TOABQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEJ4Q3PJiM5yym6ABAN779R1z6W9XrvGj4QOro0F3ip1FgNbs4mzJmY+dhlNSAQCkzOWEj7Gk1XjZOoy08vuDobAZKnJcDF2DzcG1VtThCbg4BGokYSMSCisGAQQBl1UBBQEBB0B2JyTNSo6Q1ib5/5gZWwxE14reUtgPPW7RuMCQUDwyKwMBCAeIfgQYFgoAJhYhBA8rlTrW1GIuHSCz4J4Q3PJiM5yyBQJqJGEjAhsMBQkB4TOAAAoJEJ4Q3PJiM5yyqMYBAN3ZvGO53CfMZottjtIiKmfJnX3coPpE9oty2zu2dBBaAQCvhc/5+WiIxyeaTZFvoQ2F3PIALUXUXM16Q8FmhRRqBQ=="

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
			nomad: run: argv: [
				"sh",
				"-c",
				"""
					set -eu
					ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 ubuntu@206.223.228.87 'sh -s' <<-'REMOTE'
					set -eu
					nomad=/opt/verself/profile/bin/nomad
					job=/home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/openbao/nomad.hcl
					report=/run/verself/recovery/openbao/report.json
					sudo rm -f "$report"
					NOMAD_ADDR=http://127.0.0.1:4646 "$nomad" job run -detach "$job"
					for _ in $(seq 1 240); do
						NOMAD_ADDR=http://127.0.0.1:4646 "$nomad" job status openbao
						if sudo test -f "$report"; then
							sudo cat "$report"
							if sudo python3 -c 'import json, sys; doc=json.load(open(sys.argv[1], encoding="utf-8")); conditions=doc.get("conditions", []); sys.exit(0 if any(c.get("type") == "OpenBaoRecoveryComplete" and c.get("status") == "True" for c in conditions) else 1)' "$report"; then
								exit 0
							fi
						fi
						sleep 1
					done
					exit 1
					REMOTE
					""",
			]
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
				run: argv: [
					"sh",
					"-c",
					"""
						set -eu
						rsync_bin=./bazel-bin/src/guardian-specification/tools/rsync
						test -x "$rsync_bin"
						"$rsync_bin" --version >/dev/null
						ssh_opts='ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10'
						remote=ubuntu@206.223.228.87
						remote_root=/home/ubuntu/.local/state/guardian/repo
						artifact_paths='
						bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian
						bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar
						bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover
						bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer.tar
						bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer_/nomad-observer
						bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar
						bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover
						bazel-bin/src/integrations/cloudflare/control-plane/cloudflare-control-plane-runtime.tar
						bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar
						bazel-bin/src/infrastructure-components/clickhouse/clickhouse-runtime.tar
						bazel-bin/src/infrastructure-components/clickhouse/cmd/clickhouse-recover/clickhouse-recover_/clickhouse-recover
						bazel-bin/src/infrastructure-components/electric/electric-runtime.tar
						bazel-bin/src/infrastructure-components/electric/cmd/electric-recover/electric-recover_/electric-recover
						bazel-bin/src/infrastructure-components/forgejo/forgejo-runtime.tar
						bazel-bin/src/infrastructure-components/forgejo/cmd/forgejo-recover/forgejo-recover_/forgejo-recover
						bazel-bin/src/infrastructure-components/grafana/grafana-runtime.tar
						bazel-bin/src/infrastructure-components/grafana/cmd/grafana-recover/grafana-recover_/grafana-recover
						bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar
						bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar
						bazel-bin/src/infrastructure-components/nftables/cmd/nftables-apply/nftables-apply_/nftables-apply
						bazel-bin/src/infrastructure-components/otelcol/otelcol-runtime.tar
						bazel-bin/src/infrastructure-components/otelcol/otelcol-config.tar
						bazel-bin/src/infrastructure-components/otelcol/cmd/otelcol-recover/otelcol-recover_/otelcol-recover
						bazel-bin/src/infrastructure-components/nats/nats-runtime.tar
						bazel-bin/src/infrastructure-components/nats/cmd/nats-recover/nats-recover_/nats-recover
						bazel-bin/src/infrastructure-components/spicedb/spicedb-runtime.tar
						bazel-bin/src/infrastructure-components/spicedb/cmd/spicedb-recover/spicedb-recover_/spicedb-recover
						bazel-bin/src/infrastructure-components/stalwart/stalwart-runtime.tar
						bazel-bin/src/infrastructure-components/stalwart/cmd/stalwart-recover/stalwart-recover_/stalwart-recover
						bazel-bin/src/infrastructure-components/tigerbeetle/tigerbeetle-runtime.tar
						bazel-bin/src/infrastructure-components/tigerbeetle/cmd/tigerbeetle-recover/tigerbeetle-recover_/tigerbeetle-recover
						bazel-bin/src/infrastructure-components/verdaccio/verdaccio-runtime.tar
						bazel-bin/src/infrastructure-components/verdaccio/cmd/verdaccio-recover/verdaccio-recover_/verdaccio-recover
						bazel-bin/src/infrastructure-components/zitadel/zitadel-runtime.tar
						bazel-bin/src/infrastructure-components/zitadel/cmd/zitadel-setup-apply/zitadel-setup-apply_/zitadel-setup-apply
						bazel-bin/src/infrastructure-components/zitadel/cmd/auth-control-plane-apply/auth-control-plane-apply_/auth-control-plane-apply
						bazel-bin/src/infrastructure-components/zot/zot-runtime.tar
						bazel-bin/src/infrastructure-components/zot/cmd/zot-recover/zot-recover_/zot-recover
						bazel-bin/src/infrastructure-components/spire/spire-recover_/spire-recover
						bazel-bin/src/infrastructure-components/spire/spire-runtime.tar
						bazel-bin/src/infrastructure-components/spire/identity_registry.spire_identity_registry.json
						bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service_/object-storage-service
						bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar
						'
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 "$remote" 'command -v rsync >/dev/null || { echo remote rsync missing >&2; exit 127; }'
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 "$remote" "sudo rm -rf '$remote_root/next' && mkdir -p '$remote_root/next/workspace' '$remote_root/next/bazel-bin'"
						"$rsync_bin" -a --delete --timeout=60 --filter=':- .gitignore' --exclude='.git/' --exclude='.guardian/' --exclude='bazel-*' -e "$ssh_opts" ./ "$remote:$remote_root/next/workspace/"
						"$rsync_bin" -a --timeout=60 --relative -e "$ssh_opts" .guardian/fly/document.json "$remote:$remote_root/next/workspace/"
						printf '%s\n' $artifact_paths | "$rsync_bin" -aL --mkpath --relative --files-from=- --timeout=60 -e "$ssh_opts" ./ "$remote:$remote_root/next/"
						""",
				]
				extract: argv: [
					"sh",
					"-c",
					"""
						set -eu
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 ubuntu@206.223.228.87 'sh -s' <<-'REMOTE'
						set -eu
						repo_root=/home/ubuntu/.local/state/guardian/repo
						test -f "$repo_root/next/workspace/.guardian/fly/document.json"
						test -d "$repo_root/next/bazel-bin"
						if [ -e "$repo_root/current" ] || [ -L "$repo_root/current" ]; then
							sudo rm -rf "$repo_root/previous"
							sudo mv -Tf "$repo_root/current" "$repo_root/previous"
						fi
						sudo mv -Tf "$repo_root/next" "$repo_root/current"
						REMOTE
						""",
				]
				verify: argv: [
					"sh",
					"-c",
					"""
						set -eu
						rsync_bin=./bazel-bin/src/guardian-specification/tools/rsync
						test -x "$rsync_bin"
						"$rsync_bin" --version >/dev/null
						ssh_opts='ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10'
						remote=ubuntu@206.223.228.87
						remote_root=/home/ubuntu/.local/state/guardian/repo
						artifact_paths='
						bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian
						bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar
						bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover
						bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer.tar
						bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer_/nomad-observer
						bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar
						bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover
						bazel-bin/src/integrations/cloudflare/control-plane/cloudflare-control-plane-runtime.tar
						bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar
						bazel-bin/src/infrastructure-components/clickhouse/clickhouse-runtime.tar
						bazel-bin/src/infrastructure-components/clickhouse/cmd/clickhouse-recover/clickhouse-recover_/clickhouse-recover
						bazel-bin/src/infrastructure-components/electric/electric-runtime.tar
						bazel-bin/src/infrastructure-components/electric/cmd/electric-recover/electric-recover_/electric-recover
						bazel-bin/src/infrastructure-components/forgejo/forgejo-runtime.tar
						bazel-bin/src/infrastructure-components/forgejo/cmd/forgejo-recover/forgejo-recover_/forgejo-recover
						bazel-bin/src/infrastructure-components/grafana/grafana-runtime.tar
						bazel-bin/src/infrastructure-components/grafana/cmd/grafana-recover/grafana-recover_/grafana-recover
						bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar
						bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar
						bazel-bin/src/infrastructure-components/nftables/cmd/nftables-apply/nftables-apply_/nftables-apply
						bazel-bin/src/infrastructure-components/otelcol/otelcol-runtime.tar
						bazel-bin/src/infrastructure-components/otelcol/otelcol-config.tar
						bazel-bin/src/infrastructure-components/otelcol/cmd/otelcol-recover/otelcol-recover_/otelcol-recover
						bazel-bin/src/infrastructure-components/nats/nats-runtime.tar
						bazel-bin/src/infrastructure-components/nats/cmd/nats-recover/nats-recover_/nats-recover
						bazel-bin/src/infrastructure-components/spicedb/spicedb-runtime.tar
						bazel-bin/src/infrastructure-components/spicedb/cmd/spicedb-recover/spicedb-recover_/spicedb-recover
						bazel-bin/src/infrastructure-components/stalwart/stalwart-runtime.tar
						bazel-bin/src/infrastructure-components/stalwart/cmd/stalwart-recover/stalwart-recover_/stalwart-recover
						bazel-bin/src/infrastructure-components/tigerbeetle/tigerbeetle-runtime.tar
						bazel-bin/src/infrastructure-components/tigerbeetle/cmd/tigerbeetle-recover/tigerbeetle-recover_/tigerbeetle-recover
						bazel-bin/src/infrastructure-components/verdaccio/verdaccio-runtime.tar
						bazel-bin/src/infrastructure-components/verdaccio/cmd/verdaccio-recover/verdaccio-recover_/verdaccio-recover
						bazel-bin/src/infrastructure-components/zitadel/zitadel-runtime.tar
						bazel-bin/src/infrastructure-components/zitadel/cmd/zitadel-setup-apply/zitadel-setup-apply_/zitadel-setup-apply
						bazel-bin/src/infrastructure-components/zitadel/cmd/auth-control-plane-apply/auth-control-plane-apply_/auth-control-plane-apply
						bazel-bin/src/infrastructure-components/zot/zot-runtime.tar
						bazel-bin/src/infrastructure-components/zot/cmd/zot-recover/zot-recover_/zot-recover
						bazel-bin/src/infrastructure-components/spire/spire-recover_/spire-recover
						bazel-bin/src/infrastructure-components/spire/spire-runtime.tar
						bazel-bin/src/infrastructure-components/spire/identity_registry.spire_identity_registry.json
						bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service_/object-storage-service
						bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar
						'
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 "$remote" 'command -v rsync >/dev/null || { echo remote rsync missing >&2; exit 127; }'
						workspace_delta="$("$rsync_bin" -a --omit-dir-times --dry-run --checksum --itemize-changes --delete --timeout=60 --filter=':- .gitignore' --exclude='.git/' --exclude='.guardian/' --exclude='bazel-*' -e "$ssh_opts" ./ "$remote:$remote_root/current/workspace/")"
						fly_delta="$("$rsync_bin" -a --dry-run --checksum --itemize-changes --timeout=60 --relative -e "$ssh_opts" .guardian/fly/document.json "$remote:$remote_root/current/workspace/")"
						artifact_delta="$(printf '%s\n' $artifact_paths | "$rsync_bin" -aL --mkpath --dry-run --checksum --itemize-changes --relative --files-from=- --timeout=60 -e "$ssh_opts" ./ "$remote:$remote_root/current/")"
						test -z "$workspace_delta"
						test -z "$fly_delta"
						test -z "$artifact_delta"
						ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 "$remote" 'cd /home/ubuntu/.local/state/guardian/repo/current && find workspace bazel-bin -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum'
						""",
				]
			}
			kernel: {
				openbaoPrepare: argv: [
					"ssh",
					"-T",
					"-o", "BatchMode=yes",
					"-o", "StrictHostKeyChecking=yes",
					"-o", "UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts",
					"-o", "ConnectTimeout=10",
					"ubuntu@206.223.228.87",
					"sudo /home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover prepare --repo-root=/home/ubuntu/.local/state/guardian/repo/current --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json --resource-name=openbao",
				]
				nomad: argv: [
					"ssh",
					"-T",
					"-o", "BatchMode=yes",
					"-o", "StrictHostKeyChecking=yes",
					"-o", "UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts",
					"-o", "ConnectTimeout=10",
					"ubuntu@206.223.228.87",
					"sudo /home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover --repo-root=/home/ubuntu/.local/state/guardian/repo/current --address=http://127.0.0.1:4646",
				]
				verify: argv: [
					"sh",
					"-c",
					"""
							set -eu
							ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/home/ubuntu/.ssh/known_hosts -o ConnectTimeout=10 ubuntu@206.223.228.87 'sh -s' <<-'REMOTE'
							set -eu
							nomad=/opt/verself/profile/bin/nomad
							sudo test -f /etc/verself/openbao/ca.pem
							sudo grep -q '^vault {' /etc/nomad/nomad.hcl
							NOMAD_ADDR=http://127.0.0.1:4646 "$nomad" job validate /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/openbao/nomad.hcl
							NOMAD_ADDR=http://127.0.0.1:4646 "$nomad" job validate /home/ubuntu/.local/state/guardian/repo/current/workspace/src/integrations/cloudflare/control-plane/nomad.hcl
							NOMAD_ADDR=http://127.0.0.1:4646 "$nomad" job validate /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/postgresql/nomad.hcl
							REMOTE
						""",
				]
			}
			remote: {
				repoRoot:  "/home/ubuntu/.local/state/guardian/repo/current"
				guardian:  "/home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian"
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
						name: "cloudflare-account-admin-import"
						hcl: """
							path "kv-controller/data/integrations/cloudflare/account-admin" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "openbao-reconcile-runtime"
						hcl: """
							path "sys/mounts" {
							  capabilities = ["read", "sudo"]
							}
							path "sys/mounts/*" {
							  capabilities = ["create", "update", "read", "delete", "list", "sudo"]
							}
							path "sys/auth" {
							  capabilities = ["read", "sudo"]
							}
							path "sys/auth/*" {
							  capabilities = ["create", "update", "read", "delete", "list", "sudo"]
							}
							path "sys/policies/acl/*" {
							  capabilities = ["create", "update", "read", "delete", "list", "sudo"]
							}
							path "auth/jwt-nomad/config" {
							  capabilities = ["create", "update", "read", "sudo"]
							}
							path "auth/jwt-nomad/role/*" {
							  capabilities = ["create", "update", "read", "delete", "list", "sudo"]
							}
							path "kv-runtime/data/secret/org/*" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
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
							path "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "postgresql-runtime"
						hcl: """
							path "kv-controller/data/integrations/cloudflare/r2/capabilities/recovery" {
							  capabilities = ["read"]
							}
							path "kv-runtime/data/secret/org/postgresql.pgbackrest.cipher_pass" {
							  capabilities = ["read"]
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
					{
						name: "forgejo-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/forgejo.secret_key" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/forgejo.internal_token" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/forgejo.lfs_jwt_secret" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/forgejo.oauth_jwt_secret" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/source-code-hosting-service.forgejo.automation_token" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "stalwart-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/stalwart.admin_password" {
							  capabilities = ["create", "update", "read"]
							}
							"""
					},
					{
						name: "electric-runtime"
						hcl: """
							path "kv-runtime/data/secret/org/electric.pg.password" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/electric.api_secret" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/electric-notifications.pg.password" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/electric-notifications.api_secret" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/electric-iam.pg.password" {
							  capabilities = ["create", "update", "read"]
							}
							path "kv-runtime/data/secret/org/electric-iam.api_secret" {
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
							name:     "openbao-reconcile-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "openbao"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["openbao-reconcile-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
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
							name:     "postgresql-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "postgresql"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["postgresql-runtime"]
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
						{
							name:     "forgejo-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "forgejo"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["forgejo-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "stalwart-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "stalwart"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["stalwart-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
						{
							name:     "electric-runtime"
							roleType: "jwt"
							boundAudiences: ["vault.io"]
							boundClaims: nomad_job_id: "electric"
							userClaim:            "/nomad_job_id"
							userClaimJSONPointer: true
							claimMappings: {
								nomad_namespace: "nomad_namespace"
								nomad_job_id:    "nomad_job_id"
								nomad_task:      "nomad_task"
							}
							tokenType: "service"
							tokenPolicies: ["electric-runtime"]
							tokenPeriod:         "30m"
							tokenExplicitMaxTTL: 0
						},
					]
				}
				operatorImportTokens: [
					{
						name:   "cloudflare-account-admin-import"
						policy: "cloudflare-account-admin-import"
						ttl:    "4h"
						uses:   5
					},
				]
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
				encoding: "password"
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

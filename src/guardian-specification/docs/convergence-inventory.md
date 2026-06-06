# Convergence Inventory

This inventory records the current gamma recovery state for each component that
has a component CRD in the Guardian graph. A component is converged when the
boarded graph is current, the component-owned Nomad job is healthy, and two
consecutive submissions do not create unexpected allocation churn.

## Gamma

| Component | CRD | Current State | Blocking Conditions | Dependencies Needed For Convergence |
| --- | --- | --- | --- | --- |
| Substrate boarding | `substrate.guardianintelligence.org/v1alpha1/Substrate/gamma-primary` | Converged | None | SSH access, local build artifacts, upload/extract/verify hooks |
| Nomad runtime | component bootstrap machinery | Converged | None | Boarded repo, pinned Nomad runtime artifact, `nomad-recover`, root access for systemd and host config |
| OpenBao | `openbao.guardianintelligence.org/v1alpha1/OpenBaoCluster/openbao` | Server available, baseline blocked | `RootTrustMaterialAvailable=False`, `OpenBaoBaselineReconciled=False` | Operator root authority: a valid operator token or threshold unseal shares that can generate a transient root token |
| Cloudflare Control Plane | `cloudflare.guardianintelligence.org/v1alpha1/CloudflareControlPlane/gamma-cloudflare` | Blocked | `CloudflareAccountAuthorityAvailable=False` | Operator imports Cloudflare account-admin credential into `kv-controller/data/integrations/cloudflare/account-admin` through the component import action |
| HAProxy | `haproxy.guardianintelligence.org/v1alpha1/HAProxyGateway/public-edge` | Auto-reverted to board-only allocation | `PublicTLSCertificateMaterialAvailable=False` | Public certificate files for `gamma.verself.sh` and `gamma.guardianintelligence.org` |
| nftables | `nftables.guardianintelligence.org/v1alpha1/NftablesFirewall/nftables` | Converged | None | Boarded nftables runtime artifact, root access for kernel ruleset and systemd unit installation |
| NATS | `nats.guardianintelligence.org/v1alpha1/NATSCluster/nats` | Converged | None | Boarded NATS runtime artifact, SPIFFE helper, NATS SPIFFE identity, monitoring `/varz` check |
| Nomad Observer | `nomadobserver.guardianintelligence.org/v1alpha1/NomadObserver/nomad-observer` | Converged | None | Boarded Nomad Observer runtime artifact, Nomad API, SPIFFE identity, ClickHouse `nomad_observer` user |
| OTel Collector | `otelcol.guardianintelligence.org/v1alpha1/OtelCollector/otelcol` | Converged | None | Boarded OTel Collector runtime/config artifacts, SPIFFE helper, ClickHouse `otelcol` user, PostgreSQL `otelcol` peer role |
| Zitadel/Auth Control Plane | `zitadel.guardianintelligence.org/v1alpha1/ZitadelCluster/zitadel`, `zitadel.guardianintelligence.org/v1alpha1/ZitadelAuthControlPlane/auth-control-plane` | Dependency model declared; jobs not submitted in current gamma state | OpenBao baseline blocked; runtime jobs still need artifact/config CRD cutover | OpenBao baseline roles, generated Zitadel masterkey/admin password, operator-imported SMTP/GitHub material as configured, PostgreSQL `zitadel` database |
| PostgreSQL | `postgresql.guardianintelligence.org/v1alpha1/PostgreSQLCluster/postgresql` | Converged | None | Boarded PostgreSQL runtime artifact, component-owned recovery job, service database/peer mapping config |
| ClickHouse | `clickhouse.guardianintelligence.org/v1alpha1/ClickHouseCluster/clickhouse` | Converged | None | Boarded ClickHouse runtime artifact, SPIFFE helper, server/operator SPIFFE identities, schema migrations |
| Object Storage Service | `objectstorage.guardianintelligence.org/v1alpha1/ObjectStorageService/object-storage` | Last rehearsal setup/migrations passed; job currently purged after runtime token failure | OpenBao JWT role missing: `object-storage-service-runtime` | OpenBao baseline reconciliation, Cloudflare-produced R2 credentials, and Zitadel-produced auth audience |

## Latest Gamma Evidence

Live command:

```sh
guardian fly src/guardian-specification/examples/gamma/gamma.cue -o json --stream
```

Observed results:

- boarding verified the extracted repo tree on gamma;
- latest resource graph digest:
  `sha256:07e3488cfc808a99506a2d791b0336bfab9873c856ad4e6c33d0651c28b3c014`;
- latest verified upload digest:
  `sha256:7a9424f90cade7092efef8b9b07fa7cfb8bf48bc198e95ca5133908f3fa69a3c`;
- the boarded repo contains `.guardian/fly/document.json` with OpenBao,
  PostgreSQL, ClickHouse, nftables, NATS, Nomad Observer, OTel Collector,
  Cloudflare, HAProxy, object-storage, substrate, and public-origin resources;
- remote Nomad validation succeeds for OpenBao, PostgreSQL, ClickHouse,
  nftables, NATS, Nomad Observer, OTel Collector, HAProxy, Cloudflare, and
  object-storage job files;
- Nomad is running and reachable on gamma;
- OpenBao initialized a Shamir store with PGP-encrypted init material and is
  unsealed, but baseline reconciliation is blocked until operator root
  authority is presented;
- Cloudflare cannot derive its Nomad workload OpenBao token until the OpenBao
  baseline creates the `cloudflare-integration-recovery-runtime` role;
- after OpenBao baseline reconciliation, Cloudflare recovery is expected to
  block on missing account-admin authority in OpenBao:
  `kv-controller/data/integrations/cloudflare/account-admin`;
- the Cloudflare CRD carries the OpenBao account-admin path and omits durable
  host file paths for provider token values;
- the Cloudflare CRD declares `accountAdminOpenBaoPath`, and the component
  import action can accept operator-provided OpenBao and Cloudflare authority as
  a JSON stdin payload;
- submitting the updated HAProxy job exercises the component-owned prestart and
  fails on missing `/etc/haproxy/certs/gamma.guardianintelligence.org.pem`;
- Nomad auto-reverts HAProxy to the previous board-only allocation after the
  failed public-edge update;
- component truth comes from Nomad status, component report files, service
  health checks, and telemetry rather than Guardian command output.
- object-storage-service now reads its static runtime configuration from its
  `ObjectStorageService` CRD and uses the boarded repo artifact instead of
  deployment-service artifact URIs;
- object-storage-service Nomad validation succeeds and the runtime artifact is
  present in the boarded tree;
- object-storage-service setup projects the boarded graph into
  `/run/verself/recovery/object-storage/document.json` for the service user;
- PostgreSQL now has a component CRD and a component-owned Nomad job that
  installs the boarded runtime artifact, initializes the data directory when
  empty, starts `postgres`, and runs a poststart reconciliation loop;
- PostgreSQL reports healthy in Nomad, writes
  `/run/verself/recovery/postgresql/report.json`, and exposes
  `/var/run/postgresql/.s.PGSQL.5432`;
- PostgreSQL reconciled the `object_storage_service` role, database, and peer
  mappings for `object_storage_service` and `object_storage_admin`;
- PostgreSQL reconciled the `otelcol` peer role and granted `pg_monitor` for
  the OTel PostgreSQL receiver;
- ClickHouse now has a component CRD and a component-owned Nomad job;
- ClickHouse installs the boarded runtime artifact, writes TLS/SPIFFE/systemd
  config, starts ClickHouse through systemd, applies repo migrations, and
  keeps a Nomad monitor task tied to live operator queries;
- ClickHouse reports `ClickHouseRecoveryComplete=True` in
  `/run/verself/recovery/clickhouse/report.json`;
- ClickHouse runtime digest:
  `sha256:ea1e9ed8e240f65ba8b7ce43686d5229e103a695beb0e2d0b52bd4733f2d8bfd`;
- ClickHouse bootstrap schema fingerprint:
  `sha256:35cc339b28bdc76533edc965ddbe65fa4a6e66f61bcabbbe38b59f6c018b0e6d`;
- a live operator query reports 32 tables in the `verself` database;
- nftables now has a component CRD and a component-owned Nomad batch job;
- nftables installs the boarded runtime tar into `/opt/verself/nftables`,
  writes `/etc/nftables.conf`, `/etc/nftables.d/host-firewall.nft`,
  `/etc/nftables.d/nomad.nft`, and component-owned systemd units;
- two fresh nftables Nomad batch submissions completed successfully, latest
  allocation `8d12f79e`;
- live `nft list table inet verself_host` shows default-deny host ingress with
  loopback, established/related, ICMP, SSH, SMTP, HTTP, HTTPS, WireGuard, and
  Firecracker TAP allowances;
- live `nft list table inet verself_nomad` blocks non-loopback access to
  Nomad port `4646`;
- `verself-nftables.service` is enabled and `verself-firewall.target` is
  active;
- NATS now has a component CRD and a component-owned Nomad service job;
- NATS recovery installs the boarded `nats-runtime.tar`, writes
  `/etc/nats/nats-server.conf`, writes
  `/etc/nats/nats-spiffe-helper.conf`, and reports
  `NATSRecoveryComplete=True` in
  `/run/verself/recovery/nats/report.json`;
- NATS runtime digest:
  `sha256:887608367a3327f079f10414544c0e04b2b483f08c1658a534f38394d0382e63`;
- live NATS allocation `7d67f12d` is running job version `1`, deployment
  `830f6ae1` completed successfully, and the `nats-monitoring` Nomad service
  check against `/varz` is `success`;
- NATS monitoring reports version `2.12.7`, TLS client connections required,
  JetStream enabled with `256 MB` memory and `4 GB` file storage limits, and
  `server_name` `verself-nats`;
- NATS spiffe-helper received
  `spiffe://gamma.verself.sh/svc/nats` and refreshed the X.509 material;
- re-submitting the same NATS job produced evaluation `74dac43a` without
  allocation churn; allocation `7d67f12d` remained the only running allocation;
- Nomad Observer now has a component CRD and a component-owned Nomad service
  job;
- Nomad Observer recovery installs the boarded `nomad-observer.tar`, creates
  the `nomad_observer` account, projects the boarded graph into
  `/run/verself/recovery/nomad-observer/document.json`, and reports
  `NomadObserverRecoveryComplete=True` in
  `/run/verself/recovery/nomad-observer/report.json`;
- Nomad Observer runtime digest:
  `sha256:9c6b2f38c68d873ed6e504899b323afe8694629bea56ad898402da277cee0cd0`;
- live Nomad Observer allocation `23774120` is running, deployment `794fbda2`
  completed successfully, and re-submitting the same job produced evaluation
  `2eafecab` without allocation churn;
- Nomad Observer wrote fresh ClickHouse fleet evidence:
  `verself.fleet_nodes` had one row newer than five minutes with
  `max(observed_at) = 2026-06-06 06:00:18.497`;
- the projected Nomad Observer graph and report are `root:nomad_observer`
  `0640`, allowing the service user to read configuration without exposing the
  boarded workspace tree broadly;
- OTel Collector now has a component CRD and a component-owned Nomad service
  job;
- OTel Collector recovery installs the boarded `otelcol-runtime.tar` and
  `otelcol-config.tar`, creates the `otelcol` account, and reports
  `OtelCollectorRecoveryComplete=True` in
  `/run/verself/recovery/otelcol/report.json`;
- OTel Collector runtime digest:
  `sha256:4c29347a3dad1c4e9bb60af074523fd8287b94b32aae7bd846839310887471de`;
- OTel Collector config digest:
  `sha256:60870e2bd6856ac2dcec0ec051d35c6851405e93f9db95872926a1475a8ef651`;
- live OTel Collector allocation `6078c0f2` is running, deployment `519ad6a9`
  completed successfully, and the `otelcol-health` Nomad service check is
  `success`;
- OTel Collector health endpoint returned `Server available` from
  `127.0.0.1:13133`;
- OTel Collector spiffe-helper received
  `spiffe://gamma.verself.sh/svc/otelcol` and refreshed the X.509 material;
- OTel Collector wrote fresh ClickHouse evidence newer than five minutes:
  `default.otel_metrics_sum` had `115495` rows with
  `max(TimeUnix) = 2026-06-06 06:26:17.015`,
  `default.otel_metrics_gauge` had `17144` rows with
  `max(TimeUnix) = 2026-06-06 06:26:16.996`, and `default.otel_logs` had `240`
  rows with `max(Timestamp) = 2026-06-06 06:26:05.118326000`;
- re-submitting the same OTel Collector job produced evaluation `890cb326`
  without allocation churn; allocation `6078c0f2` remained the only running
  allocation;
- artifact-only changes are not visible to Nomad when the job HCL is unchanged;
  the live OTel config update required an explicit allocation restart after
  boarding the new artifact. Future `fly` submission logic should carry a
  boarded artifact or upload digest into Nomad submission if artifact-only
  changes must roll automatically;
- object-storage-service setup now exits successfully and reaches past
  migrations through the component-owned `object-storage-service recover`
  command;
- object-storage-service runtime release on gamma:
  `/var/lib/object-storage-service/runtime/releases/sha256-bd16f981b716df64c7da7ee8b80854dee5a80e8d54b3049e9317db00f3e313a0`;
- object-storage-service runtime and admin tasks still fail before process start
  because Nomad cannot derive an OpenBao token: role
  `object-storage-service-runtime` does not exist;
- OpenBao recovery reports the hidden blocker explicitly:
  `RootTrustMaterialAvailable=False`,
  `OpenBaoBaselineReconciled=False`, and
  `OpenBaoRecoveryComplete=False` with reason `BaselineBlocked`;
- OpenBao baseline reconciliation through `--operator-token-stdin` is
  scriptable and consumes the token through stdin;
- OpenBao baseline reconciliation through `--generate-root-token-stdin` is
  implemented as a component-owned operator path that starts OpenBao
  generate-root, decodes the transient token, reconciles baseline state, and
  revokes the generated token;
- OpenBao sealed-state recovery now treats `--generate-root-token-stdin` as
  threshold unseal material first, then reuses the same material to generate a
  transient root token for baseline reconciliation;
- the live `--generate-root-token-stdin </dev/null` path reports
  `RootTrustMaterialAvailable=False/UnsealQuorumIncomplete` and does not
  attempt baseline reconciliation without threshold material;
- the patched OpenBao recovery binary was boarded to gamma in upload
  `sha256:d99aac0a65a886a951fea7e9935b580c8063be1eb8ccb6916a9306a65ff28b22`;
- the available gamma bootstrap token is not sufficient for baseline
  reconciliation: OpenBao returned 403 on `sys/mounts` and 403 on
  `auth/token/revoke-self`;
- the missing OpenBao role is caused by baseline reconciliation requiring a
  valid operator token with authority to reconcile mounts, policies, auth, and
  Nomad JWT roles.
- OpenBao now has explicit `SecretPath` resources for the object-storage
  secrets referenced by the `ObjectStorageService` CRD;
- OpenBao baseline reconciliation can create generated secret values only when
  absent; the declared `object-storage-service.credential_kek` value is
  generated as 32 random bytes encoded as hex on fresh bootstrap and left
  untouched when restored from an existing store;
- object-storage R2 credential paths are declared as produced by
  `CloudflareControlPlane/gamma-cloudflare`;
- `iam-service.zitadel.auth_audience` is declared as produced by the future
  Zitadel/auth-control-plane recovery path;
- running the updated boarded OpenBao recovery binary without operator material
  on gamma still reports `RootTrustMaterialAvailable=False` and
  `OpenBaoBaselineReconciled=False` with reason
  `OperatorRootCredentialsRequired`, confirming the graph parses cleanly and
  the remaining blocker is root authority.
- Zitadel now has component CRDs for the service cluster and auth-control-plane
  batch job, and the gamma graph declares the `zitadel` PostgreSQL database,
  peer role, OpenBao JWT roles, and OpenBao secret paths that the two jobs need;
- OpenBao can generate `zitadel.masterkey` as 32 alphanumeric characters and
  `zitadel.admin_password` as 32 random bytes encoded with base64url when those
  paths are absent;
- Zitadel-produced admin PATs and auth-control-plane-produced IAM OIDC values
  are declared as `SecretPath` resources, while SMTP and GitHub OAuth input
  material remain operator-import/provider-owned inputs;
- the current Zitadel Nomad job files still validate but remain in the old
  runtime-artifact and placeholder shape; the next Zitadel slice is the runtime
  cutover from `verself-artifact://` and `__VERSELF_*` placeholders to boarded
  artifacts plus CRD-loaded config.

Evidence commands:

```sh
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default openbao'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default cloudflare-integration-recovery'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default haproxy-upstreams'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/openbao/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/haproxy/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/nftables/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/nats/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/nomad-observer/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/otelcol/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/clickhouse/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/integrations/cloudflare/control-plane/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job validate -address=http://127.0.0.1:4646 -namespace=default /home/ubuntu/.local/state/guardian/repo/current/workspace/src/services/object-storage-service/nomad.hcl'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default object-storage-service'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc logs -address=http://127.0.0.1:4646 -namespace=default -stderr -task setup <object-storage-service-allocation-id>'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc logs -address=http://127.0.0.1:4646 -namespace=default -stderr -task recover <cloudflare-allocation-id>'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/openbao/report.json'
ssh -T ubuntu@206.223.228.87 'sudo /home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover recover --repo-root=/home/ubuntu/.local/state/guardian/repo/current --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json --resource-name=openbao --operator-token-stdin' < <operator-token-file>
ssh -T ubuntu@206.223.228.87 'sudo /home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover recover --repo-root=/home/ubuntu/.local/state/guardian/repo/current --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json --resource-name=openbao --generate-root-token-stdin' < <unseal-shares-file>
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default postgresql'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/postgresql/report.json'
ssh -T ubuntu@206.223.228.87 'sudo -u object_storage_service env LD_LIBRARY_PATH=/var/lib/postgresql/runtime/current/usr/lib/x86_64-linux-gnu:/var/lib/postgresql/runtime/current/usr/lib/postgresql/16/lib /var/lib/postgresql/runtime/current/usr/lib/postgresql/16/bin/psql -h /var/run/postgresql -p 5432 -d object_storage_service -A -t -c "select current_user;"'
ssh -T ubuntu@206.223.228.87 'sudo -u object_storage_admin env LD_LIBRARY_PATH=/var/lib/postgresql/runtime/current/usr/lib/x86_64-linux-gnu:/var/lib/postgresql/runtime/current/usr/lib/postgresql/16/lib /var/lib/postgresql/runtime/current/usr/lib/postgresql/16/bin/psql -h /var/run/postgresql -p 5432 -U object_storage_service -d object_storage_service -A -t -c "select current_user;"'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default clickhouse'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/clickhouse/report.json'
ssh -T ubuntu@206.223.228.87 'sudo /var/lib/clickhouse/runtime/current/bin/clickhouse client --config-file /etc/clickhouse-client/operator.xml --user clickhouse_operator --query "SELECT count() FROM system.tables WHERE database = '\''verself'\''"'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default nftables'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc logs -address=http://127.0.0.1:4646 -namespace=default -task apply 8d12f79e'
ssh -T ubuntu@206.223.228.87 'sudo env LD_LIBRARY_PATH=/opt/verself/nftables/current/lib/x86_64-linux-gnu /opt/verself/nftables/current/bin/nft list table inet verself_host'
ssh -T ubuntu@206.223.228.87 'sudo env LD_LIBRARY_PATH=/opt/verself/nftables/current/lib/x86_64-linux-gnu /opt/verself/nftables/current/bin/nft list table inet verself_nomad'
ssh -T ubuntu@206.223.228.87 'sudo systemctl status --no-pager verself-nftables.service verself-firewall.target'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/nats/report.json'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default nats'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default 7d67f12d'
ssh -T ubuntu@206.223.228.87 'curl -fsS http://127.0.0.1:8222/varz'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/nomad-observer/report.json'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default nomad-observer'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default 23774120'
ssh -T ubuntu@206.223.228.87 'sudo /var/lib/clickhouse/runtime/current/bin/clickhouse client --config-file /etc/clickhouse-client/operator.xml --user clickhouse_operator --query "SELECT count(), max(observed_at) FROM verself.fleet_nodes WHERE observed_at > now() - INTERVAL 5 MINUTE"'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/otelcol/report.json'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default otelcol'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default 6078c0f2'
ssh -T ubuntu@206.223.228.87 'curl -fsS http://127.0.0.1:13133/'
ssh -T ubuntu@206.223.228.87 'sudo /var/lib/clickhouse/runtime/current/bin/clickhouse client --config-file /etc/clickhouse-client/operator.xml --user clickhouse_operator --query "SELECT count(), max(TimeUnix) FROM default.otel_metrics_sum WHERE TimeUnix > now() - INTERVAL 5 MINUTE"'
ssh -T ubuntu@206.223.228.87 'sudo /var/lib/clickhouse/runtime/current/bin/clickhouse client --config-file /etc/clickhouse-client/operator.xml --user clickhouse_operator --query "SELECT count(), max(Timestamp) FROM default.otel_logs WHERE TimestampTime > now() - INTERVAL 5 MINUTE"'
ssh -T ubuntu@206.223.228.87 'for p in /etc/haproxy/certs/gamma.verself.sh.pem /etc/haproxy/certs/gamma.guardianintelligence.org.pem; do sudo test -f "$p" && echo present:$p || echo missing:$p; done'
```

Cloudflare account-admin import is an operator gate. The stdin payload shape is:

```json
{
  "openBaoToken": "<operator OpenBao token>",
  "cloudflareAccountAdminAPIToken": "<Cloudflare account-admin API token>"
}
```

The component command is:

```sh
cloudflare-control-plane \
  --action=import-account-admin \
  --site=gamma \
  --account-id=c3eaeffaadf7d4847684d4775c16d598 \
  --bucket=verself-deployment-artifacts \
  --openbao-addr=https://127.0.0.1:8200 \
  --openbao-ca-cert=/etc/verself/openbao/ca.pem \
  --account-admin-openbao-path=kv-controller/data/integrations/cloudflare/account-admin \
  --operator-import-stdin
```

## Next Component

The next mechanical recovery target is the Zitadel runtime cutover. OpenBao
baseline reconciliation remains externally gated on operator root authority, but
the graph now declares the Zitadel/OpenBao/PostgreSQL dependencies that baseline
will apply once authority is presented. Zitadel still needs to consume its CRDs
at runtime, install boarded artifacts instead of `verself-artifact://` sources,
and remove `__VERSELF_*` placeholders from its Nomad jobs. After that, OpenBao
baseline authority can be presented and the next live run should expose whether
Zitadel reaches service health or blocks on operator-imported SMTP/GitHub
material.

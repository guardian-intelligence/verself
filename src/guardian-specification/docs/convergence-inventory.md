# Convergence Inventory

This inventory records the current gamma recovery state for each component that
has a component CRD in the Guardian graph. A component is converged when the
materialized graph is current, the component-owned Nomad job is healthy, and two
consecutive submissions do not create unexpected allocation churn.

## Gamma

| Component | CRD | Current State | Blocking Conditions | Dependencies Needed For Convergence |
| --- | --- | --- | --- | --- |
| Substrate preflight | `substrate.guardianintelligence.org/v1alpha1/Substrate/gamma-primary` | Converged | None | SSH access, local build artifacts, upload/extract/verify hooks |
| Nomad runtime | component bootstrap machinery | Converged | None | Materialized repo, pinned Nomad runtime artifact, `nomad-recover`, root access for systemd and host config |
| OpenBao | `openbao.guardianintelligence.org/v1alpha1/OpenBaoCluster/openbao` | Fresh destructive bootstrap converged with single-task recovery | Initialized Shamir-sealed restart still needs a configured auto-unseal mechanism for fully autonomous host reboot | Materialized OpenBao runtime artifact, operator PGP recipients for encrypted recovery handoff, in-memory fresh-init shares, transient initial root token revoked after baseline reconcile |
| SPIRE | `spire.guardianintelligence.org/v1alpha1/SPIRECluster/spire` | Converged on latest gamma run | None in current gamma state | Materialized SPIRE runtime artifact, identity registry artifact, server/agent sockets, join-token attestation, `spire_workload` socket group |
| Cloudflare Control Plane | `cloudflare.guardianintelligence.org/v1alpha1/CloudflareControlPlane/gamma-cloudflare` | Converged in latest live recovery run; batch job purged after evidence capture | None in current gamma state | OpenBao recovered with `cloudflare-integration-recovery-runtime`, operator-imported Cloudflare account-admin credential when no restored OpenBao snapshot exists, Cloudflare API authority for DNS/TLS/R2, recovery R2 bucket |
| HAProxy | `haproxy.guardianintelligence.org/v1alpha1/HAProxyGateway/public-edge` | Converged on latest gamma run | None in current gamma state | Materialized HAProxy runtime artifact, public certificate files for `gamma.verself.sh` and `gamma.guardianintelligence.org`, PublicOrigin/Gateway route graph |
| nftables | `nftables.guardianintelligence.org/v1alpha1/NftablesFirewall/nftables` | Converged on latest gamma run | None in current gamma state | Materialized nftables runtime artifact, direct `nftables-apply` batch binary, root access for kernel ruleset and systemd unit installation |
| NATS | `nats.guardianintelligence.org/v1alpha1/NATSCluster/nats` | Converged on latest gamma run | None in current gamma state | Materialized NATS runtime artifact, `nats-recover`, SPIFFE helper, NATS SPIFFE identity, monitoring `/varz` check |
| Nomad Observer | `nomadobserver.guardianintelligence.org/v1alpha1/NomadObserver/nomad-observer` | Converged on latest gamma run | None in current gamma state | Materialized Nomad Observer runtime artifact, direct `nomad-observer` prestart binary, Nomad API, SPIFFE identity, ClickHouse `nomad_observer` user |
| OTel Collector | `otelcol.guardianintelligence.org/v1alpha1/OtelCollector/otelcol` | Converged on latest gamma run | None in current gamma state | Materialized OTel Collector runtime/config artifacts, direct `otelcol-recover` prestart binary, SPIFFE helper, ClickHouse `otelcol` user, PostgreSQL `otelcol` peer role |
| Zitadel/Auth Control Plane | `zitadel.guardianintelligence.org/v1alpha1/ZitadelCluster/zitadel`, `zitadel.guardianintelligence.org/v1alpha1/ZitadelAuthControlPlane/auth-control-plane` | Converged on latest gamma run | None in current gamma state | OpenBao baseline roles, generated Zitadel masterkey/admin password satisfying Zitadel bootstrap password policy, PostgreSQL `zitadel` database, live Zitadel admin PAT handoff to OpenBao |
| IAM Service | `iam.guardianintelligence.org/v1alpha1/IAMService/iam-service` | Converged on latest gamma run | None in current gamma state | Materialized IAM binary, IAM CRD static config, PostgreSQL `iam_service` database/peer role, OpenBao-generated/runtime Zitadel credentials, SpiceDB, ClickHouse, Zitadel OIDC issuer reachable through HAProxy |
| Deployment Service | `deployment.guardianintelligence.org/v1alpha1/DeploymentService/deployment-service` | Converged on latest gamma run | None in current gamma state | Materialized deployment-service binary, Bazel-pinned Git runtime tools, PostgreSQL `deployment_service` database/peer role, SPIRE workload identity, object-storage admin API, Nomad API, HAProxy public route |
| PostgreSQL | `postgresql.guardianintelligence.org/v1alpha1/PostgreSQLCluster/postgresql` | Converged on latest gamma run | None in current gamma state | Materialized PostgreSQL runtime artifact, generated pgBackRest cipher pass, Cloudflare recovery R2 capability, PostgreSQL service database/peer mapping config |
| ClickHouse | `clickhouse.guardianintelligence.org/v1alpha1/ClickHouseCluster/clickhouse` | Converged on latest gamma run | None in current gamma state | Materialized ClickHouse runtime artifact, SPIFFE helper, server/operator SPIFFE identities, schema migrations |
| TigerBeetle | `tigerbeetle.guardianintelligence.org/v1alpha1/TigerBeetleCluster/tigerbeetle` | Converged on latest gamma run | None in current gamma state | Materialized TigerBeetle runtime artifact, `tigerbeetle-recover`, singleton data file |
| Object Storage Service | `objectstorage.guardianintelligence.org/v1alpha1/ObjectStorageService/object-storage` | Converged on latest gamma run | None in current gamma state | OpenBao baseline reconciliation, Cloudflare-produced bucket-scoped R2 credentials, PostgreSQL, SPIRE, ClickHouse CA material, and Zitadel-produced auth audience |
| Zot | `zot.guardianintelligence.org/v1alpha1/ZotRegistry/zot` | Converged on latest gamma run | None in current gamma state | Materialized Zot runtime artifact, `zot-recover`, local storage directory, htpasswd publisher entry |
| Verdaccio | `verdaccio.guardianintelligence.org/v1alpha1/VerdaccioRegistry/verdaccio` | Converged on latest gamma run | None in current gamma state | Materialized Verdaccio runtime artifact, `verdaccio-recover`, local storage directory, generated htpasswd file, npm uplink network egress |
| SpiceDB | `spicedb.guardianintelligence.org/v1alpha1/SpiceDBCluster/spicedb` | Converged on latest gamma run | None in current gamma state | Materialized SpiceDB runtime artifact, `spicedb-recover`, OpenBao generated gRPC preshared key, PostgreSQL `spicedb` database, Nomad OpenBao workload token |
| Grafana | `grafana.guardianintelligence.org/v1alpha1/GrafanaInstance/grafana` | Converged on latest gamma run | None in current gamma state | Materialized Grafana runtime artifact, `grafana-recover`, OpenBao generated admin password and secret key, PostgreSQL `grafana` database |
| Forgejo | `forgejo.guardianintelligence.org/v1alpha1/ForgejoInstance/forgejo` | Converged on latest gamma run | None in current gamma state | Materialized Forgejo runtime artifact, direct `forgejo-recover` prestart binary, OpenBao generated Forgejo secrets, PostgreSQL `forgejo` database, Nomad OpenBao workload token |
| Stalwart | `stalwart.guardianintelligence.org/v1alpha1/StalwartMailServer/stalwart` | Converged on latest gamma run | None in current gamma state | Materialized Stalwart runtime artifact, direct `stalwart-recover` prestart binary, OpenBao generated admin secret, PostgreSQL `stalwart` database, Nomad OpenBao workload token |
| Electric | `electric.guardianintelligence.org/v1alpha1/ElectricDeployment/electric` | Converged on latest gamma run | None in current gamma state | Materialized Electric runtime artifact, direct `electric-recover` prestart binary, Electric containerd socket, OpenBao generated PostgreSQL/API secrets, PostgreSQL `electric`, `electric_notifications`, and `electric_iam` databases, Nomad OpenBao workload token |
| Temporal | `temporal.guardianintelligence.org/v1alpha1/TemporalPlatform/temporal` | Converged on latest gamma run | None in current gamma state | Materialized Temporal runtime artifact, direct `temporal-recover` prestart binary, PostgreSQL `temporal` and `temporal_visibility` databases, SPIFFE mTLS identity, Temporal namespace bootstrap |

## Latest Gamma Evidence

Live command:

```sh
guardian fly -f src/guardian-specification/examples/gamma/gamma.cue -o json --stream
```

Observed results from the latest gamma run on June 7, 2026 UTC:

- current preflight after the deployment-service slice reported
  `ready_to_fly: yes`, resource graph digest
  `sha256:0944ce1d74bfb85cbfec52d8b47349e9255ee31c4b3207774f3b0f0102f15786`,
  and verified upload digest
  `sha256:dceb14680d5f2ebbb3527f527194d54b68a288433ed0ad246ee0861472c82bb7`;
- deployment-service deployment `bd40b656` is successful, allocation
  `eb6ed0ea` is running on `127.0.0.1:30037`, and
  `http://127.0.0.1:30037/healthz` returns `ok`;
- `https://deployments.api.gamma.verself.sh/healthz` through HAProxy returns
  `ok` after the HAProxy upstream template stopped forcing `proto h2` for the
  plain HTTP/1.1 deployment-service backend;
- HAProxy deployment `f68d44d8`, allocation `68904094`, is successful; after
  deployment-service was rescheduled from allocation `1388c458` on
  `127.0.0.1:21362` to allocation `eb6ed0ea` on `127.0.0.1:30037`,
  `/etc/haproxy/nomad-upstreams.cfg` rendered
  `server srv_0 127.0.0.1:30037 check ...` for
  `be_route_product_deployments_api_deployment_service_public_api`, and the
  Nomad-owned HAProxy task restarted once from the upstream PID signal;
- the deployment-service recovery prestart installs a content-addressed runtime
  release containing `deployment-service`, `guardian`, and a Bazel-pinned Git
  runtime toolset; the service user can run Git against
  `/var/lib/deployment-service/repo`, and the origin is
  `https://github.com/guardian-intelligence/verself.git`;
- deployment-service initially blocked on PostgreSQL peer auth because the
  PostgreSQL reconciler was operating from a stale projected resource graph
  without the `deployment_service` peer mapping. The PostgreSQL sidecar now
  projects the current boarded graph, rewrites `pg_hba.conf` and
  `pg_ident.conf`, reloads PostgreSQL, and reconciles the declared database and
  role set;
- deployment-service then blocked because the runtime had no `git` in PATH.
  The service now owns `deployment-service-runtime-tools.tar`, built from
  pinned Ubuntu Git and libcurl `.deb` artifacts and uploaded by preflight;
- the first runtime-tools recovery attempt rejected Git command alias hardlinks
  in the tar archive. The Bazel tar action now uses hardlink dereferencing so
  the recovery extractor only has to accept regular files, directories, and
  safe relative symlinks;
- current preflight after the IAM/HAProxy slice reported `ready_to_fly: yes`,
  resource graph digest
  `sha256:096e2129fd26d529d229fc83a51c2c11cb50410aca9c12bbff037f3a9ff828e8`,
  and verified upload digest
  `sha256:c86703aa8d9a80cb9b1270df818a9425bb8f212c0a5b1887ec4ff0f9cefe47d8`;
- `guardian fly` still runs the OpenBao hook only; HAProxy/IAM convergence in
  this slice was completed by submitting/restarting the repo-owned Nomad jobs
  from the boarded tree after preflight verified the graph;
- HAProxy deployment `3ff78b7f` is successful, allocation `13b16d9d` is
  running, and generated route ACLs now route
  `/.well-known/openid-configuration`, `/oauth/v2/`, `/oidc/v1/`, `/ui/`, and
  `/assets/` on `gamma.verself.sh` to
  `be_route_product_auth_zitadel_oidc` before the product apex fallback;
- `https://gamma.verself.sh/.well-known/openid-configuration` through HAProxy
  returns `HTTP/2 200` with issuer `https://gamma.verself.sh`; before the route
  fix it returned `HTTP/2 503` because host-only routing selected the product
  frontend backend;
- IAM deployment `a022d423` is successful with allocations `ba5ce519` and
  `fb7698dd` running and healthy;
- IAM public `/readyz` returns `200` on both dynamic loopback ports, and Nomad
  reports both `iam-service-internal-https` and `iam-service-public-http`
  service checks as `success`;
- `https://iam.api.gamma.verself.sh/api/v1/organizations` through HAProxy
  reaches IAM and returns the expected `401` problem response for a request
  without a bearer token;
- IAM initially blocked on `identity browser auth oidc discovery: 503 Service
  Unavailable` until HAProxy gained path-scoped routing for the Zitadel OIDC
  issuer paths;
- IAM then blocked on PostgreSQL `permission denied for schema public`; the
  PostgreSQL reconciler now repairs existing database ownership and `public`
  schema ownership/grants for declared service databases;
- IAM GitHub login configuration is optional in the CRD. Gamma currently omits
  the GitHub IDP secret, so IAM starts without GitHub login enabled instead of
  treating that provider-specific input as a hard bootstrap dependency;
- `guardian fly -f src/guardian-specification/examples/gamma/gamma.cue -o json
  --stream` materialized gamma and submitted the OpenBao Nomad job;
- preflight reported `ready_to_fly: yes`;
- latest resource graph digest:
  `sha256:857c4210c721bd9c2f87706897432cd595e5aa7be861ffa545d27c9048e35b0d`;
- latest verified upload digest:
  `sha256:12b082d4c164a8b4d24089f42f5f5d2d1e84c896dff5f7a64308dd6d97b2d6b4`;
- preflight prepared `/etc/verself/openbao/ca.pem`, started Nomad 1.11.3, and
  validated OpenBao, Cloudflare, and PostgreSQL Nomad jobs without submitting
  OpenBao during preflight;
- Nomad reports `openbao` status `running`, deployment `successful`, one
  healthy allocation, and no failed allocations;
- allocation task states: `setup` and `recover` exited `0`; `server` remains
  running;
- `/run/verself/recovery/openbao/report.json` reports
  `OpenBaoRecoveryComplete=True/Recovered`;
- OpenBao evidence from the report: version `2.5.2`, Shamir seal, threshold
  `2`, cluster id `335df66b-5d2b-e6ae-c91a-740563a736a3`;
- the fresh-init path delivered encrypted init material to
  `/run/verself/recovery/openbao/init-material.json`, unsealed using in-memory
  init shares, reconciled baseline mounts/auth/policies, and revoked the
  transient initial root token;
- Cloudflare account-admin authority was imported through the encrypted
  OpenBao operator import handoff and `--operator-import-stdin`;
- the first Cloudflare recovery attempt after import reached R2 verification
  and failed on `put verification object returned status 404`, identifying the
  missing recovery bucket as the root cause;
- Cloudflare recovery now ensures the recovery bucket before minting the
  bucket-scoped recovery credential;
- the latest Cloudflare recovery allocation reported
  `RecoveryBucketReady`, `RecoveryCredentialsPersisted`, `DNSConverged`,
  `CertificatesReady`, and `ObjectStorageCredentialsPersisted`;
- the latest Cloudflare recovery report showed `bucket: verself-recovery`,
  `bucket_created: true`, `verification_object_get_status: 200`, and
  `verified_with: object-storage-proxy`;
- PostgreSQL setup initially misclassified an empty reachable pgBackRest
  repository as unreachable because `postgresql-recovery --action=info` passed
  the pgBackRest `--process-max` option to a command that does not accept it;
- `postgresql-recovery` now only passes `--process-max` to pgBackRest backup
  and restore actions;
- after re-boarding the rebuilt PostgreSQL runtime, the `postgresql` Nomad job
  deployed successfully with one healthy allocation;
- `/run/verself/recovery/postgresql/report.json` reports `status: healthy`,
  `backup_status: initial_full_backup_created`, port `5432`, socket dir
  `/var/run/postgresql`, seven reconciled databases, and eight reconciled roles;
- pgBackRest reports `status: ok`, one valid full backup, one archive timeline,
  and latest backup label `20260607-002712F`;
- PostgreSQL catalog checks confirm the `object_storage_service` and `otelcol`
  roles, the `object_storage_service` and `zitadel` databases, and `otelcol`
  membership in `pg_monitor`;
- service Unix accounts such as `object_storage_service` are intentionally
  component-owned, so direct peer-auth smoke tests must run after the owning
  service recovery task creates its local account;
- SPIRE recovery converged from the boarded runtime and identity registry
  artifacts: Nomad deployment `795e0807` completed successfully, allocation
  `1b1890c9` is running, and `/run/verself/recovery/spire/report.json` reports
  `SPIRERecoveryComplete=True`;
- SPIRE registered `23` identities and the workload socket
  `/run/spire-agent/sockets/agent.sock` is owned by `root:spire_workload` with
  mode `0770`;
- the first nftables submission in the latest run failed before starting
  because the boarded artifact set omitted the direct `nftables-apply` batch
  binary and `nftables-runtime.tar`;
- after adding those artifacts to board upload and verify, `nftables`
  allocation `a6a5a315` completed with exit code `0`;
- local and remote nftables artifact hashes matched:
  runtime `sha256:6c1c11dad7fa2e20a653501dd2c366f71d16c8029f83cc56cf59a05031d52506`
  and apply binary
  `sha256:ab8acf1dc710f47e14b6127ac3afe3672e7f88fdce2771ed640e35f14610a3fe`;
- `nftables-apply` printed `host firewall converged`, enabled
  `verself-nftables.service`, and activated `verself-firewall.target`;
- live `nft list table inet verself_host` shows default-deny host ingress with
  loopback, established/related, ICMP, SSH, SMTP, HTTP, HTTPS, WireGuard, and
  Firecracker TAP allowances;
- live `nft list table inet verself_nomad` blocks non-loopback access to Nomad
  port `4646`;
- the first NATS submission failed before starting because the boarded artifact
  set omitted the direct `nats-recover` prestart binary and
  `nats-runtime.tar`;
- after adding those artifacts to board upload and verify, `nats` deployment
  `314aed26` completed successfully with allocation `6f5326d7` running and
  healthy;
- `/run/verself/recovery/nats/report.json` reports
  `NATSRuntimeInstalled=True`, `NATSConfigWritten=True`, and
  `NATSRecoveryComplete=True`;
- NATS runtime artifact digest:
  `sha256:88a1ad53c74f1f4893560de7fbb18afb20acc030aeac926efa3a477e98e3c5a8`;
- Nomad reports the `nats-monitoring` check as `success`, `/varz` reports
  NATS `2.12.7` with JetStream enabled, and the server is listening on
  `127.0.0.1:4222` and `127.0.0.1:8222`;
- the first Nomad Observer submission in the latest run failed before starting
  because the boarded artifact set omitted the direct `nomad-observer`
  prestart binary and `nomad-observer.tar`;
- after adding those artifacts to board upload and verify, `nomad-observer`
  deployment `925054c6` completed successfully with allocation `69c490b8`
  running and healthy;
- `/run/verself/recovery/nomad-observer/report.json` reports
  `NomadObserverRuntimeInstalled=True`, `NomadObserverAccountReady=True`, and
  `NomadObserverRecoveryComplete=True`;
- Nomad Observer runtime artifact digest:
  `sha256:c903df08ae2c9d743c4c94b8b418777a6c2c2cd7a60e9e871b0942e02d9e26bf`;
- ClickHouse `verself.fleet_nodes` contains live projection rows from Nomad
  Observer, with latest `observed_at` `2026-06-07 01:36:07.765`;
- the first OTel Collector submission in the latest run failed before starting
  because the boarded artifact set omitted the direct `otelcol-recover`
  prestart binary plus `otelcol-runtime.tar` and `otelcol-config.tar`;
- after adding those artifacts to board upload and verify, `otelcol`
  deployment `94e7da8f` completed successfully with allocation `930deba3`
  running and healthy;
- `/run/verself/recovery/otelcol/report.json` reports
  `OtelCollectorRuntimeInstalled=True`,
  `OtelCollectorConfigInstalled=True`, and
  `OtelCollectorRecoveryComplete=True`;
- OTel Collector runtime artifact digest:
  `sha256:4c29347a3dad1c4e9bb60af074523fd8287b94b32aae7bd846839310887471de`;
- OTel Collector config artifact digest:
  `sha256:272205884b9702132ba2d327e2d0d15e3f08f06e17eee4f53f17353f21c6158b`;
- the collector listens on `127.0.0.1:4317`, `127.0.0.1:4318`, and
  `127.0.0.1:13133`; the `otelcol-health` Nomad service check is `success`;
- OTel Collector health returned `Server available` from
  `http://127.0.0.1:13133/`;
- the hostmetrics process scraper now delays short-lived process scrapes and
  mutes the upstream-supported process name/user read failures, removing the
  repeated `/proc/<pid>/status` scrape errors from steady-state logs;
- OTel Collector spiffe-helper received
  `spiffe://gamma.verself.sh/svc/otelcol` and refreshed the X.509 material;
- ClickHouse had fresh OTel ingestion after the restart:
  `default.otel_logs` had `546` rows with max `Timestamp`
  `2026-06-07 01:46:43.793246253`, and `default.otel_traces` had `470` rows
  with max `Timestamp` `2026-06-07 01:46:44.806890316`;
- the first Forgejo submission in the latest run failed before starting because
  the boarded artifact set omitted the direct `forgejo-recover` prestart binary
  and `forgejo-runtime.tar`;
- after adding those artifacts to board upload and verify, `forgejo`
  deployment `afc49cf6` completed successfully with allocation `eb35c7e8`
  running and healthy;
- `/run/verself/recovery/forgejo/report.json` reports
  `ForgejoRuntimeInstalled=True`, `ForgejoSecretsReady=True`,
  `ForgejoConfigWritten=True`, `ForgejoRecoveryComplete=True`, and
  `ForgejoAutomationTokenReady=True`;
- local and remote Forgejo artifact hashes matched:
  runtime `sha256:9879fbc04aca355ac36fda95cc5e2bdd64b29825c390483f1a923c8428a8fea6`
  and direct recovery binary
  `sha256:7a4bdae744b3d94d01b1bfda063a1288659642dcd9705580ef0ea3f979cc1176`;
- Nomad reports `forgejo-tcp` as `success`; `http://127.0.0.1:3000/api/healthz`
  reports `status: pass` with passing cache and database checks;
- Forgejo `recover` and `automation-token` tasks exited `0`, the `server` task
  remains running, and task stderr tails were empty;
- the first Stalwart submission in the latest run failed before starting
  because the boarded artifact set omitted the direct `stalwart-recover`
  prestart binary and `stalwart-runtime.tar`;
- after adding those artifacts to board upload and verify, `stalwart`
  deployment `e219ed1d` completed successfully with allocation `0e966a50`
  running and healthy;
- `/run/verself/recovery/stalwart/report.json` reports
  `StalwartRuntimeInstalled=True`, `StalwartSecretReady=True`,
  `StalwartConfigWritten=True`, and `StalwartRecoveryComplete=True`;
- local and remote Stalwart artifact hashes matched:
  runtime `sha256:0ca2039c7fdd1a6bb6e2ac548576b3d0b665bddd89dd55dae6733c95b7f703d5`
  and direct recovery binary
  `sha256:5957cc17296950271ab308ce1796499c0a0b51d663ffc49e9012cd935a19f6ff`;
- Nomad reports `stalwart-http-tcp` and `stalwart-smtp-tcp` as `success`;
  HTTP on `127.0.0.1:8090` returned `200 OK`, SMTP listens on
  `127.0.0.1:25`, and task stderr tails were empty;
- the first Electric containerd submission in the latest run failed before
  starting because the boarded artifact set omitted the direct
  `electric-recover` prestart binary and `electric-runtime.tar`;
- after adding those artifacts to board upload and verify, and making the local
  PostgreSQL connection string explicit with `sslmode=disable`, Electric
  containerd deployment `b8383ba9` completed successfully with allocation
  `65f25886` running and healthy;
- `electric` deployment `92b667d6` completed successfully with allocations
  `a1647620`, `74904497`, and `658fb021` running and healthy for the default,
  notifications, and IAM instances;
- local and remote Electric artifact hashes matched:
  runtime `sha256:6d00694813fd0d0e62d897f634cfd6a9d2aef08d4633602eb6472e05526b9107`
  and direct recovery binary
  `sha256:edee9bff0eed25d2a9d267d35803003bdaa0b424756598b5c8a18c17b24a6b8a`;
- `/run/electric-containerd/containerd.sock` exists and `ctr version` reports
  containerd `v2.3.1` from the materialized Electric runtime;
- Nomad reports `electric-http-tcp`, `electric-notifications-http-tcp`, and
  `electric-iam-http-tcp` as `success`;
- Electric server logs show `Starting ElectricSQL 1.5.0`, ready PostgreSQL
  admin/snapshot connection pools, and verified publications for `default`,
  `notifications`, and `iam` without the previous PostgreSQL SSL fallback
  errors;
- the first Temporal submission in the latest run failed before starting
  because the boarded artifact set omitted the direct `temporal-recover`
  prestart binary and `temporal-runtime.tar`;
- after adding those artifacts to board upload and verify, `temporal`
  deployment `ac94c216` completed successfully with allocation `31b878b3`
  running and healthy;
- `/run/verself/recovery/temporal/report.json` reports runtime digest
  `sha256:06e421b513a91c5bac79f01ac3b1da307261335d2624cde6af11783ed0be931d`,
  databases `temporal` and `temporal_visibility`, and bootstrapped namespaces
  `sandbox-rental-service`, `billing-service`, and `distribution-service`;
- local and remote Temporal artifact hashes matched:
  runtime `sha256:06e421b513a91c5bac79f01ac3b1da307261335d2624cde6af11783ed0be931d`
  and direct recovery binary
  `sha256:cb196e38c8ef3e9a4bd39d08d8c1c631c2951327521a897365a8138cf3874b91`;
- Nomad reports `temporal-frontend-grpc-tcp` and `temporal-metrics-http` as
  `success`; the `recover` prestart and `temporal-bootstrap` poststart tasks
  exited `0`, while `temporal-server` remains running;
- `temporal-bootstrap` uses the repo Temporal SDK client over SPIFFE mTLS and
  registered the declared namespaces; plain unauthenticated `tdbg` is expected
  to fail against the frontend because the server requires client certificates;
- Temporal task log scans found no `error`, `fatal`, `panic`, `failed`,
  `denied`, or `unavailable` entries after convergence;
- object-storage-service initially exposed a concurrent setup race where
  parallel prestart tasks could create fixed system users with stale or wrong
  primary group state; the recovery binary now re-reads host state after
  `useradd`/`groupadd` races and repairs a wrong primary group when the fixed
  UID is correct;
- after SPIRE recovery and the object-storage setup repair, object-storage
  setup exits `0` and repairs `object_storage_service` to uid `960`, primary
  group `object_storage_service`;
- ClickHouse recovery converged from the boarded runtime artifact: Nomad
  deployment `0950fc70` completed successfully, allocation `4c0f7962` is
  running, and `/run/verself/recovery/clickhouse/report.json` reports
  `ClickHouseRecoveryComplete=True`;
- ClickHouse recovery installed runtime artifact
  `sha256:5a18ad18185812f6dc421819ab48d08a46b0776ea1b946baf595b6f6c69fde75`,
  prepared host users/directories/TLS/SPIFFE helpers/systemd units, accepted
  an operator query, and applied migrations through
  `009_recovery_events.up.sql`;
- ClickHouse projected `/etc/verself/clickhouse/server-ca.pem` and an operator
  query counted `32` tables in database `verself`;
- after ClickHouse recovery, object-storage-service S3 allocations ran
  successfully instead of failing on the missing ClickHouse CA;
- Zitadel initially failed during migration `03_default_instance` with
  `Errors.User.PasswordComplexityPolicy.HasSymbol` because
  `zitadel.admin_password` was generated as `base64url`;
- OpenBao now supports generated `SecretPath` encoding `password`, validates
  existing generated values against the declared generator, and repairs stale
  invalid generated values;
- after a one-shot OpenBao baseline reconcile with the newly boarded
  `openbao-recover` binary, the Zitadel admin password was repaired without
  printing or persisting the plaintext value outside OpenBao;
- `zitadel` deployment `3d8272a9` completed successfully with allocation
  `72dc7d12` running and healthy;
- `auth-control-plane` completed batch allocation `82403188`, reconciled
  changed state, and produced the Zitadel/OIDC runtime secrets consumed by
  object-storage;
- object-storage admin then reached R2 provider health and failed with
  `r2 health returned status 403`, identifying an overly broad account-root R2
  readiness probe against bucket-scoped credentials;
- object-storage R2 readiness now checks the declared deployment-artifacts
  provider bucket with `HEAD` instead of requiring account-wide ListBuckets-like
  access;
- after re-boarding the rebuilt object-storage artifact,
  `object-storage-service` deployment `4434cf34` completed successfully with
  admin allocation `c6ff8184` and S3 allocations `5e30ae93` and `fe399014`
  running and healthy;
- HAProxy certificate material exists for `gamma.verself.sh` and
  `gamma.guardianintelligence.org`, but the first HAProxy retry failed because
  the boarded artifact set omitted
  `bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar`;
- after adding the HAProxy runtime tar to the board upload and verify lists,
  `haproxy-upstreams` deployment `57a009aa` completed successfully with
  allocation `bf6df579` running and healthy;
- local HTTPS readiness checks through HAProxy returned `guardian haproxy ready`
  for both `gamma.verself.sh` and `gamma.guardianintelligence.org`;
- the first TigerBeetle submission would have failed because the boarded
  artifact set omitted `tigerbeetle-runtime.tar` and the direct
  `tigerbeetle-recover` binary used by the Nomad prestart task;
- after adding those artifacts to board upload and verify, `tigerbeetle`
  deployment `f22030e4` completed successfully with allocation `7ac4b7e6`
  running and healthy;
- `/run/verself/recovery/tigerbeetle/report.json` reports
  `TigerBeetleRuntimeInstalled=True`, `TigerBeetleDataFileReady=True`, and
  `TigerBeetleRecoveryComplete=True`;
- TigerBeetle runtime artifact digest:
  `sha256:b2de1f8e1aca0d5b889ab08299faec57bcf5f4915c03f8f229bb521eacc2a47e`;
- Nomad reports `tigerbeetle-client-tcp` as `success`, and the server is
  listening on `127.0.0.1:3320`;
- the first Zot submission failed before starting because the boarded artifact
  set omitted the direct `zot-recover` prestart binary and `zot-runtime.tar`;
- after adding those artifacts to board upload and verify, `zot` deployment
  `681628a7` completed successfully with allocation `3cb064c4` running and
  healthy;
- `/run/verself/recovery/zot/report.json` reports `ZotRuntimeInstalled=True`,
  `ZotConfigWritten=True`, `ZotStaleProcessReclaimed=True`, and
  `ZotRecoveryComplete=True`;
- Zot runtime artifact digest:
  `sha256:cf53df27095f699444e3bf0d385f7d6a466973bb28054673868527a71698aa54`;
- Nomad reports the `zot-registry-v2` check as `success`, `/v2/` returns
  `200 OK` with `Docker-Distribution-Api-Version: registry/2.0`, and the
  server is listening on `127.0.0.1:5080`;
- the first Verdaccio submission failed before starting because the boarded
  artifact set omitted the direct `verdaccio-recover` prestart binary and
  `verdaccio-runtime.tar`;
- after adding those artifacts to board upload and verify, `verdaccio`
  deployment `5c66813a` completed successfully with allocation `773c8c89`
  running and healthy;
- `/run/verself/recovery/verdaccio/report.json` reports
  `VerdaccioRuntimeInstalled=True`, `VerdaccioConfigWritten=True`, and
  `VerdaccioRecoveryComplete=True`;
- Verdaccio runtime artifact digest:
  `sha256:3b513acb3b3eeb9fe0ddf3ee2967eae224b40e8e76c7628354637d53350cca8e`;
- Nomad reports the `verdaccio-http-ping` check as `success`, `/-/ping`
  returns `200 OK`, and the server is listening on `127.0.0.1:4873`;
- the first SpiceDB submission failed before starting because the boarded
  artifact set omitted the direct `spicedb-recover` prestart binary and
  `spicedb-runtime.tar`;
- after adding those artifacts to board upload and verify, `spicedb` deployment
  `8a3350ea` completed successfully with allocation `79d54723` running and
  healthy;
- `/run/verself/recovery/spicedb/report.json` reports
  `SpiceDBRuntimeInstalled=True`, `SpiceDBCredentialReady=True`,
  `SpiceDBDatastoreMigrated=True`, and `SpiceDBRecoveryComplete=True`;
- SpiceDB runtime artifact digest:
  `sha256:56c1ccf3dc826c91ee55807fb9c7643be5f52d7f49a86d8d3d9a0aa6b6ec0843`;
- Nomad reports `spicedb-grpc-tcp` and `spicedb-metrics-http` as `success`,
  the metrics endpoint responds on `127.0.0.1:21702`, and gRPC listens on
  `127.0.0.1:24640`;
- the first Grafana submission failed before starting because the boarded
  artifact set omitted the direct `grafana-recover` prestart binary and
  `grafana-runtime.tar`;
- after adding those artifacts to board upload and verify, `grafana` deployment
  `9b403dc4` completed successfully with allocation `7bacadac` running and
  healthy;
- `/run/verself/recovery/grafana/report.json` reports
  `GrafanaRuntimeInstalled=True`, `GrafanaSecretsReady=True`,
  `GrafanaConfigWritten=True`, and `GrafanaRecoveryComplete=True`;
- Grafana runtime artifact digest:
  `sha256:ef82754690e5b042098eb6b4749d351827029c823f66dc14ad2088687a110062`;
- Nomad reports `grafana-http-health` as `success`, `/api/health` reports
  database `ok` on Grafana `12.4.2`, and the server is listening on
  `127.0.0.1:4300`;
- after preflight and breakglass cleanup, the live empty-stdin breakglass
  probe reported `OpenBaoBreakglassRootToken=False/UnsealQuorumIncomplete` and
  `OpenBaoRecoveryComplete=False/BaselineBlocked`, confirming the path fails
  closed before generating a token.

Previous observed results:

- preflight verified the extracted repo tree on gamma;
- latest resource graph digest:
  `sha256:be740b4ecd230e6fca468331ce9e5821ee6671b1fcfaa6e86865969e6b5b6570`;
- latest verified upload digest:
  `sha256:f66d2ca778795159238c43bc23437eb55dbb3c58ba5cfb0cdd266d7c77619e98`;
- the materialized repo contains `.guardian/fly/document.json` with OpenBao,
  PostgreSQL, ClickHouse, nftables, NATS, Nomad Observer, OTel Collector,
  Cloudflare, HAProxy, object-storage, substrate, and public-origin resources;
- remote Nomad validation succeeds for OpenBao, PostgreSQL, ClickHouse,
  nftables, NATS, Nomad Observer, OTel Collector, HAProxy, Cloudflare, and
  object-storage job files;
- Nomad is running and reachable on gamma;
- current OpenBao live state is initialized, unsealed, and running the previous
  baseline;
- updated OpenBao, Cloudflare, and PostgreSQL jobs pass remote Nomad planning
  against gamma;
- the next destructive OpenBao drill should wipe the Raft data directory before
  starting the control loop, initialize fresh, encrypt only unseal shares for
  operators, unseal from in-memory shares, reconcile baseline with the
  transient initial root token, and revoke that token;
- Cloudflare cannot write the recovery R2 capability until OpenBao baseline
  reconciliation updates the `cloudflare-integration-recovery-runtime` policy;
- the Cloudflare CRD carries the OpenBao account-admin path and omits durable
  host file paths for provider token values;
- the Cloudflare CRD declares `accountAdminOpenBaoPath`, and the component
  import action can accept operator-provided OpenBao and Cloudflare authority as
  a JSON stdin payload;
- submitting the updated HAProxy job exercises the component-owned prestart and
  fails on missing `/etc/haproxy/certs/gamma.guardianintelligence.org.pem`;
- Nomad auto-reverts HAProxy to the previous preflight-only allocation after the
  failed public-edge update;
- component truth comes from Nomad status, component report files, service
  health checks, and telemetry rather than Guardian command output.
- object-storage-service now reads its static runtime configuration from its
  `ObjectStorageService` CRD and uses the materialized repo artifact instead of
  deployment-service artifact URIs;
- object-storage-service Nomad validation succeeds and the runtime artifact is
  present in the materialized tree;
- object-storage-service setup projects the materialized graph into
  `/run/verself/recovery/object-storage/document.json` for the service user;
- PostgreSQL now has a component CRD and a component-owned Nomad job that
  installs the materialized runtime artifact, initializes the data directory when
  empty, starts `postgres`, and runs a poststart reconciliation loop;
- PostgreSQL reports healthy in Nomad, writes
  `/run/verself/recovery/postgresql/report.json`, and exposes
  `/var/run/postgresql/.s.PGSQL.5432`;
- PostgreSQL reconciled the `object_storage_service` role, database, and peer
  mappings for `object_storage_service` and `object_storage_admin`;
- PostgreSQL reconciled the `otelcol` peer role and granted `pg_monitor` for
  the OTel PostgreSQL receiver;
- ClickHouse now has a component CRD and a component-owned Nomad job;
- ClickHouse installs the materialized runtime artifact, writes TLS/SPIFFE/systemd
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
- nftables installs the materialized runtime tar into `/opt/verself/nftables`,
  writes `/etc/nftables.conf`, `/etc/nftables.d/host-firewall.nft`,
  `/etc/nftables.d/nomad.nft`, and component-owned systemd units;
- latest nftables Nomad batch allocation `a6a5a315` completed successfully;
- latest nftables runtime digest:
  `sha256:6c1c11dad7fa2e20a653501dd2c366f71d16c8029f83cc56cf59a05031d52506`;
- latest direct nftables apply binary digest:
  `sha256:ab8acf1dc710f47e14b6127ac3afe3672e7f88fdce2771ed640e35f14610a3fe`;
- live `nft list table inet verself_host` shows default-deny host ingress with
  loopback, established/related, ICMP, SSH, SMTP, HTTP, HTTPS, WireGuard, and
  Firecracker TAP allowances;
- live `nft list table inet verself_nomad` blocks non-loopback access to
  Nomad port `4646`;
- `verself-nftables.service` is enabled and `verself-firewall.target` is
  active;
- NATS now has a component CRD and a component-owned Nomad service job;
- NATS recovery installs the materialized `nats-runtime.tar`, writes
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
- Nomad Observer recovery installs the materialized `nomad-observer.tar`, creates
  the `nomad_observer` account, projects the materialized graph into
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
  materialized workspace tree broadly;
- OTel Collector now has a component CRD and a component-owned Nomad service
  job whose recovery prestart installs `otelcol-runtime.tar` and
  `otelcol-config.tar`, creates the `otelcol` account, prepares the
  ClickHouse SPIFFE helper directory, and reports
  `OtelCollectorRecoveryComplete=True`;
- latest live OTel Collector allocation `930deba3` is running, deployment
  `94e7da8f` completed successfully, and the `otelcol-health` Nomad service
  check is `success`;
- latest OTel Collector runtime digest:
  `sha256:4c29347a3dad1c4e9bb60af074523fd8287b94b32aae7bd846839310887471de`;
- latest OTel Collector config digest:
  `sha256:272205884b9702132ba2d327e2d0d15e3f08f06e17eee4f53f17353f21c6158b`;
- the collector health endpoint returned `Server available`, OTLP gRPC/HTTP
  and health listeners were bound on loopback, and ClickHouse recorded fresh
  rows in `default.otel_logs` and `default.otel_traces`;
- OTel Collector spiffe-helper received
  `spiffe://gamma.verself.sh/svc/otelcol` and refreshed the X.509 material;
- artifact-only changes are not visible to Nomad when the job HCL is unchanged;
  the live OTel config update required an explicit allocation restart after
  materializing the new artifact. Future `fly` submission logic should carry a
  materialized artifact or upload digest into Nomad submission if artifact-only
  changes must roll automatically;
- object-storage-service setup now exits successfully and reaches past
  migrations through the component-owned `object-storage-service recover`
  command;
- object-storage-service runtime release on gamma:
  `/var/lib/object-storage-service/runtime/releases/sha256-bd16f981b716df64c7da7ee8b80854dee5a80e8d54b3049e9317db00f3e313a0`;
- object-storage-service runtime and admin tasks still fail before process start
  because Nomad cannot derive an OpenBao token: role
  `object-storage-service-runtime` does not exist;
- OpenBao recovery reported the previous non-destructive blocker explicitly:
  `OpenBaoBaselineReconciled=False` and `OpenBaoRecoveryComplete=False` with
  reason `BaselineBlocked`;
- OpenBao baseline reconciliation through `--operator-token-stdin` is
  scriptable and consumes the token through stdin, but it is an operator path,
  not the target steady-state autonomous path;
- OpenBao breakglass repair through
  `--breakglass-generate-root-token-stdin` is implemented as a component-owned
  emergency path that starts OpenBao generate-root, decodes the transient
  token, reconciles baseline state, and revokes the generated token;
- OpenBao sealed-state breakglass first treats
  `--breakglass-generate-root-token-stdin` as threshold unseal material, then
  reuses the same material to generate a transient root token for emergency
  baseline repair;
- the live `--breakglass-generate-root-token-stdin </dev/null` path reports
  `OpenBaoBreakglassRootToken=False/UnsealQuorumIncomplete` and does not
  attempt baseline reconciliation without threshold material;
- the patched OpenBao recovery binary was materialized to gamma in upload
  `sha256:d99aac0a65a886a951fea7e9935b580c8063be1eb8ccb6916a9306a65ff28b22`;
- the available gamma bootstrap token is not sufficient for baseline
  reconciliation: OpenBao returned 403 on `sys/mounts` and 403 on
  `auth/token/revoke-self`;
- the missing OpenBao role in the previous allocation is caused by baseline
  reconciliation requiring authority to reconcile mounts, policies, auth, and
  Nomad JWT roles; gamma now exercises the fresh-init path instead of preserving
  that store;
- OpenBao now has explicit `SecretPath` resources for the object-storage
  secrets referenced by the `ObjectStorageService` CRD;
- OpenBao baseline reconciliation creates missing generated secret values and
  repairs values that do not match the declared generator; the declared
  `object-storage-service.credential_kek` value is generated as 32 random bytes
  encoded as hex on fresh bootstrap and left untouched when restored from a
  valid existing store;
- object-storage R2 credential paths are declared as produced by
  `CloudflareControlPlane/gamma-cloudflare`;
- `iam-service.zitadel.auth_audience` is produced by the Zitadel
  auth-control-plane batch job after the Zitadel service is healthy;
- running the updated materialized OpenBao recovery binary against the previous
  allocation without operator material still reports
  `OpenBaoBaselineReconciled=False/BaselineAuthorityRequired`; the next proof is
  a destructive OpenBao allocation restart so the prestart wipe and fresh-init
  transaction run from zero;
- Zitadel now has component CRDs for the service cluster and auth-control-plane
  batch job, and the gamma graph declares the `zitadel` PostgreSQL database,
  peer role, OpenBao JWT roles, and OpenBao secret paths that the two jobs need;
- OpenBao can generate `zitadel.masterkey` as 32 alphanumeric characters and
  `zitadel.admin_password` as a 32-character bootstrap password with uppercase,
  lowercase, digit, and symbol classes when those paths are absent or invalid
  for the declared generator;
- Zitadel-produced admin PATs and auth-control-plane-produced IAM OIDC values
  are declared as `SecretPath` resources, while SMTP and GitHub OAuth input
  material remain operator-import/provider-owned inputs;
- the current Zitadel Nomad job files still validate but remain in the old
  runtime-artifact and site-token shape; the next Zitadel slice is the runtime
  cutover to materialized artifacts plus CRD-loaded config.

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
ssh -T ubuntu@206.223.228.87 'sudo /home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover recover --repo-root=/home/ubuntu/.local/state/guardian/repo/current --resource-graph=/home/ubuntu/.local/state/guardian/repo/current/workspace/.guardian/fly/document.json --resource-name=openbao --breakglass-generate-root-token-stdin' < <unseal-shares-file>
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default spire'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/spire/report.json'
ssh -T ubuntu@206.223.228.87 'sudo ls -l /run/spire-agent/sockets/agent.sock'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default postgresql'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/postgresql/report.json'
ssh -T ubuntu@206.223.228.87 'sudo -u postgres env LD_LIBRARY_PATH=/var/lib/postgresql/runtime/current/usr/lib/x86_64-linux-gnu:/var/lib/postgresql/runtime/current/usr/lib/postgresql/16/lib /var/lib/postgresql/runtime/current/usr/lib/postgresql/16/bin/psql -h /var/run/postgresql -p 5432 -d postgres -A -t -c "select rolname from pg_roles where rolname in ('\''object_storage_service'\'', '\''otelcol'\'') order by rolname;"'
ssh -T ubuntu@206.223.228.87 'sudo -u postgres env LD_LIBRARY_PATH=/var/lib/postgresql/runtime/current/usr/lib/x86_64-linux-gnu:/var/lib/postgresql/runtime/current/usr/lib/postgresql/16/lib /var/lib/postgresql/runtime/current/usr/lib/postgresql/16/bin/psql -h /var/run/postgresql -p 5432 -d postgres -A -t -c "select datname from pg_database where datname in ('\''object_storage_service'\'', '\''zitadel'\'') order by datname;"'
ssh -T ubuntu@206.223.228.87 'sudo -u postgres env LD_LIBRARY_PATH=/var/lib/postgresql/runtime/current/usr/lib/x86_64-linux-gnu:/var/lib/postgresql/runtime/current/usr/lib/postgresql/16/lib /var/lib/postgresql/runtime/current/usr/lib/postgresql/16/bin/psql -h /var/run/postgresql -p 5432 -d postgres -A -t -c "select pg_has_role('\''otelcol'\'', '\''pg_monitor'\'', '\''member'\'');"'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default object-storage-service'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc logs -address=http://127.0.0.1:4646 -namespace=default -stderr -task object-storage-service <object-storage-service-allocation-id>'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default <object-storage-admin-allocation-id>'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default clickhouse'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/clickhouse/report.json'
ssh -T ubuntu@206.223.228.87 'sudo test -s /etc/verself/clickhouse/server-ca.pem && sudo ls -l /etc/verself/clickhouse/server-ca.pem'
ssh -T ubuntu@206.223.228.87 'sudo /opt/verself/clickhouse/current/bin/clickhouse client --config-file /etc/clickhouse-client/operator.xml --user clickhouse_operator --query "SELECT count() FROM system.tables WHERE database = '\''verself'\''"'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default nftables'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc logs -address=http://127.0.0.1:4646 -namespace=default -task apply a6a5a315'
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
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default 930deba3'
ssh -T ubuntu@206.223.228.87 'curl -fsS http://127.0.0.1:13133/'
ssh -T ubuntu@206.223.228.87 'sudo /opt/verself/clickhouse/current/bin/clickhouse client --config-file=/etc/clickhouse-client/operator.xml --query "SELECT count(), max(Timestamp) FROM default.otel_logs"'
ssh -T ubuntu@206.223.228.87 'sudo /opt/verself/clickhouse/current/bin/clickhouse client --config-file=/etc/clickhouse-client/operator.xml --query "SELECT count(), max(Timestamp) FROM default.otel_traces"'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/forgejo/report.json'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default forgejo'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default eb35c7e8'
ssh -T ubuntu@206.223.228.87 'curl -fsS http://127.0.0.1:3000/api/healthz'
ssh -T ubuntu@206.223.228.87 'sudo cat /run/verself/recovery/stalwart/report.json'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad job status -address=http://127.0.0.1:4646 -namespace=default stalwart'
ssh -T ubuntu@206.223.228.87 '/opt/verself/profile/bin/nomad alloc status -address=http://127.0.0.1:4646 -namespace=default 0e966a50'
ssh -T ubuntu@206.223.228.87 'curl -fsSI http://127.0.0.1:8090/'
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
baseline reconciliation remains externally gated on operator authority, but
the graph now declares the Zitadel/OpenBao/PostgreSQL dependencies that baseline
will apply once authority is presented. Zitadel still needs to consume its CRDs
at runtime and install materialized artifacts instead of legacy runtime-artifact
sources. After that, OpenBao baseline authority can be presented and the next
live run should expose whether Zitadel reaches service health or blocks on
operator-imported SMTP/GitHub material.

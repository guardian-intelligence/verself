# Data Classification And Recovery Architecture

Data classification is the control surface for backup, restore, retention,
encryption, access review, audit, and recovery drills. Every durable state
source declares its recovery semantics in the owning component. The compiled
catalog lets operators answer:

- what state exists;
- who owns it;
- what impact follows from loss, disclosure, or tampering;
- which artifact proves the latest recoverable point;
- which command or API restores it;
- which drill last proved the recovery path.

The model follows the FIPS 199 security objectives of confidentiality,
integrity, and availability. Impact ratings are applied to each durable source
rather than to an entire service because a single service can own unrelated
classes of state. For example, sandbox-rental-service owns rebuildable runner
acceleration state and customer durable volume state; those sources have
different recovery requirements.

The recovery platform is a Velero-style control plane for Nomad and
source-native recovery tools. The control plane stores declarative recovery
objects, reconciles status, issues scoped grants, dispatches Nomad jobs, and
publishes signed portable metadata. Source-native tools own source consistency:
pgBackRest owns PostgreSQL base backups and WAL, vm-orchestrator owns ZFS send
and receive, ClickHouse owns ClickHouse backup and restore, and Kopia or restic
own generic filesystem repositories where no stronger application-native tool
exists.

## Recovery Objectives

The platform recovery objective is expressed as source-level invariants:

1. Every durable byte has an owning component and a recovery source entry.
2. Every recovery source has an explicit authority. Projections, caches, and
   evidence tables are not promoted into authority by being backed up.
3. Every recovery source has a loss budget, restore target, validation command,
   and last successful drill.
4. Backup artifacts are restorable without the running Verself control plane.
   The repository, recovery keys, object storage credentials, and manifests are
   sufficient for break-glass recovery.
5. Restore first lands in quarantine. Promotion into production requires
   manifest verification, owner-specific invariants, and operator approval for
   sources with high integrity impact.
6. Customer-scoped recovery never crosses tenant boundaries. Org, repository,
   durable scope, and generation identifiers are first-class manifest fields.
7. Destructive operations against backup storage require a stronger authority
   path than ordinary production writes.

## Security Objectives

Each source receives independent `confidentiality`, `integrity`, and
`availability` ratings:

| Rating | Meaning |
| --- | --- |
| `low` | Loss has limited operational or customer impact. |
| `moderate` | Loss has serious impact, requires operator intervention, or can affect customer trust. |
| `high` | Loss can prevent recovery, corrupt customer state, compromise secrets, violate billing/audit truth, or materially impair a customer workload. |

Use the high-water mark for controls. A source with low confidentiality and
high availability still receives high-grade backup controls because loss or
unavailability is the limiting risk.

## State Classes

| Class | Examples | Default CIA | Recovery posture |
| --- | --- | --- | --- |
| `tier0_recovery` | SOPS age identities, OpenTofu state, OpenBao recovery/unseal material, Cloudflare bootstrap token, root CA and SPIRE bootstrap material | C high, I high, A high | Encrypted, offline-capable, multi-destination, restored first. |
| `customer_mission_state` | Customer durable zvols, future persistent VM disks, Forgejo repos and LFS, Stalwart mailboxes, customer-owned artifacts | C high, I high, A high | Event-driven backup, tenant-indexed manifests, restore-to-quarantine, customer-visible status. |
| `platform_authority` | PostgreSQL service databases, Zitadel, IAM, SpiceDB source relationships, Temporal persistence | C moderate/high, I high, A high | Database-native backup, PITR when available, service-owned validation. |
| `financial_truth` | TigerBeetle replicas, billing PostgreSQL ledger-command rows, immutable billing documents | C moderate, I high, A high | Consensus replication preferred, deterministic reconciliation, strict restore validation. |
| `governance_evidence` | API activity events, audit payloads, HMAC chains, deploy evidence, billing events | C moderate/high, I high, A moderate/high | Append-only backup, chain verification, export-manifest parity. |
| `rebuildable_acceleration` | Golden CI workspaces and caches declared rebuildable, package caches, Docker layer caches | C moderate/high, I moderate, A low/moderate | Backup only when product policy requires faster recovery; misses must be tolerated. |
| `public_or_reproducible` | Built binaries, generated OpenAPI, published docs, reproducible release artifacts | C low, I moderate/high, A moderate | Rebuild from repo or content-addressed artifact store. |

`customer_mission_state` includes future durable volumes and persistent dev VM
disks. Current golden CI generations are classified per scope. If the scope is
only runner acceleration and the customer contract says cache misses are
acceptable, it remains `rebuildable_acceleration`. If a customer declares a
durable directory as product data or pays for persistence beyond acceleration,
it becomes `customer_mission_state`.

## Recovery Source Manifest

Each owner declares recovery sources near its code:

```text
src/services/<service>/recovery-assets.yml
src/infrastructure-components/<component>/recovery-assets.yml
src/substrate/<component>/recovery-assets.yml
src/host/recovery-assets.yml
```

Minimum shape:

```yaml
service: sandbox-rental-service
version: 1
sources:
  - id: sandbox.customer_zvol_generation
    class: customer_mission_state
    owner: sandbox-rental-service
    authority:
      system: zfs
      component: vm-orchestrator
      metadata: sandbox-rental-service.postgres
    tenant_scope:
      kind: org
      keys:
        - org_id
        - repository_id
        - durable_scope_id
        - generation_id
    impact:
      confidentiality: high
      integrity: high
      availability: high
    consistency:
      capture_point: sealed_zfs_snapshot
      promotion_barrier: durable_generation_recorded
    native:
      adapter: zfs
      repository_ref: zfs.prod.goldens
      operation: send
      dispatch:
        agent_pool: recovery.zfs
        task_kind: source_capture
    recovery:
      rpo: on_generation_seal
      rto_class: expedited
      restore_mode: zfs_receive_to_quarantine_then_promote
      validation:
        - zfs_stream_sha256
        - ext4_readonly_mount
        - generation_manifest_matches_postgres
    retention:
      policy_ref: customer_durable_volume_default
      minimum_lock_days: 30
    encryption:
      artifact: envelope_age_and_kms
      manifest: signed
    drills:
      schedule_ref: weekly_sample_by_org
```

The manifest is the contract. Runtime backup and restore code consumes the
compiled catalog rather than using ad hoc paths, bucket names, table lists, or
implicit service conventions. `native` entries identify the adapter and native
repository responsible for consistency, keeping byte movement inside
source-native tools and repositories.

## Catalog Compilation

A recovery catalog compiler reads all `recovery-assets.yml` files and produces:

- a route/catalog artifact for recovery-service internal APIs;
- CI checks that fail when durable migrations, zvol roots, or host state paths
  are added without recovery classification;
- a site-local operator inventory grouped by class, owner, and RPO;
- ClickHouse expected-evidence metadata for drills.

The compiler should inspect high-risk changes:

- PostgreSQL migrations adding tables without a source entry;
- migrations adding `org_id`, `subject_id`, `credential`, `secret`, `token`,
  `private_key`, `document`, `invoice`, `event`, or `snapshot` columns;
- new ZFS dataset roots or zvol generation paths;
- new Nomad host volumes under `/var/lib`;
- new SOPS bags or OpenTofu resources that emit sensitive state;
- new object-storage buckets or external providers.

## Recovery Control Plane

Recovery state uses declarative API objects with separate desired state and
observed status, following the Velero `spec` and `status` pattern. Postgres is
the online status cache. Object storage manifests, TUF metadata, DSSE
attestations, and offline keys remain the portable recovery inventory.

Core recovery objects:

| Object | Purpose |
| --- | --- |
| `RecoverySource` | Compiled source descriptor from `recovery-assets.yml`, including owner, class, impact, native adapter, tenant scope, validation, and policy refs. |
| `RecoveryPolicy` | RPO, RTO class, retention, immutability, encryption, drill cadence, and required promotion gates. |
| `RecoveryStorageLocation` | Object storage bucket or prefix, provider, endpoint, trust roots, lock policy, grant policy, and default status. |
| `NativeRepositoryLocation` | Source-native backup repository or snapshot location, such as a pgBackRest repo, ZFS dataset root, ClickHouse S3 endpoint, or Kopia repository. |
| `RecoverySchedule` | Cron or event-driven run template with pause, skip-immediate, and last-run status fields. |
| `RecoveryRun` | Request and status for one source-native capture. A run records native operation ids, task ids, observations, manifest refs, warnings, and errors. |
| `RecoveryObservation` | Append-only progress, log summary, native receipt, validation, and evidence facts emitted by controllers and agents. |
| `RecoveryManifest` | Signed portable description of a completed recoverable point and any source-native artifacts or repositories required to restore it. |
| `RestorePlan` | Resolved manifest, target workspace, read grants, native restore steps, validators, and promotion gates before mutation begins. |
| `RestoreRun` | Request and status for one restore workflow from manifest resolution through quarantine, validation, approval, and promotion. |
| `DrillRun` | Restore or verification exercise with selected manifest, expected evidence, observed evidence, cleanup proof, and incident status. |
| `PromotionGate` | Typed approval or invariant required before production pointers, databases, datasets, or service authority are changed. |

`RecoveryRun` starts the source-native capture adapter and records the result.
Native tools and agents stream or write data directly to their configured
repositories through scoped credentials.

Representative recovery run:

```yaml
kind: RecoveryRun
source_id: postgres.billing
mode: scheduled
storage_location: r2-prod-recovery
native_repository: pgbackrest-prod
native:
  adapter: pgbackrest
  stanza: billing
  operation: backup
policy_ref: postgres_authority_default
status:
  phase: Completed
  native_operation_id: 20260516-180001F
  manifest_ref: recovery/v1/sites/prod/sources/postgres.billing/runs/run_01/manifest.json
```

Controller phases are explicit and stable:

```text
RecoveryRun:
  New
  Validating
  IssuingWriteGrant
  DispatchingTask
  Capturing
  RunningValidation
  PublishingManifest
  Completed | PartiallyFailed | Failed

RestoreRun:
  New
  Validating
  ResolvingManifest
  IssuingReadGrant
  DispatchingTask
  RestoringToQuarantine
  RunningOwnerValidation
  AwaitingPromotionApproval
  Promoting
  Completed | PartiallyFailed | Failed
```

The controller records child tasks rather than flattening every step into the
parent run. A `RecoveryTask` or `RestoreTask` is one dispatched unit of work with
a placement target, input digest, grant reference, native adapter, timeout, and
terminal status. Parent runs aggregate child status, warnings, errors, and
evidence counts.

## Nomad Execution Model

Nomad provides placement and execution. Recovery truth remains reconstructable
from object storage metadata and offline keys.

The runtime jobs are:

| Job | Nomad type | Purpose |
| --- | --- | --- |
| `recovery-controller.nomad.hcl` | `service` | Long-running API and reconciler. It validates recovery objects, issues grants, dispatches tasks, updates status, publishes manifests, and records evidence. |
| `recovery-agent.nomad.hcl` | `system` | Host-local agent on nodes with recovery capability. It exposes typed adapters for host paths, ZFS RPC, pgBackRest config, ClickHouse config, and filesystem repositories. |
| `recovery-task.nomad.hcl` | `batch` or `sysbatch` | Parameterized job dispatched for one backup, restore, verify, drill, repository maintenance, or cleanup step. |

Agents advertise capabilities such as `zfs_send`, `zfs_receive_quarantine`,
`pgbackrest`, `clickhouse_backup`, `kopia`, and `tier0_files`. Placement uses
Nomad node metadata and source catalog constraints. Dispatch payloads include a
canonical task input digest and are signed by recovery-service; agents verify
the signature before acquiring local privileges or using restore grants.

Node-agent concurrency is configured per capability and per host. Large data
movement uses native tool concurrency and object-storage multipart behavior.
Recovery-service sizes only controller TPS, task fan-out, and observation
ingest. Repository maintenance runs as explicit tasks so maintenance CPU,
memory, cache, and network usage do not contend with the controller.

## Recovery API Surface

The Smithy source of truth lives in `src/smithy/models/verself/recovery.smithy`.
The public surface is intentionally small at first; most operations are internal
SPIFFE mTLS APIs used by controllers, agents, and operator tooling.

Core operations:

```text
POST /internal/v1/recovery/runs
GET  /internal/v1/recovery/runs/{run_id}
POST /internal/v1/recovery/runs/{run_id}/observations
POST /internal/v1/recovery/runs/{run_id}/complete
POST /internal/v1/recovery/runs/{run_id}/abort
POST /internal/v1/recovery/restore-plans
POST /internal/v1/recovery/restores
GET  /internal/v1/recovery/restores/{restore_id}
POST /internal/v1/recovery/restores/{restore_id}/observations
POST /internal/v1/recovery/promotion-gates/{gate_id}/approve
POST /internal/v1/recovery/drills
GET  /internal/v1/recovery/sources
GET  /internal/v1/recovery/status
```

object-storage-service exposes storage-location and grant operations to
recovery-service. It owns provider abstraction, scoped credentials, bucket and
prefix policy, and provider capability reporting. recovery-service owns run
reconciliation, source semantics, Nomad dispatch, native repository status, and
promotion gates. Source owners and recovery-agent adapters own host mutation,
native consistency, and validation.

## Module Structure

Recovery control-plane code is separate from object-storage provider code:

```text
src/services/recovery-service/
src/services/recovery-service/migrations/
src/services/recovery-service/internal/api/
src/services/recovery-service/internal/catalog/
src/services/recovery-service/internal/controller/
src/services/recovery-service/internal/controller/recoveryrun/
src/services/recovery-service/internal/controller/restorerun/
src/services/recovery-service/internal/controller/schedule/
src/services/recovery-service/internal/controller/drillrun/
src/services/recovery-service/internal/controller/storagelocation/
src/services/recovery-service/internal/controller/nativerepository/
src/services/recovery-service/internal/controller/gc/
src/services/recovery-service/internal/controller/sync/
src/services/recovery-service/internal/dsse/
src/services/recovery-service/internal/manifest/
src/services/recovery-service/internal/nomad/
src/services/recovery-service/internal/store/
src/services/recovery-service/internal/tuf/

src/substrate/recovery-agent/
src/substrate/recovery-agent/internal/agent/
src/substrate/recovery-agent/internal/adapters/pgbackrest/
src/substrate/recovery-agent/internal/adapters/zfs/
src/substrate/recovery-agent/internal/adapters/clickhouse/
src/substrate/recovery-agent/internal/adapters/kopia/
src/substrate/recovery-agent/internal/adapters/tier0/

src/tools/recovery/cmd/recoveryctl/
src/tools/recovery/cmd/recovery-verify/
src/tools/recovery/catalog/
src/tools/recovery/dsse/
src/tools/recovery/manifest/
src/tools/recovery/tuf/
```

The online CLI calls recovery-service. The offline verifier reads object storage
metadata, TUF roots, signed manifests, DSSE attestations, and encrypted
artifacts directly.

## Backup Artifact Model

The object storage layout is provider-neutral:

```text
recovery/v1/sites/<site>/sources/<source_id>/runs/<run_id>/manifest.json
recovery/v1/sites/<site>/sources/<source_id>/runs/<run_id>/manifest.sig
recovery/v1/sites/<site>/sources/<source_id>/runs/<run_id>/attestations/<name>.dsse.json
recovery/v1/sites/<site>/sources/<source_id>/runs/<run_id>/artifacts/<artifact_id>.bin
recovery/v1/sites/<site>/indexes/orgs/<org_id>/<source_id>/<run_id>.json
recovery/v1/sites/<site>/indexes/latest/<source_id>.json
recovery/v1/tuf/root.json
recovery/v1/tuf/snapshot.json
recovery/v1/tuf/timestamp.json
recovery/v1/tuf/targets/<delegation>.json
```

The manifest is written last and is immutable. It contains no plaintext
secrets. It records:

- source id, source version, class, impact ratings, and owner;
- site, environment, service version, git SHA, and trace id;
- tenant scope keys when tenant-scoped;
- capture timestamp and consistency point;
- artifact list, sizes, chunking, compression, hashes, and provider object
  addresses;
- encryption envelope metadata and recipient key ids;
- retention and lock policy applied by the provider;
- validation commands and expected evidence rows;
- parent run or parent snapshot for incremental sources;
- native repository references, backup labels, WAL ranges, snapshot ids, or
  source-native restore handles when bytes are stored in a native repository.

`ETag` and provider checksums are recorded as provider evidence. They are not
the artifact integrity authority. The manifest must include platform-computed
content hashes over the encrypted artifact and, where practical, over the
plaintext stream before encryption.

Some sources do not produce `artifacts/<artifact_id>.bin` objects. pgBackRest,
ClickHouse, ZFS replication repositories, and Kopia or restic repositories may
store data in native layouts. The Verself manifest binds those native repository
objects to source identity, tenant scope, policy, validation evidence, and
restore instructions.

## Encryption

Backup encryption is client-side by default.

Artifact encryption uses envelope encryption:

- a random data-encryption key per artifact or chunk group;
- recipient wrapping for an online KMS path, such as OpenBao Transit;
- recipient wrapping for at least one offline recovery identity independent of
  the site being recovered;
- authenticated metadata binding source id, run id, artifact id, and tenant
  scope into the encryption context.

Manifest signing uses a long-lived recovery signing key whose public verifier
is committed to the repository. The signature binds manifest bytes exactly.
Operators can verify manifests before OpenBao, Postgres, object-storage-service,
or the original site exists.

TUF metadata publishes the recoverable inventory and signing-key delegation
state. DSSE attestations record capture, validation, drill, restore, and
promotion evidence. TUF and DSSE metadata are signed independently from runtime
Postgres state so object storage can be synchronized into a clean recovery
workspace and verified offline.

## Access Model

recovery-service owns recovery objects, controllers, schedules, run state,
restore plans, drill records, manifest publication, and promotion gates.
object-storage-service owns provider abstraction, bucket layout, storage
locations, scoped credentials, restore grants, and provider capability reporting.
Host mutation, filesystem consistency, and application validation remain owned
by the source-native components and recovery-agent adapters.

Runtime write credentials are scoped for upload and manifest write. They do not
authorize retention policy changes or bucket deletion. Restore credentials are
read-only and issued only for drills or break-glass recovery. Deletion and
retention-shortening require a separate break-glass authority path.

Recovery controllers and agents call internal SPIFFE mTLS APIs:

```text
POST /internal/v1/recovery/runs
POST /internal/v1/recovery/runs/{run_id}/observations
POST /internal/v1/recovery/runs/{run_id}/complete
POST /internal/v1/recovery/runs/{run_id}/abort
POST /internal/v1/recovery/restore-plans
POST /internal/v1/recovery/restores
POST /internal/v1/recovery/drills
```

Native adapters write repository bytes directly to the provider through scoped
credentials or through their source-native repository configuration.
recovery-service records the manifest after the adapter reports native receipts,
artifact hashes, and validation results.

Break-glass restore uses a repo-owned offline tool that reads provider objects
and manifests directly. The tool must not depend on object-storage-service,
Postgres, Nomad, Zitadel, SPIRE, or OpenBao being healthy.

## Capture Patterns

### PostgreSQL

PostgreSQL authority sources use base backups plus continuous WAL archiving for
point-in-time recovery. pgBackRest is the default capture and restore adapter.
`pg_dump` is an export format, not the primary recovery path for authoritative
service state. Each service database has a source entry with:

- database name;
- pgBackRest stanza and repository;
- base backup cadence;
- WAL archive destination;
- PITR retention;
- restore target validation queries;
- service-owned invariants.

Restores land in a scratch cluster or renamed database first. Promotion occurs
after service-specific invariant checks and cross-service reconciliation where
the restored source participates in another authority system.

### ZFS Zvols

ZFS sources use event-driven snapshots and `zfs send` streams. vm-orchestrator
owns privileged capture for customer zvols. recovery-agent calls typed
vm-orchestrator RPCs for export, receive, quarantine mount, validation, and
promotion.

Customer durable generation backup is triggered by sealed snapshot creation.
The manifest records:

- full snapshot ref;
- dataset root and org namespace;
- generation id, source generation id, durable scope id, and operation id;
- stream type: full, incremental, or resume;
- parent snapshot or bookmark for incrementals;
- used and written byte counts;
- filesystem validation results.

Restore uses `zfs receive` into a quarantine dataset. The dataset is mounted
read-only or booted in an isolated VM before any production pointer is changed.

### TigerBeetle

TigerBeetle durability is primarily a cluster property. Production recovery
should use the recommended multi-replica topology with independent fault
domains. A single-node TigerBeetle data-file copy is a pre-release fallback and
must be labeled as such in the catalog.

For production readiness:

- use replica recovery for failed nodes;
- validate ledger state against billing PostgreSQL command rows;
- record TigerBeetle cluster id, replica count, replica index, binary version,
  and data-file hashes in recovery manifests;
- avoid treating a stale object backup as equivalent to consensus durability.

### ClickHouse

ClickHouse contains projections, traces, audit evidence, billing events, and
operator evidence. Some tables are rebuildable from PostgreSQL or service
events. Governance audit and deployment evidence are classified as
`governance_evidence` and backed up with ClickHouse-native backup and restore
where practical.

Each ClickHouse source entry declares whether it is:

- authoritative evidence that must be restored;
- rebuildable projection that can be regenerated;
- operational telemetry with bounded retention and no restore requirement.

Native ClickHouse backup metadata records database, table set, backup id, base
backup for incrementals, S3 endpoint or disk, row counts, byte counts, and
verification queries.

### Object Stores And Repositories

Forgejo repositories, LFS objects, Stalwart mail, Verdaccio storage, Zot
storage, and future customer artifact buckets are object/file authorities. They
use application-aware manifests where available and filesystem or object-store
inventory otherwise. Content-addressed stores must still back up their index
and namespace metadata because object bytes alone may be insufficient to
restore customer-visible state.

Generic filesystem capture uses Kopia or restic repositories when no stronger
application-native backup tool exists. The selected repository type is recorded
in the manifest and controls restore path selection; a restore uses the tool
that produced the backup.

### Tier 0 Files

Tier 0 material is backed up on every change and after every provisioning apply:

- SOPS bags;
- Age recipients and recovery identity metadata;
- OpenTofu state and state backups;
- OpenBao recovery/unseal metadata;
- Cloudflare/R2 bootstrap token metadata;
- root CA, SPIRE bootstrap, and Pomerium/WireGuard recovery facts.

OpenTofu state is always sensitive. Marking outputs `sensitive` only redacts
CLI output; it does not remove secrets from state. State files and saved plans
use encryption and the same immutable manifest path as other Tier 0 artifacts.

## Restore Modes

| Restore mode | Description |
| --- | --- |
| `decrypt_only` | Decrypt and verify small Tier 0 files into a clean recovery workspace. |
| `database_pitr` | Restore a database base backup and replay WAL to a target timestamp. |
| `database_snapshot` | Restore a consistent snapshot without PITR. |
| `zfs_receive_to_quarantine_then_promote` | Receive a ZFS stream into a non-production dataset, validate, then change product pointers. |
| `object_inventory_rehydrate` | Recreate object/file authorities from manifest and object inventory. |
| `rebuild_projection` | Recompute a projection from authoritative sources rather than restoring bytes. |
| `cluster_replica_recover` | Rejoin or reconstruct a database replica using the database's native recovery protocol. |

Promotion rules are source-specific. High-integrity sources require an
explicit post-restore evidence bundle before promotion.

## Drill Model

Drills are production controls. Each drill produces:

- a recovery run id or selected manifest id;
- a restore workspace id;
- verification outputs;
- trace ids and logs;
- ClickHouse evidence rows;
- operator or automated decision;
- cleanup proof.

Minimum drill schedule:

| Source class | Drill |
| --- | --- |
| `tier0_recovery` | Decrypt and verify required files from a clean workspace after each provisioning change and at least weekly. |
| `customer_mission_state` | Restore a sampled zvol generation to quarantine, mount read-only, and verify manifest weekly. |
| `platform_authority` | PITR restore one PostgreSQL service database to scratch weekly; full authority chain monthly. |
| `financial_truth` | TigerBeetle replica recovery or ledger reconciliation drill monthly, more frequently before billing launch. |
| `governance_evidence` | Verify HMAC chain and export manifest from restored artifacts weekly. |
| `rebuildable_acceleration` | Periodic cache-miss canary rather than restore drill. |

Drill failures are production incidents for high-impact classes. The system may
mark a backup source degraded when its most recent required drill is stale or
failed.

## Retention And Immutability

Retention has two layers:

1. Product retention: how long Verself promises to preserve or be able to
   restore a source.
2. Backup immutability: how long a written artifact cannot be deleted or
   overwritten.

The backup immutability window is at least the operational rollback window for
the source. High-impact sources require provider-side lock where available.
Lifecycle cleanup must never run before the strictest matching lock or product
retention rule expires.

Default policy references:

| Policy | Minimum behavior |
| --- | --- |
| `tier0_default` | Preserve indefinitely in at least two destinations, including one offline-capable path. |
| `postgres_authority_default` | Continuous WAL plus base backups with at least seven days PITR in pre-release; increase before customer commitments. |
| `customer_durable_volume_default` | Backup on sealed generation, retain current and recent generations according to customer policy, lock artifacts for at least 30 days. |
| `governance_evidence_default` | Retain according to audit policy, verify chain during drills. |
| `rebuildable_cache_default` | No offsite backup requirement unless an explicit product tier says otherwise. |

## Boundary Seams

### Owner Seam

The service or component that understands semantics owns the source entry,
validation, retention reference, and restore promotion rules. Shared backup
infrastructure transports bytes and records manifests; it does not infer
business meaning from paths or table names.

### Privilege Seam

Host mutation is isolated to privileged components such as vm-orchestrator and
host backup agents. Product services request capture and restore by source id
and stable refs. They never execute host-level filesystem commands.

### Provider Seam

Object storage providers store encrypted artifact bytes and immutable
manifests. Provider metadata is evidence, not the integrity authority.
Switching R2, S3, B2, Garage, or another provider must not change manifest
semantics.

### Restore Seam

Restore is a separate workflow from backup. It uses read grants, quarantine
targets, validation commands, and promotion operations. A successful upload does
not prove restore.

### Evidence Seam

ClickHouse records drill and backup evidence for operator queries. The signed
manifest and artifact hashes remain the portable proof used during break-glass
recovery.

### Execution Seam

Nomad allocation history is operational state. recovery-service records durable
run status in Postgres while the platform is alive and publishes the portable
truth to object storage. Rebuilding recovery status after a site loss starts by
synchronizing TUF metadata, signed manifests, and DSSE attestations from object
storage.

## Implementation Strategy

### Stage 1: Catalog And Tier 0

- Add `recovery-assets.yml` schema and compiler.
- Add manifest, TUF, and DSSE libraries plus offline verification.
- Add source entries for SOPS bags, OpenTofu state, provisioning secrets, and
  R2 backup infrastructure.
- Encrypt and upload Tier 0 artifacts after provisioning apply.
- Implement offline manifest verification.
- Add a weekly clean-workspace Tier 0 drill.

### Stage 2: One Authority Database And One Zvol Source

- Add pgBackRest base backup and WAL archiving for one service database.
- Add event-driven backup for one sealed customer zvol generation.
- Implement restore-to-quarantine for the zvol source.
- Record drill evidence in ClickHouse.
- Prove break-glass restore without object-storage-service.

### Stage 3: Recovery Control Plane

- Add recovery-service Smithy APIs for recovery runs, observations, restore
  plans, restore runs, promotion gates, schedules, and drill records.
- Add recovery-controller and recovery-agent Nomad jobs.
- Add object-storage-service grant APIs used by recovery-service.
- Compile recovery source descriptors into recovery-service at deploy time.
- Enforce source class, tenant scope, retention, and required validation fields
  at manifest commit.

### Stage 4: Service Coverage

- Add recovery manifests for every service database and infrastructure
  component state directory.
- Add CI checks for unclassified durable state.
- Add owner-local restore validators.
- Add operator views grouped by class, stale drill, RPO breach, and tenant.

### Stage 5: Cross-Provider And Full-Site Recovery

- Add second provider destination for `tier0_recovery` and
  `customer_mission_state`.
- Add full-site drill from blank host: provision, restore Tier 0, restore
  authority databases, restore selected customer zvol, deploy, verify evidence.
- Add destructive-operation controls for backup buckets and retention policies.

## Source Inventory Baseline

Initial catalog entries should cover:

- `src/host/sites/<site>/secrets/*.sops.yml`;
- `src/tools/provisioning/terraform/terraform.tfstate`;
- PostgreSQL cluster and per-service databases;
- OpenBao raft storage and recovery metadata;
- Zitadel database and bootstrap identity state;
- SPIRE server state and trust bundle bootstrap;
- TigerBeetle data file or replica set;
- ClickHouse governance, billing, deployment, and trace evidence tables;
- Garage, Forgejo, Stalwart, Verdaccio, Zot, NATS JetStream, and Temporal
  state directories;
- vm-orchestrator ZFS roots under `images/`, `orgs/<org>/workloads/`, and
  `orgs/<org>/goldens/`;
- sandbox-rental durable generation metadata in PostgreSQL;
- object-storage-service bucket metadata and provider credential records.

Every entry declares whether bytes are authoritative, evidence, projection, or
rebuildable acceleration.

## References

- NIST SP 800-34 Rev. 1, Contingency Planning Guide for Federal Information Systems: <https://csrc.nist.gov/pubs/sp/800/34/r1/upd1/final>
- FIPS 199, Standards for Security Categorization of Federal Information and Information Systems: <https://csrc.nist.gov/pubs/fips/199/final>
- CISA, Back Up Business Data: <https://www.cisa.gov/audiences/small-and-medium-businesses/secure-your-business/back-up-business-data>
- PostgreSQL Continuous Archiving and Point-in-Time Recovery: <https://www.postgresql.org/docs/current/continuous-archiving.html>
- OpenZFS send documentation: <https://openzfs.github.io/openzfs-docs/man/master/8/zfs-send.8.html>
- Cloudflare R2 Bucket Locks: <https://developers.cloudflare.com/r2/buckets/bucket-locks/>
- OpenTofu Sensitive Data in State: <https://opentofu.org/docs/language/state/sensitive-data/>
- TigerBeetle Cluster Recommendations: <https://docs.tigerbeetle.com/operating/cluster/>
- TigerBeetle Recovering: <https://docs.tigerbeetle.com/operating/recovering/>
- Velero API Types: <https://velero.io/docs/main/api-types/>
- Velero Backup Storage Locations and Volume Snapshot Locations: <https://velero.io/docs/main/locations/>
- Velero File System Backup: <https://velero.io/docs/main/file-system-backup/>
- The Update Framework Specification: <https://theupdateframework.github.io/specification/latest/>
- in-toto DSSE Envelope: <https://github.com/in-toto/attestation/blob/main/spec/v1/envelope.md>

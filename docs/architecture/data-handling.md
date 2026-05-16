# Data Handling And Recovery Architecture

Data handling is the repo-owned contract for durable state. Each service or
substrate component declares the state it owns, the impact of losing or exposing
that state, the source-native backup mechanism, the storage target, the backup
schedule, and the restore runbook. The compiled catalog drives generated Nomad
jobs, adapter execution, signed attempt manifests, and operator status queries.

The initial implementation is static and generated:

```text
data-assets.yml files
  -> compiled site catalog
  -> generated Nomad periodic jobs
  -> source-native backup adapters
  -> signed attempt manifests in object storage
  -> ClickHouse and object-storage status queries
```

There is no online recovery controller in the first implementation. Nomad owns
periodic execution and placement. Source-native tools own backup consistency.
Signed manifests in object storage are the durable record of backup attempts.
Operator status is derived from the compiled catalog, manifests, Nomad logs, and
ClickHouse evidence.

The catalog and manifest schema remain forward-compatible with a future
`recovery-service`. A later controller can consume the same catalog, invoke the
same adapters, and publish the same manifests if dynamic scheduling, short-lived
grants, online pause/resume, or coordinated restore workflows become necessary.

## Recovery Objectives

The platform recovery objective is expressed as source-level invariants:

1. Every durable source has an owning component and a catalog entry.
2. Every source declares whether its bytes are authoritative, evidence,
   projection, cache, or reproducible output.
3. Every source declares confidentiality, integrity, and availability impact.
4. Every source with backup requirements declares a native adapter, repository,
   schedule, retention policy, validation checks, and restore runbook.
5. Every backup attempt emits a signed manifest, including failed attempts.
6. Backup bytes and manifests are restorable without the running Verself control
   plane when the required repository credentials and recovery keys are present.
7. Restore automation is documented per source, but restore attempts are not a
   first-class online workflow in the initial implementation.

## Security Objectives

Each source receives independent confidentiality, integrity, and availability
ratings:

| Rating | Meaning |
| --- | --- |
| `low` | Loss has limited operational or customer impact. |
| `moderate` | Loss has serious impact, requires operator intervention, or can affect customer trust. |
| `high` | Loss can prevent recovery, corrupt customer state, compromise secrets, violate billing or audit truth, or materially impair a customer workload. |

Controls follow the highest impact rating. A source with low confidentiality and
high availability still receives high-grade backup controls because availability
is the limiting risk.

## State Classes

| Class | Examples | Default CIA | Backup posture |
| --- | --- | --- | --- |
| `tier0_recovery` | SOPS age identities, OpenTofu state, OpenBao recovery material, Cloudflare bootstrap token metadata, root CA and SPIRE bootstrap material | C high, I high, A high | Encrypted, offline-capable, multi-destination, backed up after every provisioning change. |
| `customer_mission_state` | Customer durable zvols, future persistent VM disks, Forgejo repos and LFS, Stalwart mailboxes, customer-owned artifacts | C high, I high, A high | Event-driven or frequent backup, tenant-indexed manifests, restore runbook requires quarantine. |
| `platform_authority` | PostgreSQL service databases, Zitadel, IAM, SpiceDB source relationships, Temporal persistence | C moderate/high, I high, A high | Database-native backup, PITR where available, service-owned validation. |
| `financial_truth` | TigerBeetle replicas, billing PostgreSQL ledger-command rows, immutable billing documents | C moderate, I high, A high | Consensus replication preferred, deterministic reconciliation, strict validation. |
| `governance_evidence` | API activity events, audit payloads, HMAC chains, deploy evidence, billing events | C moderate/high, I high, A moderate/high | Append-only backup or export, chain verification, manifest parity. |
| `rebuildable_acceleration` | Golden CI workspaces and caches declared rebuildable, package caches, Docker layer caches | C moderate/high, I moderate, A low/moderate | Backup only when product policy requires faster recovery; cache misses are tolerated. |
| `public_or_reproducible` | Built binaries, generated OpenAPI, published docs, reproducible release artifacts | C low, I moderate/high, A moderate | Rebuild from repo or content-addressed artifact store. |

Current golden CI generations are classified per durable scope. A scope used only
for runner acceleration remains `rebuildable_acceleration` when the customer
contract accepts cache misses. A customer-declared durable directory or paid
persistence feature is `customer_mission_state`.

## Source Declarations

Owners declare data sources near the code that owns the source semantics:

```text
src/services/<service>/data-assets.yml
src/infrastructure-components/<component>/data-assets.yml
src/substrate/<component>/data-assets.yml
src/host/data-assets.yml
```

Minimum shape:

```yaml
service: billing-service
version: 1

sources:
  - id: postgres.billing
    class: platform_authority
    owner: billing-service
    authority:
      kind: postgres_database
      database: billing
    impact:
      confidentiality: moderate
      integrity: high
      availability: high
    backup:
      adapter: pgbackrest
      repository: pgbackrest-prod
      schedule: "*/30 * * * *"
      retention_ref: postgres_authority_default
      overlap: forbid
    validate:
      checks:
        - pgbackrest_check
        - billing_reconcile_readonly
    restore:
      mode: database_pitr
      runbook: docs/runbooks/restore-postgres-billing.md
      quarantine_required: true
```

Tenant-scoped sources declare the stable tenant keys that must appear in attempt
manifests:

```yaml
tenant_scope:
  kind: org
  keys:
    - org_id
    - repository_id
    - durable_scope_id
    - generation_id
```

Repository and policy definitions live in site configuration compiled into the
same catalog:

```yaml
repositories:
  r2-prod-recovery:
    kind: object_storage
    provider: r2
    bucket: verself-prod-recovery
    prefix: data-handling/v1/sites/prod
    lock_policy_ref: recovery_default

  pgbackrest-prod:
    kind: pgbackrest
    storage_ref: r2-prod-recovery
    repo_name: repo1

policies:
  postgres_authority_default:
    minimum_success_interval: 30m
    retention: 7d
    immutability: 7d
    stale_after: 45m
```

## Catalog Compilation

The compiler reads all `data-assets.yml` files and site repository policy files.
It emits:

- a canonical site catalog with a stable digest;
- generated Nomad job specifications for scheduled backups;
- generated adapter input files per source;
- operator inventory grouped by class, owner, repository, and stale threshold;
- expected ClickHouse evidence metadata for status queries.

The catalog digest is written into every generated job and every attempt
manifest. Status tools can therefore detect attempts produced from stale catalog
inputs.

The compiler fails on high-risk unclassified changes:

- PostgreSQL migrations adding tables without a matching source entry;
- migrations adding `org_id`, `subject_id`, `credential`, `secret`, `token`,
  `private_key`, `document`, `invoice`, `event`, or `snapshot` columns without
  review;
- new ZFS dataset roots or zvol generation paths;
- new Nomad host volumes under `/var/lib`;
- new SOPS bags or OpenTofu state-producing resources;
- new object-storage buckets or external storage providers.

## Generated Nomad Jobs

Nomad periodic jobs provide scheduling, placement, retries, resource limits,
logs, and allocation history. Generated jobs call a repo-built adapter binary
with a source id and catalog digest:

```text
data-backup-adapter \
  --catalog /etc/verself/data-handling/catalog.json \
  --source postgres.billing \
  --attempt-id ${NOMAD_ALLOC_ID} \
  --write-manifest
```

Generated job examples:

```text
generated/data-handling/nomad/postgres-billing-backup.nomad.hcl
generated/data-handling/nomad/tier0-backup.nomad.hcl
generated/data-handling/nomad/clickhouse-governance-backup.nomad.hcl
generated/data-handling/nomad/sandbox-durable-zfs-backup.nomad.hcl
```

`backup.overlap: forbid` maps to a per-source lock. The first implementation may
use native tool locking when available, a host-local lock for single-node
deployment, or a database advisory lock for sources with a natural authority
database. The lock identity includes `site`, `source_id`, and repository id.

Event-driven sources can still use generated jobs. The source owner records an
event, and the job runner receives the source ref as a dispatch payload or
through a small queue table owned by the source service. Customer durable zvol
backup is triggered from sealed generation metadata rather than path scanning.

## Native Adapters

Adapters wrap source-native tools and produce normalized manifests. They do not
implement a generic backup engine.

Adapter contract:

```text
Input:
  catalog path
  catalog digest
  source id
  attempt id
  repository config
  credential paths

Output:
  native receipt
  validation result
  signed attempt manifest
  ClickHouse evidence row
```

Initial adapter locations:

```text
src/tools/data-handling/cmd/data-backup-adapter/
src/tools/data-handling/internal/catalog/
src/tools/data-handling/internal/manifest/
src/tools/data-handling/internal/signing/
src/tools/data-handling/internal/status/
src/tools/data-handling/adapters/pgbackrest/
src/tools/data-handling/adapters/clickhouse/
src/tools/data-handling/adapters/zfs/
src/tools/data-handling/adapters/tier0/
src/tools/data-handling/adapters/kopia/
```

Adapters execute typed operations only. Source declarations refer to known
validation checks by id; they do not embed arbitrary shell commands.

### PostgreSQL

PostgreSQL authority sources use pgBackRest for base backups, incrementals, WAL
archiving, integrity checks, and restore metadata. The adapter records stanza,
repository, backup label, backup type, WAL range, pgBackRest version, `info`
output digest, and validation results. `pg_dump` is an export format, not the
primary recovery path for authoritative service state.

### ZFS Durable Volumes

ZFS sources use event-driven snapshots and `zfs send` streams. vm-orchestrator
owns privileged ZFS operations. The adapter calls typed vm-orchestrator RPCs for
export and later restore helpers; product services never pass host paths or
execute ZFS commands.

Customer durable generation manifests record snapshot ref, dataset root, org
namespace, durable scope id, generation id, source generation id, operation id,
stream type, parent snapshot or bookmark, used bytes, written bytes, and
filesystem validation results.

### ClickHouse

ClickHouse sources use ClickHouse-native backup and restore where practical.
Each source entry declares whether selected tables are authoritative evidence,
rebuildable projections, or operational telemetry. The adapter records database,
table set, backup id, base backup id for incrementals, storage endpoint, row
counts, byte counts, and verification queries.

### Object And File Authorities

Forgejo repositories, LFS objects, Stalwart mailboxes, Verdaccio storage, Zot
storage, and future customer artifact stores use application-aware backups where
available. Generic filesystem capture uses Kopia or restic only when no stronger
application-native tool exists. The manifest records the tool that produced the
backup so restore uses the same repository semantics.

### Tier 0 Files

Tier 0 material is backed up after every provisioning change and on a scheduled
cadence:

- SOPS bags;
- Age recipients and recovery identity metadata;
- OpenTofu state and state backups;
- OpenBao recovery and unseal metadata;
- Cloudflare/R2 bootstrap token metadata;
- root CA, SPIRE bootstrap, Pomerium, and WireGuard recovery facts.

OpenTofu state is always sensitive. Marking outputs `sensitive` only redacts CLI
output; it does not remove secrets from state. State files and saved plans use
the same encryption and manifest path as other Tier 0 artifacts.

## Attempt Manifests

Every attempt writes a manifest, including failures. Successful manifests prove
a recoverable point. Failed manifests preserve operational evidence and make
status queries complete.

Provider-neutral layout:

```text
data-handling/v1/sites/<site>/sources/<source_id>/attempts/<attempt_id>/manifest.json
data-handling/v1/sites/<site>/sources/<source_id>/attempts/<attempt_id>/manifest.sig
data-handling/v1/sites/<site>/sources/<source_id>/latest.json
data-handling/v1/sites/<site>/indexes/classes/<class>/<source_id>.json
data-handling/v1/sites/<site>/indexes/orgs/<org_id>/<source_id>/<attempt_id>.json
```

Minimum manifest shape:

```json
{
  "schema_version": 1,
  "attempt_id": "dhat_01J...",
  "site": "prod",
  "source_id": "postgres.billing",
  "source_class": "platform_authority",
  "owner": "billing-service",
  "catalog_digest": "sha256:...",
  "adapter": "pgbackrest",
  "status": "succeeded",
  "started_at": "2026-05-16T18:00:01Z",
  "completed_at": "2026-05-16T18:03:41Z",
  "repository": {
    "id": "pgbackrest-prod",
    "storage_ref": "r2-prod-recovery"
  },
  "native": {
    "tool": "pgbackrest",
    "version": "2.x",
    "stanza": "billing",
    "backup_label": "20260516-180001F",
    "wal_start": "0000000100000000000000AA",
    "wal_stop": "0000000100000000000000AB"
  },
  "validation": [
    {
      "check": "pgbackrest_check",
      "status": "passed"
    }
  ],
  "retention_ref": "postgres_authority_default",
  "trace_id": "...",
  "log_refs": []
}
```

Manifests contain no plaintext secrets. Provider ETags and checksums are
recorded as provider evidence. Artifact integrity comes from platform-computed
hashes over encrypted bytes and, where practical, plaintext stream hashes before
encryption. Native repositories such as pgBackRest, ClickHouse, ZFS replication,
Kopia, or restic may store data in their own layout; the manifest binds native
repository handles to source identity, tenant scope, policy, and validation
evidence.

Manifest signatures bind the canonical manifest bytes. The public verifier is
committed to the repository so operators can verify manifests before Postgres,
Nomad, SPIRE, OpenBao, or object-storage-service are available.

## Encryption And Access

Backup encryption is client-side by default.

Artifact encryption uses envelope encryption:

- a random data-encryption key per artifact or native chunk group;
- recipient wrapping for an online KMS path, such as OpenBao Transit;
- recipient wrapping for at least one offline recovery identity independent of
  the site being recovered;
- authenticated metadata binding `source_id`, `attempt_id`, repository id, and
  tenant scope into the encryption context.

Runtime credentials are scoped to the generated job and source repository where
the provider allows it. Initial R2 and native repository credentials may be
provisioned through site secrets and host credential files. Short-lived dynamic
grants are a future `recovery-service` capability.

Runtime write credentials do not authorize retention policy changes or bucket
deletion. Restore credentials are read-only and used by documented restore
runbooks or operator drills. Deletion and retention-shortening require a
separate break-glass authority path.

## Restore Runbooks

Restore workflows are source-owned runbooks in the initial implementation. A
source declaration records the restore mode, runbook path, required quarantine
behavior, and validation checks. The system does not maintain online restore
state.

Restore modes:

| Restore mode | Description |
| --- | --- |
| `decrypt_only` | Decrypt and verify small Tier 0 files into a clean recovery workspace. |
| `database_pitr` | Restore a database base backup and replay WAL to a target timestamp. |
| `database_snapshot` | Restore a consistent database snapshot without PITR. |
| `zfs_receive_to_quarantine_then_promote` | Receive a ZFS stream into a non-production dataset, validate, then update product pointers by runbook. |
| `object_inventory_rehydrate` | Recreate object or file authorities from manifest and native inventory. |
| `rebuild_projection` | Recompute a projection from authoritative sources. |
| `cluster_replica_recover` | Rejoin or reconstruct a database replica using the database's native recovery protocol. |

High-integrity sources require quarantine and owner-specific validation before
production promotion. Operator drills can be recorded as signed attempt
manifests or ClickHouse evidence rows without creating a separate online drill
workflow.

## Status Queries

Status is derived from expected catalog sources and observed attempt manifests.
The operator surface answers:

- latest successful attempt by source;
- latest failed attempt by source;
- sources with no attempts;
- sources whose latest success is older than `stale_after`;
- attempts produced from stale catalog digests;
- policy violations such as missing validation, missing lock evidence, or
  unexpected repository id.

Near-term operator commands:

```text
aspect data catalog
aspect data status
aspect data stale
aspect data attempts --source postgres.billing
aspect data verify --source postgres.billing --latest
```

ClickHouse stores a narrow attempt evidence table for fast status queries:

```text
verself.data_backup_attempts
  time
  site
  source_id
  source_class
  owner
  adapter
  attempt_id
  status
  duration_ms
  bytes_written
  catalog_digest
  repository_id
  manifest_ref
  trace_id
  error_code
```

Object storage manifests remain the portable evidence. ClickHouse rows are an
operator acceleration layer and can be rebuilt by scanning signed manifests.

## Retention And Immutability

Retention has two layers:

1. Product retention: how long Verself promises to preserve or be able to
   restore a source.
2. Backup immutability: how long a written artifact cannot be deleted or
   overwritten.

The backup immutability window is at least the operational rollback window for
the source. High-impact sources use provider-side lock where available.
Lifecycle cleanup must never run before the strictest matching lock or product
retention rule expires.

Default policy references:

| Policy | Minimum behavior |
| --- | --- |
| `tier0_default` | Preserve indefinitely in at least two destinations, including one offline-capable path. |
| `postgres_authority_default` | Continuous WAL plus base backups with at least seven days PITR in pre-release; increase before customer commitments. |
| `customer_durable_volume_default` | Backup on sealed generation, retain current and recent generations according to customer policy, lock artifacts for at least 30 days. |
| `governance_evidence_default` | Retain according to audit policy and verify chain during operator checks. |
| `rebuildable_cache_default` | No offsite backup requirement unless an explicit product tier says otherwise. |

## Implementation Strategy

### Stage 1: Catalog And Tier 0

- Add `data-assets.yml` schema and compiler.
- Add source entries for SOPS bags, OpenTofu state, provisioning secrets, and R2
  backup infrastructure.
- Add manifest signing and offline verification.
- Generate the Tier 0 Nomad job or provisioning hook.
- Encrypt and upload Tier 0 attempt manifests after provisioning apply.
- Add `aspect data status` against local catalog and object-storage manifests.

### Stage 2: One Authority Database And One ZFS Source

- Add pgBackRest backup and WAL archiving for one service database.
- Add generated Nomad periodic job for that source.
- Add event-driven backup for one sealed durable zvol generation.
- Record attempt evidence in ClickHouse.
- Prove manual restore from signed manifests and native repositories.

### Stage 3: Service Coverage

- Add source entries for every service database and infrastructure state
  directory.
- Add CI checks for unclassified durable state.
- Add owner-local validation checks.
- Add operator status views grouped by class, stale threshold, owner,
  repository, and tenant.

### Stage 4: Cross-Provider And Recovery Automation

- Add second provider destination for `tier0_recovery` and
  `customer_mission_state`.
- Add regular clean-workspace verification for Tier 0.
- Add full-site runbooks from blank host: provision, restore Tier 0, restore
  authority databases, restore selected customer zvol, deploy, and verify
  evidence.
- Introduce `recovery-service` only when online coordination is required for
  dynamic scheduling, short-lived grants, pause/resume, promotion approvals, or
  customer-visible restore workflows.

## Source Inventory Baseline

Initial catalog entries cover:

- `src/host/sites/<site>/secrets/*.sops.yml`;
- OpenTofu state and state backups;
- PostgreSQL cluster and per-service databases;
- OpenBao raft storage and recovery metadata;
- Zitadel database and bootstrap identity state;
- SPIRE server state and trust bundle bootstrap;
- TigerBeetle data file or replica set;
- ClickHouse governance, billing, deployment, and trace evidence tables;
- Garage, Forgejo, Stalwart, Verdaccio, Zot, NATS JetStream, and Temporal state
  directories;
- vm-orchestrator ZFS roots under `images/`, `orgs/<org>/workloads/`, and
  `orgs/<org>/goldens/`;
- sandbox-rental durable generation metadata in PostgreSQL;
- object-storage-service bucket metadata and provider credential records.

Every entry declares whether bytes are authoritative, evidence, projection,
cache, or reproducible output.

## References

- NIST SP 800-34 Rev. 1, Contingency Planning Guide for Federal Information Systems: <https://csrc.nist.gov/pubs/sp/800/34/r1/upd1/final>
- FIPS 199, Standards for Security Categorization of Federal Information and Information Systems: <https://csrc.nist.gov/pubs/fips/199/final>
- PostgreSQL Continuous Archiving and Point-in-Time Recovery: <https://www.postgresql.org/docs/current/continuous-archiving.html>
- OpenZFS send documentation: <https://openzfs.github.io/openzfs-docs/man/master/8/zfs-send.8.html>
- Cloudflare R2 Bucket Locks: <https://developers.cloudflare.com/r2/buckets/bucket-locks/>
- OpenTofu Sensitive Data in State: <https://opentofu.org/docs/language/state/sensitive-data/>
- TigerBeetle Cluster Recommendations: <https://docs.tigerbeetle.com/operating/cluster/>
- TigerBeetle Recovering: <https://docs.tigerbeetle.com/operating/recovering/>

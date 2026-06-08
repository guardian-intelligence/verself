# Data Handling And Recovery Architecture

Recoverable product state is declared in Smithy. Service contracts carry the
state classification, recovery objective, retention policy, and status contract
for the large state that materially affects platform recovery.

Smithy is the semantic source for service-owned product state because the repo
already uses Smithy for API contracts, auth metadata, audit metadata, runtime
catalogs, and validators. Recovery metadata follows the same pattern: custom
traits attach semantic obligations to service shapes, and a projection emits a
recovery catalog for validation, operator inventory, and documentation.

Small bootstrap artifacts and host-local recovery material that sit outside
service contracts can use ad hoc host runbooks. The Smithy model is for the
big-ticket state that belongs to services, databases, customer-facing storage,
and internal product APIs.

## Recovery Model

Normal flow:

```text
Smithy service model and recovery traits
  -> Smithy validator and recovery catalog projection
  -> service/internal status endpoint
  -> source-native backup mechanism
  -> signed attempt manifest in object storage
  -> append-only ClickHouse recovery event
```

The initial design is federated. Services and infrastructure owners expose
recovery status; native backup systems produce recovery points; ClickHouse
records append-only evidence; and object storage holds portable signed
manifests.

## Scope

The Smithy recovery model covers:

- service PostgreSQL databases and logical database ownership;
- native database recovery units such as the primary PostgreSQL cluster;
- service-owned logical objects written to object storage;
- classified ClickHouse evidence tables;
- TigerBeetle, Forgejo, Stalwart, Verdaccio, Zot, NATS JetStream, and similar
  product-state systems when they become active recovery obligations;
- customer-facing durable state whose loss is not acceptable as a cache miss.

The Smithy recovery model does not need to cover every byte:

- `.tfstate` and first-host recovery material are owned by provisioning,
  component-owned recovery, and OpenBao operator authority;
- generated artifacts remain governed by generated-artifact policy;
- CI golden artifacts, package caches, Docker layer caches, and other
  acceleration state are explicitly declared as rebuildable only when they
  appear in product service semantics;
- local temporary files and process-private scratch space remain outside this
  model.

Customer ZFS volumes are encrypted at rest at the organization dataset
boundary. CI golden artifacts are `rebuildable_acceleration`: durable zvol
generations plus Firecracker vmstate/memory artifacts and their manifest. They
are excluded from backup catalogs, provider backup jobs, and object-storage
upload pipelines. Loss causes cold CI rebuilds and cache misses.

## State Classes

| Class | Examples | Default CIA | Backup posture |
| --- | --- | --- | --- |
| `customer_mission_state` | Customer durable objects, future persistent VM disks, Forgejo repos and LFS, Stalwart mailboxes | C high, I high, A high | Backed up through source-native or service-owned mechanism; restore requires quarantine when mutable customer state is involved. |
| `platform_authority` | PostgreSQL service databases, Zitadel, IAM, SpiceDB source relationships, Temporal persistence | C moderate/high, I high, A high | Native database backup, PITR where available, service-owned validation. |
| `financial_truth` | TigerBeetle replicas, billing ledger-command rows, immutable billing documents | C moderate, I high, A high | Consensus replication preferred, deterministic reconciliation, strict validation. |
| `governance_evidence` | API activity events, audit payloads, HMAC chains, deploy evidence, billing events | C moderate/high, I high, A moderate/high | Append-only backup or export, chain verification, manifest parity. |
| `rebuildable_acceleration` | Golden CI artifacts, durable workspaces and caches declared rebuildable, package caches, Docker layer caches | C moderate/high, I moderate, A low/moderate | No recovery-byte promise unless a product policy explicitly adds one. |
| `public_or_reproducible` | Built binaries, generated OpenAPI, published docs, reproducible release artifacts | C low, I moderate/high, A moderate | Rebuild from repo or content-addressed artifact store. |

## Smithy Trait Package

Recovery traits live in a shared Smithy namespace:

```text
src/smithy/models/verself/recovery.smithy
```

The traits are ordinary Smithy custom traits and use selectors to limit where
they apply.

```smithy
$version: "2"

namespace verself.recovery.v1

use smithy.api#idRef
use smithy.api#pattern
use smithy.api#required
use smithy.api#trait

@pattern("^[a-z][a-z0-9_.-]*$")
string RecoverySourceId

@pattern("^[a-z][a-z0-9_.-]*$")
string RecoveryUnitId

@pattern("^([0-9]+(s|m|h|d)|not_applicable|on_change|continuous)$")
string RecoveryObjective

@pattern("^(none|indefinite|[0-9]+(d|mo|y))$")
string RetentionPeriod

@idRef(failWhenMissing: true, selector: "operation")
string OperationShape

enum RecoveryClass {
    CUSTOMER_MISSION_STATE = "customer_mission_state"
    PLATFORM_AUTHORITY = "platform_authority"
    FINANCIAL_TRUTH = "financial_truth"
    GOVERNANCE_EVIDENCE = "governance_evidence"
    REBUILDABLE_ACCELERATION = "rebuildable_acceleration"
    PUBLIC_OR_REPRODUCIBLE = "public_or_reproducible"
}

enum ImpactRating {
    LOW = "low"
    MODERATE = "moderate"
    HIGH = "high"
}

enum CustomerCommitment {
    NONE = "none"
    INTERNAL = "internal"
    PRE_PRODUCT = "pre_product"
    PUBLIC = "public"
}

enum DataLossBehavior {
    CACHE_MISS_AND_REBUILD = "cache_miss_and_rebuild"
    REPLAY_WAL_TO_RECOVERY_TARGET = "replay_wal_to_recovery_target"
    RESTORE_LAST_SUCCESSFUL_RECOVERY_POINT = "restore_last_successful_recovery_point"
    RECONCILE_FROM_LEDGER = "reconcile_from_ledger"
    MANUAL_REPROVISION = "manual_reprovision"
}

enum RecoveryMechanism {
    NONE = "none"
    POSTGRES_PITR = "postgres_pitr"
    CLICKHOUSE_BACKUP = "clickhouse_backup"
    TIGERBEETLE_REPLICA_RECOVERY = "tigerbeetle_replica_recovery"
    OBJECT_MANIFEST_EXPORT = "object_manifest_export"
    APPLICATION_NATIVE = "application_native"
    REBUILD = "rebuild"
}

structure RecoveryImpact {
    @required
    confidentiality: ImpactRating

    @required
    integrity: ImpactRating

    @required
    availability: ImpactRating
}

structure RecoveryPolicy {
    @required
    rpo: RecoveryObjective

    @required
    rto: RecoveryObjective

    @required
    dataLossBehavior: DataLossBehavior

    @required
    customerCommitment: CustomerCommitment

    @required
    backupRequired: Boolean

    @required
    retention: RetentionPolicy
}

structure RetentionPolicy {
    @required
    productRetention: RetentionPeriod

    @required
    backupImmutability: RetentionPeriod
}

@trait(selector: "structure")
structure recoverySource {
    @required
    id: RecoverySourceId

    @required
    class: RecoveryClass

    @required
    owner: String

    @required
    impact: RecoveryImpact

    @required
    policy: RecoveryPolicy

    @required
    mechanism: RecoveryMechanism

    protectedBy: RecoveryUnitId
}

@trait(selector: "structure")
structure postgresRecoveryUnit {
    @required
    id: RecoveryUnitId

    @required
    cluster: String

    @required
    stanza: String

    @required
    repositoryRef: String
}

@trait(selector: "structure")
structure postgresDatabaseState {
    @required
    database: String

    @required
    protectedBy: RecoveryUnitId
}

@trait(selector: "structure")
structure objectRecoveryState {
    @required
    bucketClass: String

    @required
    keyPrefix: String

    grantRequired: Boolean
}

@trait(selector: "service")
structure internalStatus {
    @required
    operation: OperationShape
}
```

The exact Smithy syntax can evolve during implementation. The important
standard shape is:

- one trait marks a shape as a recoverable source;
- authority-specific traits describe native recovery units;
- service-level metadata points to a modeled internal status operation;
- validators enforce coverage and reject contradictory combinations.

## PostgreSQL And PITR

PostgreSQL point-in-time recovery is a native cluster mechanism. It combines:

- a physical base backup;
- continuous archived WAL segments;
- a recovery target such as a timestamp, transaction id, or named restore
  point.

The service databases are logical sources. The physical recovery unit is the
PostgreSQL cluster or pgBackRest stanza that owns base backups and WAL
continuity.

```smithy
namespace verself.billing.v1

use verself.recovery.v1#RecoveryClass
use verself.recovery.v1#RecoveryImpact
use verself.recovery.v1#RecoveryMechanism
use verself.recovery.v1#RecoveryPolicy
use verself.recovery.v1#postgresDatabaseState
use verself.recovery.v1#postgresRecoveryUnit
use verself.recovery.v1#recoverySource

@recoverySource(
    id: "postgres.primary",
    class: "platform_authority",
    owner: "host-postgres",
    impact: {
        confidentiality: "moderate",
        integrity: "high",
        availability: "high"
    },
    policy: {
        rpo: "5m",
        rto: "1h",
        dataLossBehavior: "replay_wal_to_recovery_target",
        customerCommitment: "internal",
        backupRequired: true,
        retention: {
            productRetention: "35d",
            backupImmutability: "35d"
        }
    },
    mechanism: "postgres_pitr"
)
@postgresRecoveryUnit(
    id: "postgres.primary",
    cluster: "primary",
    stanza: "postgres-primary",
    repositoryRef: "pgbackrest-prod"
)
structure PrimaryPostgresRecoveryUnit {}

@recoverySource(
    id: "postgres.billing",
    class: "financial_truth",
    owner: "billing-service",
    impact: {
        confidentiality: "moderate",
        integrity: "high",
        availability: "high"
    },
    policy: {
        rpo: "5m",
        rto: "1h",
        dataLossBehavior: "replay_wal_to_recovery_target",
        customerCommitment: "internal",
        backupRequired: true,
        retention: {
            productRetention: "35d",
            backupImmutability: "35d"
        }
    },
    mechanism: "postgres_pitr",
    protectedBy: "postgres.primary"
)
@postgresDatabaseState(database: "billing", protectedBy: "postgres.primary")
structure BillingPostgresState {}
```

Restoring a single service database should start by restoring the PostgreSQL
cluster to a quarantine instance at the target time. The service owner then
exports, imports, or reconciles the relevant database state through a
documented repair procedure. Production is not mutated directly from a PITR
restore.

Billing-service is the first recovery candidate. Its initial recovery contract
covers `postgres.billing` through `postgres.primary` PITR with pgBackRest. This
does not complete the full billing recovery story because billing financial
truth also includes TigerBeetle transfers and billing-related ClickHouse
evidence. The first milestone proves billing PostgreSQL recovery; later
milestones add TigerBeetle replica recovery, billing ClickHouse evidence
exports, and long-retention billing document object manifests where required.

## Service-Owned Object State

Logical service-owned objects use service code plus short-lived object-storage
grants. Broad provider credentials do not live in product services.

```smithy
@recoverySource(
    id: "billing.documents",
    class: "financial_truth",
    owner: "billing-service",
    impact: {
        confidentiality: "moderate",
        integrity: "high",
        availability: "high"
    },
    policy: {
        rpo: "on_change",
        rto: "4h",
        dataLossBehavior: "restore_last_successful_recovery_point",
        customerCommitment: "internal",
        backupRequired: true,
        retention: {
            productRetention: "7y",
            backupImmutability: "30d"
        }
    },
    mechanism: "object_manifest_export"
)
@objectRecoveryState(
    bucketClass: "recovery",
    keyPrefix: "billing/documents",
    grantRequired: true
)
structure BillingDocumentRecoveryState {}
```

The service requests a short-lived upload grant for a concrete object or
multipart upload, writes the encrypted payload directly to object storage, and
emits a signed manifest. The manifest records source id, attempt id, object
key, byte counts, checksums, retention class, validation status, and trace id.

## Rebuildable Acceleration

Acceleration state receives an explicit recovery declaration only when it is
large enough or visible enough that future readers might otherwise infer a
backup promise.

```smithy
@recoverySource(
    id: "sandbox.github-golden-artifacts",
    class: "rebuildable_acceleration",
    owner: "sandbox-rental-service",
    impact: {
        confidentiality: "moderate",
        integrity: "moderate",
        availability: "low"
    },
    policy: {
        rpo: "not_applicable",
        rto: "not_applicable",
        dataLossBehavior: "cache_miss_and_rebuild",
        customerCommitment: "none",
        backupRequired: false,
        retention: {
            productRetention: "none",
            backupImmutability: "none"
        }
    },
    mechanism: "rebuild"
)
structure SandboxGithubGoldenArtifactsState {}
```

This is the classification for CI golden artifacts. Durable ZFS snapshots and
clones remain local lifecycle artifacts for seal, promotion, retention,
pruning, and placement-affinity replication. Firecracker vmstate and memory
artifacts are retained only through golden VM manifests. Future
non-rebuildable customer zvols require a separate `customer_mission_state`
recovery source with a service-owned recovery mechanism; raw zvol backup is
outside the default recovery model.

## Internal Status Endpoint

Recovery status is part of each service's internal status surface.

Each service that owns recoverable state exposes a Smithy-modeled internal
status operation. The operation can live beside existing health/status
operations:

```smithy
@internalStatus(operation: GetBillingInternalStatus)
service BillingService {
    version: "2026-05-16"
    operations: [
        GetBillingInternalStatus
    ]
}

@readonly
@http(method: "GET", uri: "/internal/v1/status")
@identity(mode: "spiffe_mtls", audience: "billing-service", principals: ["workload"])
@rateLimit(bucket: "internal_read")
@requestBudget(maxBytes: 0)
@sdk(module: "billingInternal.status", method: "get", paginated: false, retryable: true)
operation GetBillingInternalStatus {
    output: InternalStatusOutput
}
```

Shared status shapes live in `verself.recovery.v1` or
`verself.common.v1`:

```smithy
structure InternalStatusOutput {
    @required
    service: String

    @required
    status: ServiceStatus

    @required
    checkedAt: Timestamp

    @required
    recovery: RecoveryStatusSummary
}

enum ServiceStatus {
    HEALTHY = "healthy"
    DEGRADED = "degraded"
    UNHEALTHY = "unhealthy"
}

structure RecoveryStatusSummary {
    @required
    status: RecoveryStatus

    @required
    sources: RecoverySourceStatuses
}

enum RecoveryStatus {
    HEALTHY = "healthy"
    STALE = "stale"
    UNPROTECTED = "unprotected"
    UNKNOWN = "unknown"
}

list RecoverySourceStatuses {
    member: RecoverySourceStatus
}

structure RecoverySourceStatus {
    @required
    sourceId: RecoverySourceId

    @required
    class: RecoveryClass

    @required
    mechanism: RecoveryMechanism

    protectedBy: RecoveryUnitId

    latestRecoveryPointAt: Timestamp

    latestSuccessfulAttemptAt: Timestamp

    latestManifestRef: String

    logicalBytes: Long

    bytesWritten: Long

    @required
    stale: Boolean

    staleReason: String

    @required
    integrity: IntegrityStatus
}

enum IntegrityStatus {
    NOT_APPLICABLE = "not_applicable"
    NOT_CHECKED = "not_checked"
    VERIFIED = "verified"
    FAILED = "failed"
}
```

The status endpoint reports posture. It does not execute backups. It reads
native backup status, service metadata, signed manifest indexes, and local
service state. Operator tooling can aggregate all internal status endpoints
plus ClickHouse events into a site view.

## Append-Only Evidence

Every backup attempt or recovery-relevant event emits an append-only
ClickHouse row. ClickHouse is the operational query layer; signed manifests in
object storage are the portable recovery inventory.

```text
verself.recovery_events
  time
  site
  service
  source_id
  source_class
  mechanism
  event_type
  status
  recovery_point_at
  attempt_id
  manifest_ref
  logical_bytes
  bytes_written
  product_retention
  backup_immutability
  rpo
  rto
  trace_id
  error_code
```

ClickHouse status can be rebuilt by scanning signed manifests and native
repository inventories.

## Validation Rules

Smithy validators enforce the recovery contract:

- any shape with `@recoverySource` must declare class, impact, policy,
  mechanism, owner, and retention;
- `backupRequired: true` must not use `mechanism: "none"` or
  `mechanism: "rebuild"`;
- `backupRequired: false` is valid only for `REBUILDABLE_ACCELERATION` and
  `PUBLIC_OR_REPRODUCIBLE`;
- PostgreSQL database sources using `POSTGRES_PITR` must reference a
  `@postgresRecoveryUnit`;
- a service with recoverable sources must declare an internal status operation;
- public customer commitments require a matching public policy entry before
  release;
- high-integrity sources must expose a restore or validation procedure;
- Smithy projections fail when a service database appears in repo metadata but
  has no declared recovery source.

Schema migration inspection can remain outside Smithy, but its result should be
checked against the Smithy recovery projection. For example, a newly added
service database or new high-risk table can fail CI if no matching Smithy
recovery source exists.

## Implementation Strategy

### Stage 1: Smithy Traits And Projection

- Add `verself.recovery.v1` traits and shared status shapes.
- Add a recovery catalog projection to the existing Smithy build.
- Add validators for required fields and contradictory policy combinations.
- Add declarations for `postgres.primary`, `postgres.billing`, rebuildable
  golden artifacts, and the first service-owned object source.

### Stage 2: Internal Status Endpoint

- Add the shared internal status response shape.
- Add one modeled internal status operation per service that owns recoverable
  state.
- Implement status by reading native backup metadata, service-local state, and
  manifest indexes.
- Add `aspect data status` as an aggregator over internal status endpoints and
  ClickHouse evidence.

### Stage 3: Native Backup Evidence

- Add pgBackRest base backup and WAL archive evidence for `postgres.billing`.
- Add signed object-storage manifests for successful and failed attempts.
- Add `verself.recovery_events` in ClickHouse.
- Add object-storage short-lived upload grants for service-owned logical
  objects.

### Stage 4: Coverage Enforcement

- Wire migration and storage-provider checks into the Smithy recovery
  projection.
- Fail CI on newly introduced recoverable state without a Smithy declaration.
- Add restore drills for PostgreSQL PITR into quarantine and one service-owned
  object source.
- Extend billing coverage to billing-related ClickHouse evidence and
  TigerBeetle replica recovery.

## References

- Smithy 2.0 Specification: <https://smithy.io/2.0/spec/index.html>
- Smithy Selectors: <https://smithy.io/2.0/spec/selectors.html>
- Smithy Style Guide: <https://smithy.io/2.0/guides/style-guide.html>
- PostgreSQL Continuous Archiving and Point-in-Time Recovery: <https://www.postgresql.org/docs/current/continuous-archiving.html>
- pgBackRest User Guide: <https://pgbackrest.org/user-guide.html>
- Cloudflare R2 Presigned URLs: <https://developers.cloudflare.com/r2/api/s3/presigned-urls/>
- Cloudflare R2 Temporary Credentials: <https://developers.cloudflare.com/r2/api/s3/temporary-credentials/>
- Cloudflare R2 Bucket Locks: <https://developers.cloudflare.com/r2/buckets/bucket-locks/>
- TigerBeetle Cluster Recommendations: <https://docs.tigerbeetle.com/operating/cluster/>
- TigerBeetle Recovering: <https://docs.tigerbeetle.com/operating/recovering/>

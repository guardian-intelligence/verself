# IAM Service

`iam-service` is the product authorization control plane. Zitadel remains the
authority for human, organization, and customer credential authentication.
SPIRE remains the authority for repo-owned workload identity. SpiceDB is the
relationship authorization database. `iam-service` owns the product semantics
layer over SpiceDB: schemas, relationship writes, consistency policy,
authorization APIs, projection invalidation, audit, and reconciliation.

Product services remain enforcement points. They enforce by calling generated
`iam-service` clients over SPIFFE mTLS or by using a narrow repo-owned IAM
client package that itself calls those generated clients. Product services do
not receive SpiceDB credentials, construct raw SpiceDB tuples, choose SpiceDB
consistency modes directly, or infer product authorization from browser state.

## Authority Boundaries

| Concern | Authority |
| --- | --- |
| Human authentication, org directory, OIDC, MFA, passkeys, customer API credential identity | Zitadel |
| Repo-owned workload authentication | SPIRE |
| Product authorization graph | SpiceDB |
| Product authorization API, schema lifecycle, relationship writes, audit, revocation epochs | `iam-service` |
| Product resource state | Owning service PostgreSQL database |
| Authorization evidence, canaries, audits, and operator inspection | ClickHouse plus `governance-service` |
| Public TLS and routing | HAProxy plus lego |

SpiceDB is private substrate. It has no public origin, no browser-facing route,
and no direct product-service credential distribution. HAProxy is involved only
if a future multi-node deployment needs an internal TCP/L4 gRPC frontend for
several SpiceDB processes.

## Core Invariants

- Raw SpiceDB imports are confined to `iam-service/internal/spicedb`.
- Product services use typed authorization operations, not arbitrary
  object-type, relation, permission, or subject strings.
- Authorization checks return typed decision evidence, not bare booleans.
- Every security-sensitive check carries an explicit freshness policy.
- Product mutations that require authorization accept typed decision evidence.
- Relationship writes are idempotent by default and carry transaction metadata.
- A resource is not exposed to read paths until its authorization parent edges
  have been acknowledged by SpiceDB and represented by a ZedToken.
- List endpoints declare their authorization strategy before implementation:
  coarse gate, `LookupResources`, `CheckBulkPermissions`, or bounded
  materialized projection.
- Electric shape issuance is mediated by bounded capabilities with fixed
  route, columns, predicate template, freshness, expiry, and revocation epoch
  state.
- SpiceDB Watch, IAM outbox rows, and ClickHouse canaries are part of the
  correctness model.

## Substrate

The first deployment uses a systemd-managed SpiceDB process backed by
PostgreSQL:

```text
spicedb.service
  gRPC API:       127.0.0.1:50051
  metrics/pprof:  127.0.0.1:9090
  HTTP gateway:   disabled
  datastore:      PostgreSQL database spicedb
  auth:           gRPC preshared key, private to spicedb and iam-service
```

PostgreSQL is the initial datastore because the platform is currently
single-region and single-node. PostgreSQL is also the substrate the platform
already operates, backs up, observes, and provisions through Ansible. PostgreSQL
must run with `track_commit_timestamp=on` before the SpiceDB Watch API is used.

CockroachDB is the later datastore when one of these becomes material:

- authorization throughput is bounded by PostgreSQL rather than schema shape or
  cache behavior;
- the 3-node topology needs datastore-level fault tolerance for authorization;
- multi-region authorization latency becomes a product constraint;
- CockroachDB-only SpiceDB features, especially Relationship Integrity, justify
  the additional distributed database.

The migration path between datastore types is SpiceDB-level export/import or
`zed backup`, not `pg_dump` and `pg_restore`. `iam-service` being the only
writer keeps that migration a substrate swap behind a stable product API.

## Zanzibar Model

SpiceDB implements a Zanzibar-style authorization graph. Relationships have
the shape:

```text
object_type:object_id#relation@subject_type:subject_id
object_type:object_id#relation@subject_object#subject_relation
```

Examples:

```text
org:acme#member@user:alice
role:deployers#member@user:alice
repository:repo_123#parent_org@org:acme
execution:exec_456#repository@repository:repo_123
attempt:att_789#execution@execution:exec_456
```

The schema defines object types, stored relations, and computed permissions:

```zed
use typechecking

definition user {}
definition api_credential {}

definition role {
  relation member: user | api_credential
}

definition org {
  relation owner: user
  relation admin: user
  relation member: user | api_credential
  relation execution_lister: user | api_credential | role#member

  permission read = owner + admin + member
  permission manage_iam = owner + admin
  permission list_executions = owner + admin + execution_lister
}

definition repository {
  relation parent_org: org

  permission read = parent_org->read
  permission write = parent_org->manage_iam
}

definition execution {
  relation repository: repository

  permission read = repository->read
}

definition attempt {
  relation execution: execution

  permission read_logs = execution->read
}
```

This graph makes broad revocation cheap. If a role with 1,000 members loses
`list_executions` over an org with 1,000,000 executions, the authorization
mutation is one relationship or grant change plus active-stream invalidation.
The 1,000,000 execution rows are not rewritten, and no per-principal
execution projection is deleted.

## Resource Modeling

Model authorization-relevant resources and inheritance boundaries:

- `org`
- `role`
- `api_credential`
- `project`
- `environment`
- `repository`
- `execution`
- `attempt`
- `secret`
- `transit_key`
- `document`
- `mailbox`

Avoid relationships for high-volume append-only leaf data. Execution log chunks
inherit access from `attempt`; they do not receive one SpiceDB relationship per
chunk.

Customer-facing capability toggles are represented as durable authorization
relations or role grants. They are not copied into every resource row. A toggle
change increments a coarse scope epoch and forces affected active streams to
recheck.

## Go Service Shape

`iam-service` is a Go service using the same service contract as the rest of
the product surface: Huma route declarations, generated OpenAPI clients,
SPIFFE mTLS for internal calls, sqlc for PostgreSQL, and OpenTelemetry spans
that land in ClickHouse.

Repository layout:

```text
src/iam-service/
  schema/
    verself.zed
    assertions.yaml
    expected-relations.yaml

  internal/model/
    generated object, relation, permission, caveat, and resource-ref types

  internal/spicedb/
    only package allowed to import the AuthZed Go client

  internal/authz/
    typed Check, CheckBulk, Lookup, Write, Delete, and Watch APIs

  internal/decision/
    Decision, ZedToken, Freshness, Epoch, and capability types

  internal/commands/
    product IAM commands such as grant, revoke, create role, bind role,
    write resource edge, and issue sync capability

  internal/watch/
    SpiceDB Watch consumer, epoch invalidation, reconciliation checkpoints

  internal/store/
    sqlc queries for IAM metadata, outbox, epochs, capability leases, and
    canary state

  internal/api/
    public and internal Huma APIs
```

The generated model package is the only ergonomic way to refer to schema
terms:

```go
iam.Attempt(attemptID).PermissionReadLogs()
iam.Execution(executionID).RelationRepository(repositoryID)
iam.Org(orgID).PermissionListExecutions()
iam.Role(roleID).RelationMember(principal)
```

The unsafe form stays unavailable outside `internal/spicedb`:

```go
Check("attempt", attemptID, "read_logs", "user", subjectID)
```

## Decision Evidence

Authorization checks return typed evidence:

```go
type Decision struct {
    Subject     Principal
    Resource    ResourceRef
    Permission  Permission
    Allowed     bool
    CheckedAt   ZedToken
    Freshness   Freshness
    Caveated    bool
    TraceID     string
}
```

Commands accept narrow decision wrappers:

```go
type CanCreateExecutionDecision struct {
    decision.Decision
}

func CreateExecution(
    ctx context.Context,
    tx pgx.Tx,
    decision CanCreateExecutionDecision,
    input CreateExecutionInput,
) (Execution, error)
```

The store and command layers should be difficult to call without the exact
authorization evidence for the operation. A decision wrapper is valid only for
the subject, resource, permission, and freshness class it was created for.

Bare boolean decisions are allowed only at the HTTP response boundary and in
metrics labels. They are not allowed in command APIs.

## Consistency

Freshness is explicit:

```go
type Freshness interface {
    spiceDBConsistency() *v1.Consistency
}

type MinimizeLatency struct{}
type AtLeastAsFresh struct{ Token ZedToken }
type FullyConsistent struct{}
```

`MinimizeLatency` is for non-security UI hints and low-risk affordance
rendering. Security-sensitive checks use `AtLeastAsFresh` when a relevant
ZedToken exists. `FullyConsistent` is reserved for narrow administrative paths,
break-glass inspection, and canaries.

ZedTokens are stored wherever a later authorization read must be causally fresh:

- resource rows that become visible only after an authorization edge exists;
- revocation events;
- role and membership mutations;
- sync capability issuance;
- projection checkpoints;
- outbox rows that drive cross-service authorization writes.

## Product Write Patterns

Product state and authorization relationships do not share a distributed
transaction. The service design avoids relying on one.

### Resource Creation

For a resource that inherits authorization from a parent:

1. The owning service records the resource as pending or non-syncable.
2. The owning service or its outbox worker calls
   `iam-service.WriteResourceEdges` with an idempotency key derived from the
   resource ID and parent IDs.
3. `iam-service` writes SpiceDB relationships with `TOUCH` plus preconditions
   where the parent edge must be absent or present.
4. `iam-service` returns a ZedToken.
5. The owning service marks the resource active with `authz_min_zed_token`.
6. Read paths check authorization at least as fresh as
   `authz_min_zed_token`.

If the caller needs immediate read-after-create behavior, the API returns only
after the authorization edge has been acknowledged and the resource is active.
Asynchronous edge creation is valid only for resources hidden from read and sync
surfaces until active.

### Resource Mutation

Mutations that require permission over an existing resource load the resource
row, read its `authz_min_zed_token`, call `iam-service` with
`AtLeastAsFresh`, receive a typed decision wrapper, and pass that wrapper into
the command.

The command rechecks product invariants inside the database transaction:
resource owner, optimistic version, command idempotency, state transition, and
tenant scope. SpiceDB proves product permission; the owning service still proves
resource state.

### Revocation

Revocation writes to SpiceDB first and records the returned ZedToken. The same
IAM command writes a compact invalidation event:

```text
principal_scope_epoch
org_scope_epoch
resource_scope_epoch
min_zed_token
reason
changed_by
occurred_at
```

The invalidation scope is coarse. A role losing `list_executions` increments an
org/scope epoch and cancels active streams for that scope. It does not enumerate
every member-resource pair.

## Relationship Writes

Relationship writes are command-shaped:

```text
command_id
operation_id
idempotency_key
actor
origin_subject
relationship_updates
optional_preconditions
traceparent
metadata
```

Default write mode is `TOUCH` for retryability. `CREATE` is reserved for
invariants where a duplicate relationship indicates data corruption. Deletes
use preconditions when sequencing matters.

Every write records transaction metadata for Watch consumers, audit, and
reconciliation. Metadata should include the source service, operation ID,
command ID, idempotency key hash, origin subject, and traceparent.

## Query APIs

`iam-service` exposes typed internal APIs over generated clients:

- `Check`: one request gate.
- `CheckBulk`: table rows, command affordances, dashboard cards, and batch
  render decisions.
- `LookupResources`: bounded list prefiltering when accessible IDs are
  expected to be small enough.
- `LookupSubjects`: admin views such as "who can access this repository?"
- `IssueSyncCapability`: Electric shape authorization.
- `WriteResourceEdges`: product resource parent edges.
- `Grant`, `Revoke`, `CreateRole`, `BindRole`, `DeleteRole`: product IAM
  mutations.

`ReadRelationships` is reserved for diagnostics, reconciliation, and operator
inspection APIs. It is not a product authorization path.

## List Endpoints

Every list endpoint declares one of these strategies:

| Strategy | Use |
| --- | --- |
| Coarse gate | All rows in the already-scoped query are visible if the caller has a collection permission such as `org:list_executions`. |
| `LookupResources` | The accessible resource set is small enough to use as an ID prefilter. |
| Database candidates plus `CheckBulk` | The database query owns pagination/search and IAM filters the candidate page. |
| Materialized permission view | Cardinality and latency justify a maintained projection. |

Large inherited access should use coarse gates or database candidates plus
`CheckBulk`. Per-principal materialized rows are reserved for naturally small
state such as notification inbox state, explicit assignments, or low-cardinality
admin views.

## Electric Sync

Electric sync is a UX projection, with server authorization at the gateway.
SpiceDB decisions do not run inside Electric; `iam-service` and the sync
gateway project authorization state into fixed shape capabilities and compact
scope-state rows.

Shape capabilities contain:

```text
principal
org
route
shape template
fixed table
fixed columns
fixed predicate template
predicate parameters
authz_min_zed_token
epoch vector
expires_at
capability_id
```

The gateway validates every continuation request:

1. capability signature and expiry;
2. route and template match;
3. requested Electric protocol parameters are continuation-only;
4. epoch vector is current;
5. stale epochs trigger a SpiceDB recheck at least as fresh as the recorded
   ZedToken;
6. denial closes the stream and prevents new upstream Electric requests.

The browser also syncs small state tables such as:

```text
sync_principal_scope_state
  principal_id
  org_id
  scope
  allowed
  epoch
  authz_zed_token
  updated_at
```

When a user loses `list_executions`, the browser receives `allowed=false` and
drops the view. Active streams are cancelled through gateway indexes keyed by
principal, org, role, resource, and scope. The server-side cost is proportional
to affected active streams and rechecks, not affected resources.

High-volume leaf streams such as execution log chunks use short-lived
resource-scoped capabilities and stream invalidation. They do not duplicate log
rows into per-principal sync tables.

## Watch, Epochs, And Reconciliation

SpiceDB Watch is consumed by `iam-service` for:

- revocation epoch updates;
- active stream invalidation;
- projection rebuild scheduling;
- audit correlation;
- reconciliation checkpoints;
- canary verification.

Epochs are cache-invalidation metadata. They are not the authorization source of
truth. A stale epoch forces a SpiceDB check with the relevant ZedToken.

Recommended epoch scopes:

```text
principal
principal + org + scope
org + scope
resource
resource + scope
role + scope
```

Avoid epoch rows for every transitive subject-resource pair. Broad graph changes
must remain O(changed relationships + active affected streams).

Reconciliation jobs compare:

- service-owned resource rows that require authorization edges;
- expected SpiceDB relationships;
- IAM outbox command state;
- Watch checkpoints;
- ClickHouse audit rows.

Missing edges keep resources out of sync and read surfaces until corrected.
Unexpected edges are security findings and require explicit deletion through
`iam-service`.

## Caveats

Caveats are for request-time conditions:

- source network class;
- device posture;
- step-up authentication age;
- break-glass session state;
- temporary access windows when expiring relationships are insufficient;
- environment attributes that are not durable product facts.

Durable product facts use relations. Org membership, role membership, API
credential permission grants, repository parentage, execution parentage, and
secret ownership should be represented as relationships.

## API Surface

Public APIs are for organization IAM management by authenticated users and
customer credentials:

- list effective permissions;
- create role;
- update role grants;
- bind and unbind role members;
- list members and credential grants;
- issue or revoke customer API credentials;
- inspect access to a resource;
- view audit-backed authorization history.

Internal APIs are SPIFFE-only and serve product services:

- check permission;
- bulk check permissions;
- lookup resources;
- lookup subjects;
- write resource edges;
- issue sync capabilities;
- read sync scope state;
- publish command/audit metadata.

Public route declarations include IAM metadata, audit metadata, idempotency
metadata, body limits, and rate-limit class in the Huma operation definition.
Internal routes authorize exact SPIFFE peer IDs and carry origin subject fields
explicitly.

## PostgreSQL State

`iam-service` PostgreSQL stores service-owned state around SpiceDB:

- command idempotency records;
- outbox rows;
- role display metadata;
- customer API credential metadata;
- capability lease records;
- revocation epochs;
- sync scope state;
- Watch checkpoints;
- reconciliation findings;
- canary state.

SpiceDB remains the relationship graph. PostgreSQL rows may mirror or index
authorization state for UX, audit, idempotency, and stream control, but product
permission decisions are SpiceDB decisions.

## Security

SpiceDB hardening:

- bind gRPC and metrics to loopback on the single-node topology;
- disable public HTTP gateway;
- store the gRPC preshared key outside the repo;
- make the SpiceDB key readable only by the SpiceDB unit and `iam-service`;
- keep product services away from raw SpiceDB credentials;
- use TLS and an internal L4 frontend before any multi-node or non-loopback
  SpiceDB endpoint;
- keep metrics and pprof loopback or operator-only;
- prohibit manual SQL writes to the SpiceDB datastore;
- prefer physical/ZFS backups over logical PostgreSQL dumps for SpiceDB state;
- subscribe to upstream SpiceDB security advisories.

Service hardening:

- stable problem responses with trace-backed instances;
- no raw token material or SpiceDB credentials in logs;
- body limits on every mutating operation;
- idempotency keys on every mutation;
- audit rows for allow, deny, and error decisions;
- explicit reason codes for denial and stale consistency;
- rate limits keyed by actor, org, operation, and client network;
- typed errors for policy denial, stale capability, failed precondition, and
  substrate unavailability.

## Observability

Every authorization decision emits a span with:

```text
iam.operation_id
iam.permission
iam.resource_type
iam.resource_id_hash
iam.subject_type
iam.subject_id_hash
iam.consistency_mode
iam.zed_token_present
iam.decision
iam.denial_reason
iam.spicedb.latency_ms
iam.spicedb.dispatch_count
iam.cache.hit
```

ClickHouse evidence should answer:

- which operation denied and why;
- whether a stale epoch forced a recheck;
- whether a revocation reached active streams;
- whether a resource was exposed before its authorization edge existed;
- whether Watch lag exceeded the sync revocation budget;
- whether canary principals can or cannot access the expected resources.

`aspect observe --what=iam` should surface:

- SpiceDB process health and version;
- datastore migration version;
- gRPC latency and error rate;
- Check, BulkCheck, Lookup, and Write throughput;
- Watch revision lag;
- epoch invalidation counts;
- active sync capabilities and stream cancellations;
- reconciliation findings;
- recent deny events grouped by stable reason code.

## Verification

Pre-deploy checks:

- `zed validate` for schema syntax and typechecking.
- Schema assertions and expected-relations tests.
- Generated Go model freshness check.
- OpenAPI generation and generated-client contract check.
- Static scan proving only `internal/spicedb` imports the AuthZed Go client.
- Static scan proving product services use generated IAM clients or the
  approved IAM client package.

Live canaries:

- schema read/write canary against SpiceDB;
- relationship write, check, revoke, and stale-token recheck canary;
- bad SpiceDB key denied canary;
- product resource hidden until authorization edge acknowledged;
- role grant revocation cancels active Electric execution-list stream;
- revocation of one role preserves access through an independent role;
- `CheckBulk` list canary with mixed allowed and denied candidates;
- Watch checkpoint advances and writes ClickHouse evidence;
- audit rows contain origin subject and caller SPIFFE ID for internal calls.

Completion evidence is ClickHouse rows from the live path. Unit tests and local
schema checks are guardrails, not deployment proof.

## Load Testing

Load tests exercise the actual schema and datastore:

```text
100, 500, 1k, 2k CheckPermission QPS
1 percent WriteRelationships
CheckBulk candidate pages for dashboard/list endpoints
LookupResources for bounded admin surfaces
role revocation with 1,000 active readers
execution list with 1,000,000 candidate resources
```

Report p50, p95, p99, error rate, Watch lag, Postgres CPU, SpiceDB memory,
dispatch depth, cache hit ratio, and active stream cancellation latency.

The service should preserve the intended complexity bounds:

```text
relationship change:     O(changed relationship tuples)
stream revocation:       O(active affected streams)
authorization recheck:   O(active affected principals * graph depth)
resource row rewrites:   O(0), unless the resource itself changed
```

## Schema Migration Discipline

Schema changes use expand, backfill, verify, and contract:

1. Add new definitions, relations, or permissions while keeping old callers
   valid.
2. Generate new Go model types.
3. Deploy code that writes old and new relationships when needed.
4. Backfill through `iam-service` relationship write commands.
5. Verify with schema assertions, Watch checkpoints, reconciliation, and
   ClickHouse canaries.
6. Remove old schema terms after no relationship data references them.

Deleting or renaming schema terms before relationship data is removed is a
deployment failure.

## Source Notes

- Google Zanzibar paper:
  <https://research.google/pubs/zanzibar-googles-consistent-global-authorization-system/>.
- SpiceDB schema model:
  <https://authzed.com/docs/spicedb/concepts/schema>.
- SpiceDB consistency and ZedTokens:
  <https://authzed.com/docs/spicedb/concepts/consistency>.
- SpiceDB query APIs:
  <https://authzed.com/docs/spicedb/concepts/querying-data>.
- SpiceDB Watch:
  <https://authzed.com/docs/spicedb/concepts/watch>.
- SpiceDB datastore guidance:
  <https://authzed.com/docs/spicedb/concepts/datastores>.
- SpiceDB data migration guidance:
  <https://authzed.com/docs/spicedb/ops/data/migrations>.
- SpiceDB relationship write guidance:
  <https://authzed.com/blog/writing-relationships-to-spicedb>.
- SpiceDB validation and testing:
  <https://authzed.com/docs/spicedb/modeling/validation-testing-debugging>.
- SpiceDB list endpoint modeling:
  <https://authzed.com/docs/spicedb/modeling/protecting-a-list-endpoint>.
- AuthZed best practices:
  <https://authzed.com/docs/best-practices>.
- AuthZed Go client:
  <https://authzed.com/docs/spicedb/getting-started/client-libraries>.
- SPIFFE Workload API:
  <https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/>.
- Huma:
  <https://pkg.go.dev/github.com/danielgtaylor/huma/v2>.

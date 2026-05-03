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
| Authorization evidence, audits, and operator inspection | ClickHouse plus `governance-service` |
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
- SpiceDB Watch, IAM outbox rows, and ClickHouse evidence are part of the
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
  cmd/
    iam-service/
    iam-openapi/
    iam-internal-openapi/
    iam-schema-gen/

  openapi/

  client/
  internalclient/

  migrations/

  schema/
    verself.zed
    assertions.yaml
    expected-relations.yaml

  internal/model/
    generated object, relation, permission, caveat, and resource-ref types

  internal/bootstrap/
    runtime configuration, dependency wiring, worker startup

  internal/spicedb/
    only package allowed to import the AuthZed Go client

  internal/authz/
    typed Check, CheckBulk, Lookup, Write, Delete, and Watch APIs

  internal/decision/
    Decision, ZedToken, Freshness, Epoch, and capability types

  internal/orgs/
    organization profile, organization lookup, available-org listing

  internal/members/
    directory-backed member listing, invite, and role binding commands

  internal/roles/
    customer-visible role catalog, role grants, capability replacement

  internal/credentials/
    customer API credential metadata, grants, roll, revoke, claim resolution

  internal/browser/
    OIDC login, callback, session, selected org, logout, resource tokens

  internal/actions/
    Zitadel action verification and claim response construction

  internal/syncauth/
    Electric shape capability issuance, epoch validation, active stream index

  internal/resourceedges/
    product service resource-parent relationship commands

  internal/commands/
    shared command envelope, idempotency, operation metadata, and outbox helpers

  internal/watch/
    SpiceDB Watch consumer, epoch invalidation, reconciliation checkpoints

  internal/reconcile/
    expected relationship scanners and repair plans

  internal/audit/
    governance audit writer and ClickHouse evidence helpers

  internal/problems/
    stable problem responses and tagged domain error mapping

  internal/directory/
    Zitadel adapter behind product-shaped interfaces

  internal/store/
    sqlc queries for IAM metadata, outbox, epochs, capability leases, and
    reconciliation state

  internal/api/
    route registration and boundary DTO conversion only
```

## Wire Contracts

The first implementation uses Huma/OpenAPI for `iam-service` public and
internal APIs. There is no repo-owned IAM gRPC service proto in the first cut.
The service still consumes protobufs through the upstream AuthZed Go client;
those SpiceDB request and response types remain third-party API types and are
confined to `internal/spicedb`.

Wire contract locations:

| Contract | Location | Bazel owner |
| --- | --- | --- |
| Public IAM API OpenAPI 3.0/3.1 | `src/iam-service/openapi/openapi-3.0.yaml`, `src/iam-service/openapi/openapi-3.1.yaml` | `//src/iam-service/openapi` |
| SPIFFE-only internal IAM API OpenAPI 3.0/3.1 | `src/iam-service/openapi/internal-openapi-3.0.yaml`, `src/iam-service/openapi/internal-openapi-3.1.yaml` | `//src/iam-service/openapi` |
| Generated public Go client | `src/iam-service/client/client.gen.go` | `//src/iam-service/client:client` |
| Generated internal Go client | `src/iam-service/internalclient/client.gen.go` | `//src/iam-service/internalclient:internalclient` |
| Browser TypeScript clients | `src/viteplus-monorepo/apps/verself-web/src/__generated/iam-api/` | frontend OpenAPI generation target |
| SpiceDB schema | `src/iam-service/schema/verself.zed` | `//src/iam-service/schema:schema` |
| SpiceDB schema assertions | `src/iam-service/schema/assertions.yaml`, `src/iam-service/schema/expected-relations.yaml` | `//src/iam-service/schema:schema_tests` |
| Shared DTOs used by multiple services or frontend wrappers | `src/domain-transfer-objects/go/` | `//src/domain-transfer-objects/go:dto` |
| Future shared protobuf messages | `src/domain-transfer-objects/proto/<area>/v1/*.proto` | `//src/domain-transfer-objects/proto/<area>/v1:<area>_proto` |
| Future IAM-owned gRPC-only contract | `src/iam-service/proto/v1/*.proto` | `//src/iam-service/proto/v1:iam_proto` |

Add a service-local protobuf directory only if the operation cannot be cleanly
represented by the existing OpenAPI service pattern, for example a binary
stream that should not become a public HTTP contract. If the message shape is
consumed by more than `iam-service`, put the protobuf under
`src/domain-transfer-objects/proto/` instead.

OpenAPI remains the generated-client surface for product services. A missing
service shape is fixed by adding the Huma route and regenerating the committed
OpenAPI specs and clients, not by hand-writing HTTP or gRPC calls.

## Bazel Targets

The service should create these targets:

```text
//src/iam-service:go_default_library

//src/iam-service/cmd/iam-service:iam-service
//src/iam-service/cmd/iam-service:iam-service_nomad_artifact
//src/iam-service/cmd/iam-openapi:iam-openapi
//src/iam-service/cmd/iam-internal-openapi:iam-internal-openapi
//src/iam-service/cmd/iam-schema-gen:iam-schema-gen

//src/iam-service/openapi:openapi-3.0.yaml
//src/iam-service/openapi:openapi-3.1.yaml
//src/iam-service/openapi:internal-openapi-3.0.yaml
//src/iam-service/openapi:internal-openapi-3.1.yaml

//src/iam-service/client:client
//src/iam-service/internalclient:internalclient
//src/iam-service/migrations:migrations
//src/iam-service/schema:schema
//src/iam-service/schema:schema_tests

//src/iam-service/internal/api:api
//src/iam-service/internal/bootstrap:bootstrap
//src/iam-service/internal/model:model
//src/iam-service/internal/spicedb:spicedb
//src/iam-service/internal/authz:authz
//src/iam-service/internal/decision:decision
//src/iam-service/internal/orgs:orgs
//src/iam-service/internal/members:members
//src/iam-service/internal/roles:roles
//src/iam-service/internal/credentials:credentials
//src/iam-service/internal/browser:browser
//src/iam-service/internal/actions:actions
//src/iam-service/internal/syncauth:syncauth
//src/iam-service/internal/resourceedges:resourceedges
//src/iam-service/internal/commands:commands
//src/iam-service/internal/watch:watch
//src/iam-service/internal/reconcile:reconcile
//src/iam-service/internal/audit:audit
//src/iam-service/internal/problems:problems
//src/iam-service/internal/directory:directory
//src/iam-service/internal/store:store
```

If a future service-local protobuf is added, use the existing repo pattern:

```text
//src/iam-service/proto/v1:iam_proto
//src/iam-service/proto/v1:iam_go_proto
//src/iam-service/proto/v1:proto
```

The root `BUILD.bazel` contains only the Gazelle prefix:

```text
# gazelle:prefix github.com/verself/iam-service
```

Package visibility should enforce the dependency rules below. Public visibility
belongs only on generated clients, OpenAPI artifacts, and any explicit shared
contract package.

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

## Feature Parity Surface

The first implementation covers the complete product IAM and browser-auth
surface:

| Surface | Owning package | Notes |
| --- | --- | --- |
| Browser login, callback, logout, session read, selected org update | `internal/browser` | Owns cookie/session state and OIDC token exchange. Does not perform product authorization decisions directly. |
| Browser resource tokens | `internal/browser` plus `internal/authz` | Resource token issuance requires a typed authorization plan and records token audience, org, scope, and freshness. |
| Available organizations for the caller | `internal/orgs` | Uses token role assignments and directory-backed org metadata; returns only orgs the token proves. |
| Organization profile read/update/resolve | `internal/orgs` | Profile state lives in IAM PostgreSQL; authorization is checked through typed decisions. |
| Organization members | `internal/members` | Directory-backed read model; filters service accounts out of the member table and exposes them through credentials. |
| Member invite and role update | `internal/members` plus `internal/roles` | Mutations write directory state and SpiceDB relationships through command envelopes. |
| Member capability replacement | `internal/roles` | Represented as role grants and SpiceDB relationships; no customer-editable policy language in the first cut. |
| API credential list/read/create/roll/revoke | `internal/credentials` | Secret material is returned only at create or roll. Metadata and grants are durable IAM state. |
| API credential claim resolution | `internal/actions` plus `internal/credentials` | Zitadel action calls resolve non-secret credential metadata and exact allowed permissions. |
| Human profile sync | `internal/orgs` or `internal/members` | Narrow internal operation for directory/profile propagation. |
| Resolve organization by ID or slug | `internal/orgs` | Narrow internal operation for other services and frontend server functions. |
| Authorization checks and list helpers | `internal/authz` | Typed `Check`, `CheckBulk`, `LookupResources`, and `LookupSubjects`. |
| Product resource edge writes | `internal/resourceedges` | SPIFFE-only internal API used by resource-owning services after product state writes. |
| Electric shape capability issuance | `internal/syncauth` | SPIFFE-only internal API used by the sync gateway. |

Feature parity does not mean preserving old route names, database tables,
package names, or implementation structure. The public and internal API shapes
should be cut over cleanly through regenerated clients and frontend server
functions.

## Package Boundaries

The service is organized by stable product responsibility. Shared packages
provide vocabulary and infrastructure; vertical packages own use cases.

```text
cmd/iam-service
  -> internal/bootstrap
  -> internal/api

internal/api
  -> internal/{orgs,members,roles,credentials,browser,actions,syncauth,resourceedges}
  -> internal/problems

internal/{orgs,members,roles,credentials,browser,actions,syncauth,resourceedges}
  -> internal/authz
  -> internal/commands
  -> internal/store
  -> internal/directory
  -> internal/audit

internal/authz
  -> internal/model
  -> internal/decision
  -> internal/spicedb

internal/spicedb
  -> AuthZed Go client

internal/store
  -> sqlc-generated queries
```

`cmd/iam-service` performs configuration loading, dependency construction,
HTTP server startup, background worker startup, and signal handling. It does
not contain request handlers, policy logic, OIDC flow logic, SpiceDB tuple
construction, or SQL queries.

`internal/api` owns Huma operation declarations, request/response DTO mapping,
security metadata, body limits, idempotency metadata, audit metadata, and route
registration. Handlers should normalize boundary DTOs and call one command
method. They should not contain business logic.

Vertical packages own command methods and domain validation for one product
area. They may coordinate store transactions, directory calls, SpiceDB writes,
audit emission, and outbox rows. They should not expose large "service" types
that accumulate unrelated methods.

`internal/authz` is the typed SpiceDB facade. It exposes product authorization
operations such as `CanListExecutions`, `CanReadAttemptLogs`,
`CanManageMembers`, `CheckBulkExecutionRows`, and `LookupReadableRepositories`.
It returns typed decisions and never returns a bare `bool` to vertical command
packages.

`internal/spicedb` is a substrate adapter. It converts generated model refs to
AuthZed protobuf requests, applies consistency policies, attaches request
metadata, records low-level metrics, and translates substrate errors. No other
package imports the AuthZed Go client.

`internal/store` is persistence plumbing. It should expose narrow repository
methods around sqlc queries rather than a catch-all store with every table on
one interface. A package that owns a table also owns the repository wrapper for
that table.

`internal/directory` is the Zitadel adapter. It exposes product-shaped
operations: list org members, invite member, set member role bindings, create
service account credential, remove service account credential, deactivate
service account, fetch user/org metadata. Raw Zitadel request construction stays
inside this package.

`internal/problems` maps tagged domain errors to stable public problem
responses. Domain packages return typed errors; they do not know HTTP status
codes or serialize problem documents.

## Dependency Rules

- `internal/spicedb` is the only package that imports AuthZed client packages.
- `internal/directory` is the only package that builds raw Zitadel requests.
- `internal/store` is the only package that imports sqlc-generated query
  packages.
- `internal/api` is the only package that imports Huma.
- `internal/problems` is the only package that serializes public problem
  responses.
- `internal/browser` is the only package that writes browser cookies.
- `internal/actions` is the only package that accepts Zitadel action webhook
  payloads.
- `internal/syncauth` is the only package that issues Electric shape
  capabilities.
- Vertical packages do not import each other. Cross-area behavior goes through
  small interfaces declared by the caller or through `internal/commands`
  envelopes.
- No package imports from `cmd/`.
- Generated clients are the only supported cross-service API surface.

These rules should be enforced with Bazel package visibility or a static import
check. A compile-time failure is preferable to a code review convention.

## Command Structure

Commands have one shape:

```go
type Command[I any, O any] struct {
    OperationID    OperationID
    IdempotencyKey IdempotencyKey
    Actor          Principal
    Origin         OriginSubject
    Input          I
}
```

Command execution follows one pipeline:

```text
boundary DTO
  -> command input normalization
  -> typed authorization decision
  -> product invariant checks
  -> PostgreSQL transaction and idempotency record
  -> SpiceDB write when the command changes authorization
  -> outbox/audit/capability state
  -> response DTO
```

Commands that change both product metadata and authorization relationships
record the command before external effects and finish by writing the resulting
ZedToken, relationship operation metadata, and audit state. Retry observes the
idempotency record and either returns the same result or a stable conflict.

Directory-side effects that cannot share a PostgreSQL transaction are modeled
explicitly:

- prepare local command and requested external operation;
- call directory adapter with an idempotency key when available;
- persist resulting directory identifiers and credential fingerprints;
- run compensating cleanup only for command-local failure before commit;
- reconcile durable drift after commit through a loud repair queue.

Silent best-effort cleanup is not a correctness mechanism.

## Boundary-Specific Organization

Browser auth is split into small files by concern:

```text
internal/browser/
  api.go
  config.go
  cookie.go
  csrf.go
  login.go
  callback.go
  session.go
  selected_org.go
  resource_token.go
  token_exchange.go
  token_verify.go
  userinfo.go
  store.go
  errors.go
```

OIDC token exchange, token verification, cookie serialization, session
persistence, userinfo loading, and resource-token issuance are separate units.
The browser package can call `internal/authz` for resource-token issuance; it
does not reach into SpiceDB or directory internals.

Public product APIs are split by resource:

```text
internal/api/
  api.go
  public_orgs.go
  public_members.go
  public_roles.go
  public_credentials.go
  public_browser.go
  public_actions.go
  internal_authz.go
  internal_resource_edges.go
  internal_syncauth.go
  middleware.go
  operation_policy.go
```

Each route file declares its Huma operations next to the policy metadata. A
handler should be small enough that the operation declaration, DTO parsing, and
command call fit on one screen.

Authorization vocabulary is split by schema area:

```text
internal/model/
  principal.go
  org.go
  role.go
  repository.go
  execution.go
  attempt.go
  project.go
  secret.go
  generated_schema.go
```

Generated files may be large. Hand-written files should stay narrow and should
not accumulate unrelated operations.

## Error Model

Domain errors are data:

```go
type Code string

const (
    CodePermissionDenied      Code = "permission_denied"
    CodeStaleCapability      Code = "stale_capability"
    CodeFailedPrecondition   Code = "failed_precondition"
    CodeIdempotencyConflict  Code = "idempotency_conflict"
    CodeDirectoryUnavailable Code = "directory_unavailable"
    CodeAuthzUnavailable     Code = "authz_unavailable"
)

type Error struct {
    Code      Code
    Operation OperationID
    Resource  ResourceRef
    Cause     error
}
```

Domain packages return `Error` values. `internal/problems` maps them to public
problem documents, redacts internal causes, and attaches trace-backed
instances. Stable codes also feed audit rows, ClickHouse queries, and UI
branching.

## Size And Review Constraints

Generated files are exempt. Hand-written source files should remain small:

- no multi-concern files;
- no route file with unrelated resources;
- no browser-auth file that mixes OIDC, cookies, sessions, and resource-token
  issuance;
- no command method that performs several product operations;
- no package-level `Service` that grows unrelated methods;
- no raw SQL outside sqlc query files;
- no raw tuple strings outside `internal/spicedb`;
- no raw Zitadel requests outside `internal/directory`;
- no hidden default consistency mode.

A hand-written file approaching 400 lines should be split by responsibility
before new behavior is added. The target is not an arbitrary line count; the
target is that package boundaries keep invalid control flow difficult to write.

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
ZedToken exists. `FullyConsistent` is reserved for narrow administrative paths
and break-glass inspection.

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
- operator inspection.

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
- reconciliation findings.

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
- whether Watch lag exceeded the sync revocation budget.

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
   ClickHouse evidence.
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

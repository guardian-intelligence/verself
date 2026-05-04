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
- Public IAM policy APIs follow Google Cloud IAM semantics:
  `getIamPolicy`, `setIamPolicy`, and `testIamPermissions`.
- Public HTTP routes are OpenAPI-native concrete resource routes. SDKs may
  expose Google-style resource-name helpers over those routes.
- Policy replacement requires optimistic concurrency through `etag` after
  bootstrap/default creation.
- Authorization checks return typed decision evidence, not bare booleans.
- Every security-sensitive check carries an explicit freshness policy.
- Product mutations that require authorization accept typed decision evidence.
- Relationship writes are idempotent by default and carry transaction metadata.
- Public mutations require an idempotency key. Internal mutations require a
  command request ID or idempotency key in the generated internal contract.
- List APIs use `page_size`, `page_token`, `next_page_token`, `filter`, and
  `order_by` rather than offset pagination.
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
- OAuth refresh tokens, device-code login, and PKCE remain Zitadel/OIDC
  concerns. `iam-service` validates access tokens and manages product
  authorization state; it does not become an OAuth authorization server.

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
definition service_account {}

definition role {
  relation member: user | service_account
}

definition org {
  relation owner: user
  relation admin: user
  relation member: user | service_account
  relation execution_lister: user | service_account | role#member

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
- `service_account`
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
src/services/iam-service/
  cmd/
    iam-service/
    iam-openapi/
    iam-internal-openapi/
    iam-schema-gen/

  openapi/

  client/
  internalclient/

  sdk/
    go/
    typescript/

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

  internal/policies/
    getIamPolicy, setIamPolicy, testIamPermissions, etag handling, and
    binding compilation

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
| Public IAM API OpenAPI 3.0/3.1 | `src/services/iam-service/openapi/openapi-3.0.yaml`, `src/services/iam-service/openapi/openapi-3.1.yaml` | `//src/services/iam-service/openapi` |
| SPIFFE-only internal IAM API OpenAPI 3.0/3.1 | `src/services/iam-service/openapi/internal-openapi-3.0.yaml`, `src/services/iam-service/openapi/internal-openapi-3.1.yaml` | `//src/services/iam-service/openapi` |
| Generated public Go client | `src/services/iam-service/client/client.gen.go` | `//src/services/iam-service/client:client` |
| Generated internal Go client | `src/services/iam-service/internalclient/client.gen.go` | `//src/services/iam-service/internalclient:internalclient` |
| Curated Go IAM SDK | `src/services/iam-service/sdk/go/` | `//src/services/iam-service/sdk/go:iam` |
| Browser TypeScript clients | `src/frontends/viteplus-monorepo/apps/verself-web/src/__generated/iam-api/` | frontend OpenAPI generation target |
| Curated TypeScript IAM SDK | `src/services/iam-service/sdk/typescript/` | TypeScript SDK generation target |
| SpiceDB schema | `src/services/iam-service/schema/verself.zed` | `//src/services/iam-service/schema:schema` |
| SpiceDB schema assertions | `src/services/iam-service/schema/assertions.yaml`, `src/services/iam-service/schema/expected-relations.yaml` | `//src/services/iam-service/schema:schema_tests` |
| Shared DTOs used by multiple services or frontend wrappers | `src/domain-transfer-objects/go/` | `//src/domain-transfer-objects/go:dto` |
| Future shared protobuf messages | `src/domain-transfer-objects/proto/<area>/v1/*.proto` | `//src/domain-transfer-objects/proto/<area>/v1:<area>_proto` |
| Future IAM-owned gRPC-only contract | `src/services/iam-service/proto/v1/*.proto` | `//src/services/iam-service/proto/v1:iam_proto` |

Add a service-local protobuf directory only if the operation cannot be cleanly
represented by the existing OpenAPI service pattern, for example a binary
stream that should not become a public HTTP contract. If the message shape is
consumed by more than `iam-service`, put the protobuf under
`src/domain-transfer-objects/proto/` instead.

OpenAPI remains the generated-client surface for product services. A missing
service shape is fixed by adding the Huma route and regenerating the committed
OpenAPI specs and clients, not by hand-writing HTTP or gRPC calls.

## API Layering

The product surface is organized in four layers:

1. `iam-service` exposes HTTP JSON APIs generated from Huma/OpenAPI. Internal
   product-service operations use the SPIFFE-only internal OpenAPI surface.
2. Language SDKs wrap generated clients. SDKs own retries, idempotency key
   generation, auto-pagination, resource-name helpers, error normalization,
   request tracing headers, and DTO conversion.
3. The console uses TanStack server functions as web adapters. Server
   functions call the SDK and own cookie/session/CSRF concerns.
4. The CLI calls the SDK directly. Server functions are not a CLI transport
   contract.

The SDK should make the public API feel Google-IAM-like even when the HTTP
routes are OpenAPI-native:

```go
policy, err := client.IAM.GetIAMPolicy(ctx, "organizations/org_123")
updated, err := client.IAM.SetIAMPolicy(ctx, "organizations/org_123", policy, iam.WithETag(policy.ETag))
allowed, err := client.IAM.TestIAMPermissions(ctx, "executions/exec_456", []string{"executions.read", "executions.logs.read"})
```

```text
verself iam policies get organizations/org_123
verself iam policies add-binding organizations/org_123 --role roles/execution.viewer --member user:usr_123
verself iam test-permissions executions/exec_456 executions.read executions.logs.read
```

CLI convenience commands such as `add-binding` are read-modify-write helpers
over `getIamPolicy` and `setIamPolicy`. The service API stays small.

## Huma OpenAPI Shape

The public IAM API copies Google Cloud IAM semantics:

- `getIamPolicy`: read the policy attached to a resource.
- `setIamPolicy`: replace the policy attached to a resource using `etag`
  concurrency.
- `testIamPermissions`: return the subset of requested permissions the caller
  currently has on a resource.

Google's REST docs often use gRPC-transcoding paths such as
`/{resource=projects/*}:setIamPolicy`. OpenAPI path parameters do not model
slash-containing resource names cleanly, and Huma routes should remain
ordinary OpenAPI routes. `iam-service` therefore registers concrete routes for
each policy-bearing resource type and lets SDK helpers translate resource
names to those routes.

Initial public policy routes:

```text
GET  /api/v1/organizations/{org_id}/iamPolicy
PUT  /api/v1/organizations/{org_id}/iamPolicy
POST /api/v1/organizations/{org_id}/iamPolicy:testPermissions

GET  /api/v1/projects/{project_id}/iamPolicy
PUT  /api/v1/projects/{project_id}/iamPolicy
POST /api/v1/projects/{project_id}/iamPolicy:testPermissions

GET  /api/v1/repositories/{repository_id}/iamPolicy
PUT  /api/v1/repositories/{repository_id}/iamPolicy
POST /api/v1/repositories/{repository_id}/iamPolicy:testPermissions

GET  /api/v1/secrets/{secret_id}/iamPolicy
PUT  /api/v1/secrets/{secret_id}/iamPolicy
POST /api/v1/secrets/{secret_id}/iamPolicy:testPermissions
```

Only resource types that support directly attached policy receive these routes.
Inherited leaf resources such as execution attempts may support
`testIamPermissions` without supporting `setIamPolicy`.

Huma operation declarations should stay next to policy metadata:

```go
registerIAMRoute(api, huma.Operation{
    OperationID:   "set-organization-iam-policy",
    Method:        http.MethodPut,
    Path:          "/api/v1/organizations/{org_id}/iamPolicy",
    Summary:       "Set organization IAM policy",
    DefaultStatus: http.StatusOK,
}, operationPolicy{
    Permission:         permissionIAMPolicySet,
    Resource:           "organization_iam_policy",
    Action:             "set",
    OrgScope:           "path_org_id",
    RateLimitClass:     "iam_policy_mutation",
    Idempotency:        idempotencyHeaderKey,
    AuditEvent:         "iam.policy.set",
    OperationType:      "write",
    RiskLevel:          "high",
    DataClassification: "authorization_policy",
    BodyLimitBytes:     bodyLimitSmallJSON,
}, setOrganizationIAMPolicy(svc))
```

Core wire DTOs:

```go
type Policy struct {
    Version  int32     `json:"version"`
    ETag     string    `json:"etag"`
    Bindings []Binding `json:"bindings"`
}

type Binding struct {
    Role    string   `json:"role"`
    Members []string `json:"members"`
}

type SetIAMPolicyRequest struct {
    Policy     Policy `json:"policy"`
    UpdateMask string `json:"update_mask,omitempty"`
}

type TestIAMPermissionsRequest struct {
    Permissions []string `json:"permissions" minItems:"1" maxItems:"100"`
}

type TestIAMPermissionsResponse struct {
    Permissions []string `json:"permissions"`
}
```

Policy member strings use a small fixed vocabulary:

```text
user:{zitadel_subject}
serviceAccount:{service_account_id}
```

Role names use resource-name-like strings:

```text
roles/owner
roles/admin
roles/member
roles/execution.viewer
organizations/{org_id}/roles/{role_id}
```

`setIamPolicy` compiles policy bindings into typed SpiceDB relationship writes.
It does not store or evaluate a customer-editable policy language in the first
cut. Conditions and caveats can be added later as a deliberate schema/API
extension.

Standard list APIs use the Google AIP pagination vocabulary:

```go
type ListRolesInput struct {
    Parent    string `query:"parent,omitempty" maxLength:"256"`
    PageSize  int    `query:"page_size,omitempty" minimum:"1" maximum:"100"`
    PageToken string `query:"page_token,omitempty" maxLength:"1024"`
    Filter    string `query:"filter,omitempty" maxLength:"2048"`
    OrderBy   string `query:"order_by,omitempty" maxLength:"256"`
}

type ListRolesResponse struct {
    Roles         []Role `json:"roles"`
    NextPageToken string `json:"next_page_token,omitempty"`
}
```

Page tokens are opaque, URL-safe, and authorization-neutral. The service
reauthorizes every request that includes a page token.

All public operations accept `X-Request-ID` for trace correlation. Mutating
operations declare `Idempotency-Key` in OpenAPI and reject requests that omit
it.

HTTP error responses use RFC 9457 Problem Details with stable Verself problem
types and trace-backed `instance` values. The SDK maps those problem documents
to language-native typed errors.

Authentication flows remain standard OIDC/OAuth:

- console: authorization code with PKCE and server-side sessions;
- CLI: device authorization flow or authorization code with PKCE;
- SDK workloads: bearer access token or customer API credential;
- repo-owned service calls: SPIFFE mTLS on internal clients.

Refresh tokens are issued and refreshed by Zitadel's token endpoint. They are
never sent to product resource APIs and are not represented in IAM policy DTOs.

## Bazel Targets

The service should create these targets:

```text
//src/services/iam-service:go_default_library

//src/services/iam-service/cmd/iam-service:iam-service
//src/services/iam-service/cmd/iam-service:iam-service_nomad_artifact
//src/services/iam-service/cmd/iam-openapi:iam-openapi
//src/services/iam-service/cmd/iam-internal-openapi:iam-internal-openapi
//src/services/iam-service/cmd/iam-schema-gen:iam-schema-gen

//src/services/iam-service/openapi:openapi-3.0.yaml
//src/services/iam-service/openapi:openapi-3.1.yaml
//src/services/iam-service/openapi:internal-openapi-3.0.yaml
//src/services/iam-service/openapi:internal-openapi-3.1.yaml

//src/services/iam-service/client:client
//src/services/iam-service/internalclient:internalclient
//src/services/iam-service/sdk/go:iam
//src/services/iam-service/sdk/typescript:iam
//src/services/iam-service/migrations:migrations
//src/services/iam-service/schema:schema
//src/services/iam-service/schema:schema_tests

//src/services/iam-service/internal/api:api
//src/services/iam-service/internal/bootstrap:bootstrap
//src/services/iam-service/internal/model:model
//src/services/iam-service/internal/spicedb:spicedb
//src/services/iam-service/internal/authz:authz
//src/services/iam-service/internal/decision:decision
//src/services/iam-service/internal/orgs:orgs
//src/services/iam-service/internal/members:members
//src/services/iam-service/internal/roles:roles
//src/services/iam-service/internal/policies:policies
//src/services/iam-service/internal/credentials:credentials
//src/services/iam-service/internal/browser:browser
//src/services/iam-service/internal/actions:actions
//src/services/iam-service/internal/syncauth:syncauth
//src/services/iam-service/internal/resourceedges:resourceedges
//src/services/iam-service/internal/commands:commands
//src/services/iam-service/internal/watch:watch
//src/services/iam-service/internal/reconcile:reconcile
//src/services/iam-service/internal/audit:audit
//src/services/iam-service/internal/problems:problems
//src/services/iam-service/internal/directory:directory
//src/services/iam-service/internal/store:store
```

If a future service-local protobuf is added, use the existing repo pattern:

```text
//src/services/iam-service/proto/v1:iam_proto
//src/services/iam-service/proto/v1:iam_go_proto
//src/services/iam-service/proto/v1:proto
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
| IAM policy get/set/test | `internal/policies` plus `internal/authz` | Google-IAM-style resource policy operations over Huma/OpenAPI. `setIamPolicy` requires `etag` and idempotency after bootstrap. |
| Permission and role catalog | `internal/roles` plus `internal/policies` | Exposes predefined roles, org-scoped roles, and permission metadata for SDKs, CLI, and console affordances. |
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
  -> internal/{orgs,members,roles,policies,credentials,browser,actions,syncauth,resourceedges}
  -> internal/problems

internal/{orgs,members,roles,policies,credentials,browser,actions,syncauth,resourceedges}
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

`internal/policies` owns public IAM policy semantics: policy read assembly,
`etag` verification, role/member binding validation, `setIamPolicy` compilation
to SpiceDB relationship operations, and `testIamPermissions` response shaping.
It does not import AuthZed clients directly.

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
- `internal/policies` is the only package that accepts public IAM policy
  documents and compiles them into typed authorization commands.
- Vertical packages do not import each other. Cross-area behavior goes through
  small interfaces declared by the caller or through `internal/commands`
  envelopes.
- No package imports from `cmd/`.
- Generated clients are the only supported cross-service API surface. Curated
  SDKs wrap generated clients; they do not bypass them.

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
  public_policies.go
  public_permissions.go
  public_service_accounts.go
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
customer credentials. The public surface is intentionally conventional:

| Resource | Operations |
| --- | --- |
| IAM policy | `getIamPolicy`, `setIamPolicy`, `testIamPermissions` on supported resources. |
| Roles | list predefined roles, list org roles, create org role, update org role, delete org role. |
| Members | list members, invite member, update member role bindings, remove member. |
| Service accounts | list, create, update display metadata, disable, delete. |
| API credentials | list, read metadata, create, roll, revoke. |
| Permission catalog | list permissions, inspect role-to-permission expansion, list effective permissions. |
| Authorization history | audit-backed inspection by resource, actor, role, credential, and command ID. |

Initial policy-bearing resource paths are concrete Huma routes:

```text
/api/v1/organizations/{org_id}/iamPolicy
/api/v1/projects/{project_id}/iamPolicy
/api/v1/repositories/{repository_id}/iamPolicy
/api/v1/secrets/{secret_id}/iamPolicy
```

The SDK accepts resource names such as `organizations/org_123` and dispatches
to the correct generated client method. This keeps OpenAPI generation
straightforward without forcing callers to learn every route variant.

Internal APIs are SPIFFE-only and serve product services:

- check permission;
- bulk check permissions;
- lookup resources;
- lookup subjects;
- write resource edges;
- issue sync capabilities;
- read sync scope state;
- publish command/audit metadata.

Internal APIs carry origin subject fields explicitly. They do not accept
browser cookies, console session IDs, or customer refresh tokens. Product
services call generated `internalclient` packages with SPIFFE mTLS and typed
origin context.

Public route declarations include IAM metadata, audit metadata, idempotency
metadata, body limits, rate-limit class, and response problem types in the Huma
operation definition. Internal routes authorize exact SPIFFE peer IDs and carry
their own operation policies.

## PostgreSQL State

`iam-service` PostgreSQL stores service-owned state around SpiceDB:

- command idempotency records;
- outbox rows;
- role display metadata;
- service account metadata;
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
- Google IAM policy API:
  <https://cloud.google.com/iam/docs/reference/rest/>.
- Google API pagination:
  <https://google.aip.dev/158>.
- Google API request IDs:
  <https://google.aip.dev/155>.
- Google API errors:
  <https://google.aip.dev/193>.
- RFC 9457 Problem Details:
  <https://www.rfc-editor.org/rfc/rfc9457>.
- OAuth 2.0 refresh tokens:
  <https://www.rfc-editor.org/rfc/rfc6749>.
- OAuth 2.0 Device Authorization Grant:
  <https://www.rfc-editor.org/rfc/rfc8628>.
- SCIM 2.0 protocol:
  <https://www.rfc-editor.org/rfc/rfc7644>.

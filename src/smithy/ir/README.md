# Contract IR

The Verself contract IR is the generated, normalized representation between
Smithy models and implementation-specific generators. Smithy remains the
authored contract. The IR is a deterministic build artifact that lets downstream
generators consume one simple product-shaped model without reimplementing
Smithy resource closure, mixin flattening, trait resolution, projection
membership, or HTTP binding rules.

```text
Smithy source
  -> Smithy validation and projection
  -> Verself contract IR
  -> Huma route/type generation
  -> OpenAPI compatibility generation
  -> SDK transport generation
  -> conformance fixture generation
  -> audit, IAM, and observability catalogs
```

The IR is intentionally boring JSON. It should be stable enough for Go, TypeScript,
OpenAPI, conformance, and operator tooling to consume, while remaining
generated from Smithy on every build.

## Responsibilities

The Smithy-side IR generator owns semantic interpretation:

- service and projection selection;
- resource and operation closure;
- mixin and member resolution;
- HTTP method, path, label, query, header, payload, and response-code bindings;
- required, optional, nullable, sensitive, and not-resource-property metadata;
- Verself operation traits: identity, authorization, audit, rate limiting,
  request budget, SDK behavior, conformance families, and future protobuf field
  numbers;
- error sets and RFC 9457 problem metadata;
- source locations for diagnostics and generated-code comments.

Downstream language generators own language-specific output:

- Go package names, imports, struct tags, Huma registrations, and handler
  interfaces;
- TypeScript module names, Valibot schemas, transport functions, and SDK
  wrapper inputs;
- OpenAPI schema layout and compatibility details;
- conformance test file layout and test-runner adapters.

Downstream generators should not parse Smithy traits directly. They consume the
IR and fail loudly when the IR lacks a field they need.

## Generated Artifact

Each projection produces one IR document:

```text
src/smithy/ir/generated/<package>/<projection>.json
```

The Bazel output may stay under `bazel-bin` until checked-in generated artifacts
are needed by non-Bazel tooling. Checked-in IR files are generated artifacts and
must not be edited by hand.

The top-level shape is:

```json
{
  "irVersion": "verself.contract-ir.v1",
  "package": "verself",
  "projection": "iam-public",
  "service": {},
  "shapes": {},
  "operations": [],
  "resources": [],
  "problems": [],
  "source": {}
}
```

`irVersion` is the schema version for the generated IR. It changes only when a
consumer must update its parser. Product API versions remain modeled in Smithy
and projected into service and SDK artifacts.

## Service

`service` describes the projected service boundary:

```json
{
  "shapeId": "verself.iam.v1#Iam",
  "name": "Iam",
  "version": "2026-05-12",
  "runtime": {
    "serviceName": "iam-service",
    "publicAudience": "iam-service",
    "internalAudience": "spiffe://..."
  },
  "authSchemes": ["httpBearerAuth"],
  "visibility": "public"
}
```

`visibility` is a Verself projection label such as `public`, `internal`, or
`browser-auth`. A service can emit several IR documents from the same Smithy
source when public and repo-owned internal surfaces differ.

## Shapes

`shapes` is a map keyed by Smithy shape ID. Every shape is flattened enough that
a generator can emit code without recursively interpreting Smithy semantics.

```json
{
  "verself.iam.v1#OrganizationSummary": {
    "kind": "structure",
    "name": "OrganizationSummary",
    "members": [
      {
        "name": "orgId",
        "target": "verself.iam.v1#OrgId",
        "jsonName": "orgId",
        "required": true,
        "httpBinding": {"location": "document"},
        "resourceIdentifier": "orgId",
        "protoField": {"number": 1}
      }
    ],
    "traits": {
      "resource": "verself.iam.v1#Organization"
    }
  }
}
```

Scalar shapes carry normalized constraints:

```json
{
  "kind": "string",
  "name": "OrgId",
  "constraints": {
    "pattern": "^org_[0-9A-HJKMNP-TV-Z]{26}$",
    "minLength": 1,
    "maxLength": 80
  },
  "sensitive": false
}
```

The IR preserves Smithy names and the effective wire names separately. That lets
the Go generator choose idiomatic exported identifiers while the SDK and
OpenAPI generators preserve the wire contract exactly.

## Operations

`operations` is the primary interface for server, client, SDK, and conformance
generators.

```json
{
  "shapeId": "verself.iam.v1#UpdateOrganization",
  "name": "UpdateOrganization",
  "operationId": "update-organization",
  "readonly": false,
  "idempotent": true,
  "http": {
    "method": "PATCH",
    "path": "/api/v1/orgs/{orgId}",
    "successStatus": 200
  },
  "input": "verself.iam.v1#UpdateOrganizationInput",
  "output": "verself.iam.v1#UpdateOrganizationOutput",
  "errors": [
    "verself.common.v1#ValidationFailedError",
    "verself.common.v1#PermissionDeniedError"
  ],
  "bindings": {
    "labels": [{"member": "orgId", "name": "orgId"}],
    "headers": [{"member": "idempotencyKey", "name": "Idempotency-Key"}],
    "queries": [],
    "documentMembers": ["slug", "displayName"]
  },
  "verself": {
    "identity": {
      "mode": "bearer",
      "audience": "iam-service",
      "principals": ["browser", "cli"]
    },
    "authz": {
      "permission": "iam:organization:update",
      "organization": {"source": "input_member", "member": "orgId"}
    },
    "audit": {
      "event": "iam.organization.update",
      "resource": "verself.iam.v1#Organization",
      "action": "update"
    },
    "rateLimit": {"bucket": "iam_mutation"},
    "requestBudget": {"maxBytes": 16384},
    "sdk": {
      "module": "orgs",
      "method": "update",
      "paginated": false,
      "retryable": false
    },
    "conformance": [
      "http_serialization",
      "response_parsing",
      "problem_parsing",
      "idempotency",
      "auth",
      "wrong_org",
      "trace_context"
    ]
  }
}
```

The operation record is deliberately redundant. For example, `idempotent`,
header bindings, and Verself idempotency metadata all appear because different
generators need different views of the same contract and validators can assert
that they agree.

## Resources

`resources` preserves product resource structure after Smithy closure is
resolved:

```json
{
  "shapeId": "verself.iam.v1#Organization",
  "name": "Organization",
  "identifiers": [
    {"name": "orgId", "target": "verself.iam.v1#OrgId"}
  ],
  "properties": [
    {"name": "resourceName", "target": "verself.iam.v1#OrganizationResourceName"},
    {"name": "slug", "target": "verself.iam.v1#OrgSlug"},
    {"name": "displayName", "target": "verself.common.v1#DisplayName"}
  ],
  "operations": {
    "list": "verself.iam.v1#ListOrganizations",
    "read": "verself.iam.v1#GetOrganization",
    "update": "verself.iam.v1#UpdateOrganization"
  },
  "containedResources": ["verself.iam.v1#Member"]
}
```

This is useful for SDK resource modules, CLI resource selectors, generated docs,
and IAM resource-name helpers.

## Problems

`problems` normalizes modeled errors:

```json
{
  "shapeId": "verself.common.v1#PermissionDeniedError",
  "name": "PermissionDeniedError",
  "status": 403,
  "errorKind": "client",
  "type": "urn:verself:problem:auth:permission_denied",
  "code": "auth.permission_denied",
  "members": ["type", "title", "status", "detail", "instance", "code", "requestId", "traceparent"]
}
```

SDK generators use this to produce typed error hierarchies. Server generators
use it to register stable problem mappings and conformance fixtures.

## Huma Generation

The Huma generator should emit three layers.

First, generated type definitions:

```go
type UpdateOrganizationInput struct {
    OrgID string `path:"orgId" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
    IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
    Body UpdateOrganizationBody
}

type UpdateOrganizationBody struct {
    Slug *string `json:"slug,omitempty" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
    DisplayName *string `json:"displayName,omitempty" maxLength:"120"`
}
```

Second, generated handler interfaces:

```go
type PublicHandlers interface {
    ListOrganizations(context.Context, *ListOrganizationsInput) (*ListOrganizationsOutput, error)
    GetOrganization(context.Context, *GetOrganizationInput) (*GetOrganizationOutput, error)
    UpdateOrganization(context.Context, *UpdateOrganizationInput) (*UpdateOrganizationOutput, error)
}
```

Third, generated registration:

```go
func RegisterPublic(api huma.API, runtime Runtime, handlers PublicHandlers) {
    runtime.Register(api, OperationDescriptor{
        Operation: huma.Operation{
            OperationID: "update-organization",
            Method: http.MethodPatch,
            Path: "/api/v1/orgs/{orgId}",
            DefaultStatus: http.StatusOK,
            MaxBodyBytes: 16384,
        },
        Policy: runtimeiam.OperationPolicy{
            Permission: runtimeiam.Permission("iam:organization:update"),
            Resource: runtimeiam.ResourceKind("organization"),
            Action: runtimeiam.ActionUpdate,
            OrgScope: runtimeiam.OrgScopePathOrgID,
            RateLimitClass: runtimeiam.RateLimitClass("iam_mutation"),
            Idempotency: runtimeiam.IdempotencyHeaderKey,
            AuditEvent: runtimeiam.AuditEvent("iam.organization.update"),
            BodyLimitBytes: 16384,
        },
    }, handlers.UpdateOrganization)
}
```

`Runtime` is the small handwritten adapter for one service family. It wires
common auth, IAM checks, rate limiting, audit, body budgets, and Huma
registration. Generated code supplies descriptors and typed inputs. Handwritten
service code supplies domain behavior.

The generated package should avoid importing service domain packages. It can
import shared runtime packages such as `service-runtime/iam`, `huma/v2`, and
shared DTO primitives when needed.

## Drift Gates

The first migration gate is a generated Huma registration package plus a test
that asserts the live Huma/OpenAPI catalog matches the Smithy-derived IR:

- operation ID;
- HTTP method and path;
- security requirement;
- idempotency header and requiredness;
- body budget;
- request/response type name;
- stable error set;
- `runtimeiam.OperationPolicy`;
- `x-verself-contract` extension.

For `iam-service`, the service-local Bazel build runs the Smithy generator to
produce `internal/contractapi`. That package owns typed inputs and outputs,
operation descriptors, OpenAPI extension metadata, and handler registration.
The handwritten adapter implements the generated handler interface and performs
runtime auth, rate limiting, idempotency, audit, and domain calls from the
generated descriptors.

## Security Properties

The IR should make security-relevant omissions unrepresentable for generated
services:

- public operations declare bearer auth and audience;
- internal operations declare SPIFFE mTLS and allowed peer classes;
- mutating operations declare a body budget and idempotency policy;
- authorization scope derivation is a closed enum;
- Zanzibar permission, audit event, resource kind, and action are required;
- stable problem shapes are modeled and generated into SDK error handling;
- sensitive members are marked before SDK, logs, fixtures, or docs consume
  them;
- conformance cases exist for auth failure, wrong organization, idempotency,
  problem parsing, and trace context.

The Smithy validator should reject missing metadata. The IR generator should
refuse to produce partial records. The Huma generator should refuse records it
does not understand.

## Versioning

IR schema versioning and product API versioning are separate.

`irVersion` changes when generator consumers need a parser change. Product API
versions are modeled through Smithy changes, projections, and generated SDK
defaults. A service can serve several product API versions while the IR schema
stays fixed.

When `irVersion` changes, all generators in the repository should cut over in
the same change. The IR is repo-internal tooling, so compatibility shims are not
needed.

## Initial IAM Cutover

The first `iam-service` slice should produce:

```text
iam-public.json
iam-internal.json
iam-browser-auth.json
```

`iam-public.json` covers customer and SDK operations. `iam-internal.json` covers
SPIFFE-only service-to-service operations. `iam-browser-auth.json` can either
drive documentation and conformance for browser auth routes or remain a
separate handwritten HTTP contract until those routes are ready for generated
Huma bindings.

The recommended sequence is:

1. Complete IAM Smithy coverage for the current public and internal surfaces.
2. Generate IR and add drift tests against existing Huma registrations.
3. Generate Huma operation descriptors and keep handwritten handlers.
4. Generate input/output types and update handlers to use those types.
5. Generate OpenAPI compatibility projections from the IR or directly from
   Smithy using the same projection selection.
6. Regenerate service clients, SDK transports, and conformance fixtures from
   the same projection.

## References

- Smithy code generation plugins are Smithy-Build plugins discovered through
  Java SPI and configured from `smithy-build.json`.
- Smithy HTTP binding traits define the canonical mapping of input and output
  members to labels, headers, queries, payloads, and response codes.
- Huma registers operations from `huma.Operation` plus typed Go input/output
  structs, and derives request validation and OpenAPI from those structs.

# src/contracts

`src/contracts` owns the canonical API contract model for product and internal
service surfaces. Smithy models are the source of truth for resource shapes,
operation semantics, IAM metadata, audit metadata, SDK behavior, generated
projections, and conformance cases. OpenAPI artifacts are generated
interoperability projections, not semantic authority.

## Ownership

- Put customer-facing and repo-owned HTTP/JSON service contracts under
  `models/`.
- Put shared conformance fixtures under `conformance/` once generated cases
  exist.
- Put generated OpenAPI compatibility artifacts under `openapi/` only after the
  Smithy-to-OpenAPI projection is wired.
- Put Connect/protobuf contracts under `proto/` only for RPC, streaming, or
  binary transport surfaces where protobuf is the primary protocol.
- Do not make product services import curated SDKs. Services consume generated
  transport clients or generated handler bindings from this contract model.

## Modeling Rules

- Every operation must declare Verself operation metadata: auth mode, audience,
  Zanzibar permission, resource kind, action, organization scope, rate-limit
  class, idempotency policy, audit event, request body budget, and stable error
  set.
- Public operations must be resource-oriented and SDK-shaped. Service ownership
  may appear in namespaces and deployment metadata, not in customer method
  names.
- Resource DTOs return immutable `id`, immutable `resourceName`, optional
  `slug`, and `displayName` when the resource is durable and customer-facing.
- List operations use cursor pagination. Mutating operations declare their
  idempotency policy explicitly.
- Auth contracts must encode OAuth/OIDC security expectations, including
  audience binding and the browser/CLI/workload distinction.

## Verification

- Validate the Smithy package with
  `bazelisk build //src/contracts/models/verself:smithy_validate`.
- Build Smithy projection artifacts with
  `bazelisk build //src/contracts/models/verself:smithy_build`.
- Run generated conformance suites for every SDK implementation before treating
  an SDK as supported.
- Prove deployed behavior through ClickHouse traces/logs for behavior-affecting
  contract changes.

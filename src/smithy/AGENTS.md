# src/smithy

`src/smithy` owns the canonical API contract model for product and internal
service surfaces. Smithy models are the source of truth for resource shapes,
operation semantics, IAM metadata, audit metadata, SDK behavior, generated
projections, and catalog metadata. OpenAPI artifacts are generated
interoperability projections, not semantic authority.

## Ownership

- Put customer-facing and repo-owned HTTP/JSON service contracts under
  `models/`.
- Put official Smithy OpenAPI compatibility artifacts under `openapi/` only
  when they need to be materialized for docs or packaging.
- Put Connect/protobuf contracts under `proto/` only for RPC, streaming, or
  binary transport surfaces where protobuf is the primary protocol.
- Do not make product services import curated SDKs. Repo-owned service calls use
  service-local typed clients/adapters with caller-owned SPIFFE mTLS transports;
  public SDKs live under `src/sdks/` and frontend packages.
- Downstream tooling should consume Smithy directly or the official OpenAPI
  projection rather than inventing another semantic contract format.

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
  `bazelisk test //src/smithy/models/verself:smithy_validate_test`.
- Build Smithy projection artifacts with
  `bazelisk build //src/smithy/models/verself:smithy_build`.
- Prove deployed behavior through ClickHouse traces/logs for behavior-affecting
  contract changes.

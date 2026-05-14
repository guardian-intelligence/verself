# SDK API Surface

The curated SDK is the public programming contract for Verself. The CLI,
browser server routes, customer automation, agent workflows, docs examples, and
future provider-style integrations use the SDK. Product APIs are modeled in
Smithy under `src/smithy`, and services implement the Smithy-modeled HTTP
surface. Public OpenAPI remains a generated compatibility projection for docs,
TypeScript transports, and ecosystem tooling.

The layering is:

```text
facade: CLI, browser server route, customer automation, agent
  -> curated SDK resource method
  -> SDK-owned transport core from official OpenAPI projection
  -> public service API
  -> service-owned state and integrations
```

Product services expose public and internal contract projections. Product
services do not import curated SDK packages. Service-owned Go clients remain
service-facing transport clients. SDK packages generate their own transport
cores from public OpenAPI projections and wrap them with customer-facing
resource modules. OpenAPI projections are downstream artifacts, not the API
source of truth.

Missing SDK coverage blocks public CLI commands and docs examples. If the SDK
method would be awkward, the public API shape should be revisited before the
CLI or docs grow around the awkwardness.

## Public Grammar

The SDK groups operations by product resource. Backing service ownership is an
implementation detail.

```text
verself.orgs
verself.members
verself.credentials
verself.credentials.trust
verself.projects
verself.repositories
verself.runs
verself.logs
verself.schedules
verself.secrets
verself.notifications
verself.audit
verself.billing
```

Service-shaped names such as IAM, Source, Sandbox Rental, Governance, and
Notifications may remain internal implementation names. Public examples and CLI
commands should use product nouns.

The identity model exposed by the SDK is:

| Public term | Meaning |
| --- | --- |
| Machine principal | Durable non-human IAM subject that receives permissions and appears in audit decisions. |
| Credential | One authentication method for a machine principal. Credentials can be rotated or revoked without changing the principal audit identity. |
| Workload trust | Secretless binding from an external runtime assertion into a bounded machine principal. |
| Access token | Short-lived token sent to public service APIs after credential or workload exchange. |

The internal IAM object may be named `service_account`. That name should not be
required in the common customer workflow. Customers create credentials and
workload trust bindings; the backing machine principal is created or reused by
the platform.

## Resource Identity

Every durable customer-facing resource has four identity surfaces:

| Surface | Stability | Uniqueness | Purpose |
| --- | --- | --- | --- |
| `id` | Immutable | Unique within one installation and resource type | Storage keys, joins, path parameters, and service internals. |
| `resourceName` | Immutable | Globally unique across Verself installations | IAM policies, API activities, cross-resource references, imports, exports, billing evidence, and support diagnostics. |
| `slug` | Mutable with redirect history | Unique within one parent and resource type while active | Human CLI input and readable URLs. |
| `displayName` | Mutable | Duplicates allowed | UI labels, invoices, notifications, and prose. |

Resource names use RFC 8141 URN syntax. The target namespace identifier is
`verself`; registering that namespace with IANA is required before the API is
declared stable.

```text
urn:verself:<installation-id>:<collection>/<resource-id>[/<collection>/<resource-id>...]
```

Examples:

```text
urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT
urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT/projects/proj_01J8QK22N3F7W8A9K4Z6Q0B1CD
urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/org_01J8QK0M2A7W4H3P9FQ6G1R8ZT/credentials/cred_01J8QK4M5N6P7Q8R9S0T1V2W3X
```

The namespace-specific string is `<installation-id>:<resource-path>`.
Resource paths are slash-delimited and alternate stable collection identifiers
with immutable resource IDs. Collection identifiers are product nouns, not
service names. They use plural lower-camel ASCII identifiers such as `orgs`,
`projects`, `workloadTrusts`, and `transitKeys`.

Resource paths include parent IDs only when containment is part of the durable
identity. A resource that can move between projects or repositories must use an
org-scoped path and expose the current parent as ordinary DTO state. Resource
names never contain slugs, display names, domains, regions, API versions, or
service owners.

Initial resource name formats:

| Resource | Format |
| --- | --- |
| Organization | `urn:verself:<installation-id>:orgs/<org-id>` |
| Human member | `urn:verself:<installation-id>:orgs/<org-id>/members/<member-id>` |
| Machine principal | `urn:verself:<installation-id>:orgs/<org-id>/machinePrincipals/<principal-id>` |
| Credential | `urn:verself:<installation-id>:orgs/<org-id>/credentials/<credential-id>` |
| Workload trust | `urn:verself:<installation-id>:orgs/<org-id>/workloadTrusts/<trust-id>` |
| Project | `urn:verself:<installation-id>:orgs/<org-id>/projects/<project-id>` |
| Environment | `urn:verself:<installation-id>:orgs/<org-id>/projects/<project-id>/environments/<environment-id>` |
| Repository | `urn:verself:<installation-id>:orgs/<org-id>/projects/<project-id>/repositories/<repository-id>` |
| Run | `urn:verself:<installation-id>:orgs/<org-id>/runs/<run-id>` |
| Run attempt | `urn:verself:<installation-id>:orgs/<org-id>/runs/<run-id>/attempts/<attempt-id>` |
| Schedule | `urn:verself:<installation-id>:orgs/<org-id>/schedules/<schedule-id>` |
| Secret | `urn:verself:<installation-id>:orgs/<org-id>/secrets/<secret-id>` |
| Secret version | `urn:verself:<installation-id>:orgs/<org-id>/secrets/<secret-id>/versions/<version-id>` |
| Transit key | `urn:verself:<installation-id>:orgs/<org-id>/transitKeys/<key-id>` |
| Data export | `urn:verself:<installation-id>:orgs/<org-id>/dataExports/<export-id>` |
| Invoice | `urn:verself:<installation-id>:orgs/<org-id>/invoices/<invoice-id>` |

The `installation-id` is generated once during first bootstrap and persisted as
installation state. It is independent of the installation domain so domain
renames and disaster recovery do not change resource identity. A disk-level
clone used as a forked installation must re-key the installation before it
emits public API traffic or exported evidence; a disaster-recovery restore of
the same installation keeps the original installation ID.

Resource IDs are opaque strings. New public resource families should use
type-prefixed UUIDv7-compatible values or an equivalent 128-bit identifier with
monotonic database locality and enough random bits for distributed generation.
Existing UUID resources may keep their current IDs until the owning service is
cut over. IDs never encode organization slug, domain, service owner, region, or
display name.

Resource names are server-generated, returned on every durable resource DTO, and
accepted by APIs that take cross-resource references. Parent-scoped APIs may
still use short IDs or slugs in path parameters when that improves CLI
ergonomics. SDKs normalize resource references into typed `ResourceRef` values
that can carry a resource name, typed ID, or parent-scoped slug before calling
the SDK transport core.

Resource names are not primary database keys. Hot OLTP tables use typed
immutable IDs. High-volume event tables store installation ID, resource type,
and resource ID, and add the full resource name only where the row must be
self-contained outside the originating database. A typical project resource
name is about 120 bytes. Storing that value once per million rows costs roughly
120 MB before database compression and indexes; storing compact 128-bit IDs for
hot joins costs 16 MB per million rows.

SDK request and response placement:

| Location | Rule |
| --- | --- |
| Durable resource response | Return `id`, `resourceName`, optional `slug`, and `displayName`. |
| Get, update, archive, delete, rotate, revoke | SDK methods accept a `ResourceSelector` for the target. Generated service routes may still use typed path IDs. |
| Create and list under one parent | SDK methods accept an explicit parent selector such as `org` or `project`; public API DTOs use typed parent IDs unless several parent types are possible. |
| Create and list under several possible parent types | Use `parentResourceName` so the request is unambiguous and extensible. |
| Field referencing another durable resource | Use `<noun>ResourceName` when the field can cross parents, cross resource types, appear in policy, appear in audit, or be exported. |
| IAM policy target | Use `resourceName` for the protected resource. Policy storage resolves it to typed references before evaluation. |
| Audit, billing evidence, imports, exports, and support bundles | Include full resource names because the row or file must remain meaningful outside the originating database. |
| Access tokens and hot service-to-service calls | Use compact typed IDs in claims and transport internals unless an interoperable third-party contract requires the URN. |

The public field name is `resourceName`, not `urn`. URN is the syntax; resource
name is the API concept. Google AIP-122 uses `name` for this concept; Verself
uses `resourceName` in JSON and SDK DTOs so it is never confused with slugs,
display names, profile names, or provider names. Smithy shapes should document
and validate the expected format for each field that carries a resource name;
generated OpenAPI projections carry that constraint forward.

Slug uniqueness is enforced by resource type and parent:

```text
(parent_type, parent_id, resource_type, normalized_slug) unique where active = true
```

Slug comparison is lowercase ASCII and normalization happens before validation.
Deleted slugs remain reserved while they can appear in audit records, external
webhooks, billing records, or URL redirect history. A service may permit a slug
to be reused only after the previous resource is fully tombstoned and no active
redirect, grant, schedule, or trust binding can resolve it.

Duplicate create semantics are:

| Case | Result |
| --- | --- |
| Same idempotency key and same request body | Return the original result. |
| Same idempotency key and different request body | `409 conflict.idempotency_payload_mismatch`. |
| Different idempotency key and conflicting slug or external unique key | `409 conflict.identifier_exists`. |
| Same display name | Allowed. |
| Provider duplicate, such as the same GitHub repository in one organization | `409 conflict.external_identity_exists` unless the operation is an attach/import of the existing resource. |

Internal tuple stores such as SpiceDB may store object type plus immutable ID
because the type is already part of the tuple key. Audit records, exports,
policy JSON, SDK DTOs, and support tooling should include the resource name so
the resource remains unambiguous outside the originating database.

## Client Construction

TypeScript target shape:

```ts
const verself = await Verself.fromWorkloadIdentity({
  baseURL: "https://verself.sh",
  org: "guardian-intelligence",
  provider: "github-actions",
});

const fallback = await Verself.fromCredentialFile({
  baseURL: "https://verself.sh",
  org: "acme-corp",
  path: "/run/secrets/verself-credential.json",
});
```

Go target shape:

```go
client, err := verself.FromWorkloadIdentity(ctx, verself.WorkloadIdentityOptions{
	BaseURL:  "https://verself.sh",
	Org:      "guardian-intelligence",
	Provider: verself.WorkloadProviderGitHubActions,
})
if err != nil {
	return err
}
```

SDKs accept an installation apex for the common path. Service URL overrides are
for tests, staging tunnels, and operator diagnostics. Raw access-token
construction is a diagnostic escape hatch:

```ts
const client = Verself.fromAccessToken({
  baseURL: "https://verself.sh",
  org: "acme-corp",
  accessToken,
});
```

`fromAccessToken` does not read profiles, credential files, or refresh
credentials. Production examples should use workload identity or credential
files.

## Authentication

The SDK owns token acquisition, refresh, credential parsing,
clock skew handling, and in-process token caching. Resource modules receive a
token source and never parse credential files directly.

Authentication modes, in preference order:

1. Workload identity. The runtime presents a bounded assertion such as GitHub
   Actions OIDC, Verself runner identity, or a SPIFFE JWT-SVID. The SDK
   exchanges that assertion for a short-lived Verself access token.
2. Credential file. The runtime reads one private-key JWT credential bundle from
   an owner-only file and exchanges it for short-lived access tokens.
3. Client secret. Supported only for runtimes that cannot hold asymmetric key
   material safely enough for private-key JWT.
4. Access token. Diagnostic and tests only.

The preferred long-term path is workload trust. Trust bindings must bind issuer,
audience, subject shape, repository or deployment facts, maximum permissions,
and maximum token lifetime. SPIFFE JWT-SVID assertions should be minted for a
single audience.

SDKs do not implement SPIFFE mTLS. Repo-owned service-to-service traffic uses
service-owned clients with workload-owned mTLS transports.

## Credentials

Credential lifecycle methods live at `verself.credentials`:

```ts
await verself.credentials.create({
  name: "e2e-acme",
  permissions: ["sandbox:execution:read", "sandbox:logs:read"],
  authMethod: "private_key_jwt",
});

await verself.credentials.rotate("cred_123", {
  authMethod: "private_key_jwt",
});

await verself.credentials.revoke("cred_123", {
  reason: "rotated-out-of-band",
});
```

Creation and rotation return issued material once. SDK helpers write credential
bundles only when the caller explicitly requests a file, and local writes use
owner-only file modes. The SDK must never log issued material, private keys,
client secrets, bearer tokens, or authorization headers.

Workload trust methods live under `verself.credentials.trust`:

```ts
await verself.credentials.trust.createGitHubActions({
  name: "e2e-main",
  repository: "guardianintelligence/verself-sh",
  ref: "refs/heads/main",
  permissions: ["sandbox:execution:read"],
  maxTokenTTLSeconds: 900,
});
```

## Resource Modules

The initial public module target is:

| Module | Scope |
| --- | --- |
| `orgs` | Create, inspect, list, update, and select organization context. |
| `members` | Invite, list, remove, and inspect human members. Access changes flow through IAM policy operations. |
| `credentials` | Create, list, get, rotate, revoke, and inspect non-human credentials. |
| `credentials.trust` | Create and inspect secretless workload trust bindings. |
| `projects` | Create, list, inspect, update, archive, and manage environments. |
| `repositories` | List repositories, inspect refs and trees, create checkout grants, and dispatch workflows. |
| `runs` | List execution records, inspect attempts, and read execution state. |
| `logs` | Stream and search run logs. |
| `schedules` | Create, list, pause, resume, and delete recurring dispatch schedules. |
| `secrets` | Read and write environment values, opaque credentials, and transit-key operations. |
| `notifications` | List, summarize, dismiss, clear, and update delivery preferences. |
| `audit` | Search OCSF API activities, create exports, poll export state, and download exports. |
| `billing` | Inspect entitlements, plans, contracts, credits, invoices, and current statements. |

## API Shape

Public service APIs should be designed from the SDK method outward:

1. Pick the customer resource and SDK method name.
2. Define stable Smithy shapes, typed errors, and operation traits for that
   method.
3. Generate or mirror the service HTTP binding and public/internal projections.
4. Regenerate SDK-owned transports and runtime descriptors.
5. Wrap the generated operation in the curated SDK.
6. Use the SDK method from the CLI, browser server route, docs example, or agent.

Smithy service names, operation names, generated OpenAPI tags, path names, and
schema names should support the SDK resource module. Service ownership remains
visible in code ownership, deployment, and telemetry; it should not leak into
customer method names.

The public API contract needs these primitives for SDK quality:

| Primitive | API requirement |
| --- | --- |
| Token exchange | Credential and workload assertions exchange for short-lived access tokens with explicit audience and organization context. |
| Credentials | Public lifecycle endpoints for create, list, get, rotate, and revoke. |
| Workload trust | Public lifecycle endpoints for bounded trust bindings. |
| Resource names | Durable resources return `id`, `resourceName`, optional `slug`, and `displayName`; cross-resource, policy, audit, import, and export references use resource-name fields. |
| Pagination | Cursor-based list contracts for all growing collections. |
| Idempotency | `Idempotency-Key` accepted on create, update, rotate, revoke, dispatch, schedule, and export operations. |
| Errors | RFC 9457 problem details with stable Verself error codes, request IDs, and trace IDs. |
| Retries | `Retry-After` honored for rate limits and temporary service saturation. |
| Trace context | W3C `traceparent` propagated across SDK calls. |

## Method Semantics

Read methods should return typed resources or typed page objects. TypeScript
SDKs expose async iterators for list operations:

```ts
for await (const run of verself.runs.iterate({ status: "failed" })) {
  console.log(run.id);
}
```

Go SDKs expose explicit page methods and iterator helpers:

```go
iter := client.Runs.Iterate(verself.ListRunsOptions{Status: verself.RunFailed})
for iter.Next(ctx) {
	run := iter.Value()
	_ = run
}
if err := iter.Err(); err != nil {
	return err
}
```

Mutation methods accept caller-provided idempotency keys. SDK-generated keys are
derived only from an explicit caller seed and operation identity. Observed-state
writes take resource versions and surface conflicts as typed errors.

## Errors

SDKs normalize service failures into a shared hierarchy:

```text
VerselfError
AuthError
PermissionDeniedError
ValidationError
ConflictError
RateLimitError
ServiceUnavailableError
APIError
TransportError
```

`APIError` preserves the HTTP status, RFC 9457 members, stable Verself error
code, operation name, path, request ID, trace ID, and retriability metadata.
Validation errors expose field paths when the service provides them. Transport
errors represent failure to receive a service response.

## Observability

SDK clients propagate W3C trace context and set a stable user agent that includes
SDK language, SDK version, runtime, and API version. The SDK should expose the
request ID and trace ID on successful long-running operation handles and on
errors.

Credentials and workload-trust exchanges should produce OCSF API activities that name
the machine principal, credential or trust binding, organization, requested
audience, and resulting token lifetime without recording issued secrets or
bearer token material.

## Versioning

SDK releases pin default API versions. The API version header remains available
for staged rollouts, canaries, and compatibility testing. Contract evolution is
modeled in Smithy and projected into OpenAPI artifacts, transport bindings where
tooling is reliable, and runtime descriptors. Curated SDK methods define the
public compatibility surface.

TypeScript and Go are the first curated SDKs. Python, Rust, and Terraform
provider surfaces should follow only after the public API and first two SDKs
have converged on the same resource grammar.

## Standards

- OAuth 2.0 client credentials and token endpoint semantics: [RFC 6749](https://datatracker.ietf.org/doc/rfc6749/)
- Private-key JWT client authentication: [RFC 7523](https://datatracker.ietf.org/doc/html/rfc7523)
- OAuth 2.0 token exchange for workload assertions: [RFC 8693](https://www.ietf.org/rfc/rfc8693)
- OAuth 2.0 security guidance favoring asymmetric client authentication: [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700)
- Device authorization for future human CLI login: [RFC 8628](https://www.rfc-editor.org/rfc/rfc8628)
- Problem details for HTTP APIs: [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html)
- HTTP retry and idempotency semantics: [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110)
- Idempotency-Key header draft: [draft-ietf-httpapi-idempotency-key-header](https://www.ietf.org/archive/id/draft-ietf-httpapi-idempotency-key-header-07.html)
- Trace propagation: [W3C Trace Context](https://www.w3.org/TR/trace-context/)
- Workload JWT assertions: [SPIFFE JWT-SVID](https://spiffe.io/docs/latest/spiffe-specs/jwt-svid/)
- URI generic syntax: [RFC 3986](https://www.rfc-editor.org/rfc/rfc3986)
- URN persistent names and namespace registration: [RFC 8141](https://www.rfc-editor.org/rfc/rfc8141)
- Globally unique cloud resource names: [AWS ARNs](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html), [Google Cloud full resource names](https://cloud.google.com/iam/docs/full-resource-names), and [Azure resource IDs](https://learn.microsoft.com/en-us/rest/api/resources/resources/get-by-id)
- Resource-oriented API names: [Google AIP-122](https://google.aip.dev/122)
- Object names plus immutable UIDs: [Kubernetes Object Names and IDs](https://kubernetes.io/docs/concepts/overview/working-with-objects/names/)
- UUID generation: [RFC 9562](https://www.rfc-editor.org/rfc/rfc9562)

# identity-service

`identity-service` is the Forge Metal identity control plane. Zitadel remains the
identity provider; this service owns the product control plane layered over
Zitadel identity state.

## Boundary

Zitadel owns authentication, organizations, users, service accounts, OIDC/OAuth
applications, project roles, role assignments, project grants, JWKS, MFA,
passkeys, and social identity providers.

Forge Metal owns product IAM policy documents, organization-management UX, and
the catalog projection of service-declared operations. This service is the API
surface for those Forge Metal-owned concerns.

Go product services remain authorization enforcement points. Each service owns
and enforces its own operation catalog through Huma/OpenAPI metadata such as
`x-forge-metal-iam`. `identity-service` may aggregate and expose those catalogs
for policy editing, but it must not become the runtime authorizer for other
services.

## Product Surface

The first user-facing surface is a shared React organization widget, initially
embedded in `rent-a-sandbox` and later reused by other frontend apps. The widget
talks to frontend server functions, and those server functions call
`identity-service` with server-owned Zitadel access tokens. Browser code must not
read or persist Zitadel bearer tokens.

Do not model this as an iframe, a Zitadel console extension, or a dedicated shell
app unless the product surface later needs to stand alone. The product contract
is the shared component plus generated service clients, not a specific hosting
route.

## API Shape

This service should follow the hardened Huma pattern used by
`sandbox-rental-service` for customer-facing `/api/*` routes: keep the Huma
method/path/OpenAPI declaration and the operation policy together in route
registration so IAM metadata, rate limits, idempotency, audit events, body
limits, and generated-client contracts cannot drift.

Public organization APIs derive organization scope from the validated Zitadel
token. Do not trust `org_id`, role keys, user IDs, or customer IDs supplied by
browser request bodies as proof of authority. Handlers must still validate
resource ownership against Zitadel or Forge Metal-owned storage after the
operation permission check passes.

Use `apiwire` for request/response DTOs shared across services, generated
clients, or frontend wrappers, including the public operation-catalog response
shape returned by this service. Huma route metadata such as `x-forge-metal-iam`
remains service-local because it describes enforcement behavior rather than a
wire payload.

## Policy Model

Built-in defaults must make a fresh self-hosted install usable without customers
hand-authoring raw IAM documents. Customer-editable policy documents should be a
constrained Forge Metal resource that maps role keys to service-declared
permissions.

Zitadel role assignments prove who the caller is and which organization/project
roles they hold. Forge Metal policy documents decide how those role keys map to
product permissions. Full policy documents should not be embedded into Zitadel
tokens.

Operation catalogs should be treated as code-defined service contracts. A
service operation such as `sandbox:execution:submit` is declared and enforced by
the owning service, documented through OpenAPI, and consumed by this service only
as policy-editing input.

## Zitadel Integration

All direct Zitadel Management/API calls belong behind an internal adapter
boundary. API handlers and frontend code should not build raw Zitadel requests.
The adapter should expose Forge Metal concepts such as organization membership,
invitations, service accounts, project role assignments, and project grants.

Credentials used to administer Zitadel are service credentials, not browser
tokens and not exported rehearsal persona credentials. Keep the credential source
narrow enough that systemd `LoadCredential=` can be replaced later by OpenBao
and SPIFFE/SPIRE workload identity without changing the external API contract.

Role changes are eventually reflected in new or refreshed tokens. Do not assume
that a currently issued access token immediately reflects a role update; product
flows must either use fresh tokens where needed or tolerate the token-refresh
boundary explicitly.

## API Credentials

Customer API credentials are Forge Metal-managed Zitadel service-account
credentials. `identity-service` owns the create/list/read-metadata/roll/revoke
surface, but product services remain the runtime authorization enforcement
points.

Default to private-key JWT credentials. Client credentials are acceptable when a
customer needs a simpler CI/CD secret shape. Do not make personal access tokens
the default customer-facing API key; they are long-lived bearer tokens and should
remain an internal, demo, or explicit escape-hatch path.

Secret material is visible only when created or rolled. Read/list APIs return
metadata such as display name, status, auth method, key or secret fingerprint,
exact operation permissions, created/revoked timestamps, and last-used
telemetry. Never persist or return plaintext customer credential secrets.

Use a Zitadel pre-access-token Action to append `forge_metal:credential_id`,
`org_id`, and the exact Forge Metal operation permissions granted to the active
credential.
Do not embed full Forge Metal policy documents into Zitadel tokens. Issuance and
roll must reject any requested permission that is not in a service-declared
operation catalog or is not held by the creating principal.

## Observability And Security

Public errors should be stable problem responses with trace-backed instances and
redacted internal causes. Audit logs should capture the operation, permission,
organization scope, subject, outcome, and stable failure code.

Tests and live rehearsal should prove that public operations declare
`x-forge-metal-iam`, require bearer auth, enforce idempotency where applicable,
and deny callers whose current organization role assignments do not grant the
required permission.

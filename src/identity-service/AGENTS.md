# identity-service

`identity-service` is the Verself identity control plane. Zitadel remains the
identity provider; this service owns the product control plane layered over
Zitadel identity state.

## Boundary

Zitadel owns authentication, organizations, users, service accounts, OIDC/OAuth
applications, project roles, role assignments, project grants, JWKS, MFA,
passkeys, and social identity providers.

Verself owns the fixed three-role product IAM (`owner`, `admin`, `member`),
the static code-owned capability catalog that gates the `member` role, and the
organization-management UX. This service is the API surface for those Forge
Verself-owned concerns.

Go product services remain authorization enforcement points. Each service owns
and enforces its own operation catalog through Huma/OpenAPI metadata such as
`x-verself-iam`. `identity-service` does not aggregate other services'
catalogs at runtime and is not the runtime authorizer for other services — its
catalog is consulted only for the member-capability resolution path inside its
own boundary.

## Product Surface

The first user-facing surface is a shared React organization widget, initially
embedded in `console` and later reused by other frontend apps. The widget
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
browser request bodies as evidence of authority. Handlers must still validate
resource ownership against Zitadel or Verself-owned storage after the
operation permission check passes.

Use `apiwire` for request/response DTOs shared across services, generated
clients, or frontend wrappers, including the member-capability document and
catalog payloads returned by this service. Huma route metadata such as
`x-verself-iam` remains service-local because it describes enforcement
behavior rather than a wire payload.

## Product IAM Model

The product IAM is a fixed three-role model: `owner`, `admin`, `member`. Owner
is the org singleton (one per org, transferred via a separate flow); owner and
admin are otherwise identical and resolve to the full known permission set.
Member is gated by a small, static, code-owned catalog of named **capability**
bundles in `internal/identity/capabilities.go`. There is no customer-editable
policy document and no per-member override surface.

Each operation in `internal/identity/catalog.go` carries a `MemberEligible bool`
flag. The `init()` check in `capabilities.go` panics at process start if any
capability bundles a permission whose operation is not tagged member-eligible,
or if any member-eligible permission is not covered by the baseline ∪ capability
union. Drift between the catalog and the capability list is therefore a
boot-time bug, not a runtime authorization gap.

Zitadel role assignments prove who the caller is and which org/project role
they hold. The Verself capability state is org-scoped and stored in
`identity_member_capabilities` (PostgreSQL); it is resolved per request at the
service boundary and is not embedded into Zitadel tokens.

Operation catalogs are code-defined service contracts. A service operation such
as `sandbox:execution:read` or `sandbox:execution_schedule:write` is declared
and enforced by the owning service and documented through OpenAPI. Adding a
capability or moving an operation between member-eligible and admin-only is a
code change in `identity-service`, gated by the `init()` invariant check.

The members table is human-non-owner only. `Service.Members` filters Zitadel
machine users (they hold project authorizations as service accounts but live
on the API Credentials surface) and owner-role users (the org singleton role
is non-editable from this UI per `validateRoleKeys`). `Service.Organization`
still resolves the caller from the unfiltered set so an operator who is the
owner can still see themselves in the general section.

## Zitadel Integration

All direct Zitadel Management/API calls belong behind an internal adapter
boundary. API handlers and frontend code should not build raw Zitadel requests.
The adapter should expose Verself concepts such as organization membership,
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

Customer API credentials are Verself-managed Zitadel service-account
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

Use a Zitadel pre-access-token Action to append `verself:credential_id`,
non-secret credential metadata (`verself:credential_name`,
`verself:credential_fingerprint`, owner id/display, auth method), `org_id`,
and the exact Verself operation permissions granted to the active
credential. Member capability state stays in `identity_member_capabilities`
PostgreSQL; it is not embedded into Zitadel tokens. Issuance and roll must
reject any requested permission that is not in a service-declared operation
catalog or is not held by the creating principal at the moment of issuance.

## Observability And Security

Public errors should be stable problem responses with trace-backed instances and
redacted internal causes. Audit logs should capture the operation, permission,
organization scope, subject, outcome, and stable failure code.

Tests and live rehearsal should prove that public operations declare
`x-verself-iam`, require bearer auth, enforce idempotency where applicable,
and deny callers whose current organization role assignments do not grant the
required permission.

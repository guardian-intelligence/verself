# iam-service

`iam-service` is the Verself identity control plane. Zitadel remains the
identity provider; this service owns the product control plane layered over
Zitadel identity state.

## Boundary

Zitadel owns authentication, human identity, OIDC/OAuth applications, JWKS, MFA,
passkeys, and social identity providers.

Verself owns product organizations, service accounts, API credentials, and
authorization policy. `iam-service` stores the mapping from Verself public org
IDs to identity-provider org IDs and is the API surface for organization IAM
policy.

Go product services remain authorization enforcement points. Each service owns
its operation catalog through Smithy contract metadata under `src/smithy`.
Contract packages expose DTOs, operation descriptors, and handler types;
handwritten Huma route registration consumes those types and projects the same
metadata into OpenAPI. Runtime authorization decisions are delegated to
`iam-service` over SPIFFE mTLS. Public API packages depend on the narrow
`service-runtime/iam` interface; service entrypoints wire service-local
`iam-service` internal clients.

## Product Surface

The first user-facing surface is a shared React organization widget, initially
embedded in `verself-web` and later reused by other frontend apps. The widget
talks to frontend server functions, and those server functions call
`iam-service` with server-owned Zitadel access tokens. Browser code must not
read or persist Zitadel bearer tokens.

Do not model this as an iframe, a Zitadel console extension, or a dedicated shell
app unless the product surface later needs to stand alone. The product contract
is the shared component plus service-local clients, not a specific hosting
route.

## API Shape

Customer-facing `/api/*` routes use `internal/contractapi` DTOs,
operation descriptors, and handler types. Handwritten route registration owns
the Huma wiring. The Smithy operation model, Huma method/path registration, IAM
metadata, rate limits, idempotency, API activity metadata, body limits, OpenAPI
projection, and service-client contracts must not drift.

Public route policy metadata uses the shared `service-runtime/iam.OperationPolicy`
vocabulary. `Permission` is the product permission sent to the Zanzibar-backed
iam-service authorization API; `Resource` and `Action` are operation/contract labels,
not raw SpiceDB tuples or a Cedar action expression. `OrgScope`, `Idempotency`,
and common `Action` values are closed enums. Service-owned `Resource`,
`RateLimitClass`, and `AuditEvent` values should be declared as typed constants
next to the route catalog, not repeated as anonymous strings.

Public organization APIs derive organization scope from the validated token and
the Verself public `org_id` claim. Do not trust request-body org IDs, user IDs,
or customer IDs as evidence of authority. Handlers must still validate resource
ownership against Verself-owned storage after the operation permission check
passes.

Public signup is an installation-scoped intent state machine. `StartSignup`
records a pending intent with a hashed verification token and sends
notification; it must not create Zitadel, IAM, SpiceDB, or billing state.
`VerifySignup` is the only public path that materializes a new organization:
create the Zitadel org, create and verify the Zitadel human, create the IAM org
profile, bind the human as `roles/owner`, ensure the billing org, mark the
intent completed, and emit ClickHouse evidence for each materialization step.

Use contract DTOs for public request/response payloads. Handwritten
DTOs remain appropriate for internal-only data structures that do not cross the
public contract boundary. Smithy operation traits are the settled metadata home;
the route catalog exposes the same contract metadata for runtime policy and
drift gates. Huma/OpenAPI metadata is transitional implementation detail, not
contract authority.

## Product IAM Model

The product IAM model is Zanzibar-native. The public policy document follows
the GCP IAM shape: version, etag, and role/member bindings. Roles are SpiceDB
`role#member` subject sets exposed through `principalSet://` member strings.
`roles/owner` is the human breakglass role and `SetIamPolicy` enforces that an
organization always retains at least one human owner.

Operation catalogs are code-defined service contracts. A service operation such
as `sandbox:execution:read` or `sandbox:execution_schedule:write` is declared
and enforced by the owning service and documented through contract projections.
Product services ask `iam-service` to check the current relationship graph
rather than interpreting token-embedded authorization state.

## Zitadel Integration

All direct Zitadel Management/API calls belong behind an internal adapter
boundary. API handlers and frontend code should not build raw Zitadel requests.
The adapter should expose Verself concepts such as organization membership,
invitations, service accounts, and API credential subjects.

Credentials used to administer Zitadel are service credentials, not browser
tokens and not exported rehearsal persona credentials. Keep the credential source
narrow enough that systemd `LoadCredential=` can be replaced later by OpenBao
and SPIFFE/SPIRE workload identity without changing the external API contract.

Authorization changes take effect in SpiceDB and are checked at request time.
Zitadel tokens identify a subject and organization; they do not carry product
authorization state.

## API Credentials

Customer API credentials are Verself-managed Zitadel service-account
credentials. `iam-service` owns the create/list/read-metadata/roll/revoke
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

Use a Zitadel pre-access-token Action to append `org_id` and, for API
credentials, `verself:credential_id` plus non-secret credential metadata
(`verself:credential_name`, `verself:credential_fingerprint`, owner
id/display, auth method). Credential authorization is expressed in SpiceDB and
checked by `iam-service` at request time.

## Observability And Security

Public errors should be stable problem responses with trace-backed instances and
redacted internal causes. Audit logs should capture the operation, permission,
organization scope, subject, outcome, and stable failure code.

Live rehearsal should prove that public operations declare the canonical
contract metadata, require bearer auth, enforce idempotency where applicable,
and deny callers whose current Zanzibar relationships do not grant the required
permission.

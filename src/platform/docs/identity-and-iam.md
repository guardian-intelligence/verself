# Identity And IAM Direction

Zitadel is the identity system. Forge Metal is the product IAM system. Go
services are the authorization enforcement points.

## Boundary

Zitadel owns authentication, organizations, users, service accounts, OIDC/OAuth
applications, project roles, role assignments, project grants, JWKS, MFA,
passkeys, and social identity providers.

Forge Metal services own their operation catalogs. A service operation is a
code-defined contract such as `sandbox:execution:submit`; it is not a
customer-defined resource. Huma services attach operation metadata to OpenAPI
with `x-forge-metal-iam` and enforce the required permission in the service
process. Frontend route guards and widgets are UX only.

Forge Metal product IAM is intentionally a fixed three-role model: `owner`,
`admin`, `member`. The roles are code constants — not customer-editable rows.
Owner and admin always hold the full known permission set; the org admin
console only exposes a switchboard of named **member capabilities** that bundle
permissions for the member role. Adding or changing a capability is a code
change, not a runtime mutation. Per-member overrides are not modeled — every
member of an organization sees the same capability set.

Customer configuration starts with role assignment: invite users, add them to
organizations, and assign one of `admin` or `member`. The owner role is a
singleton transferred through a separate flow and is not assigned through
the standard invite/role-update path.

External systems are not Forge Metal users. Git hosts, Stripe, Resend, and other
integrations authenticate through provider-native credentials and webhook
verification. When an external event needs an organization context, the service
must resolve that context from Forge Metal-owned state, such as a webhook
endpoint row or integration row. Do not trust organization IDs, role names, or
customer IDs supplied by external webhook payloads.

## Organization Surface

The product surface is a reusable first-party organization-management React
component in `src/viteplus-monorepo`, embedded first inside rent-a-sandbox and
then inside the other customer-facing frontend apps. It talks to
`identity-service` through frontend server functions; browser code does not
receive Zitadel bearer tokens.

The raw Zitadel Management Console remains a founder/admin identity tool at
`/ui/console`; it is not the long-term customer product console. A future
standalone organization route on the auth host is still possible, for example
`https://auth.<domain>/organization`, but it should be another embedding of the
same component and service API rather than an iframe, a Zitadel Console
extension, or a separate hand-built shell.

Zitadel custom login UI support is relevant to replacing or branding the login
flow, not to product authorization. Zitadel Actions are useful for workflow
hooks and token/role automation; the member-capability state and the static
capability catalog stay in `identity-service` PostgreSQL and Go code.

## Policy Split

Service operation catalogs:

- Live with the service that enforces them.
- Are exposed through OpenAPI `x-forge-metal-iam`.
- Include operation ID, permission, resource, action, org scope, rate-limit
  class, idempotency semantics, and audit event.
- Tag each operation with `member_eligible: true|false`. The flag is the
  denylist that prevents non-eligible permissions from ever leaking into the
  member role's resolved set, enforced at catalog init and at credential
  issuance.
- The service-local Huma metadata is not an `apiwire` DTO. Public responses
  that return an operation catalog to customers are normal request/response
  wire data and should use `apiwire`.

Zitadel role assignments:

- Prove who the caller is and which organization/project roles they hold.
- Are the membership plane for users and service accounts.
- Are surfaced to Go services through validated JWT claims or, when needed,
  through Zitadel APIs.
- Within the identity-service Zitadel project, only three role keys exist:
  `owner`, `admin`, `member`. Other services follow their own per-project role
  conventions.

Forge Metal member capabilities:

- Are a fixed, code-owned catalog of capability bundles (e.g.
  `deploy_executions`, `manage_integrations`, `invite_members`,
  `view_billing`). Each bundle pins a hardcoded set of member-eligible
  permissions.
- Are toggled per organization via a small switchboard in the org console.
  The wire shape is just `{ version, enabled_keys[] }`; the catalog itself is
  read-only.
- Apply collectively to every member of an organization. There is no
  per-member override.
- Live in `identity-service` PostgreSQL as `identity_member_capabilities`,
  keyed by `org_id` with optimistic-lock `version`.

`identity-service` owns the catalog, the document storage, and the resolution
path. Other services continue to own and enforce their own operation catalogs;
when a service adds a permission that should be member-grantable, it tags the
operation `member_eligible: true` and the identity-service catalog adds (or
extends) a capability bundle that includes that permission.

## Runtime Token Contract

Go services validate bearer JWTs with `src/auth-middleware`. The middleware
checks the token issuer and audience, verifies the signature from Zitadel JWKS,
extracts identity fields into request context, and leaves operation-specific
authorization to the service.

The runtime identity object is:

- `Subject`: Zitadel user or service-account ID from `sub`.
- `OrgID`: active organization/resource-owner ID when present.
- `ProjectID`: target service project ID whose role claim was accepted.
- `Roles`: role keys extracted only from that target service project claim.
- `RoleAssignments`: structured target-project role assignments, including project ID,
  organization ID, role key, and organization display name.
- `Email`: email claim when present.
- `Raw`: the full claim map for service-specific extraction.

Organization ID extraction is intentionally tolerant while the system converges
on one canonical token shape. Services currently accept the first non-empty
claim among:

- `urn:zitadel:iam:user:resourceowner:id`
- `urn:zitadel:iam:org:id`
- `resource_owner`
- `org_id`

Role extraction only accepts Zitadel's project-qualified claim for the target
service project:

```json
{
  "urn:zitadel:iam:org:project:<project_id>:roles": {
    "sandbox_org_admin": {
      "<org_id>": "<org_name>"
    }
  }
}
```

The middleware intentionally ignores flat `roles`, `role`, and unqualified
`urn:zitadel:iam:org:project:roles` claims for production authorization. The
requested OAuth scope uses a different spelling:
`urn:zitadel:iam:org:projects:roles`.

When a service needs to authorize an organization-scoped operation, it must use
role assignments whose `ProjectID` matches the target service project and whose
`OrganizationID` matches the request identity's `OrgID`. Missing target audience
or missing target project role claim fails closed.

OIDC and service-account setup has several sharp edges:

- Access tokens presented to Go services must be JWTs. Opaque Zitadel access
  tokens fail local JWKS validation. Frontend OIDC applications need
  `OIDC_TOKEN_TYPE_JWT`; machine users need `ACCESS_TOKEN_TYPE_JWT`.
- Service-to-service client credentials must request an audience scope for the
  target project: `urn:zitadel:iam:org:project:id:<project_id>:aud`.
- Service-to-service callers that need roles in the token must request
  `urn:zitadel:iam:org:projects:roles`. The spelling is plural `projects`.
- Callers that need a resource-owner organization ID in the token must request
  `urn:zitadel:iam:user:resourceowner`.
- `openid` and `profile` are still requested for normal OIDC token shape and
  identity claims.

Single-node deployments use a split issuer/JWKS path. The token `iss` claim is
validated against the public issuer, for example `https://auth.<domain>`, while
services can fetch keys from Zitadel over loopback, for example
`http://127.0.0.1:8085/oauth/v2/keys`. The middleware overrides the JWKS
request Host header to the issuer host so Zitadel's instance router accepts the
request. This is a single-node optimization; a three-node topology needs
topology-aware JWKS URLs and nftables egress rules.

## Service Authorization Flow

The service boundary is:

1. HTTP middleware validates the bearer JWT and attaches identity to context.
2. The Huma operation is registered through the service's secured registration
   helper.
3. Registration attaches `x-forge-metal-iam` metadata and OpenAPI
   `bearerAuth`.
4. The operation policy is enforced before the handler runs.
5. The handler still checks organization and resource ownership from storage.

An operation policy contains the required permission, resource, action,
organization scope, rate-limit class, idempotency rule, and audit event. The
permission check is intentionally small. The resolution chain is:

1. `owner` role → all known permissions (always).
2. `admin` role → all known permissions (always).
3. `member` role → baseline member permissions ∪ permissions bundled into
   each capability key the org admin has enabled.

Direct permission claims are accepted only for Forge Metal API credential
tokens carrying both `forge_metal:credential_id` and an organization scope;
normal human OAuth scopes are not permission grants.

API credential direct permissions are issued as exact operation permissions
and must be checked at issuance/roll time against the creating principal's
effective permissions. A member-role caller cannot mint a credential whose
permissions are not held by the member set under the org's current capability
configuration.

Embedded organization widgets are a special cross-service web-session path.
The rent-a-sandbox frontend owns the interactive OIDC application and stores the
browser session server-side. Before calling `identity-service`, the BFF exchanges
the session access token for an identity-service audience token and forwards that
resource token. `identity-service` validates the JWT locally and reads only
`urn:zitadel:iam:org:project:<identity-service-project-id>:roles`; it does not
resolve role assignments from Zitadel in the request path.

The `x-forge-metal-iam` metadata is documentation and a generation target, not
the security mechanism itself. The security mechanism is the service's runtime
policy enforcement and storage ownership checks. Tests should assert that every
public API operation declares IAM metadata and bearer auth, because missing
metadata is a sign the service's public contract and enforcement plane have
diverged.

## Frontend Sessions

TanStack Start frontends use server-owned OAuth web sessions. The frontend
server performs the Zitadel code exchange, stores access and refresh tokens in
the `frontend_auth` PostgreSQL database's `auth_sessions` table, and issues an
HTTP-only session cookie to the browser. The platform provisions that database
through the `frontend_auth_sessions` Ansible role.

Server functions, loaders, and route hooks read the server-owned session and
forward bearer tokens to Go services from the server side. Browser code must not
read, persist, or refresh Zitadel bearer tokens. Frontend `beforeLoad` checks
are useful for SSR gating, redirects, and user experience; they are not
authorization.

This rule is enforced at the edge by a Content-Security-Policy header with
`connect-src 'self'` applied to every customer-facing frontend domain
(`rent_a_sandbox_domain`, `stalwart_domain`). The policy lives
in the `frontend_security_headers` snippet in
`src/platform/ansible/roles/caddy/templates/Caddyfile.j2`. Because `connect-src`
is restricted to the frontend's own origin, the browser cannot issue `fetch`,
`XHR`, `WebSocket`, or `EventSource` requests to `billing.<domain>`,
`auth.<domain>`, or any other Go service origin — every API round trip must
traverse a TanStack Start server function, which is where the bearer attachment
actually happens. A regression that adds a cross-origin browser fetch surfaces
as a loud CSP console error and a blocked request rather than a silent token
leak. Same-origin sub-paths such as `/v1/shape` (Electric SQL) and `/jmap/*`
(Stalwart JMAP on the mail domain) are unaffected because Caddy fronts them on
the frontend origin.

Script CSP still allows `'unsafe-inline'` because Vinxi/TanStack Start injects
inline hydration scripts; moving to per-request nonces is a tracked hardening
step and does not change the bearer-isolation guarantee, which rides on
`connect-src`.

Do not assume a web UI persona by inserting rows into `frontend_auth.auth_sessions`.
Those sessions are coupled to the encrypted HTTP-only cookie, OAuth state,
nonce, PKCE, token expiry, and refresh semantics owned by the auth server
package. UI rehearsal should drive the normal Zitadel browser login. API
rehearsal should use seeded client-credentials machine users with project
audience and role scopes.

Only frontends need interactive OIDC applications. Go services need the Zitadel
project/application audience that should appear in tokens; they do not own
interactive login.

## Role Assignment Provisioning

Prefer Zitadel v2 APIs for new integration code. Role assignments should be
created and updated through the Authorization service:

- `zitadel.authorization.v2.AuthorizationService/CreateAuthorization`
- `zitadel.authorization.v2.AuthorizationService/UpdateAuthorization`

The assignment must include `userId`, `projectId`, `organizationId`, and the
intended `roleKeys`. This binds a user or service account to a project role
within one organization. Project grants let another organization manage role
assignments for its users; they are not a substitute for Forge Metal operation
policies.

Temporary seed or rehearsal users should follow the same model as long-lived
service accounts:

- Create the machine user in the target organization.
- Configure the machine user for JWT access tokens.
- Grant the exact project role required for the operation.
- Request the audience, role, and resource-owner scopes explicitly when
  fetching the client-credentials token.
- Delete or rotate the seed credential when it is no longer needed.

## Inbound SCIM Provisioning

Zitadel ships a SCIM 2.0 server (User resource only, currently preview,
feature-flagged) at `https://auth.<domain>/scim/v2/{orgId}/`. Customer IdPs —
Okta, Entra, JumpCloud, OneLogin — point their SCIM connector at that URL and
authenticate with a per-org Zitadel service-account credential. User CRUD,
PATCH, Bulk, filter, and `.search` route directly to Zitadel. Forge Metal does
not proxy, re-terminate, or re-implement the User half of the SCIM surface.

Groups, the `groups` field on the User resource, role-bound group provisioning,
and `/Me` are out of scope. Zitadel does not implement Groups in its SCIM
surface, and Forge Metal does not layer a Groups shim over it. Customers
assign the three-role set (`owner`, `admin`, `member`) through the Forge Metal
organization console; the IdP's group state is not consulted. Attempts to push
a `groups` attribute or a `Group` resource through Zitadel's SCIM endpoint are
rejected by Zitadel per its documented scope.

A Forge Metal Groups + role-binding shim is a deliberate future option, not
current scope. If a customer eventually demands group-driven role assignment,
the shape is a Forge Metal `/scim/v2/{orgId}/` endpoint that terminates the
full SCIM surface, handles Groups and role-binding locally, and forwards User
CRUD to Zitadel's SCIM using the same per-org service-account credential. The
three-role invariant stays intact: SCIM Groups would bind to at most one
existing role; they would not mint new roles.

Per-org SCIM credentials are provisioned through `identity-service`'s API
credential surface so they inherit the standard audit, revoke, roll, and
last-used lifecycle. The issued credential is a Zitadel service-account
credential under the hood (private-key JWT preferred, client secret
acceptable). The `urn:zitadel:scim:provisioningDomain` Zitadel metadata field
namespaces `externalId` per IdP source so multi-IdP provisioning against the
same org does not collide. SCIM access is gated by plan entitlement at
credential issuance, not at request time.

## API Credential Model

Customer API credentials are Forge Metal product resources backed by Zitadel
service accounts. They are not git-provider credentials, webhook secrets, or
human browser sessions. The durable Forge Metal resource is readable as
metadata; secret material is returned only on creation or roll and must never be
read back later.

The researched Zitadel pattern is:

- Service accounts are the machine identity model for backend/API access. They
  provide separate audit identity, independent lifecycle, and least-privilege
  role assignment for non-human callers.
- Service-account access tokens presented to Forge Metal services must be JWTs.
  A newly created Zitadel machine user must be configured for JWT access tokens;
  opaque tokens fail local JWKS verification.
- Private-key JWT is the default customer credential method because callers
  exchange a short-lived signed assertion for a short-lived access token.
  Client credentials are acceptable as the lower-friction CI/CD option when a
  customer needs a client ID and secret in a provider secret store.
- Personal access tokens are not the default customer-facing product API key.
  They are long-lived bearer tokens for service accounts and should remain an
  internal, demo, or explicit escape-hatch path only.

`identity-service` owns the customer API credential lifecycle:

- Create a Zitadel service account in the customer organization, or attach a new
  credential to an existing Forge Metal-managed service account.
- Store Forge Metal metadata: `credential_id`, `org_id`, Zitadel subject ID,
  display name, status, policy version at issue, auth method, key or secret
  fingerprint, created/updated/revoked timestamps, last-used telemetry, and the
  exact allowed operation permissions.
- List and read metadata without secret material.
- Roll by adding a new key or client secret, returning the new secret material
  once, and retiring the old material after a short grace window when configured.
- Revoke by disabling/removing the Zitadel credential and marking the Forge
  Metal credential row revoked.

Issuance and roll must validate every requested permission against the current
service-declared operation catalog and against the creating principal's
effective permissions. A caller cannot mint a credential with permissions they
do not currently hold. Credential scopes are exact operation permissions such as
`sandbox:execution:submit`, `sandbox:github_installation:write`,
`sandbox:logs:read`, `sandbox:volume:read`, `billing:read`, and future CI
operations such as `ci:workflow:dispatch`.

Token minting uses a Zitadel pre-access-token Action. The signed Action
callback is exposed through Caddy on `auth.<domain>` because Zitadel rejects
loopback/private target URLs. The Action appends `forge_metal:credential_id`,
non-secret credential metadata (`forge_metal:credential_name`,
`forge_metal:credential_fingerprint`, owner id/display, and auth method),
`org_id`, and an exact `permissions` claim from Forge Metal-owned credential
metadata. It must not embed full Forge Metal policy documents into the token.
If the Action cannot resolve an active credential or exact permission set, the
token must not receive Forge Metal direct-permission claims; services already
fail closed because direct permission claims are only accepted when the
credential marker and organization scope are present.

Product services must continue to verify issuer, signature, expiration, and
audience, but audience is not authorization. Zitadel can place requested
audience values into tokens; services must still require either a target-project
role assignment mapped through product policy or a Forge Metal API credential
marker plus exact operation permission. Human OAuth scopes are not product
permission grants.

Revocation is not instant for already-issued JWTs when services validate tokens
locally through JWKS. Keep API credential access-token lifetimes short. Add
token introspection or a credential denylist only if live rehearsal shows the
revocation window is unacceptable for customer-facing usage.

## Seeded Rehearsal Personas

`seed-system.yml` provisions three long-lived rehearsal personas for operators
and agents. Each persona has a human browser login and a matching machine user
for API rehearsal:

| Persona | Human login | Machine user | Organization | Built-in roles |
|---|---|---|---|---|
| `platform-admin` | `agent@<domain>` | `assume-platform-admin` | platform | identity-service `owner`; `sandbox_org_admin`, `forgejo_admin`, `mailbox_user` |
| `acme-admin` | `acme-admin@<domain>` | `assume-acme-admin` | Acme Corp | human/browser identity-service `owner`; machine identity-service `admin`; `sandbox_org_admin` |
| `acme-member` | `acme-user@<domain>` | `assume-acme-member` | Acme Corp | `sandbox_org_member`, identity-service `member` |

Use the Make wrappers to mint short-lived token files from the deployed
credential store. These are extremely useful utility scripts for operators and
agents; use them before reaching for ad hoc PATs, copied browser cookies, or
direct credstore reads during live rehearsal:

```bash
make assume-platform-admin
make assume-acme-admin
make assume-acme-member
make assume-persona PERSONA=platform-admin OUTPUT=/tmp/platform-admin.env
```

The default output path is `artifacts/personas/<persona>.env`, written `0600`.
The file contains browser credentials (`BROWSER_EMAIL`, `BROWSER_PASSWORD`) and
project-scoped access tokens such as `SANDBOX_RENTAL_ACCESS_TOKEN` and
`MAILBOX_SERVICE_ACCESS_TOKEN`. In file-output mode, stdout is identity-service
JSON showing the caller's effective access, all declared operations, and the
operations matched by the persona's permissions. These tokens are rehearsal
credentials, not a new persistence layer; regenerate them from Zitadel when
they expire.

Current access coverage:

| Surface | `platform-admin` | `acme-admin` | `acme-member` | Credential path |
|---|---|---|---|---|
| rent-a-sandbox / `sandbox-rental-service` | platform `sandbox_org_admin` | Acme `sandbox_org_admin` | Acme `sandbox_org_member` | Zitadel browser login and `SANDBOX_RENTAL_ACCESS_TOKEN` |
| rent-a-sandbox organization surface / `identity-service` | browser and machine `owner` | browser `owner`, machine `admin` | Acme `member` | BFF token exchange and `IDENTITY_SERVICE_ACCESS_TOKEN` |
| webmail / `mailbox-service` | `mailbox_user`, bound to `agents` | none | none | Zitadel browser login and `MAILBOX_SERVICE_ACCESS_TOKEN` |
| Forgejo OIDC login | `forgejo_admin` | none | none | Zitadel browser login and `FORGEJO_OIDC_ACCESS_TOKEN` |
| ClickHouse | founder access only | none | none | `CLICKHOUSE_OPERATOR_COMMAND`, currently `make clickhouse-query` |
| Forgejo provider API automation | founder access only | none | none | `FORGEJO_OPERATOR_CREDENTIAL`, currently the remote `forgejo-automation` token |
| Stalwart direct JMAP/IMAP/SMTP | not a persona grant | not a persona grant | not a persona grant | use `mailbox-service`/webmail or explicit founder mail tooling |
| `billing-service` direct API | service-to-service only | service-to-service only | service-to-service only | customer-facing billing access goes through `sandbox-rental-service` |

The platform admin persona intentionally does not export the Zitadel admin PAT,
ClickHouse password, or Forgejo automation token. ClickHouse access remains the
founder Make wrapper (`make clickhouse-query`) because it is not a Zitadel
resource yet. Forgejo API automation remains provider-native
`forgejo-automation` until Forgejo OIDC group/role claims are proven for the
interactive UI path and a separate provider API credential model is introduced.

## External Integration Credentials

Zitadel answers "who is allowed to configure this integration?" It does not
answer "how does Forge Metal authenticate to the customer's git host?"

Inbound webhooks should use provider-native verification: HMAC for GitHub and
similar hosts; constant-time token comparison for providers that use shared
tokens; provider-specific signature validation where available. The verified
credential must map to a Forge Metal integration row that carries `org_id`.

Private source access for CI is a separate credential plane. A webhook proves
that an event was delivered by the provider; it does not grant clone access for
private code. Future service-owned CI fetches need an org-owned credential such
as a deploy key, provider app installation token, or host-specific machine
token. Store those as integration secrets scoped to organization, provider,
provider host, and minimal repository or installation permissions. Do not reuse
a human user's browser session token for background git fetches.

## Invariants

- Services enforce authorization after validating Zitadel JWTs. Frontends only
  gate UX.
- Operation permission is necessary but not sufficient; handlers still enforce
  organization/resource ownership from storage.
- Built-in defaults must remain enough for a non-technical founder to run the
  platform.
- A member can never be granted a permission whose operation is not tagged
  `member_eligible: true`. The catalog's init() check enforces this for the
  capability bundles, and `validateCredentialPermissions` enforces it at
  credential issuance.
- The platform dogfoods the same policy and billing abstractions, with internal
  unlimited usage modeled as an adjustment rather than as a bypass.
- External webhooks and provider callbacks authenticate through their own
  verification protocols, then resolve organization context from Forge
  Metal-owned state.
- Private external-provider access tokens and deploy keys are integration
  secrets, not user sessions and not Zitadel role assignments.
- Customer API credentials are Forge Metal-managed Zitadel service-account
  credentials. Secret material is visible only at creation/roll; metadata
  remains readable for audit, rotation, and revocation.

## Current Limitations

- Active organization selection is still mostly represented by the token's
  resource owner organization. A richer multi-org UX needs an explicit active
  organization switch and services must continue filtering structured role
  assignments by that organization.
- The capability catalog and the role set are intentionally code-owned. The
  current MVP intentionally does not surface a generic permission editor to
  customers; the next-quarter roadmap is the enterprise floor (SSO, audit log,
  backups, security posture doc, MFA), not a policy matrix.
- Stalwart direct JMAP/IMAP/SMTP auth remains outside the Zitadel service-auth
  model. The repo-owned `mailbox-service` HTTP API uses Zitadel bearer tokens
  plus `mailbox_bindings`, but direct mail protocol credentials are still
  Stalwart-owned.
- The single-node JWKS loopback path is not the three-node design. Remote
  Zitadel requires topology-aware JWKS discovery and service egress policy.
- Customer API credential tables exist, and services recognize
  `forge_metal:credential_id`, but the create/list/read/roll/revoke lifecycle
  and Zitadel pre-access-token Action are not implemented yet.
- Zitadel SCIM 2.0 is preview and feature-flagged; Users only, no Groups, no
  `/Me`, no ETag, Bulk capped at 100 operations. The Forge Metal position
  (terminate at Zitadel, defer Groups) assumes the preview remains stable or
  graduates; if Zitadel removes the surface, a Forge Metal SCIM server becomes
  required, not optional.

## Source Notes

Local code anchors:

- `src/auth-middleware/auth.go`: JWT verification, split JWKS, identity claim
  extraction, role-assignment extraction.
- `src/identity-service/internal/api/policy.go`: operation policy metadata,
  capability-backed permission enforcement, idempotency and rate-limit hooks
  for the organization-management API.
- `src/identity-service/internal/identity/catalog.go`: built-in identity
  operation catalog (including the `member_eligible` denylist tags).
- `src/identity-service/internal/identity/capabilities.go`: code-owned
  capability catalog, baseline member permissions, and the init() check that
  prevents non-eligible permissions from leaking into the member role.
- `src/sandbox-rental-service/internal/api/policy.go`: operation policy
  catalog, `x-forge-metal-iam`, sandbox role bundles, direct-scope permission
  checks, idempotency and rate-limit hooks.
- `src/sandbox-rental-service/internal/serviceauth/client_credentials.go`:
  service-to-service client-credentials scopes and single-node Host override.
- `src/platform/ansible/playbooks/tasks/upsert_role_assignment.yml`: Zitadel
  Authorization service create/update calls for project role assignments.

Zitadel documents the Management Console at `/ui/console`, including its role as
an admin dashboard and the option to restrict generic Console access when
building a custom UI:
<https://zitadel.com/docs/guides/manage/console/console-overview>

Zitadel's API overview recommends v2 APIs for new integrations and lists User,
Organization, Project, Application, Role Assignment, Authorization, Action, OIDC,
and Session resources. It also lists the URL path prefixes used by Zitadel on a
custom domain:
<https://zitadel.com/docs/apis/introduction>

Zitadel Projects hold applications, roles, grants, and role assignments; project
grants let another organization manage role assignments for its own users:
<https://zitadel.com/docs/guides/manage/console/projects-overview>

Zitadel custom login UI flows use the OIDC and Session APIs; this is separate
from product organization management:
<https://zitadel.com/docs/guides/integrate/login-ui/oidc-standard>

Zitadel Actions can customize behavior such as role assignment after external
identity-provider registration:
<https://zitadel.com/docs/guides/manage/customize/behavior>

Zitadel service accounts are the recommended machine identity model. They
support private-key JWT, client credentials, and personal access tokens; the
Zitadel guidance recommends private-key JWT for most service-account scenarios
and treats PATs as convenient but long-lived bearer tokens:
<https://zitadel.com/docs/guides/integrate/service-accounts/authenticate-service-accounts>

Zitadel machine users must be configured for JWT access tokens when Forge Metal
services validate tokens locally through JWKS:
<https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.CreateUser>

Zitadel's user API supports machine-user keys that are returned once and can be
removed by key ID, matching the Forge Metal "secret visible only on create/roll"
credential contract:
<https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.AddKey>
<https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.RemoveKey>

Zitadel's audience guidance says `aud` must not be the only authorization check;
services still need roles, scopes, or custom claims:
<https://help.zitadel.com/security-best-practices-validating-audience-aud-claims-in-zitadel-access-tokens>

Zitadel Actions can append permission claims during pre-access-token issuance.
Forge Metal uses that as the mechanism for `forge_metal:credential_id`,
non-secret credential audit metadata, and exact API credential permissions, not
as a place to store full product policy:
<https://help.zitadel.com/extend-authorization-in-zitadel-with-organization-metadata-preaccesstoken-action->

Zitadel SCIM 2.0 server. Preview, User resource only, org-scoped URL, Bulk
capped at 100 operations, filter vocabulary enumerated. No Groups, no `/Me`,
no ETag. This is the surface Forge Metal points customer IdPs at directly:
<https://zitadel.com/docs/apis/scim2>
<https://zitadel.com/docs/guides/manage/user/scim2>
<https://zitadel.com/docs/guides/integrate/scim-okta-guide>

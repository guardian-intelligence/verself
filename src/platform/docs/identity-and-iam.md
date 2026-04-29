# Identity And IAM Direction

Zitadel is the human, organization, and customer/API credential identity
system. SPIRE is the repo-owned workload identity system. Verself is the
product IAM system. Go services are the authorization enforcement points.

## Boundary

Zitadel owns authentication, organizations, users, customer/API credential
service accounts, OIDC/OAuth applications, project roles, role assignments,
project grants, JWKS, MFA, passkeys, and social identity providers.

SPIRE owns repo-owned workload identity. Internal service-to-service calls
between Verself services use SPIFFE X.509-SVID mTLS or SPIFFE JWT-SVIDs,
not Zitadel machine users, shared bearer tokens, or OpenBao as an identity
source. The workload identity contract lives in
`docs/architecture/workload-identity.md`.

Verself services own their operation catalogs. A service operation is a
code-defined contract such as `sandbox:execution:read`; it is not a
customer-defined resource. Huma services attach operation metadata to OpenAPI
with `x-verself-iam` and enforce the required permission in the service
process. Frontend route guards and widgets are UX only.

Verself product IAM is intentionally a fixed three-role model: `owner`,
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

External systems are not Verself users. Git hosts, Stripe, Resend, and other
integrations authenticate through provider-native credentials and webhook
verification. When an external event needs an organization context, the service
must resolve that context from Verself-owned state, such as a webhook
endpoint row or integration row. Do not trust organization IDs, role names, or
customer IDs supplied by external webhook payloads.

## Organization Surface

The product surface is a reusable first-party organization-management React
component in `src/viteplus-monorepo`, embedded first inside console and
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
- Are exposed through OpenAPI `x-verself-iam`.
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
- Are the membership plane for users and customer/API credential service
  accounts.
- Are surfaced to Go services through validated JWT claims or, when needed,
  through Zitadel APIs.
- Within the identity-service Zitadel project, only three role keys exist:
  `owner`, `admin`, `member`. Other services follow their own per-project role
  conventions.

Verself member capabilities:

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

In Verself's Zitadel configuration, each service's bearer-token audience *is*
its Zitadel project ID — the two values are not independently configurable — so
`auth-middleware` takes only `Audience` and derives the
`urn:zitadel:iam:org:project:<audience>:roles` claim path from it. Role
assignments carried by `Identity` are keyed to that audience by construction;
tokens issued for other projects fail the OIDC `aud` check before reaching role
extraction.

The runtime identity object for Zitadel-authenticated public/customer APIs is:

- `Subject`: Zitadel user or customer/API credential service-account ID from
  `sub`.
- `OrgID`: active organization/resource-owner ID when present.
- `Roles`: role keys extracted from the configured-audience project claim.
- `RoleAssignments`: structured role assignments (role key and organization ID)
  for the configured-audience project. The claim value beside each org ID is a
  Zitadel-owned domain/name detail and is not product display metadata.
- `Email`: email claim when present.
- `Raw`: the full claim map for service-specific extraction.

Selected organization ID extraction is deliberately separate from role
assignment extraction. Services first accept the explicit selected-org claim
`urn:zitadel:iam:org:id`. If the token carries role assignments for exactly one
organization, the middleware may infer that organization for single-org service
tokens. Multi-org role claims never select an org implicitly. Only tokens with
no role assignments fall back to `urn:zitadel:iam:user:resourceowner:id`,
`resource_owner`, or `org_id`.

Role extraction only accepts Zitadel's project-qualified claim for the target
service project:

```json
{
  "urn:zitadel:iam:org:project:<project_id>:roles": {
    "admin": {
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

OIDC and customer/API credential service-account setup has several sharp edges:

- Access tokens presented to Go services must be JWTs. Opaque Zitadel access
  tokens fail local JWKS validation. Frontend OIDC applications need
  `OIDC_TOKEN_TYPE_JWT`; machine users need `ACCESS_TOKEN_TYPE_JWT`.
- Customer/API credential client-credentials flows must request an audience
  scope for the target project:
  `urn:zitadel:iam:org:project:id:<project_id>:aud`.
- Customer/API credential callers that need roles in the token must request
  `urn:zitadel:iam:org:projects:roles`. The spelling is plural `projects`.
- Callers that need a resource-owner organization ID in the token must request
  `urn:zitadel:iam:user:resourceowner`.
- `openid` and `profile` are still requested for normal OIDC token shape and
  identity claims.

Repo-owned service-to-service calls do not use Zitadel client credentials.
Those callers use SPIFFE mTLS for HTTP and SPIRE JWT-SVIDs for OpenBao
workload auth.

Services use standard OIDC discovery against the public issuer, for example
`https://auth.<domain>`. The middleware validates the token `iss` claim against
that issuer and uses the provider metadata JWKS URI for key fetches. Single-node
deployments keep this standard issuer path by bind-mounting a service-private
`/etc/hosts` file that resolves `auth.<domain>` to local Caddy
(`127.0.0.1:443`). This keeps Zitadel instance routing, TLS termination,
discovery metadata, and JWT issuer validation on the same origin. A three-node
topology can route `auth.<domain>` to the remote auth origin without changing Go
service configuration.

## External Workload Federation Boundary

GitHub Actions OIDC tokens are acceptable as input evidence for an
identity-service trust-policy match, but they are not Verself access tokens.
The matched organization, permissions, and target audience must come from an
active Verself policy, never from request-body claims. The output credential
must remain a short-lived Zitadel-issued token until the platform intentionally
cuts over every public API service to a first-party multi-issuer trust model.

Do not implement this by storing Zitadel service-account private keys in
identity-service or by adding identity-service as a second JWT issuer behind the
current middleware. Zitadel v4.13 supports RFC 8693 token exchange, including
JWT access-token output, but the documented JWT subject-token path is not a
standalone arbitrary external IdP JWT exchange. It requires the supported
Zitadel actor/JWT-profile flow, so GitHub Actions federation remains blocked
until Zitadel can issue the resulting Verself token directly or the issuer
model is redesigned as a full cutover.

## Service Authorization Flow

The service boundary is:

1. HTTP middleware validates the bearer JWT and attaches identity to context.
2. The Huma operation is registered through the service's secured registration
   helper.
3. Registration attaches `x-verself-iam` metadata and OpenAPI
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

Direct permission claims are accepted only for Verself API credential
tokens carrying both `verself:credential_id` and an organization scope;
normal human OAuth scopes are not permission grants.

API credential direct permissions are issued as exact operation permissions
and must be checked at issuance/roll time against the creating principal's
effective permissions. A member-role caller cannot mint a credential whose
permissions are not held by the member set under the org's current capability
configuration.

Product-scoped opaque credentials, such as source Git HTTPS tokens, are not
Zitadel API credentials. They are issued and verified by secrets-service as
non-retrievable opaque credential resources over SPIFFE-authenticated internal
APIs. The product service owns the customer workflow and projection rows;
secrets-service owns token material, verifier storage, roll/revoke semantics,
and credential audit rows.

Embedded organization widgets are a special cross-service web-session path.
The console frontend owns the interactive OIDC application and stores the
browser session server-side. Before calling `identity-service`, the BFF exchanges
the session access token for an identity-service audience token and forwards that
resource token. `identity-service` validates the JWT locally and reads only
`urn:zitadel:iam:org:project:<identity-service-project-id>:roles`; it does not
resolve role assignments from Zitadel in the request path.

The `x-verself-iam` metadata is documentation and a generation target, not
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
(`console_domain`, `stalwart_domain`). The policy lives
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
assignments for its users; they are not a substitute for Verself operation
policies.

Temporary seed or rehearsal API personas should follow the same model as
customer API credentials:

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
PATCH, Bulk, filter, and `.search` route directly to Zitadel. Verself does
not proxy, re-terminate, or re-implement the User half of the SCIM surface.

Groups, the `groups` field on the User resource, role-bound group provisioning,
and `/Me` are out of scope. Zitadel does not implement Groups in its SCIM
surface, and Verself does not layer a Groups shim over it. Customers
assign the three-role set (`owner`, `admin`, `member`) through the Verself
organization console; the IdP's group state is not consulted. Attempts to push
a `groups` attribute or a `Group` resource through Zitadel's SCIM endpoint are
rejected by Zitadel per its documented scope.

A Verself Groups + role-binding shim is a deliberate future option, not
current scope. If a customer eventually demands group-driven role assignment,
the shape is a Verself `/scim/v2/{orgId}/` endpoint that terminates the
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

Customer API credentials are Verself product resources backed by Zitadel
service accounts. They are not git-provider credentials, webhook secrets, or
human browser sessions. They are also not repo-owned workload identities;
repo-owned services use SPIFFE/SPIRE. The durable Verself resource is
readable as metadata; secret material is returned only on creation or roll and
must never be read back later.

The researched Zitadel pattern is:

- Service accounts are the machine identity model for backend/API access. They
  provide separate audit identity, independent lifecycle, and least-privilege
  role assignment for non-human callers.
- Service-account access tokens presented to Verself services must be JWTs.
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
  credential to an existing Verself-managed service account.
- Store Verself metadata: `credential_id`, `org_id`, Zitadel subject ID,
  display name, status, policy version at issue, auth method, key or secret
  fingerprint, created/updated/revoked timestamps, last-used telemetry, and the
  exact allowed operation permissions.
- List and read metadata without secret material.
- Roll by adding a new key or client secret, returning the new secret material
  once, and retiring the old material after a short grace window when configured.
- Revoke by disabling/removing the Zitadel credential and marking the Forge
  Verself credential row revoked.

Issuance and roll must validate every requested permission against the current
service-declared operation catalog and against the creating principal's
effective permissions. A caller cannot mint a credential with permissions they
do not currently hold. Credential scopes are exact operation permissions such as
`sandbox:execution:read`, `sandbox:execution_schedule:write`,
`sandbox:github_installation:write`, `sandbox:logs:read`, `billing:read`, and
future CI operations such as `ci:workflow:dispatch`.

Token minting uses a Zitadel pre-access-token Action. The signed Action
callback is exposed through Caddy on `auth.<domain>` because Zitadel rejects
loopback/private target URLs. The Action appends `verself:credential_id`,
non-secret credential metadata (`verself:credential_name`,
`verself:credential_fingerprint`, owner id/display, and auth method),
`org_id`, and an exact `permissions` claim from Verself-owned credential
metadata. It must not embed full Verself policy documents into the token.
If the Action cannot resolve an active credential or exact permission set, the
token must not receive Verself direct-permission claims; services already
fail closed because direct permission claims are only accepted when the
credential marker and organization scope are present.

Product services must continue to verify issuer, signature, expiration, and
audience, but audience is not authorization. Zitadel can place requested
audience values into tokens; services must still require either a target-project
role assignment mapped through product policy or a Verself API credential
marker plus exact operation permission. Human OAuth scopes are not product
permission grants.

Revocation is not instant for already-issued JWTs when services validate tokens
locally through JWKS. Keep API credential access-token lifetimes short. Add
token introspection or a credential denylist only if live rehearsal shows the
revocation window is unacceptable for customer-facing usage.

## Seeded Rehearsal Personas

`seed-system.yml` provisions three long-lived rehearsal personas for operators
and agents. Each persona has a human browser login and a matching Zitadel
machine user for customer/API rehearsal. These machine users are not the
repo-owned service-to-service identity model:

| Persona | Human login | Machine user | Organization | Built-in roles |
|---|---|---|---|---|
| `platform-admin` | `agent@<domain>` | `assume-platform-admin` | platform | sandbox-rental `owner`; identity-service `owner`; `forgejo_admin`, `mailbox_user` |
| `acme-admin` | `acme-admin@<domain>` | `assume-acme-admin` | Acme Corp | human/browser sandbox-rental `owner`; machine sandbox-rental `admin`; human/browser identity-service `owner`; machine identity-service `admin` |
| `acme-member` | `acme-user@<domain>` | `assume-acme-member` | Acme Corp | sandbox-rental `member`; identity-service `member` |

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

The default output path is `smoke-artifacts/personas/<persona>.env`, written `0600`.
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
| console / `sandbox-rental-service` | platform `owner` | Acme browser `owner`, machine `admin` | Acme `member` | Zitadel browser login and `SANDBOX_RENTAL_ACCESS_TOKEN` |
| console organization surface / `identity-service` | browser and machine `owner` | browser `owner`, machine `admin` | Acme `member` | BFF token exchange and `IDENTITY_SERVICE_ACCESS_TOKEN` |
| `mailbox-service` (webmail folded into console; frontend path TBD) | `mailbox_user`, bound to `agents` | none | none | Zitadel browser login and `MAILBOX_SERVICE_ACCESS_TOKEN` |
| Forgejo OIDC login | `forgejo_admin` | none | none | Zitadel browser login and `FORGEJO_OIDC_ACCESS_TOKEN` |
| ClickHouse | founder access only | none | none | `CLICKHOUSE_OPERATOR_COMMAND`, currently `aspect db ch query --query='...'` |
| Forgejo provider API automation | founder access only | none | none | `FORGEJO_OPERATOR_CREDENTIAL`, currently the remote `forgejo-automation` token |
| Stalwart direct JMAP/IMAP/SMTP | not a persona grant | not a persona grant | not a persona grant | use `mailbox-service` (webmail UI pending console absorption) or explicit founder mail tooling |
| `billing-service` direct API | service-to-service only | service-to-service only | service-to-service only | customer-facing billing access goes through `sandbox-rental-service` |

The platform admin persona intentionally does not export the Zitadel admin PAT,
any ClickHouse password, or Forgejo automation token. ClickHouse access
remains the founder AXL wrapper (`aspect db ch query --query='...'`) because it is not a
Zitadel resource yet. Forgejo API automation remains provider-native
`forgejo-automation` until Forgejo OIDC group/role claims are proven for the
interactive UI path and a separate provider API credential model is introduced.

## External Integration Credentials

Zitadel answers "who is allowed to configure this integration?" It does not
answer "how does Verself authenticate to the customer's git host?"

Inbound webhooks should use provider-native verification: HMAC for GitHub and
similar hosts; constant-time token comparison for providers that use shared
tokens; provider-specific signature validation where available. The verified
credential must map to a Verself integration row that carries `org_id`.

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
  Verself-owned state.
- Private external-provider access tokens and deploy keys are integration
  secrets, not user sessions and not Zitadel role assignments.
- Customer API credentials are Verself-managed Zitadel service-account
  credentials. Secret material is visible only at creation/roll; metadata
  remains readable for audit, rotation, and revocation.
- Repo-owned workload identity is SPIFFE/SPIRE. Reintroducing shared bearer
  tokens, Zitadel client secrets for internal service calls, or OpenBao as a
  workload identity source is a security regression.

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
- Single-node services use a private `/etc/hosts` bind mount for OIDC discovery
  through local Caddy. A three-node topology should remove that host override and
  allow service egress to the remote auth origin.
- Customer API credential tables exist, and services recognize
  `verself:credential_id`, but the create/list/read/roll/revoke lifecycle
  and Zitadel pre-access-token Action are not implemented yet.
- Zitadel SCIM 2.0 is preview and feature-flagged; Users only, no Groups, no
  `/Me`, no ETag, Bulk capped at 100 operations. The Verself position
  (terminate at Zitadel, defer Groups) assumes the preview remains stable or
  graduates; if Zitadel removes the surface, a Verself SCIM server becomes
  required, not optional.

## Source Notes

Local code anchors:

- `src/auth-middleware/auth.go`: JWT verification, OIDC discovery, identity claim
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
  catalog, `x-verself-iam`, sandbox role bundles, direct-scope permission
  checks, idempotency and rate-limit hooks.
- `src/auth-middleware/workload`: SPIFFE Workload API source, mTLS peer
  authorization, and workload identity trace spans.
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

Zitadel service accounts are the recommended customer/API credential machine
identity model. They support private-key JWT, client credentials, and personal
access tokens; the Zitadel guidance recommends private-key JWT for most
service-account scenarios and treats PATs as convenient but long-lived bearer
tokens:
<https://zitadel.com/docs/guides/integrate/service-accounts/authenticate-service-accounts>

Zitadel machine users must be configured for JWT access tokens when Verself
services validate tokens locally through JWKS:
<https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.CreateUser>

Zitadel's user API supports machine-user keys that are returned once and can be
removed by key ID, matching the Verself "secret visible only on create/roll"
credential contract:
<https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.AddKey>
<https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.RemoveKey>

Zitadel's audience guidance says `aud` must not be the only authorization check;
services still need roles, scopes, or custom claims:
<https://help.zitadel.com/security-best-practices-validating-audience-aud-claims-in-zitadel-access-tokens>

Zitadel Actions can append permission claims during pre-access-token issuance.
Verself uses that as the mechanism for `verself:credential_id`,
non-secret credential audit metadata, and exact API credential permissions, not
as a place to store full product policy:
<https://help.zitadel.com/extend-authorization-in-zitadel-with-organization-metadata-preaccesstoken-action->

Zitadel token exchange implements RFC 8693 and can return JWT access tokens, but
the documented JWT subject-token path is limited to the supported actor/JWT
profile flow rather than arbitrary external IdP JWTs:
<https://zitadel.com/docs/guides/integrate/token-exchange>

GitHub Actions OIDC tokens use issuer
`https://token.actions.githubusercontent.com` and include immutable policy-match
claims such as `repository_id`, `repository_owner_id`, `jti`, `ref`, and
`workflow_ref`:
<https://docs.github.com/en/actions/reference/security/oidc>
<https://token.actions.githubusercontent.com/.well-known/openid-configuration>

Zitadel SCIM 2.0 server. Preview, User resource only, org-scoped URL, Bulk
capped at 100 operations, filter vocabulary enumerated. No Groups, no `/Me`,
no ETag. This is the surface Verself points customer IdPs at directly:
<https://zitadel.com/docs/apis/scim2>
<https://zitadel.com/docs/guides/manage/user/scim2>
<https://zitadel.com/docs/guides/integrate/scim-okta-guide>

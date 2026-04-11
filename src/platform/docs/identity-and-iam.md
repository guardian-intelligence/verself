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

Forge Metal owns product policy. The platform ships working default role
bundles and policy documents, and customers can later edit those policy
documents through a constrained first-party surface. Customers should not have
to hand-author raw IAM documents to make a default install usable.

Customer configuration starts with role assignment: invite users, add them to
organizations, and assign built-in roles. Custom role-to-permission bundles come
after the default model is usable.

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

The raw Zitadel Management Console remains an operator/admin identity tool at
`/ui/console`; it is not the long-term customer product console. A future
standalone organization route on the auth host is still possible, for example
`https://auth.<domain>/organization`, but it should be another embedding of the
same component and service API rather than an iframe, a Zitadel Console
extension, or a separate hand-built shell.

Zitadel custom login UI support is relevant to replacing or branding the login
flow, not to product policy editing. Zitadel Actions are useful for workflow
hooks and token/role automation, but product policy documents remain Forge
Metal-owned resources.

## Policy Split

Service operation catalogs:

- Live with the service that enforces them.
- Are exposed through OpenAPI `x-forge-metal-iam`.
- Include operation ID, permission, resource, action, org scope, rate-limit
  class, idempotency semantics, and audit event.
- The service-local Huma metadata is not an `apiwire` DTO. Public responses that
  return an operation catalog to customers are normal request/response wire data
  and should use `apiwire`.

Zitadel role assignments:

- Prove who the caller is and which organization/project roles they hold.
- Are the membership plane for users and service accounts.
- Are surfaced to Go services through validated JWT claims or, when needed,
  through Zitadel APIs.

Forge Metal policy documents:

- Map role keys to service-declared permissions.
- Provide the product-level model the organization console edits.
- Should be stored as Forge Metal-owned state, not embedded into Zitadel tokens
  as full policy documents.
- May be evaluated locally from service code for built-in defaults, then from a
  cached shared policy source once custom customer policies span multiple
  services.

For v1, `identity-service` owns the organization policy document and seeds the
built-in Zitadel roles it can assign. Other services continue to own and enforce
their operation catalogs. As more services expose customer-editable permissions,
the policy document should reference service-declared permissions, not copied
ad hoc permission strings.

## Runtime Token Contract

Go services validate bearer JWTs with `src/auth-middleware`. The middleware
checks the token issuer and audience, verifies the signature from Zitadel JWKS,
extracts identity fields into request context, and leaves operation-specific
authorization to the service.

The runtime identity object is:

- `Subject`: Zitadel user or service-account ID from `sub`.
- `OrgID`: active organization/resource-owner ID when present.
- `Roles`: flat role keys for the current single-org path.
- `RoleAssignments`: structured project-role assignments, including project ID,
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

Role extraction accepts flat `roles` and `role` claims for simple fixtures, but
the production path is Zitadel's project-role claim:

```json
{
  "urn:zitadel:iam:org:project:<project_id>:roles": {
    "sandbox_org_admin": {
      "<org_id>": "<org_name>"
    }
  }
}
```

The middleware also accepts the unqualified
`urn:zitadel:iam:org:project:roles` claim. New provisioning should prefer the
project-qualified claim. The requested OAuth scope uses a different spelling:
`urn:zitadel:iam:org:projects:roles`.

When a service needs to authorize an organization-scoped operation, it must use
role assignments whose `OrganizationID` matches the request identity's `OrgID`.
Flat role fallback exists for in-process harnesses and early single-org callers;
it is not the target multi-org model.

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
permission check is intentionally small: a caller is allowed if the identity has
a direct OAuth scope equal to the permission, or if a role assignment for the
current organization maps through the Forge Metal policy document to that
permission.

Direct scopes are appropriate for tightly scoped service accounts and future
machine-to-machine grants. Human users should normally receive built-in roles
first, then customer-editable Forge Metal policy bundles once that surface
exists.

Embedded organization widgets are a special cross-service web-session path.
The rent-a-sandbox frontend owns the interactive OIDC application and forwards
the server-owned access token to `identity-service`, but Zitadel may only assert
the frontend application's project roles in that access token. `identity-service`
therefore treats the token as proof of subject, organization, issuer, and
audience, then resolves the caller's identity-service project role assignment
from Zitadel before evaluating the Forge Metal policy document. This keeps the
authorization decision at the target service boundary without requiring browser
access to bearer tokens or a service-local product console.

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

## Seeded Rehearsal Personas

`seed-system.yml` provisions three long-lived rehearsal personas for operators
and agents. Each persona has a human browser login and a matching machine user
for API rehearsal:

| Persona | Human login | Machine user | Organization | Built-in roles |
|---|---|---|---|---|
| `platform-admin` | `agent@<domain>` | `assume-platform-admin` | platform | `sandbox_org_admin`, `identity_org_admin`, `letters_admin`, `forgejo_admin`, `mailbox_user` |
| `acme-admin` | `acme-admin@<domain>` | `assume-acme-admin` | Acme Corp | `sandbox_org_admin`, `identity_org_admin` |
| `acme-member` | `acme-user@<domain>` | `assume-acme-member` | Acme Corp | `sandbox_org_member`, `identity_org_member` |

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
`MAILBOX_SERVICE_ACCESS_TOKEN`. These tokens are rehearsal credentials, not a
new persistence layer; regenerate them from Zitadel when they expire.

Current access coverage:

| Surface | `platform-admin` | `acme-admin` | `acme-member` | Credential path |
|---|---|---|---|---|
| rent-a-sandbox / `sandbox-rental-service` | platform `sandbox_org_admin` | Acme `sandbox_org_admin` | Acme `sandbox_org_member` | Zitadel browser login and `SANDBOX_RENTAL_ACCESS_TOKEN` |
| rent-a-sandbox organization surface / `identity-service` | platform `identity_org_admin` | Acme `identity_org_admin` | Acme `identity_org_member` | Zitadel browser login and `IDENTITY_SERVICE_ACCESS_TOKEN` |
| webmail / `mailbox-service` | `mailbox_user`, bound to `agents` | none | none | Zitadel browser login and `MAILBOX_SERVICE_ACCESS_TOKEN` |
| Letters | `letters_admin` | none | none | Zitadel browser login and `LETTERS_ACCESS_TOKEN` |
| Forgejo OIDC login | `forgejo_admin` | none | none | Zitadel browser login and `FORGEJO_OIDC_ACCESS_TOKEN` |
| ClickHouse | operator access only | none | none | `CLICKHOUSE_OPERATOR_COMMAND`, currently `make clickhouse-query` |
| Forgejo provider API automation | operator access only | none | none | `FORGEJO_OPERATOR_CREDENTIAL`, currently the remote `forgejo-automation` token |
| Stalwart direct JMAP/IMAP/SMTP | not a persona grant | not a persona grant | not a persona grant | use `mailbox-service`/webmail or explicit operator mail tooling |
| `billing-service` direct API | service-to-service only | service-to-service only | service-to-service only | customer-facing billing access goes through `sandbox-rental-service` |

The platform admin persona intentionally does not export the Zitadel admin PAT,
ClickHouse password, or Forgejo automation token. ClickHouse access remains the
operator Make wrapper (`make clickhouse-query`) because it is not a Zitadel
resource yet. Forgejo API automation remains provider-native
`forgejo-automation` until Forgejo OIDC group/role claims are proven for the
interactive UI path and a separate provider API credential model is introduced.

## External Integration Credentials

Zitadel answers "who is allowed to configure this integration?" It does not
answer "how does Forge Metal authenticate to the customer's git host?"

Inbound webhooks should use provider-native verification: HMAC for Forgejo,
GitHub, and similar hosts; constant-time token comparison for providers that use
shared tokens; provider-specific signature validation where available. The
verified credential must map to a Forge Metal integration or webhook endpoint
row that carries `org_id`.

Private repository access is a separate credential plane. A webhook proves that
an event was delivered through a configured endpoint; it does not grant clone
access for private code. Private repo imports and future service-owned CI fetches
need an org-owned git credential such as a deploy key, provider app
installation token, or host-specific machine token. Store those as integration
secrets scoped to organization, provider, provider host, and minimal repository
or installation permissions. Do not reuse a human user's browser session token
for background git fetches.

Manual webhook endpoints are the right low-ceremony path for Codeberg, Forgejo,
GitHub, and GitLab. Provider apps are a later automation layer that can create
the same underlying integration credentials and webhook endpoints
programmatically.

## Invariants

- Services enforce authorization after validating Zitadel JWTs. Frontends only
  gate UX.
- Operation permission is necessary but not sufficient; handlers still enforce
  organization/resource ownership from storage.
- Built-in defaults must remain enough for a non-technical operator to run the
  platform.
- Advanced policy editing must be constrained to service-declared permissions.
- The platform dogfoods the same policy and billing abstractions, with internal
  unlimited usage modeled as an adjustment rather than as a bypass.
- External webhooks and provider callbacks authenticate through their own
  verification protocols, then resolve organization context from Forge
  Metal-owned state.
- Private external-provider access tokens and deploy keys are integration
  secrets, not user sessions and not Zitadel role assignments.

## Current Limitations

- Active organization selection is still mostly represented by the token's
  resource owner organization. A richer multi-org UX needs an explicit active
  organization switch and services must continue filtering structured role
  assignments by that organization.
- Built-in identity role-to-permission bundles currently live in
  `identity-service`. Other services still need to publish their operation
  catalogs into the policy-editing surface before customer-editable permissions
  can span the full product.
- Stalwart direct JMAP/IMAP/SMTP auth remains outside the Zitadel service-auth
  model. The repo-owned `mailbox-service` HTTP API uses Zitadel bearer tokens
  plus `mailbox_bindings`, but direct mail protocol credentials are still
  Stalwart-owned.
- The single-node JWKS loopback path is not the three-node design. Remote
  Zitadel requires topology-aware JWKS discovery and service egress policy.

## Source Notes

Local code anchors:

- `src/auth-middleware/auth.go`: JWT verification, split JWKS, identity claim
  extraction, role-assignment extraction.
- `src/identity-service/internal/api/policy.go`: operation policy metadata,
  policy-document-backed permission enforcement, idempotency and rate-limit
  hooks for the organization-management API.
- `src/identity-service/internal/identity/catalog.go`: built-in identity
  operation catalog and default role-to-permission policy bundles.
- `src/sandbox-rental-service/internal/api/policy.go`: operation policy
  catalog, `x-forge-metal-iam`, sandbox role bundles, direct-scope permission
  checks, idempotency and rate-limit hooks.
- `src/sandbox-rental-service/internal/serviceauth/client_credentials.go`:
  service-to-service client-credentials scopes and single-node Host override.
- `src/platform/ansible/playbooks/tasks/upsert_role_assignment.yml`: Zitadel
  Authorization service create/update calls for project role assignments.
- `src/platform/ansible/playbooks/tasks/seed-forgejo-repos.yml`: live seed flow
  for JWT machine users, role assignment, and client-credentials token request.

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

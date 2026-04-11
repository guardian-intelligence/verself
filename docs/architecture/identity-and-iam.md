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

## Organization Surface

The target product surface is a Forge Metal organization console on the auth
host, for example `https://auth.<domain>/organization` or
`https://auth.<domain>/organizations`. The exact route should be chosen around
Zitadel's reserved prefixes, especially `/ui/console`, `/ui/v2/login`,
`/oauth/v2`, `/oidc/v1`, `/v2`, `/management/v1`, and the gRPC service paths.

The raw Zitadel Management Console remains an operator/admin identity tool at
`/ui/console`; it is not the long-term customer product console. The customer
organization console should be a Forge Metal app backed by Forge Metal server
functions and Zitadel APIs. A Clerk-like embedded experience should be a
first-party shared component package in `src/viteplus-monorepo`, not an iframe
or extension of the Zitadel Console.

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
- Are not `apiwire` DTOs; they describe service behavior rather than
  request/response data.

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

For v1, keep service-local policy maps and seed built-in Zitadel roles. When a
second service needs customer-editable custom bundles, introduce a shared
policy-contract package or service. Do not move these contracts into `apiwire`;
`apiwire` remains for request/response wire data.

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

## Source Notes

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

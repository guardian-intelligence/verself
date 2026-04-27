# Public Origins

Verself exposes four public origin classes. Origin shape is part of the
product contract because it controls browser CSP boundaries, public API
documentation, SDK/CLI base URLs, WAF policy, and incident isolation.

## Origin Classes

| Origin | Owner | Purpose |
|---|---|---|
| `<domain>` | Platform docs frontend | Public product docs, API reference, and legal policy tree. |
| `console.<domain>` | Console frontend | Authenticated browser product console. |
| `<service>.api.<domain>` | Owning Go service | Customer, SDK, and CLI HTTP APIs. |
| Protocol origins | Backing protocol service | Non-HTTP-product or protocol-native surfaces such as Git, JMAP, SMTP, and S3. |

The root `verself_domain` is the product apex. Guardian Intelligence company
surfaces live on `company_domain`, outside the product origin tree. The console
is not a marketing surface and does not own public API paths.

## API Origins

Each public Go service gets a service-owned API origin:

| Origin | Service |
|---|---|
| `billing.api.<domain>` | `billing-service` |
| `sandbox.api.<domain>` | `sandbox-rental-service` |
| `identity.api.<domain>` | `identity-service` |
| `profile.api.<domain>` | `profile-service` |
| `notifications.api.<domain>` | `notifications-service` |
| `projects.api.<domain>` | `projects-service` |
| `governance.api.<domain>` | `governance-service` |
| `secrets.api.<domain>` | `secrets-service` |
| `mail.api.<domain>` | `mailbox-service` HTTP API |
| `source.api.<domain>` | `source-code-hosting-service` |
| `object-storage.api.<domain>` | planned customer object-storage control API |

Service API paths remain under `/api/v1/...`. The service subdomain identifies
the owning product plane; the path identifies versioned resources inside that
plane. Do not introduce a shared `api.<domain>/<service>/...` gateway.

OpenAPI `servers` entries must use the canonical API origin for public specs.
Generated Go, TypeScript, and future CLI clients should accept a base URL, but
the documented hosted default is the service API origin.

## Console Boundary

Browser code in `console.<domain>` must not call service API origins directly.
The console uses TanStack Start server functions as the browser-facing boundary:
browser requests stay same-origin, server functions read the server-owned
session, attach service bearers, and call the appropriate `<service>.api`
origin or loopback service URL.

The console CSP should keep `connect-src 'self'` unless a specific browser
protocol requires a documented exception. This preserves bearer isolation and
lets API origins use stricter API-only security headers.

## Protocol Origins

Protocol origins are not generic API hosts:

| Origin | Backing service | Purpose |
|---|---|---|
| `git.<domain>` | `source-code-hosting-service` | Git smart HTTP. Forgejo remains a headless backing service behind source-code-hosting-service. |
| `auth.<domain>` | Zitadel | OIDC, SAML, login, and IdP administration. |
| `mail.<domain>` | Stalwart | SMTP/JMAP protocol surface. |
| `dashboard.<domain>` | Grafana | Operator observability UI. |
| `temporal.<domain>` | Temporal Web | Operator workflow UI. |

When a protocol surface also has a Verself product API, the API lives on
`<service>.api.<domain>` and not on the protocol origin.

## Implementation Source Of Truth

`src/cue-renderer` is the desired-state graph, rendered to typed Ansible
artifacts under `src/platform/ansible/group_vars/all/generated/`. Public API
origins, Caddy routes, DNS records, endpoint ports, and interface metadata
derive from that graph instead of a separate flat topology artifacts.

Required graph shape for public APIs:

```cue
components: billing: {
	endpoints: public_http: {protocol: "http", port: 4242, exposure: "loopback"}
	interfaces: public_api: {
		kind:        "huma_api"
		endpoint:    "public_http"
		path_prefix: "/api/v1"
		auth:        "zitadel_jwt"
	}
}
routes: [{kind: "public_api_origin", gateway: "public_caddy", host: "billing.api", to: {component: "billing", interface: "public_api"}}]
```

Exceptions stay explicit. Stripe webhooks, Git smart HTTP, Electric shapes,
Zitadel login routes, and future S3-compatible endpoints are not normal service
API origins.

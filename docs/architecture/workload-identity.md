# Workload Identity

Verself uses SPIFFE/SPIRE as the workload identity authority for repo-owned
services. SPIRE asserts workload identity. Product services and resource planes
enforce authorization against that identity.

The plane assignment below is normative. Every sentence in the rest of this
document, and in any other repo-owned doc, derives from it.

| Plane | Authority |
| --- | --- |
| Human, organization, and API credential identity | Zitadel |
| Repo-owned workload identity | SPIRE |
| Secrets/KMS product API | `secrets-service` |
| Secrets/KMS resource plane | OpenBao |
| Business and forensic audit | `governance-service` |
| Product state | PostgreSQL, ClickHouse, TigerBeetle |

OpenBao is not the source of truth for workload identity. OpenBao is a relying
party that accepts SPIRE-issued identity documents and maps SPIFFE subjects to
OpenBao policies.

## Trust Domain

The hosted trust domain is operator-configurable at install and immutable after
SPIRE holds initialized data:

```yaml
verself_domain: verself.sh
spire_trust_domain: "spiffe.{{ verself_domain }}"
spire_bundle_endpoint_bind_address: 127.0.0.1
spire_bundle_endpoint_bind_port: 8082
spire_jwt_bundle_endpoint_url: "https://{{ spire_bundle_endpoint_bind_address }}:{{ spire_bundle_endpoint_bind_port }}"
spire_jwt_issuer_url: "{{ spire_jwt_bundle_endpoint_url }}"
```

If `spire_trust_domain` changes while SPIRE holds initialized data, deploy
fails with tagged error `workload_identity.trust_domain_mutation_forbidden`.

The trust domain is a root of trust and a namespace. It MUST NOT encode
deployment lane, node count, host name, region, or product name. Those facts
live in service attributes, deploy metadata, registration selectors, or node
IDs. Valid forms:

```text
spiffe.verself.sh
spiffe.example.com
workload.example.com
```

## Identity Shape

Service identities:

```text
spiffe://spiffe.verself.sh/svc/identity-service
spiffe://spiffe.verself.sh/svc/governance-service
spiffe://spiffe.verself.sh/svc/billing-service
spiffe://spiffe.verself.sh/svc/secrets-service
spiffe://spiffe.verself.sh/svc/sandbox-rental-service
spiffe://spiffe.verself.sh/svc/mailbox-service
spiffe://spiffe.verself.sh/svc/nats
spiffe://spiffe.verself.sh/svc/otelcol
spiffe://spiffe.verself.sh/svc/grafana
spiffe://spiffe.verself.sh/svc/clickhouse-server
spiffe://spiffe.verself.sh/svc/clickhouse-operator
spiffe://spiffe.verself.sh/svc/temporal-server
```

These identities cover both customer-facing product services and
operator-owned infrastructure services that terminate SPIFFE-authenticated
traffic. A service may use an in-process Workload API source or
`spiffe-helper`-rendered files depending on whether its runtime can consume
SPIFFE natively.

Node identities:

```text
spiffe://spiffe.verself.sh/node/<hostname>
```

Operator tooling identities:

```text
spiffe://spiffe.verself.sh/ops/admin-cli
```

`ops/admin-cli` is the break-glass operator CLI identity used to read from
OpenBao via JWT-SVID during manual recovery or inspection. It is not used by
any repo-owned service at runtime.

SPIRE registration entries are declared by Ansible from repo-owned inventory.
Services MUST NOT self-register identities. OpenBao auth roles are generated
from the same inventory. Drift between SPIRE registrations and OpenBao role
subject bindings is a convergence failure, tagged
`workload_identity.spiffe_bao_drift`.

## Trust Domain Exclusions

Two components are deliberately outside the SPIFFE trust domain:

- `vm-orchestrator` runs as a privileged host daemon. It talks to services
  over its gRPC Unix socket and authenticates peers through filesystem ACLs on
  the socket path. See [`src/vm-orchestrator/AGENTS.md`](../../src/vm-orchestrator/AGENTS.md).
- `vm-guest-telemetry` runs inside Firecracker guest VMs and streams over
  vsock 10790. Guests are never SPIFFE peers; the vsock boundary is the trust
  edge.

Neither component issues nor consumes SVIDs. Attempts to register a SPIFFE ID
under `/host/` or `/guest/` are rejected by inventory validation.

## Systemd-Native Attestation

The deployment is systemd-native on bare metal. SPIRE Agent exposes the
Workload API over a Unix domain socket to service users in the workload socket
group. The Unix workload attestor derives selectors from kernel process
metadata at Workload API call time.

Single-node bootstrap uses SPIRE join-token node attestation. The token is
generated at agent start, written only under `/run/spire-agent/private`, and
deleted by the unit after startup. The smoke test asserts no join token remains in
credstore or runtime state. A residual token is tagged
`workload_identity.join_token_residual`.

## Internal Service Calls

Repo-owned internal HTTP calls use SPIFFE X.509-SVID mTLS with exact peer ID
authorization. Authorized edges:

```text
sandbox-rental-service -> billing-service
sandbox-rental-service -> governance-service
sandbox-rental-service -> secrets-service
source-code-hosting-service -> sandbox-rental-service
source-code-hosting-service -> secrets-service
secrets-service        -> billing-service
secrets-service        -> governance-service
identity-service       -> governance-service
billing-service        -> governance-service
mailbox-service        -> governance-service
```

Repo-owned service-to-service calls MUST NOT present shared bearer tokens or
Zitadel client-credential JWTs. Reintroducing either is a security regression;
see Failure Semantics.

Public customer and user API boundaries validate Zitadel JWTs through
`auth-middleware`. SPIFFE is for workload identity; Zitadel is for human,
organization, and API credential identity.

## Request Context Propagation

A request that crosses from a Zitadel-authenticated public API into an
internal SPIFFE mTLS hop carries two distinct principals:

- **Caller.** The workload making the internal call. Authenticated by the
  mTLS peer ID. Non-forgeable.
- **Origin subject.** The end user, customer API credential, or service
  workflow actor the request is being processed for.

Internal APIs use generated `internalclient` packages and carry origin context
as explicit typed fields (`org_id`, `actor_id`, external task IDs, and
idempotency keys) unless the downstream service must independently re-enforce
customer IAM from the original token. If the downstream service does need that
customer IAM decision, the caller forwards the original Zitadel JWT and the
downstream service re-validates it through `auth-middleware` before using any
subject claim.

Governance audit rows written by downstream services carry both the caller
SPIFFE ID and the origin subject. The two fields are independent and both are
required on internal calls. A row with a populated origin subject and an empty
caller SPIFFE ID is a data-integrity violation.

## OpenBao Workload Auth

Repo-owned workloads that authenticate directly to OpenBao do so by
exchanging a SPIRE JWT-SVID for a short-lived OpenBao token:

```text
issuer:   https://127.0.0.1:8082
audience: openbao
subject:  spiffe://spiffe.verself.sh/svc/<service-name>
mount:    auth/spiffe-jwt
```

SPIRE server exposes a loopback-only HTTPS bundle endpoint for OpenBao JWT
validation. The endpoint serves the trust bundle in SPIFFE/JWKS-compatible
JSON at `/`, uses a private local CA pinned in OpenBao's JWT auth config, and
is not a public federation endpoint. A workload fetches a JWT-SVID for
audience `openbao`, logs in to OpenBao, and receives an OpenBao token
constrained by the policies bound to its SPIFFE subject. The JWT-SVID is used
for the login exchange; subsequent KV and Transit requests use the returned
OpenBao token. Repo-owned service-to-service calls remain on SPIFFE
X.509-SVID mTLS.

OpenBao role bindings are generated from the same repo-owned inventory that
produces SPIRE registrations. Drift is tagged
`workload_identity.spiffe_bao_drift` and fails convergence.

The OpenBao customer path is separate. Customer and API-credential requests
present Zitadel JWTs and are authorized through the customer secrets/KMS
model.

## Database Auth

Repo-owned services on the single bare-metal host authenticate to PostgreSQL
through local Unix socket peer authentication with `pg_ident.conf` mappings.
PostgreSQL peer auth obtains the client operating-system user from the kernel
and is supported only for local connections, matching the systemd service-user
boundary.

PostgreSQL `trust` authentication is prohibited. Service PostgreSQL password
DSNs are prohibited where peer auth covers the service.

**Current state.** ClickHouse client authentication is certificate-backed via
SPIFFE X.509-SVIDs on the secure native protocol. TigerBeetle client
connections remain outside peer auth and still require their own credential
model.

**Target state.** Password-backed clients are eliminated by wrapping them in
SPIFFE-authenticated connection brokers or by moving to drivers that accept
local socket peer auth.

## Runtime Provider Secrets

Runtime third-party provider credentials live as org-scoped secrets in the
platform organization and are resolved through `secrets-service` over SPIFFE
mTLS. Repo-owned services do not read provider credentials from OpenBao
directly. The current platform-org secret names are:

```text
billing-service.stripe.secret_key
billing-service.stripe.webhook_secret
mailbox-service.resend.api_key
mailbox-service.stalwart.admin_password
sandbox-rental-service.github.private_key
sandbox-rental-service.github.webhook_secret
sandbox-rental-service.github.client_secret
```

`mailbox-service.stalwart.admin_password` holds only the Stalwart Management
API admin password. Stalwart's internal mailbox-user credentials (ceo/agents)
remain SOPS-sealed bootstrap material under "human mailbox protocol passwords"
below, because mail clients authenticate to Stalwart over SMTP/IMAP/JMAP and
those protocols do not speak SPIFFE.

**Persistent bootstrap material.** The following remain outside SPIFFE and
OpenBao runtime reads and are managed through SOPS and systemd
`LoadCredential=`:

```text
Zitadel masterkey
OpenBao unseal and recovery material
OpenBao root bootstrap token (retained only for operator ceremony)
governance audit HMAC key
database admin password
external provider bootstrap backup values
human mailbox protocol passwords (while direct mail protocol auth exists)
```

SOPS is bootstrap and disaster-recovery material for operator-owned external
credentials. SOPS is not the runtime distribution path for repo-owned
services.

## Product Opaque Credentials

Product-issued bearer material that should not be retrievable after generation
is modeled as an opaque credential in `secrets-service`, not as a retrievable
secret. Repo-owned services call secrets-service over SPIFFE mTLS with generated
internal clients to create, verify, roll, and revoke that material while
retaining their own product-specific state projections.

The first consumer is `source-code-hosting-service` for Git HTTPS credentials:
source owns repository UX and PostgreSQL projections; secrets-service owns
token generation, OpenBao Transit HMAC verifier storage, status, expiry, scope
verification, and credential audit rows.

## Failure Semantics

Services fail closed if they cannot obtain required SVID material at startup.

- X.509-SVID TTL: `1h`. Refresh: SPIRE streaming Workload API, no polling.
- JWT-SVID TTL: `5m` per audience. Refresh: cached per audience, refreshed at
  `TTL/2`.
- Startup readiness: fails if no X.509-SVID is available within `30s` of
  systemd unit start. Tagged error:
  `workload_identity.svid_bootstrap_timeout`.
- Streaming refresh stall: readiness flips to `NOT READY` at `TTL - 2m` if
  refresh has not succeeded. Tagged error:
  `workload_identity.svid_refresh_stalled`.
- OpenBao JWT-SVID login has no static client-secret fallback.

Any reintroduction of static internal bearer tokens, Zitadel machine-user
client secrets for repo-owned service calls, or PostgreSQL password DSNs for
peer-auth-capable services is a security regression.

## Trust Bundle Rotation

SPIRE server rotates the trust bundle on its default cadence. Agents receive
rotation via the Workload API stream. Services consuming X.509-SVIDs via the
SPIFFE Workload API pick up bundle rotation without restart.

File-backed consumers use the same operating contract everywhere:
`spiffe-helper` runs as a systemd-managed sibling process, renders SVID and
bundle material to a private runtime directory, and wakes the workload using
the narrowest reload primitive that workload supports. Current consumers:

| Consumer | Rotation contract |
| --- | --- |
| NATS | `spiffe-helper` writes server SVID/key/bundle and sends `SIGHUP` via `/run/nats/nats.pid`; `nats-server` reloads in place. |
| ClickHouse server | `spiffe-helper` writes the SPIRE trust bundle consumed by `<openSSL><server><caConfig>` and sends `SIGHUP` via `/run/clickhouse-server/clickhouse-server.pid`; ClickHouse reloads in place. |
| ClickHouse operator client | `spiffe-helper` keeps client SVID/key/bundle fresh; each `clickhouse-client` invocation reads current files. |
| OTel collector | `spiffe-helper` keeps ClickHouse client SVID/key/bundle fresh; exporter TLS uses `reload_interval: 60s`. |
| Grafana | `spiffe-helper` keeps ClickHouse client SVID/key/bundle fresh and runs the datasource provisioning refresh command on renewal. |

`make spiffe-rotation-smoke-test` verifies the file-backed rotation contract
without a canary dependency: it reloads NATS and ClickHouse in place,
asserts stable PIDs and healthy post-reload queries, verifies helper
configuration for every file-backed consumer, and asserts the smoke-test spans
and ClickHouse query-log evidence in ClickHouse.

## Federation Scope

Cross-trust-domain federation is out of scope for the single-node topology
and the three-node topology target. Customer SPIRE trust domains are not
federated into `spiffe.verself.sh`. Customer workloads
authenticate to Verself through customer-facing APIs with Zitadel
credentials, not as SPIFFE peers.

## Observability

Workload identity is operator-visible through
`make observe WHAT=workload-identity`. The surface exposes:

- SPIRE server and bundle endpoint state, plus SPIRE agent systemd state.
- Workload API socket ownership and reachability.
- Desired vs live SPIRE registration entries.
- Per-service SVID status: SPIFFE ID, issuer, expiration, and TTL remaining.
- JWT-SVID fetch results by service and audience.
- OpenBao JWT login results by subject, role, audience, and request ID.
- mTLS edge table: caller SPIFFE ID, callee SPIFFE ID, route, status, and
  failure reason.
- Governance audit rows grouped by `actor_spiffe_id`.
- Credential inventory diff: expected remaining bootstrap secrets vs
  unexpected runtime secrets.

Required spans:

```text
auth.spiffe.source.init
auth.spiffe.mtls.client
auth.spiffe.mtls.server
auth.spiffe.jwt_svid.fetch
secrets.bao.jwt_svid.login
governance.audit.append
```

Healthy internal billing call:

```text
sandbox-rental-service: sandbox.billing.usage.record
  auth.spiffe.mtls.client
    billing-service: auth.spiffe.mtls.server
      billing.usage.record
        governance.audit.append
```

Healthy OpenBao service auth:

```text
secrets-service: secrets.secret.inject
  auth.spiffe.jwt_svid.fetch
  secrets.bao.jwt_svid.login
  secrets.bao.kv.get
  governance.audit.append
```

Smoke-test queries assert:

- **every** `auth.spiffe.mtls.server` span carries a non-empty `spiffe.peer_id`
  attribute, and the peer ID resolves to an identity in the canonical
  registration inventory;
- no internal bearer-token spans on repo-owned service-to-service routes;
- no missing `spiffe.peer_id` on internal mTLS server spans;
- no unexpected peer IDs;
- no OpenBao JWT login with an unbound subject;
- no service audit rows missing `actor_spiffe_id`;
- no removed credential names or sentinel values in logs, traces, audit
  payloads, Caddy logs, journals, or smoke-test artifacts.

## Source Notes

- Current system context: [system-context.md](../system-context.md).
- Service plane split: [service-architecture.md](service-architecture.md).
- Secrets product and resource split:
  [secrets-service.md](../../src/platform/docs/secrets-service.md).
- Audit actor fields:
  [audit-data-contract.md](../../src/governance-service/docs/audit-data-contract.md).
- Listener and port inventory:
  [`src/cue-renderer`](../../src/cue-renderer), rendered to
  [`src/platform/ansible/group_vars/all/generated/`](../../src/platform/ansible/group_vars/all/generated/).
- Trust domain exclusion for the privileged host daemon:
  [`src/vm-orchestrator/AGENTS.md`](../../src/vm-orchestrator/AGENTS.md).
- SPIRE trust domains and attestation:
  <https://spiffe.io/docs/latest/deploying/configuring/>.
- SPIRE server federation bundle endpoint:
  <https://spiffe.io/docs/latest/deploying/spire_server/>.
- SPIFFE Workload API, X.509-SVID, and JWT-SVID:
  <https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/>.
- SPIFFE federation bundle endpoint semantics:
  <https://spiffe.io/docs/latest/spiffe-specs/spiffe_federation/>.
- OpenBao JWT/OIDC auth:
  <https://openbao.org/docs/2.4.x/auth/jwt/>.
- PostgreSQL peer authentication:
  <https://www.postgresql.org/docs/18/auth-peer.html>.

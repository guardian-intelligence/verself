# Workload Identity

Forge Metal uses SPIFFE/SPIRE as the workload identity authority for repo-owned
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
forge_metal_domain: guardianintelligence.org
spire_trust_domain: "spiffe.{{ forge_metal_domain }}"
spire_oidc_bind_address: 127.0.0.1
spire_oidc_bind_port: 8082
spire_oidc_issuer_url: "https://{{ spire_oidc_bind_address }}:{{ spire_oidc_bind_port }}"
```

If `spire_trust_domain` changes while SPIRE holds initialized data, deploy
fails with tagged error `workload_identity.trust_domain_mutation_forbidden`.

The trust domain is a root of trust and a namespace. It MUST NOT encode
deployment lane, node count, host name, region, or product name. Those facts
live in service attributes, deploy metadata, registration selectors, or node
IDs. Valid forms:

```text
spiffe.guardianintelligence.org
spiffe.example.com
workload.example.com
```

## Identity Shape

Service identities:

```text
spiffe://spiffe.guardianintelligence.org/svc/identity-service
spiffe://spiffe.guardianintelligence.org/svc/governance-service
spiffe://spiffe.guardianintelligence.org/svc/billing-service
spiffe://spiffe.guardianintelligence.org/svc/secrets-service
spiffe://spiffe.guardianintelligence.org/svc/sandbox-rental-service
spiffe://spiffe.guardianintelligence.org/svc/mailbox-service
```

Node identities:

```text
spiffe://spiffe.guardianintelligence.org/node/<hostname>
```

Operator tooling identities:

```text
spiffe://spiffe.guardianintelligence.org/ops/admin-cli
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
deleted by the unit after startup. Proof asserts no join token remains in
credstore or runtime state. A residual token is tagged
`workload_identity.join_token_residual`.

## Internal Service Calls

Repo-owned internal HTTP calls use SPIFFE X.509-SVID mTLS with exact peer ID
authorization. Authorized edges:

```text
sandbox-rental-service -> billing-service
sandbox-rental-service -> governance-service
sandbox-rental-service -> secrets-service
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
- **On-behalf-of subject.** The end user or API credential the request is
  being processed for. Carried in-band as the original Zitadel JWT, forwarded
  verbatim as an `X-Forge-Subject-JWT` header on the internal call.

Downstream services re-validate the forwarded JWT through `auth-middleware`
against Zitadel JWKS on every hop, exactly as a public boundary would. A
service MUST NOT trust any subject claim unless it originated from a
re-validated Zitadel JWT in the same request.

Governance audit rows written by downstream services carry both
`actor_spiffe_id` (caller) and `subject` (on-behalf-of). The two fields are
independent and both required on internal calls. A row with a populated
`subject` and an empty `actor_spiffe_id` is a data-integrity violation.

## OpenBao Workload Auth

OpenBao authenticates repo-owned services through SPIRE JWT-SVIDs:

```text
issuer:   https://127.0.0.1:8082
audience: openbao
subject:  spiffe://spiffe.guardianintelligence.org/svc/<service-name>
mount:    auth/spiffe-jwt
```

SPIRE OIDC Discovery Provider exposes the OIDC discovery document and JWKS
for OpenBao JWT validation on a loopback-only TLS listener. The listener uses
a private local CA pinned in OpenBao's JWT auth config; it is not a public
federation endpoint. A service fetches a JWT-SVID for audience `openbao`, logs
in to OpenBao, and receives an OpenBao token constrained by the policies bound
to its SPIFFE subject.

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

**Current state.** The following clients remain password-backed because peer
auth does not cover them: ClickHouse driver connections and TigerBeetle client
connections. Passwords are stored in OpenBao and fetched by
SPIFFE-authenticated startup code.

**Target state.** Password-backed clients are eliminated by wrapping them in
SPIFFE-authenticated connection brokers or by moving to drivers that accept
local socket peer auth.

## Runtime Provider Secrets

Runtime third-party provider credentials are fetched from OpenBao by
SPIFFE-authenticated services. OpenBao paths:

```text
platform/providers/stripe/billing-service
platform/providers/resend/mailbox-service
platform/providers/github/sandbox-rental-service
platform/providers/clickhouse/billing-service
platform/providers/clickhouse/governance-service
platform/providers/clickhouse/sandbox-rental-service
```

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

## Federation Scope

Cross-trust-domain federation is out of scope for the single-node topology
and the three-node topology target. Customer SPIRE trust domains are not
federated into `spiffe.guardianintelligence.org`. Customer workloads
authenticate to Forge Metal through customer-facing APIs with Zitadel
credentials, not as SPIFFE peers.

## Observability

Workload identity is operator-visible through
`make observe WHAT=workload-identity`. The surface exposes:

- SPIRE server, agent, and OIDC Discovery Provider systemd state.
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

Proof queries assert:

- **every** `auth.spiffe.mtls.server` span carries a non-empty `spiffe.peer_id`
  attribute, and the peer ID resolves to an identity in the canonical
  registration inventory;
- no internal bearer-token spans on repo-owned service-to-service routes;
- no missing `spiffe.peer_id` on internal mTLS server spans;
- no unexpected peer IDs;
- no OpenBao JWT login with an unbound subject;
- no service audit rows missing `actor_spiffe_id`;
- no removed credential names or sentinel values in logs, traces, audit
  payloads, Caddy logs, journals, or proof artifacts.

## Source Notes

- Current system context: [system-context.md](../system-context.md).
- Service plane split: [service-architecture.md](service-architecture.md).
- Secrets product and resource split:
  [secrets-service.md](../../src/platform/docs/secrets-service.md).
- Audit actor fields:
  [audit-data-contract.md](../../src/governance-service/docs/audit-data-contract.md).
- Listener and port inventory:
  [`src/platform/ansible/group_vars/all/services.yml`](../../src/platform/ansible/group_vars/all/services.yml).
- Trust domain exclusion for the privileged host daemon:
  [`src/vm-orchestrator/AGENTS.md`](../../src/vm-orchestrator/AGENTS.md).
- SPIRE trust domains and attestation:
  <https://spiffe.io/docs/latest/deploying/configuring/>.
- SPIFFE Workload API, X.509-SVID, and JWT-SVID:
  <https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/>.
- SPIRE OIDC Discovery Provider:
  <https://pkg.go.dev/github.com/spiffe/spire/support/oidc-discovery-provider>.
- OpenBao JWT/OIDC auth:
  <https://openbao.org/docs/2.4.x/auth/jwt/>.
- PostgreSQL peer authentication:
  <https://www.postgresql.org/docs/18/auth-peer.html>.

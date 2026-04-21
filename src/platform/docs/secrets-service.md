# Secrets Service Direction

`secrets-service` is the Forge Metal product boundary for customer-managed
secrets, variables, transit keys, and execution-time injection into sandboxed
workloads. The customer-facing surface stays narrow: no AWS/GCP IAM
integration, no outward OIDC provider, no dynamic database credentials, no
customer-visible backend identity.

The service is a Huma v2 HTTP API. The target backend is OpenBao; the current
envelope-encrypted PostgreSQL store and the `fmtransit:v1:` / `fmsig:v1:` wire
formats are stop-gap and will cut over cleanly — no customer-issued material
exists to preserve, so there is no dual-write window and no compatibility
layer. The public API must not expose backend paths, OpenBao mount or
namespace names, host paths, or privileged runtime details.

## Target Architecture: OpenBao

All secret storage and transit cryptography move to OpenBao; envelope-wrapped
PG is dropped at cutover.

### Tenancy

Namespace-per-org. Each Forge Metal org gets its own OpenBao namespace with
its own KV v2 and Transit mounts, policies, and JWT auth config. If the
deployed OpenBao build lacks namespaces, fall back to one KV + one Transit
mount per org with path-prefixed, policy-scoped access — tenancy stays an
OpenBao primitive, never application code. Org deletion marks the namespace
disabled rather than destroying it, deferring retention and legal-hold
design.

### KV layout

`kv/data/<kind>/{org|source|env|branch}/…/<name>`. Resolution walks four
deterministic subtrees rather than listing-and-filtering metadata. Branch
names stay SHA256-hashed at the path
(`.../branch/<source>/<env>/<branch_hash>/<name>`) — data minimization,
OWASP ASVS V8. Variables share the same KV mount as secrets; `<kind>/`
distinguishes. Different sensitivity uses different paths, not different
mounts.

### Transit algorithms

- `aes256-gcm96` for encrypt/decrypt. Semantically identical to current behavior.
- `ed25519` for sign/verify. Asymmetric — verifiers no longer need the key.

### Auth to OpenBao

No long-lived service token. Two identity inputs, explicit boundary:

- User path: the caller's Zitadel JWT is exchanged for a short-lived OpenBao
  token per request, cached on `(jti, namespace)`.
- Workload path: `secrets-service` fetches a SPIRE JWT-SVID with audience
  `openbao`, logs in through OpenBao JWT auth, and receives a short-lived
  OpenBao token bound to its SPIFFE subject. The loopback internal injection
  endpoint is authenticated with SPIFFE mTLS and the persisted execution
  attempt/grant context, not a static service-account client secret.

Raw JWT capture lives in a wrapper middleware inside `secrets-service`;
shared `auth-middleware` stays single-purpose (verify and extract claims).

### Policy split

- Zitadel = IdP (authn).
- SPIRE = repo-owned workload identity.
- OpenBao = resource-level authz for the secrets plane (KV path + Transit key
  capabilities, per-namespace HCL). OpenBao is a relying party for workload
  identity, not the source of truth for it.
- `policy.go` keeps the coarse has-any-secrets-role check, governance audit
  emission, rate limits, idempotency, and body limits. The fine-grained
  role/capability matrix moves into OpenBao policies.
- OPA = future operation-level PDP. `policy.go` stays declarative so the
  eventual translation is mechanical.
- governance-service = audit.

Mirrors AWS Identity-Center / IAM / KMS / CloudTrail.

### List semantics

`GET /api/v1/secrets` fans out LIST + per-path metadata read over loopback,
bounded by the existing `limit<=200`. Preserves the current DTO. Revisit with
a read-through cache only if telemetry shows contention.

### Namespace provisioning

Idempotent Ansible playbook reconciles Zitadel orgs → OpenBao namespaces,
mounts, and policies. Declarative, runs after seed and on demand. A Zitadel
Action webhook for lower-latency creation can land later; the reconciler
remains source of truth.

### Single-node deployment

Single-node Raft. Manual unseal from credstore shards. 2-node OpenBao and
auto-unseal deferred until growth past one node. Token-cache TTL bounds,
cache-key shape, and the manual-unseal runbook go into the updated
`secrets_service` and new `openbao` Ansible roles alongside this doc.

### Migration sequence

1. OpenBao Ansible role.
2. Namespace provisioning playbook (driven by identity-service's org inventory).
3. New `baostore.go` backend behind the current `Service` interface.
4. `policy.go` rewrite for JWT pass-through.
5. One-shot PG → OpenBao data migration tool.
6. Cutover.
7. Drop PG tables; remove `envelope-key` from credstore/SOPS.

## Product Boundary

The customer-facing surface is `secrets-service`:

- KV secrets and variables scoped to org, source, environment, or branch.
- Resolution order `branch > environment > source > org`.
- Transit keys for encrypt, decrypt, sign, verify, and key rotation.
- Internal execution-time injection for `sandbox-rental-service`.

`sandbox-rental-service` stores only secret references on execution rows. At
worker time it calls the loopback-only internal injection endpoint over SPIFFE
mTLS with attempt-scoped persisted grant context. Secret values are resolved
immediately before
`vm-orchestrator.StartExec` and are passed only in `ExecSpec.Env`; they are not
persisted by sandbox-rental-service, written to ClickHouse, or included in
audit payloads.

## Identity

User and API credential calls use the standard Forge Metal Zitadel path:
`auth-middleware` validates the JWT issuer, audience, JWKS, org claim, and
project roles. `secrets-service` owns its operation catalog and maps roles as:

- `owner` and `admin`: read, write, delete, key create, key rotate, and key use.
- `member`: read secrets/variables and use transit keys.
- API credentials: exact `permissions` claims emitted by identity-service.

Sandbox injection is service-to-service, not user-token forwarding. The sandbox
worker sends `org_id`, `actor_id`, `execution_id`, `attempt_id`, and a bounded
set of references to `/internal/v1/injections/resolve`. That endpoint is not in
OpenAPI, is loopback-only by nftables, and requires SPIFFE mTLS with the exact
`sandbox-rental-service` workload ID. `secrets-service` verifies the SPIFFE
peer and the persisted attempt/grant context before reading from OpenBao with a
SPIRE JWT-SVID-authenticated OpenBao token.

SPIFFE/SPIRE is the service-to-service workload identity primitive. In-VM
per-request secret access is still deferred; injection remains launch-time
environment materialization controlled by sandbox-rental-service.

## Resource Model

Secrets and variables share the same metadata model:

```text
org_id
kind: secret | variable
name
scope_level: org | source | environment | branch
source_id
env_id
branch_hash + branch_display
current_version
```

Branch names are hashed for lookup. Audit rows use `secret_path_hash` rather
than plaintext path segments so names like `PROD_STRIPE_KEY` do not become
analytics data.

Transit keys are org-scoped. Key versions are envelope-wrapped; ciphertexts and
signatures carry an explicit version prefix so old material remains decryptable
or verifiable after rotation.

## API Surface

Committed OpenAPI specs live in `src/secrets-service/openapi/`:

- `PUT /api/v1/secrets/{name}`
- `GET /api/v1/secrets/{name}`
- `GET /api/v1/secrets`
- `DELETE /api/v1/secrets/{name}`
- `POST /api/v1/transit/keys`
- `POST /api/v1/transit/keys/{key_name}/rotate`
- `POST /api/v1/transit/keys/{key_name}/encrypt`
- `POST /api/v1/transit/keys/{key_name}/decrypt`
- `POST /api/v1/transit/keys/{key_name}/sign`
- `POST /api/v1/transit/keys/{key_name}/verify`

Mutations require `Idempotency-Key`. Request bodies are size-bounded. Public
routes carry `x-forge-metal-iam` metadata so identity-service can expose the
operation catalog and generated clients can treat permissions as data.

## Audit

Every public operation and every sandbox injection read sends a structured
governance audit record. Governance persists the HMAC chain and the full event
row to PostgreSQL first, then projects to ClickHouse. A background projector
retries pending rows so a transient ClickHouse outage does not lose a
high-risk secrets audit event.

Expected evidence for a successful injection path:

```text
secrets.secret.write
secrets.secret.read
secrets.transit_key.create
secrets.transit_key.encrypt
secrets.transit_key.decrypt
secrets.secret.inject
```

The corresponding trace path in `default.otel_traces` includes:

```text
secrets-service: secrets.secret.put
secrets-service: secrets.secret.read
secrets-service: secrets.transit.encrypt
secrets-service: secrets.transit.decrypt
sandbox-rental-service: sandbox-rental.execution.submit
sandbox-rental-service: sandbox-rental.execution.run
sandbox-rental-service: sandbox-rental.secrets.resolve
vm-orchestrator: rpc.StartExec
```

## Deployment

The Ansible role creates:

- `secrets_service` Unix user with SPIRE workload socket group access.
- loopback-only public and internal listeners; the internal listener requires
  SPIFFE mTLS.
- loopback-only nftables rules for Zitadel, OpenBao, SPIRE JWT bundle endpoint,
  billing, governance audit, and OTLP egress.
- a Zitadel project named `secrets-service` with `owner`, `admin`, and
  `member` roles.

`seed-system.yml` grants the platform and Acme seed personas matching
secrets-service roles. `assume-persona.sh` emits
`SECRETS_SERVICE_ACCESS_TOKEN` for those personas.

## Verification

Completion requires live ClickHouse evidence, not only unit tests:

```bash
make deploy TAGS=deploy_profile,identity_service,governance_service,secrets_service,sandbox_rental_service
make seed-system
make secrets-proof
```

`make secrets-proof` creates a secret, reads it back, creates and uses a
transit key, submits a sandbox execution with `secret_env`, verifies the guest
only prints the secret hash, and asserts:

- `secret_resources` has the expected versioned row.
- `sandbox_rental.execution_secret_env` stores only references.
- `default.otel_traces` contains secrets-service and sandbox resolver spans.
- `forge_metal.audit_events` contains the expected secrets audit sequence.

## Invariants

- Secret values are never written to ClickHouse, sandbox PostgreSQL, audit
  records, command strings, or proof artifacts.
- Sandbox execution commands may reference injected environment variables but
  must not embed secret values.
- Runtime product services never receive host privilege. Secret injection is an
  HTTP boundary from sandbox-rental-service to secrets-service; VM launch still
  goes through vm-orchestrator.
- The historical local envelope key is stop-gap deployment secret material; it
  is removed when the OpenBao backend is the only executable path.
- Secret names should not encode sensitive facts; audit still treats path names
  as sensitive and records hashes.
- Cloud IAM integration, outward OIDC federation, and dynamic credentials
  remain future phases outside the current product contract.
- The OpenBao backend is not customer-visible: mount names, namespace names,
  and backend paths never appear in API responses, error messages, or
  customer-facing UI copy.
- OpenBao is not the repo-owned workload identity source of truth. It accepts
  SPIRE-issued identity documents and maps trusted SPIFFE subjects to resource
  policies.

## Source Anchors

- Service architecture: `docs/architecture/service-architecture.md`
- Workload identity: `docs/architecture/workload-identity.md`
- Identity and IAM split: `src/platform/docs/identity-and-iam.md`
- Audit contract: `src/governance-service/docs/audit-data-contract.md`
- Sandbox control plane: `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- Wire contracts: `src/apiwire/docs/wire-contracts.md`
- Huma v2 API framework: <https://pkg.go.dev/github.com/danielgtaylor/huma/v2>
- OpenTelemetry trace API: <https://opentelemetry.io/docs/specs/otel/trace/api/>
- PostgreSQL partial indexes: <https://www.postgresql.org/docs/current/indexes-partial.html>
- ClickHouse OpenTelemetry schema usage in this repo: `make clickhouse-schemas`

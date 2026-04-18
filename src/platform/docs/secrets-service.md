# Secrets Service Direction

`secrets-service` is the Forge Metal product boundary for customer-managed
secrets, variables, transit keys, and execution-time injection into sandboxed
workloads. The first implementation is intentionally narrow: no AWS/GCP IAM
integration, no outward OIDC provider, no dynamic database credentials, and no
customer-visible OpenBao dependency.

The service is a Huma v2 HTTP API backed by a local envelope-encrypted
PostgreSQL store. That adapter is the product implementation until OpenBao is
introduced for operational reasons; the public API must not expose backend
paths, OpenBao mount names, host paths, or privileged runtime details.

## Product Boundary

The customer-facing surface is `secrets-service`:

- KV secrets and variables scoped to org, source, environment, or branch.
- Resolution order `branch > environment > source > org`.
- Transit keys for encrypt, decrypt, sign, verify, and key rotation.
- Internal execution-time injection for `sandbox-rental-service`.

`sandbox-rental-service` stores only secret references on execution rows. At
worker time it calls the loopback-only internal injection endpoint with a
shared service token. Secret values are resolved immediately before
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
OpenAPI, is loopback-only by nftables, and requires
`/etc/credstore/secrets-service/internal-injection-token`.

SPIFFE/SPIRE remains the right future direction for in-VM per-request secret
access, but it is not part of this first shipped cut. Until then, injection is
launch-time environment materialization controlled by sandbox-rental-service.

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

- `secrets_service` Unix user and PostgreSQL role.
- `secrets_service` PostgreSQL database.
- `/etc/credstore/secrets-service/pg-dsn`
- `/etc/credstore/secrets-service/envelope-key`
- `/etc/credstore/secrets-service/internal-injection-token`
- loopback-only nftables rules for PostgreSQL, Zitadel, governance audit, and
  OTLP egress.
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
- The local envelope key is deployment secret material and must be 32 bytes.
- Secret names should not encode sensitive facts; audit still treats path names
  as sensitive and records hashes.
- Cloud IAM, outward OIDC federation, dynamic credentials, and OpenBao adapter
  work are future phases, not part of the current product contract.

## Source Anchors

- Service architecture: `docs/architecture/service-architecture.md`
- Identity and IAM split: `src/platform/docs/identity-and-iam.md`
- Audit contract: `src/governance-service/docs/audit-data-contract.md`
- Sandbox control plane: `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- Wire contracts: `src/apiwire/docs/wire-contracts.md`
- Huma v2 API framework: <https://pkg.go.dev/github.com/danielgtaylor/huma/v2>
- OpenTelemetry trace API: <https://opentelemetry.io/docs/specs/otel/trace/api/>
- PostgreSQL partial indexes: <https://www.postgresql.org/docs/current/indexes-partial.html>
- ClickHouse OpenTelemetry schema usage in this repo: `make clickhouse-schemas`

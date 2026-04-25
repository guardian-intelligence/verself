# Secrets Service Direction

`secrets-service` is the Forge Metal product boundary for customer-managed
retrievable secrets, non-secret variables, opaque credentials, transit keys, and
execution-time injection into sandboxed workloads. The backend is OpenBao. The
public API does not expose OpenBao mount names, backend paths, namespace names,
host paths, or privileged runtime details.

## Architecture

Each Forge Metal org gets an OpenBao KV v2 mount, Transit mount, JWT auth roles,
and ACL policies reconciled by Ansible from identity-service organization
inventory. Current single-node deployments use mount-per-org tenancy:

```text
kv-<org_id>
transit-<org_id>
jwt-<org_id>
```

The customer API remains the authority for product semantics. OpenBao enforces
resource capabilities after the service maps an authenticated Forge Metal
principal or SPIFFE workload caller to a short-lived OpenBao token.

### KV Layout

Retrievable secrets and non-secret variables share a KV mount but are separated
by kind:

```text
secret/{org|source|environment|branch}/.../<name>
variable/{org|source|environment|branch}/.../<name>
opaque_credential/org/<credential_id>
```

Resolution walks deterministic subtrees in order
`branch > environment > source > org`; it does not list-and-filter the whole
mount. Branch names are SHA256-hashed in paths. Audit rows use path hashes so
names such as `PROD_STRIPE_KEY` do not become analytics data.

### Opaque Credentials

Opaque credentials are one-time-material resources for product-specific bearer
tokens such as source Git HTTPS credentials. The API returns token material only
from create and roll operations. GET, LIST, revoke, audit, ClickHouse traces,
and PostgreSQL projections expose metadata only.

Credential token format is:

```text
fmoc_<credential_uuid>_<base64url_random_256_bits>
```

OpenBao stores only metadata plus a Transit HMAC verifier. Verification parses
the credential UUID from the token, reads the metadata document from KV, checks
status, expiry, kind, and required scopes, then asks Transit
`/hmac/:name/:algorithm` or `/verify/:name/:algorithm` to validate the HMAC.
The Transit key is org-local and type `hmac`.

### Transit

- `aes256-gcm96` for encrypt/decrypt.
- `ed25519` for sign/verify.
- `hmac` for opaque credential verifiers.

Transit key versions remain explicit in API DTOs, audit rows, and ClickHouse
spans.

## Identity

User and API credential calls use the standard Forge Metal Zitadel path:
`auth-middleware` validates issuer, audience, JWKS, org claim, and project
roles. `secrets-service` owns its operation catalog and maps roles as:

- `owner` and `admin`: full secrets, variables, opaque credential, and transit
  permissions.
- `member`: read secrets/variables and use transit keys.
- API credentials: exact `permissions` claims emitted by identity-service.

OpenBao access is per request:

- User/API credential path: the caller's Zitadel JWT is exchanged for a
  short-lived OpenBao token for the exact direct role required by the operation.
- Workload path: `secrets-service` fetches a SPIRE JWT-SVID with audience
  `openbao`, logs in through OpenBao JWT auth, and receives a short-lived
  OpenBao token bound to its SPIFFE subject and the selected workload role.

Sandbox secret injection is service-to-service, not user-token forwarding. The
sandbox worker sends `org_id`, `actor_id`, `execution_id`, `attempt_id`, and a
bounded set of references to `/internal/v1/injections/resolve`. The endpoint is
not public OpenAPI, requires SPIFFE mTLS with the exact
`sandbox-rental-service` workload ID, and resolves values immediately before VM
execution.

Source Git credentials are also service-to-service. `source-code-hosting-service`
calls the generated secrets-service `internalclient` over SPIFFE mTLS:

```text
POST /internal/v1/credentials
POST /internal/v1/credentials:verify
```

The source service owns its PostgreSQL projection and Git UX. Secrets-service
owns token generation, verifier storage, rotation, revocation, and scope
verification. Attribution remains the origin requester: the source service
sends `org_id`, `actor_id`, `kind`, scopes, and metadata; secrets-service audit
rows record both the delegated actor and the caller SPIFFE ID.

Platform-owned runtime provider secrets follow the same service boundary:
repo-owned services call secrets-service over SPIFFE mTLS; they do not
authenticate directly to OpenBao.

## Resource Model

Secrets:

```text
secret_id
kind: secret
name
scope_level: org | source | environment | branch
source_id
env_id
branch_hash + branch_display
current_version
```

Variables use the same persisted metadata with public DTO names
`variable_id` and `variables`. They are retrievable non-secret configuration,
not a subtype of a secret in the API.

Opaque credentials:

```text
credential_id
org_id
kind
subject
display_name
status: active | revoked
token_prefix
scopes[]
metadata{}
current_version
expires_at
last_used_at
created_at
updated_at
revoked_at
```

The verifier material is not retrievable after generation. Roll creates fresh
material and replaces the stored verifier; revoke marks the metadata inactive.

## API Surface

Committed OpenAPI specs live in `src/secrets-service/openapi/`.

Secrets:

- `PUT /api/v1/secrets/{name}`
- `GET /api/v1/secrets/{name}`
- `GET /api/v1/secrets`
- `DELETE /api/v1/secrets/{name}`

Variables:

- `PUT /api/v1/variables/{name}`
- `GET /api/v1/variables/{name}`
- `GET /api/v1/variables`
- `DELETE /api/v1/variables/{name}`

Opaque credentials:

- `POST /api/v1/credentials`
- `GET /api/v1/credentials/{credential_id}`
- `GET /api/v1/credentials`
- `POST /api/v1/credentials/{credential_id}/roll`
- `POST /api/v1/credentials/{credential_id}/revoke`

Transit:

- `POST /api/v1/transit/keys`
- `POST /api/v1/transit/keys/{key_name}/rotate`
- `POST /api/v1/transit/keys/{key_name}/encrypt`
- `POST /api/v1/transit/keys/{key_name}/decrypt`
- `POST /api/v1/transit/keys/{key_name}/sign`
- `POST /api/v1/transit/keys/{key_name}/verify`

Mutations require `Idempotency-Key`. Request bodies are size-bounded. Public
routes carry `x-forge-metal-iam` metadata so identity-service can expose the
operation catalog and generated clients can treat permissions as data.

## Audit And Traces

Every public operation and every internal service-to-service credential or
runtime secret operation sends a structured governance audit record.
Governance persists the HMAC chain and the full event row to PostgreSQL first,
then projects to ClickHouse. A background projector retries pending rows so a
transient ClickHouse outage does not lose a high-risk secrets audit event.

Expected audit events for the live secrets proof include:

```text
secrets.secret.write
secrets.secret.read
secrets.secret.list
secrets.secret.delete
secrets.variable.write
secrets.variable.read
secrets.variable.list
secrets.variable.delete
secrets.credential.create
secrets.credential.read
secrets.credential.list
secrets.credential.roll
secrets.credential.revoke
secrets.transit_key.create
secrets.transit_key.rotate
secrets.transit_key.encrypt
secrets.transit_key.decrypt
secrets.transit_key.sign
secrets.transit_key.verify
```

Expected trace spans in `default.otel_traces` include:

```text
secrets-service: secrets.secret.*
secrets-service: secrets.variable.*
secrets-service: secrets.credential.*
secrets-service: secrets.transit.*
secrets-service: secrets.bao.*
secrets-service: secrets.billing.*
```

The source Git proof additionally asserts this cross-service sequence:

```text
source-code-hosting-service: source.secrets.git_credential.create
source-code-hosting-service: auth.spiffe.mtls.client
secrets-service: auth.spiffe.mtls.server
secrets-service: secrets.credential.internal_create
secrets-service: secrets.credential.create
secrets-service: secrets.bao.transit.hmac
source-code-hosting-service: source.secrets.git_credential.verify
secrets-service: secrets.credential.internal_verify
secrets-service: secrets.credential.verify
secrets-service: secrets.bao.transit.verify_hmac
```

## Deployment

The Ansible roles create:

- `secrets_service` Unix user with SPIRE workload socket group access.
- loopback-only public and internal listeners; the internal listener requires
  SPIFFE mTLS.
- loopback-only nftables rules for Zitadel, OpenBao, SPIRE JWT bundle endpoint,
  billing, governance audit, source-code-hosting-service, and OTLP egress.
- a Zitadel project named `secrets-service` with `owner`, `admin`, and
  `member` roles.
- per-org OpenBao KV, Transit, JWT roles, direct API policies, and SPIFFE
  workload policies.

`seed-system.yml` grants the platform and Acme seed personas matching
secrets-service roles. `assume-persona.sh` emits
`SECRETS_SERVICE_ACCESS_TOKEN` for those personas.

## Verification

Completion requires live ClickHouse evidence, not only unit tests:

```bash
make deploy TAGS=deploy_profile,identity_service,governance_service,billing_service,secrets_service,source_code_hosting_service,sandbox_rental_service
make seed-system
make secrets-proof
make source-code-hosting-proof
```

`make secrets-proof` creates and deletes a secret, creates and deletes a
variable, creates/reads/lists/rolls/revokes an opaque credential, creates and
uses transit keys, exercises API-credential access, and asserts:

- `default.otel_traces` contains the expected secrets-service spans.
- `forge_metal.audit_events` contains the expected secrets audit sequence.
- `forge_metal.metering` contains settled secrets operation rows.
- OpenBao does not contain legacy service-token bootstrap material.

`make source-code-hosting-proof` creates a Git credential through
source-code-hosting-service, pushes to `git.<domain>`, verifies the credential
through secrets-service over SPIFFE mTLS, asserts source PostgreSQL projections,
and asserts the source -> secrets -> OpenBao trace sequence in ClickHouse.

## Invariants

- Secret values and opaque credential tokens are never written to ClickHouse,
  PostgreSQL projections, audit records, logs, or proof artifacts.
- Secrets-service never receives host privilege or vm-orchestrator access.
- Secret and variable names should not encode sensitive facts; audit still
  treats path names as sensitive and records hashes.
- Cloud IAM integration, outward OIDC federation, and dynamic credentials
  remain future phases outside the current product contract.
- OpenBao is not customer-visible.
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
- OpenBao Transit API: <https://openbao.org/api-docs/secret/transit/>
- Huma v2 API framework: <https://pkg.go.dev/github.com/danielgtaylor/huma/v2>
- OpenTelemetry trace API: <https://opentelemetry.io/docs/specs/otel/trace/api/>
- ClickHouse OpenTelemetry schema usage in this repo: `make clickhouse-schemas`

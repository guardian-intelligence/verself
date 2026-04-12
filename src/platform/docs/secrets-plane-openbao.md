# Secrets Plane With OpenBao

`identity-service` and `secrets-service` are separate control planes:

- `identity-service`: customer API credentials and Zitadel-backed service account lifecycle.
- `secrets-service`: customer workload secrets, encrypted secret material, and secret access brokering for workloads.

This document focuses on `secrets-service` hosting with OpenBao and on a billing model that intentionally mirrors AWS Secrets Manager + AWS KMS.

## Product Boundary

Expose a Forge Metal `secrets-service` HTTP API as the product surface. OpenBao is the preferred backend implementation detail, not the customer-facing API contract.

Reasons:

- We need org/policy semantics that are consistent with Forge Metal operation catalogs and Zitadel identity enforcement (`auth-middleware` + service-local policy checks).
- We need billing and dogfooding through the same product control plane paths customers use.
- We need long-term adapter freedom for migrations/import/export (for example, Infisical import path) without changing customer contracts.

## GitHub-Actions-Compatible UX Contract

Adopt the same high-level semantics operators already expect from GitHub Actions:

- Scopes:
  - Organization secrets and variables
  - Repository secrets and variables
  - Environment secrets and variables
- Separate resources:
  - Variables (non-sensitive configuration)
  - Secrets (encrypted, redacted)
- Trust boundary:
  - Withhold secret material for fork-style untrusted runs
- Credential model:
  - Prefer OIDC federation and short-lived credentials over long-lived static cloud secrets

Primary source anchors:

- GitHub secrets scopes and hierarchy: <https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets>
- GitHub variables scopes: <https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-variables>
- GitHub forked PR behavior: secrets are withheld except `GITHUB_TOKEN`: <https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets>
- GitHub OIDC recommendation (short-lived tokens, no long-lived cloud secrets): <https://docs.github.com/en/actions/concepts/security/openid-connect>

## OpenBao Backend Research

### Why OpenBao As Default Backend

OpenBao provides the primitives we need for a first-class secrets plane:

- KV v2 secret storage with version lifecycle (soft delete, undelete, destroy, metadata)
- Transit encryption service for envelope encryption and crypto operations
- Dynamic credentials (database engine) with lease/revocation model
- Path/capability ACL model that maps directly to org/repo/environment path prefixes
- Audit devices with request/response coverage and hash-redaction

Primary source anchors:

- KV v2 docs: <https://openbao.org/docs/secrets/kv/kv-v2/>
- Transit docs: <https://openbao.org/docs/secrets/transit/>
- Database dynamic credentials: <https://openbao.org/docs/secrets/databases/>
- Policies and capabilities: <https://openbao.org/docs/concepts/policies/>
- Audit devices: <https://openbao.org/docs/audit/>

### Hosting Topology Recommendation

#### Single-node (current deployment target)

Use OpenBao Integrated Storage (Raft) on local disk:

- OpenBao recommends integrated storage for most use cases.
- It reduces operational complexity and avoids an external DB hop for storage.

Source: <https://openbao.org/docs/configuration/storage/>

Concrete host placement:

- Run `openbao` in the private subnet ring, not as an internet-exposed service.
- Bind listener to private interface and enforce nftables allowlist (only approved service callers).
- Keep TLS enabled (default) and do not set `tls_disable=true`.

Source: <https://openbao.org/docs/configuration/listener/tcp/>

#### Three-node (future upgrade path)

Use a 3-voter Raft cluster:

- `cluster_addr` is required for integrated storage clustering.
- Use `retry_join` for deterministic node discovery/bootstrap.
- Use `autopilot` health/state and peer management commands in operations runbooks.

Sources:

- <https://openbao.org/docs/configuration/storage/raft/>
- <https://openbao.org/docs/next/commands/operator/raft/>

### Seal / Unseal Strategy

Recommended staged rollout:

1. First cut: Shamir unseal with strict operational runbook.
2. Production hardening cut: transit auto-unseal (OpenBao transit seal) to eliminate manual restarts and support key rotation workflows.

Transit seal supports:

- autoseal via transit engine
- explicit ACL requirements on encrypt/decrypt paths
- key rotation with constraints on old key availability for decryption

Source: <https://openbao.org/docs/configuration/seal/transit/>

### Audit and Observability

Enable at least two audit devices:

- OpenBao recommends multiple audit devices.
- Requests can block/hang if audit devices are blocked and none can accept writes.

Source: <https://openbao.org/docs/audit/>

Telemetry signals to ingest into ClickHouse:

- include high-cardinality usage gauges such as `vault.kv.secret.count`
- tune `usage_gauge_period` for desired billing granularity

Source: <https://openbao.org/docs/internals/telemetry/metrics/>

### API/Path Modeling Caveat

Do not encode sensitive information in key names or path segments.

The KV API list behavior does not policy-filter key names for sensitivity semantics and explicitly warns against encoding sensitive info in key names.

Source: <https://openbao.org/api-docs/secret/kv/kv-v1/>

### Versioning and Stability Notes

At time of writing:

- latest stable release on GitHub releases page is `v2.4.4` (2025-11-24)
- `v2.5.0-beta20251125` exists as pre-release

Source: <https://github.com/openbao/openbao/releases>

Recommendation: pin production deploys to stable tags and track upgrade guides before adopting beta-only features.

## Secrets-Service Data and Access Model

### Resource Hierarchy

Define explicit Forge Metal resources:

- `org_secret`
- `repo_secret`
- `env_secret`
- `org_variable`
- `repo_variable`
- `env_variable`
- `transit_key` (customer-visible key metadata)

Map to OpenBao mount/path conventions:

- KV mount for secrets data (`kv-v2`)
- Transit mount for key operations

Path pattern example:

- `kv/data/orgs/{org_id}/repos/{repo_id}/envs/{env_id}/secrets/{secret_name}`
- `kv/metadata/...` for lifecycle/list metadata ops

Policy generation model:

- compile Forge Metal operation grants into OpenBao ACL policies by path prefix
- keep OpenBao ACLs as backend enforcement, while `secrets-service` remains the public policy boundary

### Workload Access Flow

For sandbox workloads:

1. Caller requests execution in `sandbox-rental-service`.
2. Scheduler classifies trust class (`trusted` vs `untrusted_fork` style event).
3. `sandbox-rental-service` asks `secrets-service` for an execution-scoped materialization token.
4. `secrets-service` fetches/mints secret material from OpenBao and returns a short-lived one-time bundle (or direct lease/token with strict TTL/use limit).
5. Bundle is injected into VM at launch; never persisted in frontend/session stores.

For untrusted/fork-style runs:

- deny secret materialization by policy default.
- optionally allow explicit operator-approved allowlist for specific low-risk secrets.

## Billing Model: Copy AWS Secrets Manager + KMS

Model the product as `product_id = "secrets"` with SKU-based catalog entries.

### AWS Price Shape To Mirror

Pricing snapshot date: 2026-04-11 (re-verify before launch because AWS pricing pages change).

Secrets Manager:

- charge for secrets stored (`$0.40 / secret / month`)
- charge for API calls (`$0.05 / 10,000 API calls`)

Source: <https://aws.amazon.com/secrets-manager/pricing/>

KMS:

- charge per customer-managed key (`$1 / key / month`)
- charge per request (example shows `$0.03 / 10,000` for symmetric encrypt/decrypt usage; `$0.15 / 10,000` for asymmetric signing usage)
- free tier: `20,000 requests / month` with noted exclusions

Source: <https://aws.amazon.com/kms/pricing/>

### Forge Metal Catalog Mapping

Credit buckets:

- `secrets_storage`
- `secrets_api`
- `kms_key_storage`
- `kms_requests`
- `kms_asymmetric_requests` (optional split; preserves AWS-like differential rate)

SKUs:

- `secrets_active_secret_hour`
- `secrets_api_call`
- `kms_active_key_hour`
- `kms_crypto_request`
- `kms_asymmetric_crypto_request`

Unit rationale:

- Use hour-granularity storage units (`secret-hour`, `key-hour`) so short-lived secret billing behaves like AWS proration examples.
- Use request-count units for API/crypto calls, aggregated to per-10,000 pricing by rate card.

### Rate card formulas

Keep public pricing formulas AWS-compatible:

- secret storage:
  - `secret_storage_usd = secret_hours * (0.40 / (30 * 24))`
- secrets API:
  - `secrets_api_usd = (api_calls / 10000) * 0.05`
- KMS-like key storage:
  - `kms_key_storage_usd = key_hours * (1.00 / (30 * 24))`
- KMS-like symmetric crypto requests:
  - `kms_crypto_usd = (max(0, symmetric_crypto_calls - free_tier_calls) / 10000) * 0.03`
- KMS-like asymmetric/signing requests:
  - `kms_asymmetric_usd = (asymmetric_crypto_calls / 10000) * 0.15`

`free_tier_calls` defaults to `20,000` per org monthly for symmetric-compatible operations, matching the AWS KMS free-tier shape.

### Metering Events

Emit metering rows from `secrets-service` into `forge_metal.metering` via billing dual-write conventions:

- storage gauge projection:
  - hourly job computes active secret count and active key count per org
  - settle window with `actual_quantity = active_count * 3600` for each category
- request projection:
  - per-request counters for read/write/list/delete/rotate and transit crypto operations
  - aggregate and settle at short cadence windows (for example, 60 seconds)

Dimension recommendations:

- `scope_level` (`org|repo|environment`)
- `operation` (`get|put|list|delete|rotate|encrypt|decrypt|sign|verify`)
- `backend` (`openbao`)
- `trust_class` (`trusted|untrusted`)

Usage evidence recommendations:

- `active_secret_count`
- `active_transit_key_count`
- `api_call_count`
- `crypto_request_count`
- `crypto_request_count_asymmetric`

### Entitlements and Free Tier

To stay AWS-like while aligning with existing grant mechanics:

- include monthly grant for `kms_requests` equivalent to 20k requests (converted to native units)
- do not include free storage by default
- allow plan-specific bundled credits for storage/request buckets

### Dogfood Rule (No Bypass)

Internal usage must traverse the same product APIs and metering paths as customers:

- Forge Metal platform org receives unlimited usage through entitlement + invoice-time adjustment
- no direct backend bypass, no hidden "operator-only free path"

This follows repo IAM/billing invariants:

- [identity-and-iam.md](./identity-and-iam.md)
- [billing-architecture.md](../../billing-service/docs/billing-architecture.md)

## Operational Runbook Requirements

### Backups and Restore Rehearsal

Raft snapshots:

- `bao operator raft snapshot save`
- `bao operator raft snapshot restore`

Source: <https://openbao.org/docs/next/commands/operator/raft/>

For Forge Metal:

- schedule periodic snapshot export into operator-controlled backup target
- pair with ZFS dataset snapshots on single-node
- include restore drills in CI fixture/canary workflows

### Security Baseline

- TLS required on all listeners (`tls_disable=false`)
- keep unauthenticated rekey endpoints disabled (`disable_unauthed_rekey_endpoints=true` default in 2.5)
- deny-by-default policies, explicit per-path capabilities
- multiple audit devices enabled before production traffic
- strict token TTLs for workload-issued tokens; avoid long-lived bearer credentials

Sources:

- <https://openbao.org/docs/configuration/listener/tcp/>
- <https://openbao.org/docs/concepts/policies/>
- <https://openbao.org/docs/audit/>

## Adapter Strategy

Backend adapter interface in `secrets-service`:

- `PutSecret`, `GetSecretVersion`, `ListSecrets`, `DeleteSecretVersion`, `DestroySecretVersion`
- `PutVariable`, `GetVariable`, `ListVariables`
- `Encrypt`, `Decrypt`, `Sign`, `Verify`, `RotateKey`
- `IssueDynamicCredential`, `RevokeLease`
- `EmitUsage` hooks with deterministic idempotency keys

Implementations:

- `openbao` (default)
- `local-envelope` (temporary bootstrap fallback only; no dynamic creds, limited feature set)

No compatibility shims in public API: adapter differences are internal, product contract is single and strict.

## Recommended Build Sequence

1. Create `secrets-service` with org/repo/environment resources and operation catalog only (no workload injection yet).
2. Add OpenBao adapter with KV v2 + Transit.
3. Add billing metering + SKU catalog for `secrets` product.
4. Add trusted/untrusted event gating in `sandbox-rental-service` and wire to `secrets-service`.
5. Add dynamic credential support (database engine first) and OIDC federation paths.
6. Add platform dogfood default plan + adjustment path and verify invoice net-zero behavior.

## Verification Protocol (Live Evidence)

Do not treat unit tests as completion proof. Completion proof requires ClickHouse evidence:

1. Secret write/read flow emits traces for `secrets-service` and OpenBao backend call path.
2. Metering row exists for both storage and request dimensions in `forge_metal.metering`.
3. Statement preview includes `secrets` SKU line items with expected bucket aggregation.
4. Untrusted/fork-style run attempts to fetch secrets and is denied with stable policy error.
5. Dogfood platform org shows billed usage but invoice net-zero after adjustment.

Example query skeleton:

```sql
SELECT
  product_id,
  source_type,
  mapKeys(component_charge_units) AS sku_ids,
  component_quantities,
  component_charge_units,
  bucket_charge_units,
  usage_evidence,
  recorded_at
FROM forge_metal.metering
WHERE product_id = 'secrets'
ORDER BY recorded_at DESC
LIMIT 50;
```

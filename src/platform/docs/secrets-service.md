# Secrets Service Direction

`secrets-service` is the customer-facing control plane for secrets, variables,
dynamic credentials, and crypto operations. OpenBao is the backend
implementation today; it is not the product contract and not the customer
surface.

This document covers identity, OIDC provider role, resource model, workload
access, CLI, audit, and billing. Deployment and bootstrap specifics live
separately in the service's Ansible role and runbook once the service is
scaffolded.

## Product Boundary

Expose a Huma v2 HTTP API as the single product surface. Customers, sandbox
workloads, and dogfood platform services call it; none of them talk to OpenBao
directly.

Reasons:

- Org, project, and environment semantics must match the rest of Forge Metal's
  operation catalog and Zitadel identity enforcement.
- Billing, metering, and dogfood invariants run through the service, not the
  backend.
- Backend adapter freedom for migration, import, and export (OpenBao import,
  Infisical import) without changing the public contract.

Backend adapter interface covers secret storage, variables, transit crypto,
dynamic credentials, leases, and usage emission. The default adapter is
`openbao`; a `local-envelope` bootstrap adapter exists only for
pre-OpenBao-is-up scenarios and does not implement dynamic credentials.

No compatibility shims in the public API. Adapter differences are internal.

## Identity Model

Two separate identity planes feed `secrets-service`:

**Users and API credentials: Zitadel.** Human callers and customer API
credentials authenticate via the standard `auth-middleware` JWT path: issuer
and audience validation, JWKS verification, identity claims into request
context. Operation permissions are enforced via the shared Forge Metal product
IAM (`owner`, `admin`, `member` with code-owned capability bundles; customer
API credentials carry `forge_metal:credential_id` plus exact permissions). See
[identity-and-iam.md](./identity-and-iam.md).

**Sandbox workloads: SPIFFE/SPIRE.** Running VMs, scheduled workloads, and CI
runners authenticate via SPIFFE JWT-SVIDs, not bearer bundles. `vm-orchestrator`
is the node attestor: it already owns VM lifecycle, jailer configuration,
rootfs hash, TAP assignment, and ZFS dataset identity, so it is the only
component with enough ground truth to sign attestation claims for a specific
VM. The SPIRE Server runs alongside platform services; the SPIRE Agent runs
inside the guest over vsock (or inside the VM as a minimal binary) and exposes
the Workload API on a Unix socket.

SPIFFE IDs look like:

```
spiffe://<trust-domain>/org/<org_id>/source/<source_id>/env/<env_id>/task/<task_id>
```

The sandbox agent calls the Workload API and receives auto-rotating JWT-SVIDs.
`secrets-service` verifies the SVID against SPIRE's JWKS and scopes policy by
the path components. Policy checks happen **per secret request**, not once at
VM launch.

Lease revocation rides existing vm-orchestrator termination hooks: when the VM
ends, SPIRE de-registers the workload and all derived OpenBao leases are
revoked by TTL within the window.

Fork-style untrusted events receive SVIDs with a narrowed trust-class claim
(`forge_metal:trust_class=untrusted_fork`); `secrets-service` policy default
denies secret materialization for that class except for explicitly
operator-allowlisted entries.

## OIDC Provider Outward

For CI and long-running workloads, customers want Forge Metal compute to
federate directly to their own AWS, GCP, or Vault without a bootstrap API
key. `secrets-service` runs an OIDC Provider endpoint at
`https://oidc.<domain>/.well-known/openid-configuration` with JWKS at
`https://oidc.<domain>/keys`. OpenBao 2.5 exposes this via its OIDC provider
with Client Credentials flow; we front it with a public issuer URL and stable
claim schema.

Three delivery modes, picked per customer integration:

1. **Passthrough (default at v1).** The customer's workflow runs on our
   Forge Metal Actions runner, but GitHub or Forgejo Actions mints the OIDC
   token (`token.actions.githubusercontent.com` or the Forgejo equivalent).
   Their existing AWS trust continues to work unchanged. Required for
   Blacksmith-parity.

2. **Forge Metal as OP (v1.5).** `secrets-service` mints the OIDC ID token
   from the SPIFFE SVID. Claims mirror GitHub Actions' shape so customer
   trust policies migrate mechanically:

   - `sub: repo:<org>/<repo>:ref:<ref>` or `repo:<org>/<repo>:environment:<env>`
   - `repository`, `ref`, `environment`, `actor`, `sha`
   - `forge_metal:vm_id`, `forge_metal:task_id`, `forge_metal:trust_class`
   - `forge_metal:org_id`, `forge_metal:source_id`, `forge_metal:env_id`

   Customer runs `aws iam create-open-id-connect-provider --url
   https://oidc.<domain>` once and binds trust on `sub` or the
   `forge_metal:*` claims. For Lambda-like and long-running VM workloads
   there is no GitHub issuer at all; this is the only path.

3. **Chained (nice-to-have).** We verify the upstream GitHub or Forgejo
   Actions OIDC token and re-sign as Forge Metal with enriched claims
   (`forge_metal:vm_id`, `forge_metal:trust_class`). Customer configures one
   trust anchor for all Forge Metal compute. Not v1.

Operational consequences of running the issuer: JWKS rotation runbook, issuer
uptime SLO (downtime means mid-run CI can't federate to customer cloud), claim
schema is a product contract (adding is safe, renaming or removing is a
breaking change), key-compromise rotation playbook.

## Resource Model

Match GitHub Actions scope semantics so operators do not have to relearn the
mental model.

Customer-visible resources:

- `org_secret`, `org_variable`
- `source_secret`, `source_variable` (a source is a repo-equivalent or a
  Forgejo project, keyed by Forge Metal source_id)
- `env_secret`, `env_variable`
- `env_branch_override` (per-branch overlay on `env_secret` / `env_variable`)
- `transit_key` (KMS-equivalent, customer-visible key metadata)
- `dynamic_credential_role` (Postgres, MySQL, cloud IAM, etc.)
- `oidc_trust_binding` (customer-configured federation policy if we act as
  inbound OIDC verifier, e.g. for their self-hosted CI calling our API)

Variables are non-sensitive, returned plaintext, not redacted. Secrets are
encrypted, redacted in audit logs, and withheld by policy default on
`trust_class=untrusted_fork` events.

Resolution order for a workload at `(org, source, env, branch)`:

```
branch override  >  env  >  source  >  org
```

First hit wins. The resolution path is visible in audit rows so a developer
can answer "which entry did my workload actually read?" without guessing.

OpenBao path convention (implementation detail, not contract):

```
kv/data/orgs/<org_id>/sources/<source_id>/envs/<env_id>/secrets/<name>
kv/data/orgs/<org_id>/sources/<source_id>/envs/<env_id>/branches/<branch>/secrets/<name>
transit/keys/orgs/<org_id>/keys/<key_name>
database/roles/orgs/<org_id>/sources/<source_id>/envs/<env_id>/<role_name>
```

Forge Metal operation grants compile to OpenBao ACL policies by path prefix.
OpenBao ACLs are defence-in-depth; `secrets-service` is the public policy
boundary.

Never encode sensitive information in secret names or path segments; OpenBao's
KV LIST surface does not policy-filter names.

## Secret And Key Types

Shipped day 1:

- **KV secrets**, versioned with soft-delete, undelete, destroy, metadata.
- **Variables**, plaintext non-sensitive config with the same scope model.
- **Transit crypto**, symmetric encrypt and decrypt plus asymmetric sign and
  verify. Customer apps never hold raw keys; they call crypto operations.
- **Dynamic credentials**, Postgres and MySQL on day 1. Two modes matching
  OpenBao's database engine: dynamic (fresh user per request, revoked on
  lease expiry) and static-roles (Vault-style scheduled password rotation
  for existing accounts). Cloud IAM dynamic creds (AWS, GCP) land in the
  same cut because OpenBao's cloud plugins are first-party.

Deferred past v1:

- Certificate issuance (PKI secrets engine). Infisical competes here, we
  don't need to at launch.
- SSH CA signing. Track for v2 if sandbox-rental-service needs it for its own
  operational paths.

Rotation model follows AWS staging-label semantics, adapted:

- Versions are explicit and integer-ordered (OpenBao native).
- Aliases map to versions: `current`, `pending`, `previous`. Exactly one
  version holds `current` at a time; `pending` is the staged
  next-version. Customer code always reads `current` by default.

## Workload Access Flow

```
1.  sandbox-rental-service starts an execution.
2.  vm-orchestrator launches the VM, registers the workload with SPIRE,
    and attests node + workload attributes.
3.  The in-VM agent obtains a JWT-SVID via the Workload API.
4.  The agent calls secrets-service (or directly OpenBao via secrets-service
    broker endpoint) presenting the SVID.
5.  secrets-service verifies the SVID against SPIRE JWKS, applies policy by
    (org, source, env, branch, task, trust_class), resolves the secret using
    scope override precedence, and returns material or a lease.
6.  For transit and dynamic-credential operations the same flow applies;
    leases are tied to the SVID TTL.
7.  On VM termination, vm-orchestrator notifies SPIRE which de-registers;
    leases expire within the TTL window.
```

For fork-style untrusted events the default policy denies secret
materialization. An explicit customer opt-in allows per-name allowlisting for
specific low-risk secrets, matching GitHub Actions' semantics.

For human CLI callers (`fm secrets run`, `fm secrets get`), the auth path is
Zitadel JWT through `auth-middleware`; SPIFFE is only for workload identity.

## CLI And Injection

`fm secrets run -- <cmd>` injects resolved secrets and variables as
environment variables into the child process and exits when the child
exits. No file on disk, no env pollution of the parent shell. Mirrors
`doppler run`, `op run`, `esc run`.

Flags:

- `--env <name>`: pick environment (default resolved from context).
- `--branch <name>`: pick branch override (default `git rev-parse HEAD`'s
  branch if in a repo).
- `--only <a,b,c>`: scope injection to a specific set.
- `--exclude <a,b,c>`: deny-list.
- `--dry-run`: print the would-be env var names (never values) and exit.

Programmatic access: the Huma client is generated from the service's OpenAPI
3.1 spec like every other Forge Metal service; no hand-rolled SDK.

Kubernetes injection, sidecar mount, and `SecretsManagerSync`-style push
destinations are deferred past v1. Dogfood and sandbox workloads cover the v1
access patterns.

## Audit

Dual-write: OpenBao's native audit device (JSON file, rotated and shipped)
plus a structured Forge Metal governance audit row into ClickHouse on the
request path. The ClickHouse row is the customer-queryable surface; the
OpenBao row remains raw integration evidence.

Secrets rows use the same `forge_metal.audit_events` contract documented in
`src/governance-service/docs/audit-data-contract.md`. The product-specific
fields are:

```
source_product_area = 'Secrets'
operation_type = read|write|delete|authz
risk_level = high|critical for secret value reads, writes, key use, and rotation
target_kind = secret|secret_environment|secret_branch|transit_key|lease
secret_mount, secret_path_hash, secret_version, secret_operation
lease_id_hash, lease_ttl_seconds, key_id
openbao_request_id, openbao_accessor_hash
```

Each row carries `row_hmac = HMAC(prev_hmac || canonical_serialization,
audit_signing_key)`. Tampering is detectable because any deletion or edit
breaks the chain at the downstream row. The audit signing key lives in a
Transit-sealed keyring separate from customer Transit keys and rotates on a
separate schedule. The hash chain is verified by a periodic reconciliation
job (same cadence as billing `Reconcile()`).

Customer-visible audit query runs through `secrets-service`, not direct
ClickHouse. Operator audit query is direct ClickHouse via
`make clickhouse-query`.

Secret material is never logged. Request bodies are hashed; audit rows
include `content_sha256` for write operations for evidence without exposure.

## Billing

Unified product `product_id = "secrets"`. AWS Secrets Manager + KMS shape is
the reference price shape because customers already know it; we are
substantially more generous on the storage side where our marginal cost is
near-zero on self-hosted hardware.

SKUs:

- `secrets_active_secret_hour`: hour-granularity storage metering.
- `secrets_active_variable_hour`: same for variables.
- `secrets_api_call`: non-crypto API operations (get, put, list, delete,
  rotate).
- `kms_active_key_hour`: transit key storage.
- `kms_symmetric_request`: encrypt, decrypt, data-key generation.
- `kms_asymmetric_request`: sign, verify.
- `dynamic_credential_lease_hour`: time a lease is outstanding.
- `active_environment_hour`: per-`(source, env)` surface that captures
  modern preview-env pricing patterns the flat AWS model misses.

Rate card is AWS-mirroring for easy customer-side cost translation:

- secret storage:
  `secret_storage_usd = secret_hours * (0.40 / (30 * 24))`
- variables: 10% of secret storage rate.
- secrets API:
  `secrets_api_usd = (api_calls / 10000) * 0.05`
- transit key storage:
  `kms_key_storage_usd = key_hours * (1.00 / (30 * 24))`
- symmetric crypto:
  `kms_symmetric_usd = (max(0, symmetric_calls - free_symmetric) / 10000) * 0.03`
- asymmetric crypto:
  `kms_asymmetric_usd = (asymmetric_calls / 10000) * 0.15`
- dynamic credential lease:
  `dyn_lease_usd = lease_hours * 0.01`

### Free Tier (per org, per billing period)

Positioned to make the product free for any reasonable CI usage:

- 3,000 active secrets
- 3,000 active variables
- 10 active transit keys
- 1,000,000 symmetric crypto requests
- 50,000 asymmetric crypto requests
- 100,000 non-crypto API calls
- 500 dynamic-credential lease-hours

Tier multipliers: Pro is 10x, Team is 30x, Enterprise is unmetered. The
precise plan bundling lives in the billing product catalog; these numbers are
the control-plane defaults `secrets-service` ships.

### Metering

Emit metering rows from `secrets-service` into `forge_metal.metering` via the
standard billing dual-write conventions. Dimensions:

- `scope_level` (org, source, environment, branch)
- `operation` (get, put, list, delete, rotate, encrypt, decrypt, sign, verify,
  lease_open, lease_close)
- `secret_kind` (kv, variable, transit, dynamic)
- `trust_class` (trusted, untrusted_fork)
- `backend` (openbao)

Storage SKUs are settled from an hourly gauge projection
(`actual_quantity = active_count * 3600`). Request SKUs are settled from
per-request counters aggregated on a short cadence. Dynamic credential leases
are settled on `lease_close` with `actual_quantity = (close_ts - open_ts)`.

### Dogfood

Forge Metal platform org consumes through the same API, the same metering
path, and the same SKUs. Usage is unlimited by entitlement on the platform
plan; invoice nets to zero via adjustment. No hidden operator bypass.

## Operational Posture

Single-node (current deployment target):

- OpenBao with Integrated Storage (Raft) on local disk.
- Listener bound to private interface only; never internet-exposed.
  `secrets-service` is the internet-adjacent hop and carries the Huma API.
- `tls_disable=false`. Certificate material from the platform's Caddy-issued
  chain, not a self-signed.
- Two audit devices minimum (OpenBao's requirement to avoid blocking writes
  when one device is unavailable): a local JSON file device and the
  `secrets-service` HTTP audit sink that writes ClickHouse rows.
- Shamir unseal for the first production cut, migrate to Transit auto-unseal
  once a second OpenBao instance exists to serve as the seal keyring.

Three-node (future):

- Raft 3-voter cluster with `cluster_addr` and `retry_join`.
- `autopilot` in the operational runbook.
- Transit auto-unseal becomes standard; the seal keyring lives on a separate
  instance from the data instance.
- OIDC issuer and JWKS endpoint become topology-aware (loopback-only nftables
  rules break; the service needs real cross-node egress like the Zitadel
  JWKS path).

Backups: Raft snapshots via `bao operator raft snapshot save`, paired with
ZFS snapshots of the data dataset. Restore drills run as a CI canary against
a disposable instance.

## Build Sequence

1. Scaffold `secrets-service` with Huma, org/source/env/branch resources,
   operation catalog, and `auth-middleware` integration. Local-envelope
   adapter for bootstrap only.
2. Bring up OpenBao in the platform Ansible role; add the OpenBao adapter
   covering KV v2, Transit, audit device wiring, and the two-audit-device
   requirement.
3. Add SPIRE Server and SPIRE Agent wiring; `vm-orchestrator` becomes the
   node attestor; sandbox VMs receive SVIDs at boot.
4. Wire `sandbox-rental-service` execution path to SPIRE registration and
   trust-class classification; secrets-service enforces SVID-based policy.
5. Add dynamic credentials (Postgres, MySQL, AWS IAM, GCP IAM) with
   lease-based metering.
6. Add the OIDC provider endpoint; passthrough mode first, Forge Metal as OP
   second. Document the claim schema as a product contract.
7. Add `fm secrets run` CLI.
8. Add billing metering, SKU catalog, and dogfood adjustment path; verify
   invoice net-zero behaviour for the platform org.
9. Add the ClickHouse audit sink with HMAC chain and the reconciliation job.

## Verification Protocol

Unit tests do not count as completion. Completion requires ClickHouse
evidence from a live rehearsal:

1. Secret write and read emits traces for `secrets-service` and the OpenBao
   backend call path, with SPIFFE principal on the workload-initiated read.
2. Metering rows exist for storage, request, and lease dimensions in
   `forge_metal.metering`.
3. Statement preview includes `secrets` SKU line items with expected bucket
   aggregation.
4. Untrusted-fork-style run attempts secret materialization and is denied
   with a stable, structured policy error.
5. Dogfood platform org shows billed usage and invoice nets to zero after
   adjustment.
6. OIDC passthrough federation: a Forge Metal runner job with GitHub Actions
   OIDC acquires AWS credentials via STS AssumeRoleWithWebIdentity against a
   customer-configured trust policy. The trace shows the GitHub token's
   `sub` claim reaching STS.
7. Forge-Metal-as-OP federation: a Lambda-like workload with no GitHub in
   scope acquires AWS credentials via our issuer. The customer's OIDC
   provider configuration trusts `https://oidc.<domain>` and the trust
   policy binds on `forge_metal:org_id` and `forge_metal:source_id`.
8. Audit chain verification job runs clean over a window of rows; a
   deliberate tampering test fails the verification as expected.

Example query skeleton:

```sql
SELECT
  product_id,
  mapKeys(component_charge_units) AS sku_ids,
  component_quantities,
  bucket_charge_units,
  usage_evidence,
  recorded_at
FROM forge_metal.metering
WHERE product_id = 'secrets'
ORDER BY recorded_at DESC
LIMIT 50;
```

## Invariants

- `secrets-service` is the public boundary. Customers, sandbox workloads,
  and platform services never reach OpenBao directly.
- Sandbox workload identity is SPIFFE JWT-SVID, not a bearer bundle. Policy
  is checked per request, not once at VM launch.
- Zitadel authenticates users and API credentials; it does not authenticate
  workloads.
- Resolution order is branch > env > source > org, first hit wins, and the
  resolved path is recorded in audit.
- Fork-style events default to denying secret materialization.
- The OIDC claim schema is a product contract: additive changes are safe,
  renames and removals are breaking.
- Dogfood traverses the same API, metering, and SKUs customers use. Internal
  unlimited usage is modelled as entitlement plus invoice-time adjustment,
  not a bypass.
- Audit rows are HMAC-chained; tampering is detectable by the reconciliation
  job.
- Secret names and path segments never encode sensitive information.

## Source Anchors

Primary sources that back claims in this document, cited so a future reader
can re-verify:

- OpenBao KV v2: <https://openbao.org/docs/secrets/kv/kv-v2/>
- OpenBao Transit: <https://openbao.org/docs/secrets/transit/>
- OpenBao database dynamic credentials: <https://openbao.org/docs/secrets/databases/>
- OpenBao policies: <https://openbao.org/docs/concepts/policies/>
- OpenBao audit devices: <https://openbao.org/docs/audit/>
- OpenBao OIDC provider with Client Credentials (2.5 release): <https://openbao.org/docs/release-notes/2-5-0/>
- OpenBao integrated storage (Raft): <https://openbao.org/docs/configuration/storage/raft/>
- OpenBao Transit auto-unseal: <https://openbao.org/docs/configuration/seal/transit/>
- SPIFFE overview: <https://spiffe.io/docs/latest/spiffe-about/overview/>
- Vault JWT auth claim binding (applies to OpenBao): <https://developer.hashicorp.com/vault/docs/auth/jwt>
- GitHub Actions OIDC claim shape: <https://docs.github.com/en/actions/concepts/security/openid-connect>
- GitHub Actions secrets and fork-PR withholding: <https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets>
- AWS Secrets Manager pricing (price-shape reference): <https://aws.amazon.com/secrets-manager/pricing/>
- AWS KMS pricing (price-shape reference): <https://aws.amazon.com/kms/pricing/>
- AWS IAM Roles Anywhere (SPIFFE X.509 federation target): <https://docs.aws.amazon.com/rolesanywhere/latest/userguide/introduction.html>

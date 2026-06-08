# Cloudflare Integration

`cloudflare-integration-service` owns every Cloudflare provider interaction for
Verself. Object-byte workflows enter through `object-storage-service`;
`object-storage-service` uses Cloudflare integration as its R2 provider driver.
DNS, ACME DNS-01, Email Routing, and Cloudflare account authority remain direct
Cloudflare integration capabilities.

Cloudflare is a single global provider account anchored to prod authority.
`site` selects target DNS records, R2 object prefixes, runtime capability
credentials, and evidence labels. It does not select a site-local Cloudflare
account.

## Authority

The service owns Cloudflare account-admin lifecycle. Account-admin values are
stored through `secrets-service`; `secrets-service` enforces SPIFFE/JIT access,
records audit evidence, and stores encrypted material in OpenBao. OpenBao stores
provider secret material and supplies Transit/KV primitives. It does not own
Cloudflare policy, rotation, verification, or provider state machines.

The bootstrap account-admin credential is one Cloudflare API token:

```text
cloudflare.account_admin
```

Required account-admin permissions:

- Account API Tokens Read and Account API Tokens Write on the Cloudflare account.
- Workers R2 Storage Read and Workers R2 Storage Write on the Cloudflare account.
- Workers R2 Storage Bucket Item Read and Workers R2 Storage Bucket Item Write
  on the Cloudflare account.
- Zone Read and DNS Write for every managed hosted zone.
- Email Routing permissions for managed forwarding zones when email routing is
  reconciled.

The provider returns a token value only when it is created or rolled.
`cloudflare-integration-service` writes it to `secrets-service` before any
follow-up mutation and records only token ID and value fingerprints in reports.
Overlapping token generations are created after bootstrap by the Cloudflare
integration owner, not by the host bootstrap path.

## Capability API

Callers request Cloudflare capabilities, not raw provider credentials.

- `object-storage-service` requests R2 capability reconciliation, scoped R2
  credentials, and presigned S3-compatible object transfer handles for
  deployment artifacts, product object storage, and recovery bytes.
- `deployment-service` requests deployment-artifact object write sessions from
  `object-storage-service`.
- edge/TLS tooling requests ACME DNS-01 TXT records and deletes them after
  issuance.
- site provisioning requests DNS reconciliation for exact desired records.
- email provisioning requests Cloudflare Email Routing reconciliation.

The service may use Cloudflare's S3-compatible R2 API internally. S3-compatible
transfer handles are issued by `object-storage-service` to trusted workloads;
Cloudflare provider selection remains hidden from those workloads.

## R2 Model

Global account-owned buckets are declared in `src/integrations/cloudflare/account.json`.
Target-site isolation is by object prefix and credential scope:

```text
verself-deployment-artifacts/<site>/sha256/<artifact-sha256>/...
verself-deployment-artifacts/<site>/candidate/<deploy-run-key>/...
verself-recovery/<site>/...
```

R2 capability credentials are bucket-scoped Cloudflare child credentials.
`cloudflare-integration-service` creates them, verifies real R2 access, and
stores them through `secrets-service`. `object-storage-service` is the runtime
consumer for object transfer, deployment artifact, product object, and recovery
byte workflows.

Capability credential rotation follows this state machine:

```text
desired
  -> account_authority_loaded_jit
  -> provider_token_created
  -> provider_roundtrip_verified
  -> persisted_to_secrets_service
  -> active
  -> draining_old_generation
  -> revoked
```

## DNS And TLS

DNS records are target-site resources inside global hosted zones. Site metadata
names hosted zones explicitly with values such as `cloudflare_product_zone` and
`cloudflare_company_zone`; callers must not infer hosted zones by trimming
labels from a domain.

DNS reconciliation accepts explicit desired records, resolves hosted zones,
builds a provider diff, applies creates/updates/deletes, and verifies provider
state. It emits evidence and produces no runtime credential.

ACME DNS-01 issuance uses the same Cloudflare DNS capability through short-lived
TXT records:

```text
request challenge record
  -> resolve hosted zone
  -> create TXT record
  -> verify public DNS visibility
  -> caller completes ACME authorization
  -> delete TXT record
  -> emit challenge evidence
```

## Email Routing

Cloudflare Email Routing is a Cloudflare capability. Email provisioning requests
managed destination, MX, SPF, and routing-rule reconciliation from
`cloudflare-integration-service`. Human destination verification remains a
provider-side step when Cloudflare requires a mailbox verification click.

## Recovery Boundary

Artifact delivery does not use Cloudflare. Fresh recovery requires an
operator-provided Cloudflare admin API token for the provider account.
OpenTofu may reconcile non-secret global resources such as zones, buckets, and
provider policy scaffolding. Provider token values and child credential
generations are imported or created by the Cloudflare integration recovery job
and stored through OpenBao or `secrets-service` once that boundary is available.

## Failure Rules

Provider failures are data. The service records the operation, phase, provider
status, request ID, token fingerprint, affected site/capability, retry decision,
and recovery action. New credential generations are never made active until real
provider verification succeeds. Old generations are revoked only after the new
generation is persisted and verified.

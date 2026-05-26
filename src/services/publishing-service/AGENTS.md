# publishing-service

Deferred service split. Do not implement this service until package-owned
release tooling and distribution-service admission require external provider
fan-out.

The future publishing-service owns external publication fan-out for artifacts
that distribution-service has already admitted. It translates verified immutable
artifacts into provider-specific public actions such as npm publish, GitHub
Release creation, newsroom publication, docs updates, and package-index updates.

## Boundary

- Treat this file as the target boundary for the later extraction.
- Owns provider-specific publication workflows, credentials, idempotency,
  provider receipts, reconciliation, and provider audit evidence.
- Consumes immutable artifact digests and verification state from
  distribution-service. It does not build artifacts or decide artifact-channel
  resolution.
- Uses provider APIs directly only from provider adapters owned by this service.
  Package-local Bazel targets provide release bundles and metadata, not provider
  credentials or arbitrary publish code.
- Treats external providers as separate systems of record. Every publication
  must reconcile provider ground truth after dispatch.
- Does not mutate channel pointers directly. Stable, RC, nightly, and other
  channels are distribution-service state exposed through OCI admission and
  read gating.
- Does not sign SLSA build provenance. It may sign provider receipt attestations
  or request distribution-service verification summaries when needed.

## Standards

Use open standards and provider-native security mechanisms:

- OCI digest and descriptor references identify the source artifact from Zot.
- in-toto Statement v1 wraps provider receipt attestations when receipts are
  stored as OCI referrers.
- SLSA VSA can summarize distribution verification decisions consumed before
  publication.
- npm Trusted Publishing should be preferred when the provider supports the CI
  environment. If the provider cannot trust Verself CI OIDC yet, use a narrowly
  scoped provider credential stored through secrets-service/OpenBao and document
  the downgrade in the publication policy.
- GitHub Releases, npm dist-tags, newsroom posts, and docs updates are provider
  projections of an already-admitted artifact, not the source of artifact truth.

Do not add a repo-specific "release ledger" abstraction. Publication state is
provider workflow state plus receipts tied to immutable OCI digests.

## API Contract

Canonical contracts belong in Smithy under `src/smithy/models/verself` before
handlers are added. Public APIs use Zitadel bearer auth and IAM. Internal
service calls use SPIFFE mTLS and service-local typed clients.

Initial operations:

- `CreatePublication`: request publication of an admitted artifact digest to
  one or more providers.
- `GetPublication`: read publication state, provider tasks, policy decisions,
  attempts, and receipts.
- `ApprovePublication`: record a human or policy approval before irreversible
  provider actions.
- `DispatchPublication`: start provider fan-out. This should normally be
  internal/worker-only.
- `RecordReceipt`: persist a provider response, provider URL, provider version,
  dist-tag, digest, or transaction id.
- `ReconcilePublication`: re-read provider truth and repair local state.
- `CreatePublishAuthorization`: issue a short-lived signed authorization for a
  delegated CI publish when a provider requires CI-native OIDC.
- `CancelPublication`: stop a publication before an irreversible provider
  action starts.

Required resource names:

- `Publication`
- `PublicationProviderTask`
- `PublicationApproval`
- `PublicationReceipt`
- `PublishAuthorization`
- `ProviderCredentialBinding`

Use stable problem types for artifact not admitted, missing distribution
verification, channel policy denied, approval required, provider credential
missing, provider auth failed, provider version immutable, provider conflict,
provider rate limited, receipt mismatch, and reconciliation mismatch.

## State Machine

Publication state:

```text
requested
  -> waiting_for_distribution
  -> policy_verified
  -> approval_required
  -> approved
  -> dispatching
  -> provider_pending
  -> provider_verified
  -> complete

policy_verified -> denied
approval_required -> denied
approved -> cancelled
dispatching -> retryable_failed
dispatching -> terminal_failed
provider_pending -> retryable_failed
provider_verified -> reconciled
```

Provider task state:

```text
created
  -> credential_ready
  -> idempotency_checked
  -> dispatched
  -> accepted
  -> reconciled

idempotency_checked -> already_exists_verified
dispatched -> retryable_failed
dispatched -> terminal_failed
accepted -> receipt_mismatch
```

External provider versions are often immutable. Treat duplicate provider
versions as a reconciliation problem, not as a reason to republish bytes.

## Provider Workflows

Stable release:

- Confirm distribution-service admitted the digest and promoted the intended
  stable channel.
- Publish immutable provider versions where required, such as npm `0.2.0`.
- Move provider aliases/tags only after immutable version publication succeeds,
  such as npm `latest`.
- Create GitHub Release and newsroom publication only from the same admitted
  digest and source commit.

Release candidate:

- Confirm the digest is admitted and promoted to an RC channel.
- Publish provider prerelease versions only when that channel opts into external
  visibility, such as npm `0.2.0-rc.1` with dist-tag `next` or `rc`.
- Mark GitHub Releases as prereleases.
- Do not publish newsroom posts by default unless the package policy requests
  it.

Nightly:

- Default to no external provider publication. Nightlies should usually be
  distributed through the internal OCI registry and distribution-service
  channel resolution only.
- If public nightlies are required, publish immutable prerelease versions with a
  clear nightly version scheme and provider tag such as `nightly`.
- Never overwrite or reuse a provider version. Provider history is append-only
  even when the distribution channel pointer advances frequently.

## Security and Credentials

- Provider credentials are owned by provider adapters and stored through
  secrets-service/OpenBao. Package code and Bazel release bundles must never see
  raw provider credentials.
- Prefer provider OIDC/trusted-publisher flows over stored tokens. When stored
  tokens are unavoidable, scope them to the package/provider/channel and rotate
  them through secrets-service.
- Publish authorizations must be short-lived, audience-bound, artifact-digest
  bound, package-bound, provider-bound, and single-use.
- Provider adapters must own API version headers, retries, rate-limit handling,
  idempotency keys, pagination, redirect policy, and telemetry.
- All provider callbacks or webhooks must use provider-native authenticity
  checks before changing publication state.
- Do not allow package-owned code to execute arbitrary provider calls under
  publishing-service credentials.

## SLSA and Evidence

Before provider dispatch, publishing-service must verify or fetch a
distribution-service verification result for the artifact:

- Artifact digest is admitted and not quarantined.
- Required SLSA provenance and signatures are present.
- Channel policy permits the requested provider and provider tag.
- The provider request references the same immutable digest that was admitted.
- Any delegated CI publication authorization is bound to the same digest and
  expires before it can be replayed.

publishing-service may attach provider receipt attestations as OCI referrers to
the subject artifact. Receipt attestations should use in-toto Statement v1 with
a provider-specific predicate URI until a stronger ecosystem standard exists.

## IAM, Governance, and Metering

- IAM controls publication creation, approval, cancellation, retry, credential
  binding, and provider reconciliation.
- Governance audit is required for all irreversible provider actions, approval
  decisions, denied attempts, credential use, and reconciliation mismatches.
- ClickHouse events must include `org_id`, package, version, artifact digest,
  provider kind, provider package id, provider version, provider tag, request
  id, trace id, actor, and publication id.
- Publication operations are control-plane operations. Provider fees or
  third-party metering should be modeled explicitly if a future provider charges
  per publication or egress.

## Retention and Recovery

- Retain provider receipts for at least the artifact support and incident
  response window.
- Reconciliation should be able to rebuild local publication state from provider
  truth, distribution-service artifact state, and stored receipts.
- `/recoveryz` must report PostgreSQL migration state, distribution-service
  reachability, provider credential reachability, provider adapter health,
  provider reconciliation lag, outbox backlog, and ClickHouse write health.

## Non-goals

- No build execution.
- No Zot byte storage ownership.
- No distribution channel pointer mutation.
- No Nomad release orchestration.
- No arbitrary package-owned provider plugin execution with privileged
  credentials.

# distribution-service

Deferred service split. release-service records release artifacts and install
options until this boundary is extracted.

The future distribution-service owns artifact admission, channel resolution,
signed update metadata, retention, and edge replication over Zot-hosted OCI
artifacts. It does not build artifacts, publish to external package providers,
or replace Zot as the byte store.

## Boundary

- Do not implement this service while release-service is the active release
  control plane. Treat this file as the target boundary for the later
  extraction.
- Owns the product-neutral distribution plane for release artifacts: artifact
  admission, channel pointers, TUF metadata, edge replication intent, retention
  policy, quarantine, and resolver APIs.
- Uses Zot and the OCI Distribution/Image specs for bytes, manifests, digests,
  and referrers. Do not invent a parallel artifact store or release ledger.
- Uses in-toto statements as the attestation envelope for SLSA provenance,
  SBOMs, verification summaries, and other supply-chain evidence.
- Uses TUF metadata for client-facing update/channel trust: root, timestamp,
  snapshot, targets, and delegated package roles.
- Calls publishing-service only when a distribution decision should trigger an
  external provider action. Distribution remains valid without external
  publication.
- Does not call npm, GitHub Releases, newsroom, package indexes, or other
  external providers directly.
- Does not build or sign build provenance. CI owns build execution, artifact
  upload, artifact signatures, and SLSA provenance.

## Standards

Use open standards as the durable contract surface:

- OCI Distribution v1.1+ for content-addressed artifact push, pull, manifest
  discovery, and referrers.
- OCI Image/Artifact manifests for typed release payloads and subject
  relationships.
- in-toto Statement v1 for attestations.
- SLSA provenance `https://slsa.dev/provenance/v1` for build provenance.
- SLSA Verification Summary Attestation `https://slsa.dev/verification_summary/v1`
  when distribution-service records a policy verification decision.
- TUF for signed channel/update metadata and rollback protection.

Do not add a repo-specific "release ledger" abstraction. If extra state is
needed, model it as distribution state over standard OCI/TUF/in-toto objects.

## API Contract

Canonical contracts belong in Smithy under `src/smithy/models/verself` before
handlers are added. Public APIs use Zitadel bearer auth and IAM. Internal
service calls use SPIFFE mTLS and service-local typed clients.

Initial operations:

- `AdmitArtifact`: register an immutable Zot OCI digest after CI pushed bytes,
  signature, provenance, SBOM, and required referrers.
- `GetArtifact`: return artifact state, verification result, retention class,
  quarantine state, and relevant OCI descriptor data.
- `PromoteTarget`: advance a package channel to a verified immutable digest and
  publish signed TUF metadata.
- `ResolveTarget`: resolve package, channel, platform, audience, and policy to
  an immutable OCI digest and download endpoint.
- `ListChannelTargets`: return channel history with signed target metadata
  versions and supersession reason.
- `QuarantineArtifact`: prevent future resolution of a digest without deleting
  bytes or evidence.
- `EnsureReplication`: request or repair artifact and referrer replication to
  edge mirrors.
- `GetTUFMetadata`: serve TUF metadata for clients and mirrors.

Required resource names:

- `DistributionArtifact`
- `DistributionChannel`
- `DistributionTarget`
- `DistributionVerification`
- `DistributionReplication`
- `DistributionQuarantine`

Use stable problem types for missing OCI referrers, digest mismatch, untrusted
builder, untrusted signer, source policy failure, channel policy failure,
TUF signing failure, replication failure, and quarantined artifact resolution.

## State Machines

Artifact state:

```text
zot_pushed
  -> submitted
  -> verifying
  -> admitted
  -> available
  -> superseded
  -> retained
  -> expired
  -> gc_eligible
  -> deleted

verifying -> rejected
available -> quarantined
quarantined -> retained
```

Channel target state:

```text
draft_target
  -> policy_checked
  -> tuf_signed
  -> published
  -> superseded

policy_checked -> denied
published -> rollback_target_published
```

State transitions must be explicit, auditable, idempotent, and monotonic where
possible. Do not silently move a channel pointer when verification or signing
state is incomplete.

## Security and SLSA

Admission must verify the artifact and its evidence before a digest can resolve
from any channel:

- The OCI descriptor digest matches the in-toto subject digest.
- Required OCI referrers exist and are immutable.
- SLSA provenance uses predicate type `https://slsa.dev/provenance/v1`.
- The signer identity and `runDetails.builder.id` pair is allowed for the
  package and channel.
- The source repository, source commit, Bazel target, package name, version, and
  channel are authorized by package policy.
- The artifact was produced by CI, not by distribution-service.
- Required SBOM and test evidence are present before stable or RC promotion.
- Nightly policy may be weaker, but the channel must make that visible.

SLSA target:

- Near term: Build L2 for CI-produced artifacts with signed provenance from a
  hosted/hardened Verself builder mode.
- Longer term: Build L3 only after build steps cannot access signing material or
  tamper with provenance generation.

distribution-service may sign a SLSA VSA for its verification decision. It must
not sign build provenance.

## IAM, Governance, and Metering

- IAM controls package/channel mutation, artifact quarantine, retention changes,
  and replication administration.
- Resolver reads can be anonymous only for channels explicitly configured as
  public. Private/internal channels require auth and entitlement checks.
- Every admission, verification, promotion, rollback, quarantine, retention, and
  replication decision emits governance audit evidence.
- ClickHouse events must include `org_id`, package, channel, artifact digest,
  OCI repository, source commit, SLSA builder id, policy id, request id, trace
  id, and actor.
- Distribution operations are control-plane operations. They are not product
  usage charges unless a future plan explicitly meters artifact egress or
  private retention.

## Retention and Recovery

- Bytes remain in Zot until retention and quarantine policy allow garbage
  collection. Never delete evidence before all policy windows expire.
- Nightlies should default to short retention and high churn.
- RCs should retain through the stable release and incident-response window.
- Stable releases should retain for the published support/data-retention period.
- `/recoveryz` must report PostgreSQL migration state, Zot reachability,
  referrer query health, TUF signing key availability, TUF metadata freshness,
  replication lag, and ClickHouse write health.

## Non-goals

- No provider fan-out.
- No npm/GitHub/newsroom credentials.
- No build execution.
- No Nomad release orchestration.
- No package-manager-specific release semantics beyond channel metadata and
  artifact resolution.

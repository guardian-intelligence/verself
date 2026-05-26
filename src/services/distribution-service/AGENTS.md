# distribution-service

distribution-service owns artifact admission, channel resolution,
signed update metadata, retention, and edge replication over Zot-hosted OCI
artifacts. It does not build artifacts, publish to external package providers,
or replace Zot as the byte store.

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

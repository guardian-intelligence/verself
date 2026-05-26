# distribution-service

distribution-service is the first-party artifact distribution control plane for
Verself-owned artifacts. It admits immutable OCI digests after required evidence
is present, serves channel resolution APIs, and gates public OCI reads for
admitted artifacts.

## Boundaries

distribution-service owns:

- artifact admission records for first-party OCI digests;
- verification decisions over required OCI referrers and release policy inputs;
- channel target promotion to immutable digests;
- artifact quarantine and replication repair requests;
- public OCI read admission for distribution-hosted artifacts;
- append-only `verself.distribution_events` evidence in ClickHouse.

Package-owned release tooling owns release naming, build execution, approval
workflow, release notes, provider fan-out requests, package update metadata, and
release-facing install-option read models. It calls distribution-service over
SPIFFE mTLS or a future short-lived release authorization to admit already-built
OCI artifacts and promote distribution targets.

Zot remains the byte store. distribution-service records and authorizes
content-addressed state over Zot-hosted OCI objects.

## APIs

Canonical contracts are modeled in
`src/smithy/models/verself/distribution.smithy`.

Public operations:

- `distribution.updates.check`
- `distribution.upgrades.recordVerifiedDownload`
- `distribution.targets.resolve`
- `distribution.releases.get`
- `distribution.artifacts.get`
- `distribution.channels.listTargets`

Internal SPIFFE mTLS operations:

- `AdmitDistributionArtifact`
- `PromoteDistributionTarget`
- `QuarantineDistributionArtifact`
- `EnsureDistributionReplication`
- `GetDistributionArtifactInternal`

Public read-only protocol endpoints:

- `/releases/{package_name}/{version}`
- `/v2/`
- `/v2/{name}/manifests/{reference}`
- `/v2/{name}/blobs/{digest}`
- `/v2/{name}/referrers/{digest}`

Mutable OCI references are denied on the public read path. Clients resolve
channels through the distribution API, then pull immutable digests.

## Admission

Admission requires:

- package, version, channel, platform, OCI repository, digest, media type, and
  size;
- trusted builder id and signer identity;
- source repository, source commit, source ref, Bazel target, and policy ref;
- cosign signature evidence;
- SLSA provenance with predicate type `https://slsa.dev/provenance/v1`;
- SBOM evidence;
- test evidence for stable and RC channels.

All admission and mutation operations require idempotency keys. Idempotency
payload mismatches return a conflict instead of replacing state.

## OCI Read Path

The public OCI read path accepts digest-addressed requests only. Manifest reads
are authorized by exact admitted repository digest. Blob reads are authorized
only when the digest appears as the config or a layer descriptor in an admitted
manifest for the same repository. Referrer reads are authorized by admitted
subject digest.

Admission should persist the manifest descriptor graph before the registry path
is placed under high traffic, so blob authorization does not need to refetch
manifests from the origin registry.

## Observability

distribution-service writes `verself.distribution_events` with stable event
types such as:

- `distribution.artifact.admit_requested`
- `distribution.artifact.verification_started`
- `distribution.artifact.verification_allowed`
- `distribution.artifact.available`
- `distribution.target.promote_requested`
- `distribution.target.policy_allowed`
- `distribution.target.published`
- `distribution.target.resolve_allowed`
- `distribution.target.resolve_denied`
- `distribution.release.metadata_resolved`
- `distribution.release.metadata_denied`
- `distribution.oci.manifest_served`
- `distribution.oci.blob_served`
- `distribution.upgrade.download_verified`
- `distribution.artifact.quarantined`
- `distribution.artifact.replication_ensured`

ClickHouse inserts use `batch.AppendStruct` and `ch` struct tags.

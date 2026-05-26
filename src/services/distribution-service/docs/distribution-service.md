# distribution-service

distribution-service is the first-party artifact distribution control plane for
Verself-owned artifacts. It admits immutable OCI digests after required evidence
is present, publishes signed TUF channel metadata, serves public update
resolution APIs, and gates public OCI reads for admitted artifacts.

## Boundaries

distribution-service owns:

- artifact admission records for first-party OCI digests;
- verification decisions over required OCI referrers and release policy inputs;
- channel target promotion to immutable digests;
- TUF root, targets, snapshot, and timestamp metadata for package channels;
- artifact quarantine and replication repair requests;
- public OCI read admission for distribution-hosted artifacts;
- append-only `verself.distribution_events` evidence in ClickHouse.

release-service owns release naming, approval workflow, release notes, provider
fan-out requests, and release-facing install-option read models. OCI publication
dispatch calls distribution-service over SPIFFE mTLS to promote a distribution
target. release-service no longer probes the registry directly to decide whether
an OCI publication is complete.

Zot remains the byte store. distribution-service records and authorizes
content-addressed state over Zot-hosted OCI objects.

## APIs

Canonical contracts are modeled in
`src/smithy/models/verself/distribution.smithy`.

Public operations:

- `distribution.updates.check`
- `distribution.upgrades.recordVerifiedDownload`
- `distribution.targets.resolve`
- `distribution.artifacts.get`
- `distribution.channels.listTargets`

Internal SPIFFE mTLS operations:

- `AdmitDistributionArtifact`
- `PromoteDistributionTarget`
- `QuarantineDistributionArtifact`
- `EnsureDistributionReplication`
- `GetDistributionArtifactInternal`

Public read-only protocol endpoints:

- `/tuf/{package}/root.json`
- `/tuf/{package}/timestamp.json`
- `/tuf/{package}/snapshot.json`
- `/tuf/{package}/targets.json`
- `/v2/`
- `/v2/{name}/manifests/{reference}`
- `/v2/{name}/blobs/{digest}`
- `/v2/{name}/referrers/{digest}`

Mutable OCI references are denied on the public read path. Clients resolve
channels through the distribution API or TUF metadata, then pull immutable
digests.

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

## TUF

Promoting a target signs a complete TUF metadata set for the package. The first
implementation uses an Ed25519 online signing key loaded from service
credentials:

- root version is currently fixed at `1`;
- targets, snapshot, and timestamp versions advance monotonically per package;
- replaying a `PromoteDistributionTarget` idempotency key returns the published
  target without advancing TUF versions.

Key rotation should add multi-key root metadata and threshold policy before
production customer usage.

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
- `distribution.target.tuf_signed`
- `distribution.target.published`
- `distribution.target.resolve_allowed`
- `distribution.target.resolve_denied`
- `distribution.oci.manifest_served`
- `distribution.oci.blob_served`
- `distribution.tuf.metadata_served`
- `distribution.upgrade.download_verified`
- `distribution.artifact.quarantined`
- `distribution.artifact.replication_ensured`

ClickHouse inserts use `batch.AppendStruct` and `ch` struct tags.

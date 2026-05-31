# Release Architecture

Verself releases are package-owned build flows admitted by a package-agnostic
distribution control plane. The first vertical slice is `mksk`, but the
architecture is for any first-party CLI or SDK that can emit OCI artifacts and
standard evidence.

The invariant is:

> Package tooling owns release semantics. distribution-service owns immutable
> artifact admission, public OCI read gates, and channel pointers.

This keeps package-specific build knowledge out of distribution-service while
still giving releases a trusted signing and publishing path.

The target release pipeline is:

```text
prepare
  -> build
  -> sign/publish bytes
  -> admit
  -> promote
```

Promotion is intentionally later work. The first end-to-end goal is to cut a
nightly `mksk` release that is built in a trusted environment, published to Zot
as immutable OCI content with referrers, admitted by distribution-service, and
visible at `https://oci.verself.sh/releases/mksk/<version>`.

## Goals

- Build and publish `mksk` nightly, RC, and stable releases from exact source
  commits.
- Keep all package-specific versioning and build targets under
  `src/make-skill`.
- Build release bytes with Bazel.
- Emit standard evidence: SLSA provenance, SPDX SBOMs, license evidence, test
  evidence, signatures, and trusted-build attestation.
- Publish artifact bytes and evidence as OCI manifests, blobs, and referrers in
  Zot.
- Make distribution-service verify registry truth instead of trusting submitted
  evidence fields.
- Gate public OCI reads for admitted immutable digests.
- Keep version/SHA authority explicit and auditable.

## Non-Goals

- Do not introduce a repo-specific release ledger format.
- Do not make distribution-service a build system.
- Do not use cargo-dist as a second build graph competing with Bazel.
- Do not require a workflow YAML for the first manual vertical slice.
- Do not make `aspect release` own version bump or release-note policy for all
  ecosystems.
- Do not implement channel promotion before build, publish, and admission are
  trustworthy.

## Component Boundaries

### Package Release Tooling

Package release tooling lives with the package.

For `mksk`:

```text
src/make-skill/release/
  cmd/mksk-release/
  docs/release.md
```

Responsibilities:

- derive package-owned versions;
- resolve `--source-ref` to an exact git commit;
- validate release subjects;
- run package-owned Bazel tests and build targets;
- generate build-local evidence;
- publish OCI artifact and referrers by calling the release builder and
  distribution admission surfaces.

Package release tooling does not hold root signing material.

### Aspect Task Surface

`aspect release mksk` is an operator convenience wrapper over package-owned
targets.

Target command catalog:

```text
aspect release mksk --nightly --source-ref=main
aspect release mksk --nightly --source-ref=main --publish

aspect release mksk --rc --source-ref=HEAD
aspect release mksk --stable --source-ref=<rc-source-sha>
aspect release mksk --stable --from-rc=mksk-v0.2.0-rc.2

aspect release mksk --channel=nightly --version=0.2.0-nightly.20260528.1 --source-ref=main
aspect release mksk --channel=rc --version=0.2.0-rc.1 --source-ref=HEAD
aspect release mksk --channel=stable --version=0.2.0 --source-ref=HEAD
```

The wrapper should stay thin. It builds the package release tool and passes
flags through.

There is no public `plan` command. Subject derivation is an internal step used
by `build` and `publish`.

### Trusted Release Builder

The release builder is distribution-owned infrastructure, not
distribution-service request handling code.

Responsibilities:

- launch a trusted builder VM on TPM 2.0-capable production hardware;
- pass the release subject into the guest;
- receive artifact/evidence outputs from the guest;
- verify TPM quote, AK enrollment, PCR policy, and builder image identity;
- authorize a short-lived signing delegation;
- push immutable OCI content to Zot.

The first implementation can be a manual host command. Later, release-service
or Temporal can call the same command.

### OpenBao Transit

OpenBao Transit owns non-exportable root signing keys.

Responsibilities:

- hold release root signing keys;
- sign short canonical delegations over the guest ephemeral public key, release
  subject, artifact digest, and accepted TPM quote transcript;
- verify signatures for admission tooling and operator diagnostics.

OpenBao Transit should never receive artifact bytes. Sign digests or short
canonical envelopes.

### Zot

Zot is the byte store.

Responsibilities:

- store OCI manifests and blobs;
- expose OCI Distribution 1.1 referrers;
- keep pushed content addressable by digest.

Zot does not make release policy decisions.

### distribution-service

distribution-service is the artifact control plane.

Responsibilities:

- verify registry truth for an OCI digest;
- verify required referrers and evidence;
- persist admitted artifact identity and descriptor graph;
- serve release metadata from admitted artifact state;
- gate public digest-addressed OCI reads;
- later, promote package channels to admitted digests.

distribution-service must not know Bazel labels for `mksk`.

## Release Subject

The release subject is the package-agnostic identity of a releasable artifact.
It is required before build and is recorded in SLSA provenance, OCI
annotations, signatures, admission records, and metadata pages.

```go
type ReleaseSubject struct {
    Package      PackageName
    Channel      ReleaseChannel
    Version      PackageVersion
    SourceRepo   SourceRepository
    SourceRef    SourceRef
    SourceCommit GitCommit
    Platform     Platform
    Flavor       ArtifactFlavor
}

type Platform struct {
    OS   string
    Arch string
}
```

All fields are required after command parsing. Do not carry nullable fields
through build, publish, or admission. Command-specific parsing should normalize
into this type.

Boundary command request types:

```go
type NightlyCutRequest struct {
    SourceRef string
}

type RCCutRequest struct {
    SourceRef string
}

type StableCutRequest struct {
    RCSourceCommit string
}

type ExactCutRequest struct {
    Channel string
    Version string
    SourceRef string
}
```

The release tool converts one of these into a complete `ReleaseSubject`.

## Version Authority

Version authority is package-owned.

For `mksk`:

- nightly derives from the next base version and UTC date/counter:
  `0.2.0-nightly.20260528.1`;
- RC derives from the next unreleased base and the next RC tag:
  `0.2.0-rc.2`;
- stable derives from a selected RC lineage but rebuilds from the same source
  commit with final SemVer, for example `0.2.0`, because the version is
  embedded in the binary.

Tags are created only after publish and admission succeed:

```text
mksk-v0.2.0-rc.1
mksk-v0.2.0
```

## Source Authority

Source SHA authority comes from git and is resolved by package tooling:

1. Resolve `--source-ref` to a 40-character commit.
2. Verify the commit is in the expected repository history.
3. Build from that exact commit.
4. Record the resolved commit in SLSA provenance, OCI annotations, signatures,
   and admission.

The operator's local workspace is not authority for published bytes. Local
dirty builds are allowed only for inspection bundles.

## Build Bundle

The side-effect-free build step emits an inspectable bundle under:

```text
artifacts/releases/<package>/<run>/
```

For `mksk`:

```text
artifact/make-skill.tar
artifact/make-skill.tar.sha256
sbom/make-skill.artifact.spdx.json
sbom/make-skill.source.spdx.json
licenses/make-skill.cargo-about.json
evidence/make-skill.provenance.intoto.json
README.txt
checksums.sha256
```

Bundle data model:

```go
type EvidenceFile struct {
    Path      string
    Digest    Digest
    SizeBytes int64
    MediaType string
}

type BuildBundle struct {
    Subject       ReleaseSubject
    Artifact      EvidenceFile
    SourceSBOM    EvidenceFile
    ArtifactSBOM  EvidenceFile
    Licenses      EvidenceFile
    Provenance    EvidenceFile
}
```

Nightly, RC, and stable use the same build target. Channel policy controls
which evidence is required for admission.
Package-owned tests run before bundle emission. Raw test reports remain CI and
ClickHouse evidence.

## OCI Layout

Use standard OCI manifests, blobs, and referrers.

Repository:

```text
releases/mksk
```

Public immutable reference:

```text
oci.verself.sh/releases/mksk@sha256:<manifest-digest>
```

The subject manifest contains the release tar as a layer and package metadata
as OCI annotations:

```text
artifactType: application/vnd.verself.mksk.release.tar
annotations:
  org.opencontainers.image.source=<source_repo>
  org.opencontainers.image.revision=<source_commit>
  org.opencontainers.image.version=<version>
  sh.verself.release.package=<package>
  sh.verself.release.channel=<channel>
  sh.verself.release.platform=<os>/<arch>
  sh.verself.release.flavor=<flavor>
```

Referrers attached to the subject digest:

```text
application/vnd.in-toto+json
application/spdx+json
application/vnd.verself.release.licenses+json
application/vnd.verself.release.tests+xml
application/vnd.dev.cosign.artifact.sig.v1+json or notation signature
application/vnd.verself.release.tpm2-attestation
application/vnd.verself.release.signing-delegation
```

The exact signature artifact format should be chosen by tool compatibility.
The durable requirement is that signatures are OCI referrers bound to the
subject digest.

## Admission Request

Admission should not submit evidence truth. It should submit the claimed
subject and the OCI subject digest to verify.

```go
type AdmitArtifactRequest struct {
    Subject           ReleaseSubject
    OriginRegistryURL string
    PublicRegistryURL string
    OCIRepository     string
    OCIDigest         Digest
    PolicyRef         string
    SubmittedBy       string
    IdempotencyKey    string
}
```

distribution-service derives the rest by reading Zot:

- manifest media type;
- manifest size;
- layer descriptors;
- config descriptor;
- referrer descriptors;
- evidence document digests;
- signer identity;
- builder id;
- source repository and commit;
- release metadata URL.

This removes request-trusted evidence fields from the security boundary.

## distribution-service Data Model

Keep the existing core tables:

```text
distribution_artifacts
distribution_artifact_evidence
distribution_channel_targets
distribution_idempotency_keys
```

Add a descriptor graph table before public OCI traffic is high enough for
origin refetching to matter:

```text
distribution_oci_descriptors
  descriptor_id uuid primary key
  artifact_id uuid not null
  oci_repository text not null
  parent_digest text not null
  descriptor_digest text not null
  descriptor_media_type text not null
  descriptor_size_bytes bigint not null
  descriptor_role text not null
  artifact_type text not null default ''
  annotations_sha256 text not null default ''
  created_at timestamptz not null
```

`descriptor_role` values:

```text
subject_manifest
config
layer
referrer
referrer_blob
```

Use empty strings instead of nullable optional metadata. If a descriptor does
not have `artifactType` or annotations, record `''`.

Release metadata pages should be derived from admitted artifact and descriptor
state. Do not add a separate release metadata schema unless the derived view
becomes too expensive or needs independent retention.

## State Machines

### Release Run

Release run state can be workflow-local at first. It should become Temporal
workflow state later, but not a custom distribution ledger.

```text
planned
  -> source_resolved
  -> build_started
  -> build_succeeded
  -> publish_started
  -> published
  -> admission_requested
  -> admitted
  -> tag_created

build_started -> build_failed
publish_started -> publish_failed
admission_requested -> admission_rejected
tag_created -> complete
```

`tag_created` is only valid for RC and stable. Nightly cuts do not need git
tags.

### Build Bundle

```text
declared
  -> source_checked_out
  -> tests_passed
  -> artifact_built
  -> evidence_generated
  -> bundle_complete

source_checked_out -> source_rejected
tests_passed -> tests_failed
artifact_built -> artifact_mismatch
evidence_generated -> evidence_failed
```

The build step is side-effect free. It must not mutate distribution-service or
Zot.

### Signing Session

```text
ephemeral_key_created
  -> guest_attested
  -> attestation_verified
  -> root_delegation_signed
  -> artifacts_signed

guest_attested -> attestation_rejected
attestation_verified -> delegation_denied
artifacts_signed -> signature_verification_failed
```

Root signing material never leaves OpenBao Transit. The guest only holds an
ephemeral key.

### OCI Publish

```text
subject_blobs_uploaded
  -> subject_manifest_pushed
  -> evidence_blobs_uploaded
  -> referrer_manifests_pushed
  -> referrers_verified
  -> publish_complete

subject_manifest_pushed -> digest_mismatch
referrer_manifests_pushed -> missing_referrer
```

All public reads use digest references. Mutable tags are not part of the public
release contract.

### Artifact Admission

distribution-service owns this durable state:

```text
submitted
  -> verifying
  -> admitted
  -> available

verifying -> rejected
available -> quarantined
available -> superseded
quarantined -> retained
```

`available` means the immutable digest is publicly readable through the
distribution OCI gate. It does not mean a channel points at it.

### Channel Target

Promotion is later work, but the target state remains:

```text
draft_target
  -> policy_checked
  -> published
  -> superseded

policy_checked -> denied
published -> rollback_target_published
```

## Security Model

### Trust Boundaries

Operator workstation:

- trusted to request a release;
- not trusted to produce published bytes;
- not trusted to sign release artifacts.

Package release tooling:

- trusted to define package release semantics and Bazel targets;
- not trusted with root signing keys;
- not trusted by admission unless evidence and registry truth validate.

Trusted release builder host:

- trusted to launch the builder VM and forward outputs;
- not trusted to mutate builder inputs without detection by TPM quote and PCR
  policy;
- still part of the trusted computing base for availability and launch
  correctness.

Trusted release builder guest:

- trusted only after attestation verifies expected TPM AK identity, PCR
  selection, event log, builder image, and release input digest binding;
- holds the ephemeral artifact signing key for one release run;
- never receives OpenBao root signing material.

OpenBao Transit:

- trusted root for release signing authorization;
- holds non-exportable root signing keys;
- signs only delegations or digests.

Zot:

- trusted for content-addressed storage availability;
- not trusted for policy;
- all bytes are verified by digest and referrer graph.

distribution-service:

- trusted to verify admission policy;
- trusted to gate public OCI reads;
- trusted to publish channel pointers later;
- not trusted to produce build provenance.

### TPM Builder Policy

The release builder must verify before signing:

- TPM AK identity is enrolled for the builder pool;
- TPM quote signature verifies against the enrolled AK;
- quote `extraData` equals the release input digest;
- PCR values match an approved builder measurement policy;
- event log replay matches quoted PCRs when an event log is supplied;
- TPM certification proves the ephemeral release public key was created under
  the quoted TPM;
- builder root filesystem image digest matches policy.

### Signing Policy

Use a two-level model:

1. The guest creates an ephemeral key for the release run.
2. distribution-service verifies TPM attestation and asks OpenBao Transit to
   sign a delegation over:

```text
release_input_digest =
  H(domain_separator,
    distribution_challenge,
    package,
    version,
    source_commit,
    platform,
    flavor,
    oci_manifest_digest,
    artifact_digest,
    provenance_digest,
    sbom_digest,
    tpm_release_public_name,
    tpm_release_public_blob_digest)
```

The ephemeral key signs artifact and evidence referrers. The OpenBao root
delegation makes the ephemeral key trusted for exactly one release subject and
digest.

Admission rejects signatures when:

- the delegation is missing;
- the delegation is expired;
- the delegation subject differs from the admission subject;
- the delegation digest differs from the OCI subject digest;
- the ephemeral signature does not verify;
- the root signature does not verify against the configured OpenBao public key;
- the root key version is not trusted for the channel.

### Admission Policy

distribution-service verifies:

- manifest exists at Zot by digest;
- manifest digest and size match the request;
- required subject annotations match `ReleaseSubject`;
- artifact layer digest matches SLSA subject digest;
- OCI referrers exist for required evidence kinds;
- SLSA predicate type is `https://slsa.dev/provenance/v1`;
- SLSA builder id is trusted for package/channel;
- SLSA source repository and commit match the request;
- package, version, channel, platform, and flavor match the request;
- SBOM and license evidence exist;
- signer delegation is trusted;
- TPM attestation policy passed;
- channel/version policy is valid.

Nightly policy may be weaker than RC/stable, but the difference must be visible
in admission metadata and ClickHouse events.

### Replay and Idempotency

Admission idempotency scope:

```text
admit:<package>:<channel>:<version>:<platform>:<flavor>:<source_commit>:<digest>
```

Publishing the same bytes for the same subject is idempotent. Publishing
different bytes for the same subject is a conflict. Publishing the same digest
for a different subject is rejected unless the subject metadata also matches.

Git tags are created only after admission succeeds. If tag creation fails after
admission, the operator can retry tag creation without rebuilding.

### Public Read Policy

Public OCI reads are digest-addressed only:

```text
GET /v2/<repo>/manifests/sha256:<digest>
GET /v2/<repo>/blobs/sha256:<digest>
GET /v2/<repo>/referrers/sha256:<digest>
```

Mutable references must resolve through distribution APIs. Blob reads are
allowed only when the blob digest appears in the admitted descriptor graph for
the requested repository. Quarantined artifacts deny manifest, blob, referrer,
target resolution, and update checks.

## API Surfaces

Package-owned release command:

```text
mksk-release build
mksk-release publish
mksk-release admit
mksk-release tag
```

`aspect release mksk` can combine these for operator ergonomics.
`mksk-release build` and `mksk-release publish` both derive complete subjects
from intent flags before executing.

distribution-service internal admission:

```text
POST /internal/v1/distribution/artifacts:admit
```

distribution-service public release metadata:

```text
GET /releases/{package}/{version}
```

distribution-service public OCI read gate:

```text
GET /v2/{name}/manifests/{digest}
GET /v2/{name}/blobs/{digest}
GET /v2/{name}/referrers/{digest}
```

## Manual mksk Flow

Nightly from main:

```text
aspect release mksk --nightly --source-ref=main --publish
```

Expected actions:

1. Resolve `main` to a commit.
2. Derive `0.2.0-nightly.YYYYMMDD.N`.
3. Launch trusted builder.
4. Build `//src/make-skill:release_tar`.
5. Generate evidence.
6. Sign and publish OCI subject plus referrers.
7. Ask distribution-service to admit the digest.
8. Render release metadata at `/releases/mksk/<version>`.

RC from current commit:

```text
aspect release mksk --rc --source-ref=HEAD --publish
```

Stable from selected RC source:

```text
aspect release mksk --stable --source-ref=<rc-source-sha> --publish
```

Stable rebuilds from the RC source commit with the final SemVer.

## Observability

Every release run should emit ClickHouse traces/events for:

- source resolution;
- version derivation;
- builder launch;
- guest attestation verification;
- Bazel test/build;
- SBOM/license/provenance generation;
- OCI push;
- referrer verification;
- OpenBao Transit signing;
- distribution admission;
- release metadata serving.

distribution-service event names should remain stable:

```text
distribution.artifact.admit_requested
distribution.artifact.verification_started
distribution.artifact.verification_allowed
distribution.artifact.available
distribution.artifact.verification_denied
distribution.oci.manifest_served
distribution.oci.blob_served
distribution.oci.referrers_served
distribution.artifact.quarantined
```

The release builder should include the release subject and OCI digest on spans,
not the full artifact payload.

## Implementation Sequence

1. Extend `mksk-release build` and `mksk-release publish` with version
   derivation for nightly, RC, and stable.
2. Add `mksk-release publish` against Zot using OCI subject manifests and
   referrers.
3. Add TPM release input digest and distribution-service attestation
   verification.
4. Add OpenBao release Transit convergence: mount, key, policy, role, and
   signer client.
5. Harden distribution-service admission to read Zot and verify descriptor
   truth.
6. Persist the OCI descriptor graph for admitted artifacts.
7. Add `/releases/{package}/{version}` backed by admitted artifact state.
8. Fix `mksk` consumer channel/flavor behavior before channel promotion.
9. Add channel promotion after admission is reliable.

## Source Notes

- OCI Distribution Specification:
  <https://specs.opencontainers.org/distribution-spec/>
- OCI image/referrer behavior and artifact manifests:
  <https://oci-playground.github.io/specs-latest/specs/distribution/v1.1.0-rc2/oci-distribution-spec.html>
- SLSA provenance v1:
  <https://slsa.dev/spec/v1.1/provenance>
- OpenBao Transit:
  <https://openbao.org/docs/secrets/transit/>
- OpenBao Transit API:
  <https://openbao.org/api-docs/secret/transit/>
- Trusted Computing Group TPM 2.0 Library:
  <https://trustedcomputinggroup.org/resource/tpm-library-specification/>
- Google go-attestation:
  <https://github.com/google/go-attestation>
- Zot OCI artifact workflow:
  <https://zotregistry.dev/v2.1.0/articles/workflow/>

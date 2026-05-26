# release-service

The release-service is the current internal control plane for Verself release
state. It records release packages, versions, immutable Zot-hosted OCI
artifacts, supply-chain evidence, install options, lifecycle advisories, and
provider publication tasks.

This service exists because distribution-service and publishing-service are
deliberate future splits. Until those services are extracted, release-service
owns the registry of release intent and provider fan-out, but it still treats
bytes, signatures, and provenance as external immutable facts produced by CI.

## Boundary

- Owns release registry state, policy decisions, lifecycle overlays,
  provider-publication coordination, provider receipts, and operator explorer
  read models.
- Does not build packages, run Bazel, sign build provenance, or upload bytes on
  behalf of CI.
- Uses Zot and OCI artifacts as the artifact store. Do not create a parallel
  blob store or a repo-specific release ledger.
- Uses in-toto statements, SLSA provenance, SBOMs, and provider receipts as
  evidence documents associated with immutable OCI digests.
- Uses provider-native semantics for external ecosystems: npm dist-tags and
  deprecation, PyPI yanking, crates.io yanking, Homebrew deprecate/disable, and
  package-index metadata for apt repositories.
- Keeps all APIs internal. Do not add curated SDK or verself-cli surfaces for
  release-service operations.

## Canonical Doc

Read `src/services/release-service/docs/release-service.md` before adding API,
schema, worker, or UI code. That document defines the data model, lifecycle
state diagrams, versioning model, invalidation model, and SLSA boundary.

## API Contract

Canonical contracts belong in Smithy under `src/smithy/models/verself` before
handlers are added. Internal callers use SPIFFE mTLS and service-local typed
clients. Operator browser surfaces should call a service-local backend, not the
release-service database directly.

Initial internal operations:

- `RegisterPackage`
- `CreateReleaseVersion`
- `AttachArtifact`
- `AttachEvidence`
- `VerifyRelease`
- `ApproveRelease`
- `PromoteReleaseChannel`
- `CreatePublication`
- `DispatchPublication`
- `RecordPublicationReceipt`
- `ReconcilePublication`
- `SetReleaseLifecycleStatus`
- `CreateReleaseAdvisory`
- `ListInstallOptions`

Use stable problem types for unknown package, duplicate version, invalid version
projection, missing OCI digest, digest mismatch, missing referrers, untrusted
builder, untrusted signer, policy denied, approval required, provider conflict,
provider immutable version, receipt mismatch, quarantined release, and revoked
release.

## Data Model

Release identity is separate from provider identity:

- `release_packages`: repo-owned package identity and policy.
- `release_versions`: canonical upstream version, channel kind, lifecycle
  state, source commit, and release sequence.
- `release_channels`: mutable package aliases such as stable, rc, nightly, and
  canary that point to immutable release versions.
- `release_channel_events`: append-only channel pointer history and rollback
  evidence.
- `release_provider_versions`: provider-specific version strings, tags,
  suites, formula versions, or channel aliases.
- `release_artifacts`: immutable OCI descriptors and platform variants.
- `release_evidence`: signatures, SLSA provenance, SBOMs, tests, VSAs, and
  provider receipts.
- `release_policy_checks`: verification decisions with policy inputs and
  structured denial reasons.
- `release_install_options`: source, OCI, and deferred third-party install
  instructions.
- `release_publications`: provider fan-out requests and approvals.
- `release_status_events`: append-only lifecycle overlays such as deprecated,
  yanked, quarantined, and revoked.
- `release_advisories`: OSV/CSAF/OpenVEX-oriented vulnerability and defect
  notices.

Do not collapse these into one generic ledger table. The durable facts are
typed rows plus standard evidence documents keyed by immutable artifact digests.

## Versioning

Use a canonical package version and explicit provider projections. Do not write
one universal comparator for all ecosystems.

- SemVer 2.0 is the default for CLIs and API-bearing packages.
- PEP 440, Debian version policy, npm SemVer behavior, Cargo SemVer behavior,
  Homebrew formula versions, and opaque firmware/image identifiers are provider
  projections.
- Channels, dist-tags, suites, taps, and aliases are mutable pointers. They are
  not versions.
- Stable releases, release candidates, nightlies, and canaries are release
  kinds with distinct policy, retention, and external publication defaults.

## Invalidation

Prefer immutable artifact history with mutable lifecycle overlays.

- `superseded`: a newer release should be preferred.
- `deprecated`: still installable, but users should receive a warning and a
  replacement recommendation.
- `yanked`: dependency resolution and default install paths should avoid it,
  but exact historical installs may remain possible where the ecosystem allows.
- `quarantined`: Verself resolvers must stop serving it immediately.
- `revoked`: signing identity, builder identity, provenance, or artifact
  integrity is no longer trusted.

Security events must preserve evidence, stop default resolution, publish a
standard advisory, update provider-native status, and issue a fixed release
instead of mutating historical artifacts.

## Security and SLSA

CI owns build execution, artifact upload, artifact signing, and SLSA build
provenance. release-service verifies and records those facts before approving a
release or provider publication.

Near-term target is SLSA Build L2 for CI-produced artifacts with signed
provenance from a hosted Verself builder path. Longer-term Build L3 requires
stronger builder isolation so build steps cannot alter provenance or signing
decisions.

Provider credentials belong in provider adapters and must be stored through
secrets-service/OpenBao. Prefer provider OIDC/trusted-publisher flows when an
ecosystem supports them.

## Non-goals

- No Nomad release orchestration.
- No Bazel graph execution inside the service.
- No public customer SDK surface.
- No arbitrary package-owned publish plugins running with service credentials.
- No mutable artifact bytes.

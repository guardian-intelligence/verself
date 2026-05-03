# Artifact Admission

Artifact admission is the boundary between upstream package distribution and
Verself-controlled installation. Direct upstream install paths are inventory
inputs; admitted artifacts are the only installable outputs.

The Artifacts Service owns admission policy, scanner execution, signing,
catalog generation, and evidence emission. Storage and metadata services are
implementation details below that boundary.

Internal distribution uses `zot` as the OCI registry for admitted artifacts.
OCI Distribution is content-type agnostic, so container images, tarballs, kernel
images, rootfs inputs, SBOMs, provenance statements, scanner outputs, signatures,
and attestations can share a single digest-addressed distribution surface. See
the OCI Distribution Specification and zot documentation:
<https://github.com/opencontainers/distribution-spec/blob/main/spec.md> and
<https://zotregistry.dev/>.

Internal admission flow:

```text
upstream source
  -> Artifacts Service fetch by explicit URL and expected digest
  -> quarantine age check
  -> scanner and source-reputation evidence ingestion
  -> SBOM and provenance generation
  -> OCI artifact push to zot
  -> SBOM/provenance/scanner/signature evidence in the OCI manifest
  -> Cosign or Notation signature and admission attestation verification
  -> generated admitted-artifacts catalog
  -> Ansible, Bazel, guest image staging, and developer tooling consume by digest
```

Internal consumers use `registry/repository@sha256:<digest>` references. Tags are
diagnostic labels and are never install authorities. Pullers verify the digest,
the Verself admission signature, and the admission attestation before unpacking
or executing bytes. Verification failures are policy failures.

The internal zot instance is loopback-only in the single-node topology. It
allows anonymous reads, rejects unauthenticated writes, and grants create/update
only to the generated local publisher identity used by artifact admission.

zot may mirror upstream OCI registries only when the mirror is configured for
the admission policy. Docker-format image conversion can change digests and
invalidate upstream signatures unless compatibility and digest preservation are
configured; mirrored OCI inputs that cannot preserve the upstream digest are
treated as newly admitted artifacts under the resulting digest. See zot
mirroring and signature-verification docs:
<https://zotregistry.dev/v2.1.15/articles/mirroring/> and
<https://zotregistry.dev/v2.1.5/articles/verifying-signatures/>.

zot's CVE scanning extension uses Trivy. It is acceptable for internal registry
visibility when the zot build, embedded Trivy dependency set, and vulnerability
database artifacts are admitted and pinned. zot must not fetch vulnerability
databases from the public internet during host convergence or artifact
installation; database refreshes are admitted artifacts mirrored into internal
zot first. Admission policy treats scanner execution failures as failed
admissions and records the scanner version and database digest with each result.

## Metadata

Each artifact source has a policy identity and an admission record in
`src/host-configuration/supply-chain/policy.json`:

- source path, source kind, surface, and artifact name
- upstream URL
- sha256 digest when upstream bytes are directly fetched
- release time and observed time
- minimum-age result
- scanner result pointer and scanner/database digests
- SBOM pointer
- SLSA/in-toto provenance pointer
- OCI repository, OCI manifest digest, and OCI artifact media type
- OCI referrer digests for signature, admission attestation, SBOM, provenance,
  and scanner output
- storage URI for large evidence objects kept outside zot

Policy evaluation is fail-closed for untracked source paths. Existing direct
sources remain `provisional` until their bytes are fetched, aged, scanned,
stored, signed, and published to zot. `admitted` entries must carry an OCI
manifest digest, an admission signature, and an admission attestation.

The generated admitted-artifacts catalog is the handoff between policy and
installers. It is produced by the Artifacts Service after admission and contains
only digest-addressed OCI references, expected signatures, attestation subjects,
media types, unpack instructions, and policy evidence pointers. Bazel repository
rules, Ansible roles, guest rootfs staging, `scripts/bootstrap-*`, and developer
tool installers read the catalog rather than embedding upstream URLs.

## Signing

Admission signatures bind the OCI manifest digest to the Verself admission
policy identity. Cosign is the default signing path for OCI artifacts because it
supports OCI-registry signature storage, key-based verification, keyless
verification, and attestation verification. Notation is available when X.509
trust-policy semantics are a better match for a target consumer. See Sigstore
Cosign verification docs:
<https://docs.sigstore.dev/cosign/verifying/verify/>.

The admission attestation is an in-toto statement whose subject is the OCI
manifest digest. Its predicate includes the upstream URL, expected digest,
observed time, minimum-age decision, scanner summaries, SBOM subject, provenance
subject, policy version, and admission decision. SLSA provenance is attached
when the artifact is built by Verself; third-party fetched bytes carry observed
source metadata and upstream provenance pointers when available.

## Enforcement

`verself-deploy supply-chain check` scans the repo for install/fetch paths and
compares the result with the policy file. `aspect check --kind=supply-chain`
runs that gate in review and is also the first gate in `aspect check --kind=all`.

`aspect deploy` is the deployment entry point. `verself-deploy run` evaluates
the same policy gate before host convergence and records the evaluation after
host convergence so first-run ClickHouse migrations can create the evidence
table. During the admission rollout, deploy uses provisional mode: unknown or
rejected sources fail the deploy, while tracked-but-not-yet-admitted artifacts
continue as provisional rows. It inserts one row per source into
`verself.supply_chain_policy_events`, with the deploy run key, source surface,
policy result, admission state, distribution references, storage URI, and
trace/span IDs.

Operational breakglass is founder-held root SSH access to the host. It is not a
routine deployment mode and is not part of `aspect deploy`.

Deployment evidence query:

```sql
SELECT
  deploy_run_key,
  site,
  surface,
  source_kind,
  policy_result,
  admission_state,
  count() AS findings
FROM verself.supply_chain_policy_events
WHERE deploy_run_key = {deploy_run_key:String}
GROUP BY deploy_run_key, site, surface, source_kind, policy_result, admission_state
ORDER BY surface, source_kind, policy_result;
```

`aspect artifacts evidence --run-key=<deploy-run-key>` is the post-deploy
assertion gate. It recomputes the local policy evaluation, then verifies that
ClickHouse contains exactly the expected number of policy rows for the deploy
run, zero rejected rows, non-empty trace IDs, one supply-chain trace ID, OK
`policy_check` and `policy_record` spans, and a succeeded deploy event without a
failed deploy event. Provisional rows are expected until admission metadata is
complete.

Artifact admission and install verification are deploy-flow internals rather
than operator-facing deploy choices. Admission fetches upstream bytes, verifies
the declared sha256 digest, enforces release age, requires scanner/signature/
provenance evidence, generates a CycloneDX SBOM, publishes an OCI manifest to
zot, and records the OCI manifest digest, evidence digests, storage URI, GUAC
subject, and trace/span IDs in `verself.artifact_admission_events`. Install
verification fetches the digest-addressed manifest from zot, verifies the
manifest digest, requires the expected admission signature and attestation
digests, and records the installer, surface, artifact, OCI reference, policy
result, and trace/span IDs in
`verself.artifact_install_verification_events`.

`aspect artifacts admission-evidence --run-key=<deploy-run-key>` verifies the
artifact admission and install verification rows and the corresponding
`artifacts.admit` and `artifacts.install_verify` spans.

Installers emit verification events with artifact name, OCI reference, manifest
digest, signature digest, attestation digest, policy result, deploy run key, and
trace/span IDs. ClickHouse remains the live proof surface for host convergence
and guest image staging. GUAC is the graph layer over SBOMs, provenance,
vulnerability data, signatures, admission attestations, and install evidence; it
feeds policy analysis, incident response, and blast-radius queries.

## Node Policy

pnpm hardening is configured in the workspace so agents and CI resolve the same
policy. pnpm 10 supports dependency age quarantine via `minimumReleaseAge`,
reviewed dependency build scripts via `strictDepBuilds`, and explicit build
allow/deny maps via `allowBuilds`; those settings are documented in pnpm's
workspace settings reference:
<https://pnpm.io/settings#minimumreleaseage>,
<https://pnpm.io/settings#strictdepbuilds>, and
<https://pnpm.io/settings#allowbuilds>.

`onlyBuiltDependencies` is intentionally retained as compatibility debt for
`aspect_rules_js` 3.0.3. The repo's canonical pnpm build-script policy is
`allowBuilds`, but the Bazel `npm_translate_lock` integration still requires
`onlyBuiltDependencies` to decide which package lifecycle hooks it may generate.
The two allowlists must stay byte-for-byte equivalent until `aspect_rules_js`
recognizes `allowBuilds` directly.

Vite+ owns package-manager execution in the frontend workspace. Agents and
deploy tooling run `vp install` pointed at the Verdaccio mirror; direct `npm`,
`pnpm`, Yarn, and Corepack invocations are outside the frontend install
contract.

## Implementation Plan

1. Extend the policy and evidence schema with OCI distribution fields:
   `install_url`, `oci_repository`, `oci_manifest_digest`, `oci_media_type`,
   `signature_digest`, `attestation_digest`, `sbom_digest`,
   `provenance_digest`, `scanner_result_digest`, `scanner_name`,
   `scanner_version`, `scanner_database_digest`, and `guac_subject`.

2. Add a `zot` host role backed by an admitted zot artifact. Configure local ZFS
   storage for the single-node topology, S3-compatible storage when Garage is
   ready, authenticated push access for the Artifacts Service, and read access
   for host convergence and build clients. Disable unauthenticated pushes and
   public upstream egress from zot.

3. Implement admission inside the deployment-tools artifact package. Admission
   fetches upstream bytes, verifies the expected digest, enforces quarantine
   age, requires scanner/signature/provenance evidence, generates an SBOM,
   publishes an OCI artifact to zot, writes policy evidence, and emits
   ClickHouse rows. Replace the small in-process OCI HTTP publisher with ORAS
   libraries before broad cutover to avoid carrying a registry client as
   platform logic. Wire Cosign or Notation verification, Syft SBOM generation,
   Grype/OSV-Scanner/GuardDog/Scorecard scanner execution, and zot/Trivy scan
   results into the same evidence contract.

4. Generate an admitted-artifacts catalog as a Bazel output. The catalog becomes
   the only source for host binary downloads, Bazel external artifact rules,
   guest image inputs, dev-tool bundles, Verdaccio packaging, uv tools, Ansible
   collections, and bootstrap binaries. Repo checks reject new raw upstream
   install paths outside the admission implementation.

5. Cut over installers by surface. Start with high-risk host and guest paths:
   guest rootfs apt staging, Verdaccio, Bazel `http_file` server tools, guest
   kernel/Firecracker/runner inputs, dev-tool binaries, uv tools, and Ansible
   Galaxy collections. Each cutover removes the direct upstream path and adds a
   verifier that consumes the admitted catalog by digest.

6. Enforce the canonical deploy boundary. `aspect deploy` is how deployment is
   done; `verself-deploy` subcommands remain implementation seams for the task
   surface and e2e assertions. Operational breakglass is founder-held root SSH
   access to the host, outside the routine deploy surface.

7. Ingest admission and installation evidence into GUAC. GUAC receives SBOMs,
   provenance, vulnerability findings, signatures, admission attestations, and
   ClickHouse install evidence. Operational gates continue to use local policy
   verification and ClickHouse evidence.

RSTUF and public artifact distribution are out of scope for this design and
need a separate public-update-system design before implementation.

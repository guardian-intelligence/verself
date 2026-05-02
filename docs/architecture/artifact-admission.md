# Artifact Admission

Artifact admission is the boundary between upstream package distribution and
Verself-controlled installation. Direct upstream install paths are inventory
inputs; admitted artifacts are the only installable outputs.

The admission contract follows TUF's target model: targets metadata binds a
target path to length and hashes, snapshot metadata pins target metadata
versions, and timestamp metadata gives clients the current snapshot entry point.
See the TUF specification for top-level roles and target metadata semantics:
<https://theupdateframework.github.io/specification/v1.0.19/>.

The repository implementation target is Repository Service for TUF (RSTUF). RSTUF
separates the REST API from the worker that updates, signs, and publishes TUF
metadata, and its deployment model requires Redis/RabbitMQ for task transport,
Redis for result/settings state, and Postgres for persistent state. See the RSTUF
guide and deployment-planning docs:
<https://repository-service-tuf.readthedocs.io/en/v0.12.0/guide/> and
<https://repository-service-tuf.readthedocs.io/en/v0.12.0/guide/deployment/planning/deployment.html>.

## Metadata

Each artifact source has a policy identity and an admission record in
`src/host-configuration/supply-chain/policy.json`:

- source path, source kind, surface, and artifact name
- upstream URL
- sha256 digest when upstream bytes are directly fetched
- release time and observed time
- minimum-age result
- scanner result pointer
- SBOM pointer
- SLSA/in-toto provenance pointer
- TUF target path
- storage URI

Policy evaluation is fail-closed for untracked source paths. Existing direct
sources remain `provisional` until their bytes are fetched, aged, scanned,
stored, and published behind signed TUF metadata. `admitted` entries must carry a
TUF target path and storage URI.

## Enforcement

`verself-deploy supply-chain check` scans the repo for install/fetch paths and
compares the result with the policy file. `aspect check --kind=supply-chain`
runs that gate in review and is also the first gate in `aspect check --kind=all`.

`verself-deploy run` evaluates the same policy gate before host convergence and
records the evaluation after host convergence so first-run ClickHouse migrations
can create the evidence table. It inserts one row per source into
`verself.supply_chain_policy_events`, with the deploy run key, source surface,
policy result, admission state, TUF target path, storage URI, and trace/span IDs.

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

## Node Policy

pnpm hardening is configured in the workspace so agents and CI resolve the same
policy. pnpm 10 supports dependency age quarantine via `minimumReleaseAge`,
reviewed dependency build scripts via `strictDepBuilds`, and explicit build
allow/deny maps via `allowBuilds`; those settings are documented in pnpm's
workspace settings reference:
<https://pnpm.io/settings#minimumreleaseage> and
<https://pnpm.io/settings#strictdepbuilds>.

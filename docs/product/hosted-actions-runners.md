# Hosted Actions Runners

Verself hosted Actions runners rent short-lived Firecracker VMs for CI jobs. A
customer connects a source provider, selects runner classes, and runs existing
GitHub Actions or Forgejo Actions workflows on Verself-controlled bare metal.
The product surface is workflow execution compatibility plus optional
Verself-owned storage APIs, observability, IAM, and billing.

This document is the product contract for the hosted runner surface. Internal
implementation docs describe the VM execution state machine, ZFS lifecycle, and
provider webhooks.

## Product Contract

Unmodified workflows that run on GitHub-hosted runners or Blacksmith runners
must run on Verself hosted runners unless they depend on an intentionally
unsupported provider-only feature. Workflow authors should be able to migrate by
changing `runs-on` labels, then adopt optional Verself or Blacksmith-compatible
acceleration features when the workflow benefits from them.

The compatibility surface has four layers:

1. Runner execution compatibility: JavaScript actions, composite actions, Docker
   actions, shell steps, service containers, job containers, environment files,
   outputs, step summaries, annotations, PATH mutation, post actions, and tool
   cache conventions.
2. Provider service compatibility: upstream actions keep using provider-owned
   service contracts such as GitHub cache, artifacts, logs, OIDC, runtime
   tokens, job metadata, checkout, and tool cache behavior.
3. Explicit acceleration APIs: Verself-owned storage and cache features adopted
   through Verself actions, Verself APIs, or documented compatibility actions.
4. Blacksmith compatibility: documented Blacksmith behavior used by customer
   workflows, including sticky disks, checkout caching actions, Docker caching
   actions, and dependency cache actions that customers explicitly reference.

Provider-owned action boundary:

- Workflows using `actions/cache` use GitHub Actions cache and GitHub's runtime
  cache service. Verself does not rewrite those calls to a Verself backend.
- Verself cache is an alternative API. Customers opt in by using a Verself cache
  action, a Verself SDK/API, or a documented compatibility action that targets
  Verself.
- Twirp, Azure Blob upload semantics, GitHub runtime tokens, and provider
  webhook payloads are provider internals for pass-through behavior and
  diagnostics, not Verself product abstractions.

## Runner Classes

The hosted runner catalog launches with a single class.

| `runs-on:` label | vCPU | RAM | Architecture | OS |
| --- | --- | --- | --- | --- |
| `verself-4vcpu-ubuntu-2404` | 4 | 16 GB | x86_64 | Ubuntu 24.04 LTS |

Additional sizes are a roadmap item that opens when the launch class sustains
over 40% utilization on a single `f4.metal.medium`-class host. Until then the
catalog stays narrow so scheduler behavior, cache eviction, billing telemetry,
and customer-facing analytics stabilize against one shape.

Architecture and OS scope is x86_64 only. ARM64 labels (`-arm64`, `-arm-`) are
a non-goal: Latitude.sh `f4`-class economics do not justify a parallel ARM
fleet. Ubuntu 22.04 may be added on demand. Windows and macOS runners are
non-goals: the host substrate runs Linux Firecracker guests, and the macOS
licensing model does not fit the per-attempt rental shape.

Hosted Verself does not accept Blacksmith-named `runs-on` labels. Workflows
migrating from Blacksmith change the label to a Verself name. Blacksmith
*action* compatibility (sticky disks, checkout caching, Docker caching) is a
separate surface documented in Feature Surface and Integrations; it does not
extend to label aliasing, to renaming Verself outputs and environment
variables under a Blacksmith namespace, or to advertising the Verself product
under the Blacksmith name in any customer-visible surface.

## Pricing

The launch class bills at a single per-minute rate with millisecond resolution.

| SKU | $/min | $/vCPU-min |
| --- | --- | --- |
| `verself-4vcpu-ubuntu-2404` | 0.016 | 0.004 |

Billing semantics:

- Resolution. Each attempt's billing window is measured in elapsed
  milliseconds. The charge is `(elapsed_ms / 60_000) × per_minute_rate`,
  computed as integer microcents in the TigerBeetle ledger to avoid float
  drift.
- No minimum. There is no per-job floor and no rounding up to the next
  minute. A 12,345 ms attempt on `verself-4vcpu-ubuntu-2404` charges
  `$0.003292`.
- Window start. The window opens when `vm-orchestrator` reports the guest has
  accepted JIT runner registration and the runner has signaled ready to the
  source provider.
- Window stop. The window closes when the runner reports job completion or
  when a workflow-level cancel propagates to the runner via the source
  provider's webhook.
- Excluded time. Queue depth, VM provisioning, JIT bootstrap, sticky disk
  saveback, ZFS snapshot promotion, and post-job teardown are not metered.
- Cancellations. Cancelled attempts bill the elapsed window through the
  cancel acknowledgement timestamp only.

Invoice line items expose the fields a customer needs to reconcile against
workflow logs without operator help.

| Field | Description |
| --- | --- |
| `attempt_id` | Verself execution attempt identity. |
| `provider_run_id`, `provider_job_id` | Source-provider run and job IDs. |
| `runner_class` | `runs-on:` label served. |
| `elapsed_ms` | Billed elapsed milliseconds. |
| `rate_per_minute` | Headline per-minute rate at attempt time. |
| `amount_microcents` | Computed charge in integer microcents. |

Pricing applies identically to GitHub Actions and Forgejo Actions runner
attempts. Storage (sticky disk volumes, Verself archive cache blobs) and egress
beyond the Latitude.sh-included 20 TB are roadmap line items and are not billed
at launch; the only metered resource is runner-attempt compute.

## Customer Onboarding

Hosted onboarding:

1. Create or select an organization.
2. Install the Verself GitHub App or connect a Forgejo instance.
3. Select repositories and runner classes.
4. Update workflow `runs-on` labels.
5. Run a workflow and inspect run history, logs, telemetry, and billing.
6. Enable acceleration features: Verself archive cache, sticky disks, checkout
   caching, Docker build caching, and language setup actions.

Self-hosted onboarding:

1. Use the bootstrap CLI to render a site onto operator-owned Latitude.sh bare
   metal.
2. Configure the installation domain, identity provider, billing provider, and
   source integrations.
3. Connect repositories to the independent installation.
4. Run the same hosted runner workflows against the self-hosted control plane.

The docs should lead with the hosted path. The self-hosted path belongs in
operator docs and should refer back to the hosted runner product contract for
workflow behavior.

## Integrations

Source provider integrations:

- GitHub App: repository installation, `workflow_job` webhooks, JIT runner
  registration, Actions runtime compatibility, GitHub token handling, and
  repository metadata.
- Forgejo Actions: repository webhook registration, runner demand sync, runner
  registration, logs, artifacts, and provider-owned job metadata.

Actions ecosystem integrations:

- Upstream GitHub actions such as `actions/checkout`, `actions/cache`,
  `actions/upload-artifact`, `actions/download-artifact`, `actions/setup-node`,
  `actions/setup-go`, `actions/setup-python`, `actions/setup-java`, and
  `docker/build-push-action`.
- Blacksmith actions such as `useblacksmith/stickydisk`,
  `useblacksmith/stickydisk-delete`, `useblacksmith/checkout`,
  `useblacksmith/setup-bazel`, and Docker caching actions.
- Verself actions for customers who want explicit Verself names in new
  workflows.

Provider-owned service contracts stay provider-owned. Product APIs expose
Verself resources such as runs, runner classes, Verself-owned archive cache
entries, sticky disks, artifacts, and usage.

## Feature Surface

| Feature | Contract | Primary surface | Status |
| --- | --- | --- | --- |
| Ephemeral runner VMs | Each job runs in an isolated Firecracker VM with a clean workspace and metered resources. | `runs-on` labels, run history, billing | Implemented foundation |
| GitHub Actions cache passthrough | Upstream cache actions use GitHub Actions cache through the provider runtime environment. | `actions/cache@v5`, setup-action cache options | Required |
| Verself archive cache | Explicit Verself cache APIs store archive blobs on Verself-owned storage with key, version, scope, and restore-key semantics. | Verself cache action/API, documented compatibility actions | Required |
| Sticky disks | A keyed ext4 filesystem is mounted at a requested path before job execution and committed after successful saveback. | `verself/stickydisk`, `useblacksmith/stickydisk` | Partially implemented |
| Sticky disk deletion | Customers can delete a sticky disk by key or reset through inventory surfaces. | Console, CLI, SDK, `useblacksmith/stickydisk-delete` | Partial API/CLI |
| GitHub checkout passthrough | `actions/checkout` uses the provider repository service and GitHub token behavior expected by upstream workflows. | `actions/checkout` | Required |
| Checkout caching | Explicit checkout acceleration uses a colocated mirror or persistent checkout disk. | `useblacksmith/checkout`, Verself checkout action/API | Partial custom action |
| Docker build caching | Docker layer state persists across jobs on runner-local storage. | Docker actions, BuildKit cache, Blacksmith-compatible actions | Required |
| Container caching | Service and job container images are hydrated close to the runner and observable as cache events. | Workflow containers, service containers | Required |
| Artifacts | Upload and download artifacts behave like GitHub Actions artifacts. | Upstream artifact actions | Required |
| OIDC | Workflows can request OIDC credentials with provider-compatible claims and Verself audit evidence. | `id-token: write`, OIDC token endpoint | Required |
| Tool cache | Language setup actions find and populate the expected hosted-tool-cache layout. | `RUNNER_TOOL_CACHE`, setup actions | Partial foundation |
| Run history and logs | Customers can search, filter, and inspect workflow jobs with trace-linked logs. | Console, CLI, SDK | Required |
| Analytics | Cache hits, misses, bytes, durations, queue time, VM startup, job time, and cost are queryable. | Console, ClickHouse, public API | Partial foundation |

## Roadmap Capabilities

These are committed roadmap items not in the launch contract. Each graduates
into a Feature Surface row when ready for general availability.

- SSH-debug-into-runner. A customer with sufficient role on a run attaches an
  interactive shell to a live or recently-failed attempt for diagnosis.
  Sessions tunnel through Pomerium with the same SSO and audit controls as
  operator SSH and are bounded by the attempt's billing window. Sticky disk
  saveback during a debug-extended attempt follows the same observed-state
  generation rules as a normal attempt.
- Custom runner images. An organization pre-bakes a rootfs based on the
  Verself runner image. Custom images flow through artifact admission
  (`docs/architecture/artifact-admission.md`) before any `runs-on:` label can
  reference them, and are scoped to the owning organization. Per-class
  default images stay Verself-managed.
- Static egress IPs. An organization requests a stable IPv4 pool for outbound
  runner traffic so customer vendors can allowlist by IP. Pools are scoped
  per organization, allocated from Latitude.sh-assigned blocks, and
  reconciled by the host networking layer alongside HAProxy and nftables.

## Cache Model

There are several cache-related features with different ownership and
semantics.

GitHub Actions cache passthrough is provider-owned. `actions/cache` and
setup-action cache options create tar archives, upload through GitHub's cache
service, and restore through GitHub's matching rules. Verself proves runner
compatibility and action-level outcomes, but does not store, match, serve, or
rewrite GitHub cache entries.

Verself archive cache is an explicit alternative API. It can mirror the useful
parts of GitHub's archive semantics, including key, version, branch scope,
default-branch fallback, ordered restore keys, and immutable entries. Workflows
adopt it through a Verself action/API or a documented compatibility action.

Verself archive cache quotas match GitHub's published limits so workflows
migrated from `actions/cache` to the Verself archive cache see no behavior
delta beyond ownership: 10 GB total per repository, entries evicted after 7
days of inactivity, and immutable per `(scope, key, version)` tuple. Quotas
apply per repository and are not pooled across an organization. Eviction at
the cap is least-recently-used by `last_used_at`; the same field also drives
the 7-day idle sweep.

Sticky disks are mounted filesystems. The runner attaches the last committed
generation for a key before the job starts, exposes it at the requested path,
and commits a new generation after the job requests saveback. Sticky disks are
for workloads where repeated archive extraction is the bottleneck, such as
Bazel repository caches, Docker build state, package manager stores, and large
`node_modules` directories.

The product should present these as separate features:

- Use GitHub Actions cache passthrough for workflows already using
  `actions/cache` or setup-action cache options.
- Use Verself archive cache when the workflow explicitly chooses
  Verself-owned archive storage.
- Use sticky disks when the workflow wants mounted persistent storage and can
  tolerate filesystem-generation semantics.
- Use checkout caching for repository object reuse.
- Use Docker/container caching for image and layer reuse.

Terminology in docs, APIs, and telemetry should avoid using `cache` as an
umbrella for different resources. Use `GitHub Actions cache`,
`Verself archive cache`, `sticky disk`, `checkout cache`, and `Docker cache`
where the semantics differ.

## Data Model

Core runner resources:

- `runner_provider_installations`: source-provider installation state.
- `runner_provider_repositories`: provider repository IDs bound to Verself
  organizations.
- `runner_jobs`: provider demand facts.
- `runner_allocations`: Verself capacity records for provider jobs.
- `executions` and `execution_attempts`: Verself billing and lifecycle truth.

Verself archive cache resources:

- `cache_entries`: immutable committed cache entries keyed by provider,
  installation, repository, scope ref, key hash, and cache version.
- `cache_uploads`: in-flight upload reservations with attempt-scoped write
  tokens, expected object identity, received bytes, and finalization state.
- `cache_blobs`: content-addressed archive objects with size, digest,
  compression metadata, and storage location.

Sticky disk resources:

- `runner_sticky_disk_generations`: current committed generation for a key.
- `execution_sticky_disk_mounts`: per-attempt mount plan, source generation,
  target source ref, saveback state, and commit evidence.

Checkout and Docker cache resources should follow the same pattern: a small
PostgreSQL projection for authoritative identity and lifecycle, storage objects
or ZFS datasets owned by the substrate, and ClickHouse events for evidence.

## Invariants

Security:

- Verself cache, sticky disk, checkout, and artifact requests authenticate with
  attempt-scoped credentials tied to `execution_id` and `attempt_id`.
- Org, repository, installation, run, and job identity are recovered from the
  execution/allocation record. Guests do not supply tenant identity.
- Runtime provider tokens are bearer credentials and must not land in logs,
  ClickHouse string columns, traces, process args, or persisted request bodies.
- Verself cache keys may contain sensitive repository structure. Telemetry
  stores key hashes and match class by default; restricted product state may
  store the full key for inventory and deletion.
- Sticky disk paths must be absolute, non-root, mountable paths outside kernel
  pseudo-filesystems.

Correctness:

- Verself archive cache entries are immutable after finalization.
- Concurrent Verself archive saves for the same scope, key, and version resolve
  to one committed entry and observable conflicts.
- Verself archive restore matching follows the documented API contract for
  exact key, primary-key prefix, ordered restore-key matches, and
  default-branch retry under provider scope rules.
- Sticky disk saveback promotes a generation only if the base generation has not
  changed while the job was running.
- Compatibility adapters fail loudly and observably. Verself-owned cache APIs
  do not fall back to provider-owned cache services on miss or failure.

Performance:

- Verself cache blob traffic stays on the host-service plane whenever the
  runner and storage are colocated.
- Hot blob paths validate tokens once, then stream with bounded memory and
  kernel-assisted file transfer where possible.
- ZFS clone, snapshot, and send/receive work stays behind `vm-orchestrator`.
  Product services refer to source refs and mount plans, not host device paths.
- Verself archive cache operations emit bytes, duration, compression, match
  class, and storage backend attributes.
- Sticky disk operations emit source generation, target generation, mount path,
  duration, ZFS snapshot/source refs, and saveback status.

Observability:

- A successful GitHub Actions cache passthrough restore requires evidence that
  the upstream cache action ran under the provider runtime environment and
  reported a restore result.
- A successful Verself archive cache restore requires evidence for lookup,
  match decision, download URL issuance, blob transfer, and action-level restore
  result.
- A successful sticky disk restore requires evidence for mount planning, source
  generation, VM mount state, save request, ZFS commit, and generation
  promotion.
- Completion evidence lives in ClickHouse with `trace_id`, `execution_id`,
  `attempt_id`, provider run/job IDs, repository, runner class, operation,
  status, error class, bytes, and duration.

## Public Documentation

The public docs should expose this information architecture:

1. Get started with hosted runners.
2. Connect GitHub.
3. Connect Forgejo.
4. Choose runner classes.
5. Migrate from GitHub-hosted runners.
6. Migrate from Blacksmith.
7. Cache dependencies.
8. Use sticky disks.
9. Cache Git checkout.
10. Cache Docker builds.
11. Use artifacts.
12. Use OIDC.
13. Inspect run history and logs.
14. Query analytics.
15. Pricing, billing, and usage.
16. Limits, retention, and security.
17. Troubleshooting archive cache misses and sticky disk saveback.
18. API reference.
19. CLI reference.
20. SDK reference.

The docs should show GitHub-compatible examples first, Blacksmith-compatible
examples second, and Verself-native examples third.

## Console

The browser console should give operators and customers these views:

- Installation status, repository access, and webhook health.
- Runner classes, queue depth, assignment state, and recent failures.
- Run history with provider run/job IDs, execution IDs, trace IDs, logs,
  artifacts, cost, and timing breakdown.
- Verself archive cache inventory with key hash, optional full key display for
  authorized users, scope, version, size, last used, and delete action.
- Sticky disk inventory with key hash, path history, current generation, size,
  last used, last saveback state, and reset/delete action.
- Checkout and Docker cache usage.
- GitHub Actions cache passthrough observations, Verself archive cache,
  checkout, Docker cache, and sticky disk analytics by repository, branch,
  workflow, runner class, and time window.

## CLI

The CLI mirrors public APIs. Operator-only deployment stays in `aspect` and the
operator-local `verself deploy` surface. Target grammar:

```text
verself auth login
verself orgs use <org>

verself runners classes list
verself runners runs list [--repository owner/name]
verself runners runs logs <run-id>
verself runners runs inspect <run-id> --json

verself caches list [--repository owner/name] [--kind archive|checkout|docker]
verself caches delete --repository owner/name --key-hash <hash>

verself sticky-disks list [--repository owner/name]
verself sticky-disks reset --installation-id <id> --repository-id <id> --key-hash <hash>

verself integrations github install
verself integrations github repos list
verself integrations github repos enable <owner/name>
```

Operator-local deploy commands remain separate from customer runner commands.

## SDK

Public SDKs should expose stable resource APIs over generated clients:

- `sandbox.runnerClasses.list`
- `sandbox.runs.list`
- `sandbox.runs.get`
- `sandbox.runs.logs`
- `sandbox.archiveCaches.list`
- `sandbox.archiveCaches.delete`
- `sandbox.stickyDisks.list`
- `sandbox.stickyDisks.reset`
- `sandbox.artifacts.list`
- `sandbox.artifacts.downloadURL`
- `integrations.github.installations.list`
- `integrations.github.repositories.enable`

SDKs should normalize pagination, idempotency keys, retryable errors, trace
headers, and DTO conversion. Product services must keep using service-owned
generated clients for repo-owned calls; curated SDK packages are for customers,
operators, CLIs, and browser server functions.

## Implementation Anchors

- VM execution control plane:
  `src/services/sandbox-rental-service/docs/vm-execution-control-plane.md`
- GitHub runner allocation and JIT bootstrap:
  `src/services/sandbox-rental-service/internal/jobs/github_runner.go`
- Preboot sticky-disk planner:
  `src/services/sandbox-rental-service/internal/jobs/github_workflow_sticky.go`
- Sticky disk saveback and generation promotion:
  `src/services/sandbox-rental-service/internal/jobs/sticky_disk.go`
- Guest-visible internal routes:
  `src/services/sandbox-rental-service/internal/api/github_webhook.go`
- Firecracker host-service HAProxy frontend:
  `src/infrastructure-components/haproxy/templates/haproxy.cfg.j2`
- Current checkout and sticky disk ClickHouse projection:
  `src/infrastructure-components/clickhouse/migrations/001_initial_schema.up.sql`

## Primary References

- GitHub dependency caching reference:
  <https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching>
- GitHub Actions toolkit cache implementation:
  <https://github.com/actions/toolkit/tree/main/packages/cache>
- GitHub Actions runner action environment setup:
  <https://github.com/actions/runner/tree/main/src/Runner.Worker/Handlers>
- Blacksmith cache action compatibility:
  <https://docs.blacksmith.sh/blacksmith-caching/dependencies-actions>
- Blacksmith sticky disks:
  <https://docs.blacksmith.sh/blacksmith-caching/dependencies-sticky-disks>
- Blacksmith checkout caching:
  <https://docs.blacksmith.sh/blacksmith-caching/git-checkout-caching>

## Iteration Order

1. Publish this product contract internally and keep terminology stable.
2. Add public docs pages for getting started, cache dependencies, sticky disks,
   Blacksmith migration, and troubleshooting.
3. Prove GitHub Actions cache passthrough on Verself runners with ClickHouse
   evidence.
4. Add Verself archive cache data model, action/API, and ClickHouse evidence.
5. Add `useblacksmith/stickydisk` and `useblacksmith/stickydisk-delete`
   compatibility.
6. Expand console inventory and analytics for Verself archive caches and sticky
   disks.
7. Align CLI grammar with the product resources.
8. Align public SDKs with the CLI and browser server functions.
9. Add checkout, Docker, and container cache compatibility.
10. Make scheduled browser/API canaries prove GitHub Actions cache passthrough,
    Verself archive cache restore, and sticky disk restore with ClickHouse
    evidence.
11. Add SSH-debug-into-runner with Pomerium-bounded sessions and ClickHouse
    audit evidence.
12. Add custom runner images through artifact admission with per-organization
    scoping.
13. Add static egress IP pools per organization reconciled alongside HAProxy
    and nftables.

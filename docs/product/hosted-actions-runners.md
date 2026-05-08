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

Unmodified workflows that run on a standard Linux GitHub Actions self-hosted
runner must run on Verself hosted runners unless they depend on an intentionally
unsupported provider-only feature. Workflows migrating from Blacksmith can
migrate by changing `runs-on` labels when they use GitHub-compatible actions.
Blacksmith-specific acceleration actions require documented migration to
Verself Checkpoints and Verself-native cache features.

The compatibility surface has four layers:

1. Runner execution compatibility: JavaScript actions, composite actions, Docker
   actions, shell steps, service containers, job containers, environment files,
   outputs, step summaries, annotations, PATH mutation, post actions, and tool
   cache conventions.
2. Provider service passthrough: upstream actions keep using provider-owned
   service contracts such as GitHub cache, artifacts, logs, OIDC, runtime
   tokens, job metadata, checkout, and tool cache behavior.
3. Explicit acceleration APIs: Verself-owned Checkpoints and cache features
   adopted through Verself actions, Verself APIs, CLI commands, or SDK calls.
4. Migration familiarity: Blacksmith and other runner accelerators are product
   references for workflow shape and documentation examples. Their runner-local
   services, environment variables, and backend APIs are migration inputs rather
   than Verself compatibility contracts.

Provider-owned action boundary:

- Workflows using `actions/cache` use GitHub Actions cache and GitHub's runtime
  cache service. Verself forwards provider-owned runtime behavior unchanged.
- Verself cache is an alternative API. Customers opt in by using a Verself cache
  action, a Verself SDK/API, or a CLI command that targets Verself.
- Twirp, Azure Blob upload semantics, GitHub runtime tokens, cache service
  endpoints, and provider webhook payloads are provider internals for
  pass-through behavior and diagnostics.

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
migrating from Blacksmith change the label to a Verself name.

Verself Checkpoints are the Verself product for persisted, ZFS-backed CI state.
A Checkpoint is a keyed, generationed ext4 volume mounted into a runner at a
requested path and committed after a successful save request. Checkpoints store
customer build artifacts and dependency state as filesystem generations so
subsequent CI runs can start from a local ZFS clone instead of downloading and
extracting archives. The term "sticky disk" appears only in Blacksmith
migration material and refers to Blacksmith's product.

Checkout caching, Docker caching, and dependency acceleration are either
Checkpoint consumers or separate Verself-owned cache features with familiar
workflow shapes. Verself does not rename outputs or environment variables under
a Blacksmith namespace or advertise the product under the Blacksmith name in
any customer-visible surface.

## Compatibility Limits

Provider-owned service APIs use passthrough. Verself does not implement a
shadow GitHub cache service, artifact service, OIDC issuer, job metadata API, or
runtime-token service for upstream GitHub actions. The supported contract is
that those upstream actions execute correctly under the provider runtime
environment and that Verself records enough evidence to diagnose provider
service outcomes.

Known limits:

- `actions/cache` and setup-action cache options remain GitHub-backed. Cache
  hit and miss behavior follows GitHub's key, version, branch scope, restore-key,
  quota, and eviction rules.
- Docker BuildKit `type=gha` cache remains GitHub-backed because it uses the
  GitHub Actions cache service contract.
- `actions/upload-artifact`, `actions/download-artifact`, and OIDC token
  requests remain provider-backed.
- Blacksmith actions that require Blacksmith runner-local services,
  Blacksmith-provided environment variables, Blacksmith API keys, or
  Blacksmith-managed storage require migration to Verself actions or APIs.
- Blacksmith `runs-on` labels, CPU architectures outside the Verself runner
  catalog, Windows, and macOS require workflow or infrastructure changes.

Public migration docs should map Blacksmith examples to Verself-native surfaces:
Blacksmith sticky disk examples to Verself Checkpoints, Blacksmith dependency
cache examples to provider passthrough, Verself archive cache, or Checkpoints
depending on the workload, Blacksmith checkout caching to Verself checkout
acceleration, and Docker cache examples to the Verself Docker cache surface.
Each migration page must state which behavior remains provider-backed.

## Pricing

The launch class bills through a single runner-attempt SKU with millisecond
resolution. SKU selection is the billing abstraction: the runner class supplies
the SKU, and elapsed runner milliseconds supply the quantity. Rates are chosen
as whole Verself ledger units per runner-ms so the billing service uses the
existing integer `unit_rate` model.

| `runs-on:` label | Billing SKU | ledger units / runner-ms | $/min | $/vCPU-min |
| --- | --- | --- | --- | --- |
| `verself-4vcpu-ubuntu-2404` | `sandbox_ci_runner_4vcpu_ubuntu_2404_runner_ms` | 3 | 0.018 | 0.0045 |

Billing semantics:

- Resolution. Each attempt's billing window is measured in elapsed
  milliseconds. The charge is `elapsed_ms × unit_rate` for a runner-class
  allocation of `1`, computed as integer ledger units in the TigerBeetle ledger
  to avoid float drift. The ledger scale is `100_000` units per cent.
- No minimum. There is no per-job floor and no rounding up to the next
  minute. A 12,345 ms attempt on `verself-4vcpu-ubuntu-2404` charges
  `37,035` ledger units, or `$0.0037035`.
- Catalog mapping. The billing catalog owns the SKU and integer rate. The
  runner catalog maps each `runner_classes` row to a billing SKU with
  `billing_sku_id`. A runner attempt reserves the allocation
  `{billing_sku_id: 1}` and settles that allocation against elapsed
  milliseconds. Source provider, execution kind, and lifecycle fields remain
  reconciliation metadata.
- Window start. The window opens when `vm-orchestrator` reports the guest has
  accepted JIT runner registration and the runner has signaled ready to the
  source provider.
- Window stop. The window closes when the runner reports job completion or
  when a workflow-level cancel propagates to the runner via the source
  provider's webhook.
- Excluded time. Queue depth, VM provisioning, JIT bootstrap, Checkpoint
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
| `unit_rate` | Integer ledger units per runner-ms. |
| `amount_ledger_units` | Computed charge in integer ledger units. |

Pricing applies identically to GitHub Actions and Forgejo Actions runner
attempts. Storage (Checkpoint volumes, Verself archive cache blobs) and egress
beyond the Latitude.sh-included 20 TB are roadmap line items and are not billed
at launch; the only metered resource is runner-attempt compute.

## Customer Onboarding

Hosted onboarding:

1. Create or select an organization.
2. Install the Verself GitHub App or connect a Forgejo instance.
3. Select repositories and runner classes.
4. Update workflow `runs-on` labels.
5. Run a workflow and inspect run history, logs, telemetry, and billing.
6. Enable acceleration features: Verself archive cache, Checkpoints, checkout
   caching, Docker build caching, and language-specific Verself actions.

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
- Verself actions such as `verself/cache`, `verself/checkpoint`, and
  `verself/checkout` for workflows that opt into Verself-owned storage.
- Migration docs for customers coming from Blacksmith actions. The migration
  target is the Verself API/action surface, not Blacksmith environment variables
  or Blacksmith backend compatibility.

Provider-owned service contracts stay provider-owned. Product APIs expose
Verself resources such as runs, runner classes, Verself-owned archive cache
entries, Checkpoints, artifacts, and usage.

## Feature Surface

| Feature | Contract | Primary surface | Status |
| --- | --- | --- | --- |
| Ephemeral runner VMs | Each job runs in an isolated Firecracker VM with a clean workspace and metered resources. | `runs-on` labels, run history, billing | Implemented foundation |
| GitHub Actions cache passthrough | Upstream cache actions use GitHub Actions cache through the provider runtime environment. | `actions/cache@v5`, setup-action cache options, BuildKit `type=gha` | Required |
| Verself archive cache | Explicit Verself cache APIs store archive blobs on Verself-owned storage with key, version, scope, and restore-key semantics. | Verself cache action/API | Required |
| Verself Checkpoints | A keyed ext4 filesystem generation is restored from a ZFS snapshot, mounted at a requested path, and committed after successful saveback. | `verself/checkpoint`, API | Rewrite target |
| Checkpoint deletion | Customers can delete a Checkpoint by key or reset its current generation through inventory surfaces. | API, CLI, SDK | Rewrite target |
| GitHub checkout passthrough | `actions/checkout` uses the provider repository service and GitHub token behavior expected by upstream workflows. | `actions/checkout` | Required |
| Checkout caching | Explicit checkout acceleration uses a colocated mirror or persistent checkout disk. | Verself checkout action/API | Partial custom action |
| Docker build caching | Docker layer state persists across jobs on runner-local storage. | Docker actions, BuildKit cache, Verself API | Required |
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
  operator SSH and are bounded by the attempt's billing window. Checkpoint
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

## Acceleration Storage Model

There are several acceleration features with different ownership and semantics.
Terminology separates archive caches, mounted filesystem generations, checkout
mirrors, Docker layer state, and provider-owned services.

GitHub Actions cache passthrough is provider-owned. `actions/cache`,
setup-action cache options, and Docker BuildKit `type=gha` create archives,
upload through GitHub's cache service, and restore through GitHub's matching
rules. Verself proves runner compatibility and action-level outcomes while
GitHub stores, matches, serves, and evicts those cache entries.

Verself archive cache is an explicit alternative API. It can mirror the useful
parts of GitHub's archive semantics, including key, version, branch scope,
default-branch fallback, ordered restore keys, and immutable entries. Workflows
adopt it through a Verself action/API.

Verself archive cache quotas match GitHub's published limits so workflows
migrated from `actions/cache` to the Verself archive cache see no behavior
delta beyond ownership: 10 GB total per repository, entries evicted after 7
days of inactivity, and immutable per `(scope, key, version)` tuple. Quotas
apply per repository and are not pooled across an organization. Eviction at
the cap is least-recently-used by `last_used_at`; the same field also drives
the 7-day idle sweep.

Verself Checkpoints are mounted ZFS-backed filesystems. The runner attaches the
last committed generation for a key before the job starts, exposes it at the
requested path, and commits a new generation after the job requests saveback.
Checkpoints are for workloads where repeated archive extraction or network
transfer is the bottleneck, such as Bazel repository caches, Bazel disk caches,
Docker build state, package manager stores, browser binary caches, and large
`node_modules` directories.

The product should present these as separate features:

- Use GitHub Actions cache passthrough for workflows already using
  `actions/cache`, setup-action cache options, or BuildKit `type=gha`.
- Use Verself archive cache when the workflow explicitly chooses
  Verself-owned archive storage.
- Use Verself Checkpoints when the workflow wants mounted persistent storage
  and can tolerate filesystem-generation semantics.
- Use checkout caching for repository object reuse.
- Use Docker/container caching for image and layer reuse.

Use `GitHub Actions cache`, `Verself archive cache`, `Verself Checkpoint`,
`checkout cache`, and `Docker cache` where the semantics differ. Use
Blacksmith's `sticky disk` term only when discussing Blacksmith migration or
compatibility expectations.

## Data Model

The core tables are control-plane truth. ClickHouse tables are evidence tables
for analytics, debugging, and benchmarks. ClickHouse rows must not decide cache
matching, generation promotion, deletion, IAM, billing, retry behavior, or
future resource state. OpenTelemetry spans preserve detailed internal
sequencing; ClickHouse fact tables record typed operation outcomes.

Core runner resources:

- `runner_classes`: served labels, VM resource shape, runtime image, and
  `billing_sku_id`.
- `runner_provider_installations`: source-provider installation state.
- `runner_provider_repositories`: provider repository IDs bound to Verself
  organizations.
- `runner_jobs`: provider demand facts.
- `runner_allocations`: Verself capacity records for provider jobs.
- `executions` and `execution_attempts`: Verself billing and lifecycle truth.

Verself archive cache resources:

- `cache_entries`: immutable committed archive entries keyed by provider,
  installation, repository, scope ref, key hash, and cache version. It stores
  `cache_entry_id`, `org_id`, provider identity, repository identity,
  `repository_full_name`, `scope_ref`, `default_branch_ref`, `key_hash`,
  restricted `key`, `cache_version`, `blob_id`, `state`,
  `created_by_execution_id`, `created_by_attempt_id`,
  `last_used_by_execution_id`, `last_used_by_attempt_id`, `created_at`,
  `last_used_at`, and `deleted_at`.
- `cache_uploads`: in-flight upload reservations with attempt-scoped write
  authority. It stores `cache_upload_id`, `org_id`, provider identity,
  repository identity, `scope_ref`, `key_hash`, `cache_version`,
  `upload_token_hash`, `expected_blob_id`, `storage_object_key`, `state`,
  `reserved_by_execution_id`, `reserved_by_attempt_id`, `received_bytes`,
  `digest`, `expires_at`, `created_at`, and `finalized_at`.
- `cache_blobs`: immutable physical archive metadata. It stores `blob_id`,
  `org_id`, `digest`, `size_bytes`, `compression`, `storage_backend`,
  `bucket_alias`, `object_key`, and `created_at`.

Archive cache invariants:

- Matching reads `cache_entries`, never ClickHouse.
- A committed entry is unique for `(provider, installation, repository,
  scope_ref, key_hash, cache_version)`.
- Finalizing `cache_uploads` is the only transition that can create
  `cache_entries`.
- Concurrent saves for the same tuple resolve to one committed entry; losing
  uploads become `conflicted`.
- `cache_blobs` stores object identity and physical metadata. It does not know
  branch scope or restore-key matching rules.

Checkpoint volume resources:

- `volumes`: stable customer-visible storage resources. It stores `volume_id`,
  `org_id`, provider identity, repository identity, `repository_full_name`,
  `product_kind`, `component_kind`, `key_hash`, restricted `key`,
  `display_name`, `retention_policy`, `state`, `created_at`, `updated_at`, and
  `last_used_at`. `product_kind = ci_checkpoint` identifies customer-visible
  Checkpoints. `component_kind` distinguishes `checkpoint`,
  `checkout_mirror`, and `docker_build_cache`.
- `volume_generations`: immutable generation ledger. It stores
  `volume_generation_id`, `volume_id`, `org_id`, `generation`,
  `parent_generation_id`, `trust_class`, `zfs_source_ref`, `zfs_snapshot_ref`,
  `used_bytes`, `written_bytes`, `state`, `created_by_execution_id`,
  `created_by_attempt_id`, `created_at`, `last_used_at`, and `expires_at`.
- `volume_current_generation`: readable pointer per trust class. It stores
  `org_id`, `volume_id`, `trust_class`, `current_generation`,
  `volume_generation_id`, and `updated_at`.
- `execution_volume_mounts`: per-attempt mount plan and saveback state. It
  stores `mount_id`, `execution_id`, `attempt_id`, `allocation_id`,
  `volume_id`, `mount_name`, `mount_path`, `source_generation_id`,
  `target_source_ref`, `save_requested`, `save_state`,
  `committed_generation_id`, `failure_reason`, `created_at`, `requested_at`,
  and `completed_at`.

Checkpoint volume invariants:

- Checkpoints, checkout mirrors, and Docker build caches use the same volume
  generation model when they need mounted or ZFS-backed durable state.
- A successful saveback creates a `volume_generations` row even when it loses
  the `volume_current_generation` rotation race.
- Current generation rotation is separate from generation creation.
- Product services store and pass service-authorized refs. ZFS dataset names,
  zvol paths, host paths, and device paths stay behind `vm-orchestrator`.

## Checkpoint Flow

Checkpoint creation is generation creation. A stable `volumes` row can be
created lazily on first use or explicitly through the public API, but the
customer-visible acceleration event is the creation and promotion of a
`volume_generations` row.

Checkpoint load:

1. The runner integration compiles Checkpoint requests before the VM is
   submitted. Requests include key, mount path, save policy, and optional
   retention policy. Tenant, repository, provider installation, run, and job
   identity come from the runner allocation and execution records.
2. sandbox-rental normalizes the key, stores a restricted copy for inventory,
   computes the key hash, validates the mount path, and resolves or creates the
   stable `volumes` row for `(org, provider, repository, component_kind,
   key_hash)`.
3. sandbox-rental selects the readable base generation from
   `volume_current_generation` for the request trust class. A miss produces an
   empty writable mount plan with no `source_generation_id` and records a
   `checkpoint_operations` load miss. A hit records the selected
   `source_generation_id` and the service-authorized source ref.
4. sandbox-rental persists one `execution_volume_mounts` row per mount before
   the attempt is submitted. This row is the attempt-scoped authority for
   mounting and saveback.
5. sandbox-rental calls vm-orchestrator `StartExec` with mount refs only.
   vm-orchestrator resolves the refs to ZFS snapshots, clones zvols, attaches
   them to Firecracker, and returns guest mount metadata. Host paths, zvol
   names, and device paths remain inside vm-orchestrator and guest telemetry.
6. The guest exposes each mounted filesystem at the requested path before the
   workflow step that needs it starts. Checkpoint load is considered successful
   only when the control plane has the mount plan, vm-orchestrator has attached
   the clone, and ClickHouse has a `checkpoint_operations` row correlated to
   the execution trace.

Checkpoint saveback:

1. The workflow or Checkpoints action requests saveback with an attempt-scoped
   credential and a mount ID. The request carries no organization,
   installation, repository, run, job, ZFS, or host-path authority.
2. sandbox-rental validates the attempt credential with constant-time
   comparison against material derived for that execution attempt, locks the
   `execution_volume_mounts` row, and marks `save_requested`.
3. During attempt finalization, sandbox-rental commits only requested mounts.
   Each commit call to vm-orchestrator identifies the lease, exec, and mount
   with service-authorized IDs.
4. vm-orchestrator snapshots the writable clone, creates an immutable source
   ref for the new generation, measures used/written bytes from ZFS, and
   returns storage metadata. It does not update product state.
5. sandbox-rental inserts a `volume_generations` row with state `committed`.
   This insertion is append-only product truth for the generation even when it
   will not become current.
6. sandbox-rental promotes the generation by compare-and-swap on
   `volume_current_generation`: the source generation observed at load time
   must still be current for the same trust class. If another attempt promoted
   first, the new generation remains addressable for audit/retention and the
   operation records `promotion_conflict`.
7. sandbox-rental updates `execution_volume_mounts` with save state,
   committed generation, failure reason, and completion time. It writes
   `checkpoint_operations` rows for commit and promotion with bytes, duration,
   generation IDs, result, and trace ID.

The load path is optimized for pre-job latency. The save path is optimized for
correctness and observability; generation creation is durable before current
pointer rotation and every losing race remains explicit evidence.

Checkout cache resources:

- `checkout_cache_entries`: repository object reuse projection. It stores the
  organization, provider repository identity, requested ref/SHA, bundle or
  mirror identity, size, state, created attempt, last served attempt, and
  timestamps. Git objects and bundles live in substrate-owned storage; this
  table owns inventory and authorization.

Docker cache resources:

- Docker build cache starts as a Checkpoint-backed `volumes` consumer with
  `component_kind = docker_build_cache`. BuildKit local-cache import/export
  state belongs to `volume_generations` and `execution_volume_mounts`. A
  separate projection is added only when container image hydration needs image
  digest, registry, platform, or snapshotter-specific lifecycle state.

ClickHouse fact tables:

- `github_cache_observations`: provider-owned GitHub Actions cache
  passthrough observations. It stores `execution_id`, `attempt_id`,
  provider run/job IDs, `repository_full_name`, `action_ref`, `key_hash`,
  `restore_keys_hash`, `runner_class`, `reported_cache_hit`,
  `reported_primary_key_hash`, `reported_matched_key_hash`, `observed_at`, and
  `trace_id`.
- `archive_cache_operations`: Verself archive cache restore/save facts. It
  stores `operation_id`, `execution_id`, `attempt_id`, `cache_entry_id`,
  `cache_upload_id`, `blob_id`, provider run/job IDs, `repository_full_name`,
  `runner_class`, `scope_ref`, `key_hash`, `cache_version`, `operation`,
  `result`, `match_class`, `bytes`, `compression`, `duration_ms`,
  `error_class`, `observed_at`, and `trace_id`.
- `checkpoint_operations`: Checkpoint mount/saveback facts. It stores
  `operation_id`, `execution_id`, `attempt_id`, `mount_id`, `volume_id`,
  `source_generation_id`, `committed_generation_id`, provider run/job IDs,
  `repository_full_name`, `runner_class`, `key_hash`, `mount_path_hash`,
  `operation`, `result`, `trust_class`, `used_bytes`, `written_bytes`,
  `duration_ms`, `error_class`, `observed_at`, and `trace_id`.
- `checkout_cache_operations`: Verself checkout acceleration facts. It stores
  `operation_id`, `execution_id`, `attempt_id`, `repository_full_name`,
  `provider_repository_id`, provider run/job IDs, `runner_class`,
  `requested_ref`, `requested_sha`, `bundle_id`, `operation`, `result`,
  `bundle_bytes`, `duration_ms`, `error_class`, `observed_at`, and `trace_id`.
- `docker_cache_operations`: Docker build cache import/export facts. It stores
  `operation_id`, `execution_id`, `attempt_id`, `volume_id`,
  `source_generation_id`, `committed_generation_id`, provider run/job IDs,
  `repository_full_name`, `runner_class`, `key_hash`, `backend`, `operation`,
  `result`, `imported_bytes`, `exported_bytes`, `duration_ms`, `error_class`,
  `observed_at`, and `trace_id`.

ClickHouse fact table invariants:

- Fact tables are typed by resource. Do not add a generic
  `acceleration_events` table.
- Fact tables contain key hashes, not full keys.
- Fact tables do not contain bearer tokens, upload tokens, provider runtime
  tokens, storage credentials, or mutable authority.
- Fact table rows are sufficient for analytics and benchmark assertions. They
  are not sufficient for resource lifecycle decisions.

## Invariants

Security:

- Verself cache, Checkpoint, checkout, and artifact requests authenticate with
  attempt-scoped credentials tied to `execution_id` and `attempt_id`.
- Org, repository, installation, run, and job identity are recovered from the
  execution/allocation record. Guests do not supply tenant identity.
- Runtime provider tokens are bearer credentials and must not land in logs,
  ClickHouse string columns, traces, process args, or persisted request bodies.
- Verself cache keys may contain sensitive repository structure. Telemetry
  stores key hashes and match class by default; restricted product state may
  store the full key for inventory and deletion.
- Checkpoint paths must be absolute, non-root, mountable paths outside kernel
  pseudo-filesystems.

Correctness:

- Verself archive cache entries are immutable after finalization.
- Concurrent Verself archive saves for the same scope, key, and version resolve
  to one committed entry and observable conflicts.
- Verself archive restore matching follows the documented API contract for
  exact key, primary-key prefix, ordered restore-key matches, and
  default-branch retry under provider scope rules.
- Checkpoint saveback promotes a current generation only if the base generation
  has not changed while the job was running.
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
  class, and storage backend attributes into `archive_cache_operations`.
- Checkpoint operations emit source generation, target generation, mount path,
  duration, ZFS snapshot/source refs, and saveback status into
  `checkpoint_operations`.

Observability:

- A successful GitHub Actions cache passthrough restore requires evidence that
  the upstream cache action ran under the provider runtime environment and
  reported a restore result.
- A successful Verself archive cache restore requires evidence for lookup,
  match decision, download URL issuance, blob transfer, and action-level restore
  result.
- A successful Checkpoint restore requires evidence for mount planning, source
  generation, VM mount state, save request, ZFS commit, and generation
  promotion.
- Completion evidence lives in resource-specific ClickHouse fact tables with
  `trace_id`, `execution_id`, `attempt_id`, provider run/job IDs, repository,
  runner class, operation, status or result, error class, bytes, and duration.
  Detailed step sequencing lives in OpenTelemetry spans.

## Public Documentation

The public docs should expose this information architecture:

1. Get started with hosted runners.
2. Connect GitHub.
3. Connect Forgejo.
4. Choose runner classes.
5. Migrate from GitHub-hosted runners.
6. Migrate from Blacksmith.
7. Compatibility limits and provider passthrough.
8. Cache dependencies.
9. Use Verself Checkpoints.
10. Cache Git checkout.
11. Cache Docker builds.
12. Use artifacts.
13. Use OIDC.
14. Inspect run history and logs.
15. Query analytics.
16. Pricing, billing, and usage.
17. Limits, retention, and security.
18. Troubleshooting archive cache misses and Checkpoint saveback.
19. API reference.
20. CLI reference.
21. SDK reference.

The docs should show unmodified provider-compatible examples first, migration
examples second, and Verself-native acceleration examples third.

## Console

The browser console should give operators and customers these views:

- Installation status, repository access, and webhook health.
- Runner classes, queue depth, assignment state, and recent failures.
- Run history with provider run/job IDs, execution IDs, trace IDs, logs,
  artifacts, cost, and timing breakdown.
- Verself archive cache inventory with key hash, optional full key display for
  authorized users, scope, version, size, last used, and delete action.
- Checkpoint inventory with key hash, path history, current generation, size,
  last used, last saveback state, and reset/delete action.
- Checkout and Docker cache usage.
- GitHub Actions cache passthrough observations, Verself archive cache,
  checkout, Docker cache, and Checkpoint analytics by repository, branch,
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

verself checkpoints list [--repository owner/name]
verself checkpoints reset --repository owner/name --key-hash <hash>

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
- `sandbox.checkpoints.list`
- `sandbox.checkpoints.reset`
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
- Guest-visible internal routes and provider webhooks:
  `src/services/sandbox-rental-service/internal/api/github_webhook.go`
- Firecracker host-service HAProxy frontend:
  `src/infrastructure-components/haproxy/templates/haproxy.cfg.j2`
- Checkout ClickHouse projection and future Checkpoint evidence table:
  `src/infrastructure-components/clickhouse/migrations/001_initial_schema.up.sql`

## Primary References

- GitHub dependency caching reference:
  <https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching>
- GitHub Actions toolkit cache implementation:
  <https://github.com/actions/toolkit/tree/main/packages/cache>
- GitHub Actions runner action environment setup:
  <https://github.com/actions/runner/tree/main/src/Runner.Worker/Handlers>
- Blacksmith dependency cache reference:
  <https://docs.blacksmith.sh/blacksmith-caching/dependencies-actions>
- Blacksmith sticky disk migration reference:
  <https://docs.blacksmith.sh/blacksmith-caching/dependencies-sticky-disks>
- Blacksmith checkout caching reference:
  <https://docs.blacksmith.sh/blacksmith-caching/git-checkout-caching>

## Iteration Order

1. Publish this product contract internally and keep terminology stable.
2. Add public docs pages for getting started, compatibility limits, cache
   dependencies, Checkpoints, Blacksmith migration, and troubleshooting.
3. Prove GitHub Actions cache passthrough on Verself runners with ClickHouse
   evidence.
4. Add Verself archive cache data model, action/API, and ClickHouse evidence.
5. Implement Verself Checkpoints on the volume model with action/API surfaces,
   inventory, reset/delete, and `checkpoint_operations` evidence.
6. Align CLI grammar with the product resources.
7. Align public SDKs with the CLI and browser server functions.
8. Add checkout, Docker, and container cache compatibility.
9. Make scheduled browser/API canaries prove GitHub Actions cache passthrough,
    Verself archive cache restore, and Checkpoint restore with ClickHouse
    evidence.
10. Add SSH-debug-into-runner with Pomerium-bounded sessions and ClickHouse
    audit evidence.
11. Add custom runner images through artifact admission with per-organization
    scoping.
12. Add static egress IP pools per organization reconciled alongside HAProxy
    and nftables.

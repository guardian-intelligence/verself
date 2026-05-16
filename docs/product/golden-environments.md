# Golden Environments And Cache Volumes

Verself hosted runners accelerate CI by booting each job with ordinary Linux
filesystems that already contain rebuildable state from prior successful jobs.
The public surface is the Verself runner label, the Verself checkout action,
and optional cache-volume declarations. Customer workflows remain ordinary
GitHub Actions YAML.

A golden environment is a set of durable volume generations selected for one
job shape. The GitHub workspace is a platform-owned durable volume mounted at
the normal runner `_work` tree. Customer cache volumes are path-based durable
volumes mounted outside `GITHUB_WORKSPACE` and bound into the paths that tools
already use for local state: compiler caches, package-manager caches, database
data directories, Docker or BuildKit storage, generated SDK output, and other
rebuildable directories.

All cache volumes are rebuildable. Promotion is best-effort. The previous
golden remains authoritative until a new generation is sealed and promoted.
Ambiguous seal results skip promotion.

## Customer Contract

A repository opts into cache volumes with a checked-in manifest:

```yaml
version: 1

cache:
  - name: build-cache
    paths:
      - ~/.cache/bazel-disk
      - ~/.cache/bazel-repo

  - name: postgres-seed
    paths:
      - /verself/cache/postgres
```

Declarations define named volumes and the guest-visible paths that should be
backed by those volumes. Verself does not interpret the contents. Customers
configure their tools to write to those paths. Capacity is governed by the
organization storage quota, not by per-volume manifest declarations.

Examples:

```text
bazel --disk_cache=$HOME/.cache/bazel-disk \
      --repository_cache=$HOME/.cache/bazel-repo \
      build //...
```

```text
docker run --rm \
  -v /verself/cache/postgres:/var/lib/postgresql/data \
  postgres:16
```

The cache contract is the same for Bazel, PostgreSQL, SQLite, Gradle, Cargo,
pnpm, Docker BuildKit, and unknown tools. Verself provides mounted directories,
not tool-specific cache APIs.

## Product Semantics

1. A GitHub job is scheduled for a Verself runner.
2. The control plane derives repository, ref, run, job, matrix, runner class,
   platform image, trust class, and cache declaration identity from persisted
   GitHub state.
3. The control plane selects the current compatible durable generation for each
   volume scope.
4. vm-orchestrator prepares a fresh VM with static block devices for the root
   disk, platform toolchains, GitHub workspace, and customer cache volumes.
5. vm-bridge mounts those devices before the runner starts.
6. The Verself checkout action updates `GITHUB_WORKSPACE` to the event commit.
7. Customer steps execute normally and read or write cache paths as ordinary
   directories.
8. After the runner exits, sandbox-rental waits for the attempt-specific
   GitHub workflow job to reach `status=completed` and `conclusion=success`.
   vm-bridge then attempts to seal each writable durable volume by syncing and
   unmounting guest mounts.
9. vm-orchestrator flushes, snapshots, clones, ZFS-promotes, and seals each
   volume that the guest sealed cleanly.
10. The service records committed generations observed from the host result.
11. A protected target-branch workflow run promotes per-job, per-volume
    generations only after the provider run's required jobs are green.
12. Failed jobs, cancelled jobs, non-promotable trust contexts, and ambiguous
    seals leave the current pointer unchanged. Successful non-promotable jobs
    may retain a generation for debugging and later pruning.

A job can succeed while cache persistence is skipped. Cache persistence is an
acceleration artifact, not a correctness requirement for CI.

## Buildkite-Inspired Behavior

Buildkite hosted cache volumes are attached on a best-effort basis, behave like
regular Linux filesystems, require workflows to tolerate misses, commit a new
version after successful jobs, and abandon volumes after failed jobs. Verself
adopts that user model and changes the trust model for GitHub branch and PR
execution.

Verself differences:

- Volume promotion is scoped by repository, target ref, workflow job identity,
  runner class, platform image, architecture, and component compatibility hash.
- Pull requests may read a protected branch's secretless cache generation, but
  PR writes never promote that protected branch pointer.
- Protected branch promotion is gated by the provider workflow result, not by a
  single job's local exit code.
- Seal and commit eligibility for GitHub Actions jobs is gated by the GitHub
  workflow-job conclusion for the observed run attempt, not by the
  actions-runner process exit code.
- Cache volumes are ZFS-backed block devices attached to Firecracker guests,
  not archive uploads or downloads.

## Declaration Rules

Manifest `version` is required and must be `1`.

Each manifest `cache` entry declares one volume:

```text
name      stable volume name, unique within the declaration
paths     one or more directories backed by the volume
```

The declaration has no tool profiles. `name` and `paths` are the public API.

Declaration source rules:

- `.verself/cache.yml` is repository-scoped.
- Pull request jobs read the manifest from the trusted base ref.
- Push jobs read the manifest from the pushed head ref.
- If no manifest is present, the job has no customer cache volumes.

Path rules:

- Paths are directories.
- Paths may be absolute or `~/...`.
- Relative paths are rejected.
- Paths under `GITHUB_WORKSPACE` are rejected; the workspace has its own
  golden lifecycle.
- `/`, `/bin`, `/boot`, `/dev`, `/etc`, `/lib`, `/lib64`, `/proc`, `/run`,
  `/sbin`, `/sys`, and `/usr` are rejected.
- Paths containing `..` after normalization are rejected.
- Duplicate target paths are rejected.
- Nested target paths are rejected.
- A target path that exists as a non-directory is a mount miss.
- A target path that exists as a non-empty directory is a mount miss.

Invalid declarations are configuration errors. Verself fails the job before
customer steps start and reports the rejected source, volume, path, and rule.
Runtime mount misses are recorded as degraded cache state and the job continues
without the affected cache volume. Cache volumes are always best-effort after a
valid declaration has been accepted.

Mounts are volume-atomic. If one path for a volume cannot be bound, vm-bridge
unmounts any prior bind targets for that volume and marks the whole volume as a
miss. Partial multi-path cache volumes are not exposed to customer code.

## Static VM Composition

Cache volumes are composed before Firecracker starts. The VM receives a static
virtio-block topology for the full job lifetime:

```text
root disk                 /dev/vda
GitHub workspace volume   /dev/vdb
cache volume 0            /dev/vdc
cache volume 1            /dev/vdd
...
```

There is no dynamic block-device attach path after guest boot. Mount
availability is part of lease acquisition and guest initialization, before any
customer process starts.

Read-only runner toolchain images may still declare vm-bridge writable
overlays for image-owned scratch paths. Those overlays are tmpfs and are not
durable volume generations. They are only for paths that belong to the
read-only toolchain image and must be writable for the runner process to
function.

The GitHub Actions runner toolchain currently declares writable overlays in
`src/substrate/vm-orchestrator/guest-images/gh-actions-runner/writable-overlays`.
The golden-workspace cutover removes `/opt/actions-runner/_work` from that
file. The `_work` tree is a platform-owned durable workspace volume, so a
toolchain tmpfs overlay must not mount over it or hide it. `_diag` and `_temp`
remain valid tmpfs overlays because they are runner-local diagnostic and
scratch paths, not reusable customer state.

The host prepares each writable volume as a ZFS zvol:

```text
if current generation exists:
  zfs clone <current_snapshot> orgs/<org>/workloads/<lease>/mounts/<idx>-<name>
else:
  zfs create -s -V <nominal_size> orgs/<org>/workloads/<lease>/mounts/<idx>-<name>
  mkfs.ext4 /dev/zvol/<dataset>
```

vm-orchestrator waits for every `/dev/zvol/...` device, bind-mounts the block
devices into the jailer chroot, configures Firecracker drives, starts the VM,
and sends the mount manifest to vm-bridge over the guest control protocol.
vm-bridge returns a per-filesystem mount result before the runner starts.
Required mount failures fail lease acquisition. Optional cache mount failures
are recorded as degraded cache state and the job continues without that cache.

## Guest Mounting

One declared cache volume becomes one guest filesystem mounted at an internal
root:

```text
/verself/.mounts/<cache-name>
```

Each requested customer path is backed by a subdirectory in that root and a
Linux bind mount:

```text
/verself/.mounts/build-cache/paths/home/runner/.cache/bazel-disk
  -> /home/runner/.cache/bazel-disk
/verself/.mounts/build-cache/paths/home/runner/.cache/bazel-repo
  -> /home/runner/.cache/bazel-repo
```

Bind mounts are the default implementation. Symlinks are not the default
because they change path identity, can be removed by customer code, and are
handled inconsistently by filesystem walkers, database tools, and archive
utilities. Symlinks are outside the cache-volume contract. The product contract
is directory mount semantics.

Guest mount flags:

```text
MS_NOATIME
MS_NODEV
MS_NOSUID
```

`noexec` is not the default because build caches and tool output directories
commonly contain executable files. Customers that need stricter policy should
choose paths whose tools do not execute cache contents.

vm-bridge is responsible for:

- Expanding `~/...` against the runner user's home directory.
- Creating parent directories for target paths.
- Ensuring target paths are absent or empty directories before binding.
- Chowning bind-target directories it creates so sibling tool directories under
  paths such as `~/.cache` remain writable by the runner user.
- Creating source subdirectories under the mounted cache root.
- Chowning writable cache roots and bind source subdirectories to the runner
  UID and GID.
- Recording every mounted root and bind target for later seal.

The runner receives environment metadata for observability and debugging:

```text
VERSELF_CACHE_MOUNTS=/verself/.mounts/build-cache:/verself/.mounts/postgres-seed
VERSELF_CACHE_BUILD_CACHE=/verself/.mounts/build-cache
VERSELF_COMPOSED_ZVOL_MOUNTS=<all platform-composed zvol mount roots>
```

## Seal Semantics

A seal is filesystem-level quiescence, not application-level quiescence.
Verself does not call `pg_ctl`, SQLite checkpoint APIs, Docker APIs, Bazel APIs,
or package-manager cleanup commands. The job owns any graceful application
shutdown it requires.

vm-bridge seal procedure for each writable volume:

1. Stop accepting new exec work for the lease.
2. Run normal runner/job cleanup already implied by the provider execution.
3. Issue `sync` in the guest.
4. Unmount bind targets for the volume in reverse mount order.
5. Unmount the internal cache root.
6. Return a sealed result only if every unmount succeeds.

vm-orchestrator commit procedure after guest seal succeeds:

1. Flush the host block device.
2. Snapshot the working zvol.
3. Clone the working snapshot into the golden generation namespace.
4. Promote the clone so the generation no longer depends on the ephemeral
   lease dataset.
5. Create `@sealed` on the promoted generation.
6. Return snapshot reference, used bytes, written bytes, and commit time.

Seal eligibility and product promotion are separate service decisions.
Sandbox-rental waits briefly for GitHub's attempt-specific workflow-job result
after the local runner exits so the provider terminal state, not GitHub API
propagation timing, decides seal eligibility. Failed, cancelled, lease-expired,
and provider-non-success executions skip seal and commit. A successful
non-promotable execution may still commit a retained generation, but it cannot
advance a protected branch current pointer.

Ambiguous seal states skip promotion:

- Guest control socket disappears before a seal result.
- `sync` or unmount times out.
- A bind target or root mount returns busy.
- The host block flush fails.
- ZFS snapshot, clone, promote, or final seal fails.
- Host journal recovery cannot prove a terminal committed phase.
- The GitHub job is cancelled or does not conclude `success`.

The job result does not change because of ambiguous cache seal. The previous
current generation remains authoritative.

## Database Directories

Database files are ordinary cache volume contents.

Customers should persist database directories, not individual files. SQLite WAL
mode writes sidecar files next to the main database. PostgreSQL, MySQL, Redis,
Elasticsearch, and similar services expect directory-level ownership,
permissions, locks, and temporary files. Directory mounts avoid partial-file
selection and keep tool behavior inside the customer's own runtime contract.

DB-specific behavior:

- Verself does not disable, enable, or inspect fsync behavior.
- Verself does not checkpoint WALs.
- Verself does not repair databases.
- Verself does not infer health from database contents.
- Crashy or non-fsync data directories are cache misses in practice.
- Repeated seal skips or corrupt cache reads are customer-debuggable through
  logs, traces, and cache-volume metadata.

A database cache that cannot be reused consistently remains a performance
issue, not a platform correctness issue.

## Logical Data Model

The schema is a full cutover. The committed schema should contain only the
current model. Prior development tables and migration compatibility shims are
removed before merge; git history is the only record of obsolete shapes.

### Cache Declaration

```text
cache_declaration
  cache_declaration_id
  repository_id
  source_kind
  source_ref
  source_sha
  source_path
  workflow_identity
  job_identity
  step_identity
  declaration_sha256
  declaration_hash
  parsed_at
```

`source_kind` values:

```text
manifest
none
```

`source_ref` and `source_sha` record the last observed source for diagnostics.
The declaration identity is repository, normalized declaration hash, source
kind, source path, workflow identity, job identity, and step identity. Runtime
lineage is keyed later per component by `job_shape`, `durable_scope.scope_ref`,
and the component compatibility hash, so a manifest edit only invalidates
volumes whose compatibility-affecting fields changed.

`declaration_hash` is the canonical hash of the normalized declaration. It
changes when volume names, paths, or mount policy change.

### Cache Volume Spec

```text
cache_volume_spec
  cache_volume_spec_id
  cache_declaration_id
  name
  path_set_hash
  mount_policy_hash
  normalized_paths_json
  created_at
```

The spec is immutable. Changing any compatibility-affecting field creates a
new spec hash and therefore a new cache lineage.

### Job Shape

```text
job_shape
  job_shape_id
  repository_id
  provider
  workflow_identity
  called_workflow_identity
  job_identity
  matrix_key
  runner_class
  guest_arch
  platform_image_id
  kernel_image_id
  runner_toolchain_image_id
  workspace_policy_hash
  cache_declaration_hash
  created_at
```

`job_shape` is the compatibility boundary for generated state. The
`cache_declaration_hash` column stores the component compatibility hash:
workspace policy for the workspace volume, and volume name, path set, and mount
policy for a cache volume. `guest_arch` is explicit so x86_64 and aarch64 never
share cache generations.

### Durable Scope

```text
durable_scope
  durable_scope_id
  repository_id
  provider
  provider_repository_id
  scope_kind
  scope_ref
  job_shape_id
  component_name
  component_kind
  trust_class
  created_at
```

`component_kind` values:

```text
github_workspace
cache_volume
platform_toolchain
```

Customer cache volumes use `component_kind=cache_volume` and
`component_name=<declaration cache name>`. The GitHub workspace is represented
by the same durable scope machinery but is platform-owned and mounted at the
runner `_work` tree.

### Durable Operation

```text
durable_operation
  operation_id
  execution_id
  attempt_id
  allocation_id
  durable_scope_id
  source_generation_id
  source_snapshot_ref
  candidate_generation_id
  mount_name
  internal_mount_path
  bind_paths_json
  trust_class
  requested_at
  host_accepted_at
  mounted_at
  seal_started_at
  sealed_at
  result_recorded_at
  final_state
  failure_reason
```

`final_state` values:

```text
requested
mounted
committed
skipped
failed
```

`skipped` means no reusable generation was produced and the previous current
pointer remains authoritative.

Failed, cancelled, lease-expired, and ambiguous executions set the operation to
`skipped` or `failed` and do not create a durable generation row. The current
pointer is only updated by `durable_current_pointer` CAS promotion after a
committed generation exists.

### Durable Generation

```text
durable_generation
  durable_generation_id
  durable_scope_id
  operation_id
  source_generation_id
  head_sha
  tree_hash
  provider_run_id
  provider_run_attempt
  provider_job_id
  result
  promotion_eligible
  state
  zfs_snapshot_ref
  used_bytes
  written_bytes
  sealed_at
  committed_at
  last_used_at
  expires_at
```

`state` values:

```text
committed
retained
invalidated
prunable
pruned
```

A committed generation requires sealed host storage. No database row may claim
`state=committed` before vm-orchestrator returns a successful seal result.

### Durable Current Pointer

```text
durable_current_pointer
  durable_scope_id
  current_generation_id
  promoted_by_operation_id
  promoted_at
```

Promotion is compare-and-swap against the source generation observed before the
VM started:

```sql
update durable_current_pointer
set current_generation_id = :candidate_generation_id,
    promoted_by_operation_id = :operation_id,
    promoted_at = now()
where durable_scope_id = :durable_scope_id
  and current_generation_id is not distinct from :source_generation_id;
```

If zero rows are affected, another operation won the race. The candidate is
retained or pruned by retention policy.

### Host Durable Journal

```text
host_durable_journal
  operation_id
  host_id
  lease_id
  mount_name
  phase
  source_dataset_ref
  working_dataset_ref
  sealed_dataset_ref
  error_code
  error_message
  recorded_at
```

Every host mutation has an operation ID before the mutation starts and a
journal row after it finishes. This table belongs to vm-orchestrator's local
host state database. The service database records observed durable operations
and generations after terminal host phases. PostgreSQL locks are not held
across ZFS operations.

## Scope Identity

A cache generation is reusable only when every compatibility dimension matches
or policy explicitly permits a broader read.

Compatibility dimensions:

```text
organization_id
repository_id
provider_repository_id
scope_kind
scope_ref
workflow_identity
called_workflow_identity
job_identity
matrix_key
runner_class
guest_arch
platform_image_id
kernel_image_id
runner_toolchain_image_id
component_compatibility_hash
component_name
component_kind
trust_class
```

Matrix values are canonicalized after GitHub expands the job. Jobs with
different Node versions, Python versions, CPU architecture, service topology,
or runner class naturally receive different scopes because their job identity,
matrix key, runner class, platform image, or component compatibility hash
differs.

## CPU Architecture

The supported hosted runner architecture for this design is Linux `x86_64` on
Firecracker. Linux `aarch64` is design-compatible because the durable volume
contract is a Linux block-device and ext4 contract, but it is a separate
compatibility lineage. Cache generations never cross architectures.

Architecture is part of the compatibility key for three reasons:

- Build outputs frequently contain architecture-specific object files,
  binaries, native package prebuilds, JIT caches, and Docker layers.
- Tooling may encode CPU feature assumptions beyond the ISA name.
- The guest kernel, platform image, runner toolchain image, and package manager
  lockfiles can resolve different artifacts on different architectures.

`runner_class` records the minimum CPU feature contract exposed to the guest.
If the fleet mixes x86_64 hosts with materially different exposed feature sets,
those feature sets use separate runner classes or platform image identities. A
cache produced on `x86_64-v3` is not reused by a runner class that only
guarantees `x86_64-v2`. A cache produced on `aarch64` is not reused by
`x86_64`. Endianness is not used to broaden compatibility; architecture is a
hard boundary.

The product does not support Windows or macOS cache volumes in this design.
Those operating systems need separate mount, filesystem, and runner lifecycle
contracts.

## Concurrency And Races

### Concurrent Jobs For The Same Scope

Two jobs may start from the same current generation. Each records the observed
source generation before lease acquisition. Both may seal candidate generations.
Only one CAS promotion can advance the current pointer. The loser remains a
retained generation or becomes prunable.

### Workflow-Level Promotion

A protected branch pointer advances only after the provider run's required job
set is observed green. The promotion batch is derived from GitHub workflow run
and job state. Each job's cache volumes still promote independently by durable
scope CAS. If a job has three cache volumes and only two seal cleanly, the two
sealed volumes may promote and the ambiguous volume remains on its previous
current generation.

### Pull Requests

Pull request jobs use the target branch's current secretless cache generation
as their source when policy allows. PR candidate writes are isolated to PR or
retained candidate generations and cannot promote the target branch pointer.

For untrusted PRs, the cache declaration is read from the trusted base branch,
not from PR head. Manifest edits from PR head are ignored for host mount
planning. A PR cannot introduce new host mount paths for code that has not
been merged into the trusted branch.

### Declaration Changes

A compatibility-affecting change to a volume's name, path set, or mount policy
changes that volume's component compatibility hash and creates a new durable
scope. Existing current pointers remain available for older scopes until
retention prunes them. Adding, removing, or changing one cache volume does not
invalidate the workspace volume or unrelated cache volumes.

### Lease Cancellation

A provider cancellation, timeout, or control-plane cancellation can terminate
customer execution after cache volumes were mounted. The operation records the
provider terminal state. Seal is skipped when the provider job does not conclude
`success`, even if the guest process exits with code `0` during cancellation
cleanup.

### Host Crash

Host-local journal phases drive recovery. A recovery pass classifies operations
from the host journal and ZFS state:

```text
no accepted phase                    -> service operation remains requested/failed
accepted without working dataset      -> failed
working dataset without sealed result -> skipped and destroy working dataset
sealed dataset without service row     -> record committed or retain orphan by policy
current pointer to missing snapshot    -> platform invariant violation
```

Destroying orphan working datasets is allowed only after journal reconciliation
proves they are not referenced by a live lease or a committed generation.

### Retention Race

Retention never destroys a generation referenced by `durable_current_pointer`,
a running `durable_operation.source_generation_id`, a retained debug pin, or a
sealed generation whose promotion decision is still pending. Retention reads
references and destroys through vm-orchestrator-owned host mutation, not by
service-side shell commands.

### Org Storage Quota

Durable zvols are sparse and use a fixed nominal volsize. The customer manifest
does not carry a volume size. The host applies a ZFS `quota` to the
organization dataset so the org subtree, including child workload and
generation zvols, cannot consume more than the plan allows.

Firecracker virtio-block does not expose discard to the guest. Bytes freed
inside a job are reclaimed when the host destroys unreferenced generations, not
while the job is running. A full org quota produces normal filesystem errors in
the job, usually `ENOSPC`.

### Path Conflicts

Binding a cache over non-empty image content would hide files from the job.
Verself treats this as a mount miss for the affected cache volume. The job
continues cold and the mount miss is recorded. Customers choose clean cache
paths for reliable hit rates.

### Clock And Idempotency

Operation IDs and generation IDs are service-generated before host mutation.
Host mutation APIs are idempotent by operation ID. Wall clock is metadata; it
is not used to order promotions. Promotion ordering is by observed source
identity and CAS.

## Security Model

Generic CI jobs are secretless. Cache volumes are readable by later jobs in
compatible scopes, including PR jobs when policy allows reading the target
branch's secretless current generation. Customers must not store secrets in
cache volumes.

The cache-volume design does not implement content-based secret tainting for
customer cache volumes.
The security model relies on lane separation:

- Generic build/test CI does not receive repository, organization, or
  environment secrets.
- Jobs with staging or production authority run in a trusted lane and are not
  reusable by lower-trust cache scopes.
- OIDC or JWT credential exchange for trusted lanes produces separate trust
  scopes.
- Fork PR jobs cannot promote target branch cache generations.

Mount hardening:

- Cache filesystems are guest block devices, not host bind mounts.
- Guest mounts use `nodev` and `nosuid`.
- Cache paths under system roots are rejected.
- Product services never receive host ZFS authority.
- vm-orchestrator is the only runtime process that mutates ZFS, jailer state,
  Firecracker devices, TAP networking, or `/dev/zvol` devices.

## ZFS Layout

The host stores durable volume generations under the golden root:

```text
vspool/orgs/<org_id>/goldens/<durable_scope_id>/generations/<durable_generation_id>
vspool/orgs/<org_id>/goldens/<durable_scope_id>/generations/<durable_generation_id>@sealed
```

Lease working datasets live under the workload root:

```text
vspool/orgs/<org_id>/workloads/<lease_id>/mounts/<index>-<mount_name>
```

The same ZFS lifecycle applies to the GitHub workspace and customer cache
volumes:

```text
clone source snapshot or create empty zvol
attach as Firecracker block device
mount in guest
seal in guest
flush host block device
snapshot working dataset
clone into generation namespace
promote clone
snapshot @sealed
record service generation
CAS promote current pointer
```

No `zfs receive -F` or rollback-style overwrite is used to resolve conflicts.
Conflicts are represented by pointer CAS results and retention metadata.

## Observability

Every durable operation emits ClickHouse rows in `verself.durable_events` and
adds matching OpenTelemetry span events. Rows are keyed by `operation_id`,
`durable_scope_id`, `durable_generation_id`, `attempt_id`, `provider_run_id`,
and `provider_job_id`.

Canonical event names:

```text
durable.declaration.resolve
durable.volume.prepare
durable.volume.select
durable.volume.mount
durable.volume.bind
durable.volume.seal
durable.volume.commit
durable.volume.promote
durable.volume.retain
durable.volume.prune
durable.volume.reconcile
```

Expected cache-volume sequence for a mounted successful protected-branch run:

```text
durable.declaration.resolve  cache_declaration  manifest|none  succeeded
durable.volume.prepare       cache_volume        <name>         succeeded
durable.volume.select        cache_volume        <name>         hit|miss
durable.volume.mount         cache_volume        <name>         mounted
durable.volume.bind          cache_volume        <name>         mounted
durable.volume.seal          cache_volume        <name>         succeeded
durable.volume.commit        cache_volume        <name>         succeeded
durable.volume.promote       cache_volume        <name>         succeeded|already_current|conflicted
```

Mount misses remain visible and non-terminal after declaration acceptance:

```text
durable.volume.mount         cache_volume        <name>         skipped
durable.volume.bind          cache_volume        <name>         skipped
durable.volume.seal          cache_volume        <name>         skipped
```

Failed, cancelled, and lease-expired jobs skip seal and commit:

```text
durable.volume.seal          cache_volume        <name>         skipped
```

Successful non-promotable contexts may retain committed generations:

```text
durable.volume.commit        cache_volume        <name>         succeeded
durable.volume.retain        cache_volume        <name>         succeeded
```

Required row fields for debugging and verification:

```text
event_name
result
reason
component_kind
component_name
mount_name
source_generation_id
candidate_generation_id
current_generation_id
zfs_snapshot_ref
used_bytes
written_bytes
trace_id
span_id
```

Customer debugging surfaces show cache hit or miss, selected source generation,
mount misses, seal result, commit result, promotion result, and retention state.
Cache misses are expected operational states and should not require support
access to diagnose.

## References

- Buildkite hosted cache volumes:
  <https://buildkite.com/docs/agent/buildkite-hosted/cache-volumes>
- Linux bind mounts and mount flags:
  <https://man7.org/linux/man-pages/man2/mount.2.html>
- Firecracker drive composition is implemented in repo-owned vm-orchestrator:
  `src/substrate/vm-orchestrator/AGENTS.md`,
  `src/substrate/vm-orchestrator/docs/zfs-volume-lifecycle.md`, and
  `src/substrate/vm-orchestrator/proto/v1/vm_service.proto`.
- GitHub Actions variables and `GITHUB_WORKSPACE`:
  <https://docs.github.com/en/actions/reference/workflows-and-actions/variables>

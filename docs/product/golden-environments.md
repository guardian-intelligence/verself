# Golden Artifacts, VM Snapshots, And Durable Caches

Verself hosted runners accelerate CI by restoring each job from ordinary Linux
filesystems and, when compatible, a Firecracker VM snapshot that already holds
rebuildable process state from prior successful jobs. The public surface is the
Verself runner label, the Verself checkout action, and optional durable-cache
declarations. Customer workflows remain ordinary GitHub Actions YAML.

A golden artifact is the product-level aggregate for one compatible job shape:
the workspace durable generation, declared durable-cache generations, root
checkpoint identity, Firecracker `vmstate` and memory artifacts, and an
immutable manifest that couples those pieces. `workspace` is the built-in
durable cache mounted at the normal runner `_work` tree. The Verself checkout
action reconciles it to the event commit before customer steps run. Manifest
entries add more durable caches for paths outside `GITHUB_WORKSPACE`: compiler
caches, package-manager caches, database data directories, Docker or BuildKit
storage, generated SDK output, and other rebuildable directories.

All durable caches and VM snapshots are rebuildable. Promotion is best-effort.
The previous golden remains authoritative until a new generation set and VM
manifest are checkpointed and promoted. Ambiguous checkpoint or seal results
skip promotion.

The customer model is:

1. The repository switches to the Verself runner and checkout action.
2. A pull request runs CI normally.
3. After merge, CI runs on the target branch. When the target-branch run's
   required jobs are green, Verself promotes one golden artifact per job shape.
4. Future pull requests restore the target branch's latest compatible golden
   artifact, then checkout reconciles `GITHUB_WORKSPACE` to the PR head.
5. Failed, cancelled, non-promotable, and ambiguous runs leave the target
   branch pointer on the last promoted artifact.

For a workflow with separate jobs such as `test-node-20`, `test-node-22`,
`lint`, `integration`, and `build-docker`, each job/matrix shape gets its own
lineage. Node 20 and Node 22 differ because native package artifacts encode the
Node ABI. `integration` commonly has the largest artifact because databases and
local services are in declared durable caches. `build-docker` is a different
shape because Docker and BuildKit layer state are unrelated to the Node test
toolchain.

Promotion requires the protected target-branch run's required jobs to be
green. A Bazel, npm, Docker, database, or generated-output cache may be stale
or corrupt; consumers must miss and rebuild. Cache state is not semantic truth.

The shorthand for GitHub pull-request lookup is `(organization, repository,
target-branch, workflow-id, job-id, matrix-key)`, plus compatibility
dimensions: runner class, guest architecture, platform/kernel/toolchain image
IDs, cache spec hash, cache name, trust class, Firecracker ABI, exact durable
generation set, and hook profile. Not every job has a matrix key. A workflow
YAML edit creates a new lineage only when it changes a compatibility-affecting
job or cache dimension.

`tree_hash` is metadata on generations and VM manifests. Checkout uses it to
compute the workspace diff from the artifact tree to the event tree. Promotion
may use it to retag an existing artifact when the target branch tree exactly
matches an already-built candidate.

GitHub `services:` containers stay per-job. When a workflow declares
`services: postgres:16`, GitHub starts a fresh service container for that job.
The warm-state speedup applies to customer-managed services and data
directories that live in the job VM and are declared as durable caches.

The customer-facing durable mount promise is:

> Any directory a CI job writes outside `GITHUB_WORKSPACE` can be declared as
> durable. Verself mounts the latest trusted version before the job starts,
> lets the job mutate it normally, and checkpoints the mounted volumes and
> warm VM after success. Pull requests start from the target branch's last
> green compatible artifact, and their writes cannot poison the target branch.

Future debugging surfaces can list golden artifacts, inspect manifest metadata,
invalidate artifacts, start a VM from an artifact ID, provide Pomerium-gated
SSH into a debug VM, and export a manifest with its reusable storage refs.

Customer zvols are encrypted at rest under the organization ZFS namespace
before any guest-visible filesystem is created. ZFS snapshots and clones serve
the golden lifecycle, retention, and placement-affinity model only. Backup and
recovery catalogs exclude customer zvol byte streams; loss of these volumes is
handled as a cache miss and cold rebuild.

The organization ZFS key is host operational state for a healthy org, not a
lease-scoped secret. vm-orchestrator loads the key during org runtime warmup
before lease acquisition and keeps the ZFS key available while the org remains
healthy on that host. Lease cleanup releases raw key material from daemon
memory; it does not unload the kernel-held ZFS key. Key unload or rotation is
an explicit security event, host drain/shutdown policy, or org tombstone
action.

## Customer Contract

A repository opts into additional durable caches with a checked-in manifest:

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

The built-in `workspace` cache always stores `GITHUB_WORKSPACE`; it is not
declared in YAML. Manifest declarations define additional named caches and the
guest-visible paths that should be backed by those caches. Verself does not
interpret the contents. Customers configure their tools to write to those
paths. Capacity is governed by the organization storage quota, not by
per-cache manifest declarations.

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

The durable cache contract is the same for the workspace, Bazel, PostgreSQL,
SQLite, Gradle, Cargo, pnpm, Docker BuildKit, and unknown tools. Verself
provides mounted directories, not tool-specific cache APIs.

## Product Semantics

1. A GitHub job is scheduled for a Verself runner.
2. The control plane derives repository, ref, run, job, matrix, runner class,
   platform image, trust class, and cache declaration identity from persisted
   GitHub state.
3. The control plane selects the current compatible durable generation for each
   cache scope and then looks for a golden VM manifest that references exactly
   those generations plus a compatible Firecracker runtime ABI.
4. sandbox-rental warms the org runtime for the selected quota and platform
   image refs before it asks vm-orchestrator to acquire a lease.
5. vm-orchestrator prepares the static block-device graph for the root disk,
   platform toolchains, workspace cache, and manifest caches.
6. On a golden VM hit, vm-orchestrator restores Firecracker from the manifest
   and vm-bridge runs `AfterRestore` to rebind lease identity, network, host
   control, runner bootstrap material, and chrony/KVM PTP clock
   synchronization with a 1ms time-sync gate.
7. On a golden VM miss, vm-orchestrator cold boots the VM and vm-bridge runs
   `LeaseInit` to mount filesystems, apply lease state, and prove chrony/KVM
   PTP clock synchronization with a 1ms source-offset gate.
8. The Verself checkout action updates `GITHUB_WORKSPACE` to the event commit.
9. Customer steps execute normally and read or write cached paths as ordinary
   directories.
10. After the runner exits, sandbox-rental waits for the attempt-specific
   GitHub workflow job to reach `status=completed` and `conclusion=success`.
11. For promotable protected-branch success, vm-bridge runs
   `BeforeGoldenSnapshot`, optional customer expunge hooks, and a guest sync
   while mounts and warm processes are still present.
12. vm-orchestrator checkpoints the running VM and snapshots the root,
   workspace, and durable zvols as one candidate generation set and golden VM
   manifest.
13. vm-bridge then attempts to seal each writable durable cache by syncing and
   unmounting guest mounts for ordinary durable lifecycle cleanup.
14. vm-orchestrator clones, ZFS-promotes, and seals the checkpointed durable
   snapshots into the generation namespace. If no golden VM checkpoint was
   produced, the ordinary durable path snapshots the working zvol after guest
   seal.
15. The service records committed generations and the golden VM manifest
   observed from the host result.
16. A protected target-branch workflow run promotes per-job golden artifacts
   only after the provider run's required jobs are green.
17. Failed jobs, cancelled jobs, non-promotable trust contexts, ambiguous
   checkpoints, and ambiguous seals leave the current pointer unchanged.
   Successful non-promotable jobs may retain an artifact for debugging and
   later reaping.

A job can succeed while cache persistence is skipped. Cache persistence is an
acceleration artifact, not a correctness requirement for CI.

## Buildkite-Inspired Behavior

Buildkite hosted cache volumes are attached on a best-effort basis, behave like
regular Linux filesystems, require workflows to tolerate misses, commit a new
version after successful jobs, and abandon volumes after failed jobs. Verself
adopts that user model and changes the trust model for GitHub branch and PR
execution.

Verself differences:

- Cache promotion is scoped by repository, target ref, workflow job identity,
  runner class, platform image, architecture, and cache spec hash.
- Pull requests may read a protected branch's secretless cache generation, but
  PR writes never promote that protected branch pointer.
- Protected branch promotion is gated by the provider workflow result, not by a
  single job's local exit code.
- Seal and commit eligibility for GitHub Actions jobs is gated by the GitHub
  workflow-job conclusion for the observed run attempt, not by the
  actions-runner process exit code.
- Durable caches are ZFS-backed block devices attached to Firecracker guests,
  not archive uploads or downloads.

## Declaration Rules

Manifest `version` is required and must be `1`.

Each manifest `cache` entry declares one additional cache:

```text
name      stable cache name, unique within the declaration
paths     one or more directories backed by the cache
```

The declaration has no tool profiles. `name` and `paths` are the public API.
`workspace` is reserved for the built-in `GITHUB_WORKSPACE` cache.

Declaration source rules:

- `.verself/cache.yml` is repository-scoped.
- Pull request jobs read the manifest from the trusted base ref.
- Push jobs read the manifest from the pushed head ref.
- If no manifest is present, the job uses only the built-in `workspace` cache.

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
customer steps start and reports the rejected source, cache, path, and rule.
Runtime mount misses are recorded as degraded cache state and the job continues
without the affected cache. Durable caches are always best-effort after a
valid declaration has been accepted.

Mounts are cache-atomic. If one path for a cache cannot be bound, vm-bridge
unmounts any prior bind targets for that cache and marks the whole cache as a
miss. Partial multi-path caches are not exposed to customer code.

## Static VM Composition

Durable caches are composed before Firecracker starts. The VM receives a static
virtio-block topology for the full job lifetime:

```text
root disk            /dev/vda
workspace cache      /dev/vdb
manifest cache 0     /dev/vdc
manifest cache 1     /dev/vdd
...
```

There is no dynamic block-device attach path after guest boot. Mount
availability is part of lease acquisition and guest initialization, before any
customer process starts.

The same rule applies on a golden VM restore. vm-orchestrator clones every zvol
generation named by the golden VM manifest before `LoadSnapshot`, binds those
devices into the jailer chroot with the manifest's drive IDs, and only then
resumes the guest. A VM snapshot is a miss if any disk generation, drive ID,
mount path, bind path, filesystem type, read/write flag, or runtime ABI does
not match the manifest.

Read-only runner toolchain images may still declare vm-bridge writable
overlays for image-owned scratch paths. Those overlays are tmpfs and are not
durable cache generations. They are only for paths that belong to the
read-only toolchain image and must be writable for the runner process to
function.

The GitHub Actions runner toolchain currently declares writable overlays in
`src/substrate/vm-orchestrator/guest-images/gh-actions-runner/writable-overlays`.
The golden-workspace cutover removes `/opt/actions-runner/_work` from that
file. The `_work` tree is the built-in `workspace` durable cache, so a
toolchain tmpfs overlay must not mount over it or hide it. `_diag` and `_temp`
remain valid tmpfs overlays because they are runner-local diagnostic and
scratch paths, not reusable customer state.

The host prepares each writable cache as a ZFS zvol:

```text
if current generation exists:
  zfs clone <current_snapshot> orgs/<org>/workloads/<lease>/mounts/<idx>-<name>
else:
  zfs create -s -V <nominal_size> orgs/<org>/workloads/<lease>/mounts/<idx>-<name>
  mkfs.ext4 /dev/zvol/<dataset>
```

vm-orchestrator waits for every `/dev/zvol/...` device, bind-mounts the block
devices into the jailer chroot, configures Firecracker drives, and either
restores the manifest's Firecracker snapshot or cold boots the VM. Restored VMs
run `AfterRestore`; cold VMs receive the mount manifest over the guest control
protocol through `LeaseInit`. vm-bridge returns a per-filesystem mount or
restore result before the runner starts. Required `workspace` failures fail
lease acquisition. Optional manifest cache failures are recorded as degraded
cache state and the job continues without that cache.

## Guest Mounting

One manifest cache becomes one guest filesystem mounted at an internal
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
utilities. Symlinks are outside the durable-cache contract. The product contract
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

A durable seal is filesystem-level quiescence, not application-level
quiescence. Golden VM checkpointing is the process-level capture path. Verself
does not infer tool-specific health from PostgreSQL, SQLite, Docker, Bazel, or
package-manager internals; customers can use snapshot hooks to prepare or
expunge state before capture.

Golden VM checkpoint procedure for promotable protected-branch success:

1. Stop accepting new exec work for the lease.
2. Confirm provider job terminal success without running platform lease
   teardown.
3. Run `BeforeGoldenSnapshot` in vm-bridge.
4. Run optional customer expunge hooks.
5. Issue `sync` in the guest.
6. Pause Firecracker.
7. Snapshot the root, workspace, and declared durable zvols that form the VM's
   disk graph.
8. Create Firecracker vmstate and memory snapshot artifacts.
9. Publish one manifest that couples the VM snapshot to those exact zvol
   snapshots.

vm-bridge seal procedure for each writable volume:

1. Stop accepting new exec work for the lease.
2. Issue `sync` in the guest.
3. Unmount bind targets for the volume in reverse mount order.
4. Unmount the internal cache root.
5. Return a sealed result only if every unmount succeeds.

vm-orchestrator commit procedure after guest seal succeeds:

1. Flush the host block device.
2. Use the checkpoint zvol snapshot when a golden VM checkpoint exists;
   otherwise snapshot the working zvol.
3. Clone the selected snapshot into the golden generation namespace.
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

Ambiguous checkpoint or seal states skip promotion:

- Firecracker pause, memory snapshot, vmstate snapshot, or manifest publish
  fails.
- Guest control socket disappears before a seal result.
- `sync` or unmount times out.
- A bind target or root mount returns busy.
- The host block flush fails.
- ZFS snapshot, clone, promote, or final seal fails.
- Host journal recovery cannot prove a terminal committed phase.
- The GitHub job is cancelled or does not conclude `success`.

The job result does not change because of ambiguous checkpoint or cache seal.
The previous current artifact remains authoritative.

## Database Directories

Database files are ordinary durable cache contents. A golden VM snapshot can
also preserve the running database process and guest-local client connection
state when the database was started by the customer's job and its backing data
directory is part of the manifest's disk graph.

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
  logs, traces, and durable-cache metadata.

A database cache that cannot be reused consistently remains a performance
issue, not a platform correctness issue.

## Logical Data Model

The schema is a full cutover. The committed schema should contain only the
current model. Prior development tables and migration compatibility shims are
removed before merge; git history is the only record of obsolete shapes.

### Cache Manifest

```text
cache.yml
  version
  cache[]
    name
    paths[]
```

The manifest is parsed at job preparation time. Its normalized hash is recorded
on spans and durable events for diagnostics, but there is no declaration table.
Lineage is keyed per cache by `job_shape`, `durable_scope.scope_ref`, and the
cache compatibility hash, so a manifest edit only invalidates caches whose
compatibility-affecting fields changed.

`workspace` is a reserved built-in cache name for `GITHUB_WORKSPACE`.
Customer-declared caches use the same storage model and cannot declare that
name. `workspace` reconciles through git checkout; declared caches have no
platform reconciliation policy.

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
  cache_spec_hash
  created_at
```

`job_shape` is the compatibility boundary for generated state. The
`cache_spec_hash` column stores the cache compatibility hash: cache name, mount
policy, reconcile policy, and visible paths. `guest_arch` is explicit so x86_64
and aarch64 never share cache generations.

### Durable Scope

```text
durable_scope
  durable_scope_id
  org_id
  repository_id
  provider
  provider_repository_id
  scope_kind
  scope_ref
  job_shape_id
  cache_name
  trust_class
  created_at
```

`cache_name=workspace` is the built-in workspace cache. Other cache names come
from `.verself/cache.yml`. The name is part of the scope identity and
`workspace` is reserved, so customers cannot shadow the built-in cache.

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
  source_skip_reason
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
  zfs_snapshot_guid
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
reapable
reaped
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
retained or reaped by retention policy.

### Golden VM Operation

```text
golden_vm_operation
  operation_id
  execution_id
  attempt_id
  allocation_id
  org_id
  repository_id
  provider
  provider_repository_id
  scope_kind
  scope_ref
  job_shape_id
  trust_class
  source_generation_set_hash
  generation_set_hash
  candidate_golden_vm_snapshot_id
  promotion_eligible
  lease_id
  exec_id
  create_job_id
  snapshot_key
  root_snapshot_ref
  root_snapshot_guid
  vmstate_artifact_ref
  memory_artifact_ref
  mount_snapshots_json
  state_bytes
  memory_bytes
  requested_at
  create_queued_at
  creating_started_at
  created_at
  publishing_started_at
  published_at
  promoting_started_at
  promoted_at
  result_recorded_at
  state
  failure_reason
```

`state` values:

```text
requested
create_queued
creating
created
publishing
published
promoting
committed
skipped
failed
```

A golden VM operation spans the async checkpoint lifecycle after a successful
workload exits. It is separate from `durable_operation`, which remains per
mount. The operation records the source generation set observed before lease
acquisition, the River job that owns snapshot creation, the lease and exec being
held for checkpointing, and the candidate VM snapshot ID the service expects to
publish after Firecracker checkpointing succeeds.

### Golden VM Snapshot

```text
golden_vm_snapshot
  golden_vm_snapshot_id
  operation_id
  org_id
  repository_id
  provider
  provider_repository_id
  scope_kind
  scope_ref
  job_shape_id
  trust_class
  generation_set_hash
  root_snapshot_ref
  root_snapshot_guid
  snapshot_key
  vmstate_artifact_ref
  memory_artifact_ref
  state_bytes
  memory_bytes
  drive_manifest_hash
  mount_manifest_hash
  firecracker_abi_hash
  host_abi_hash
  network_model_hash
  vsock_model_hash
  clock_model_hash
  vmproto_version
  after_restore_hook_version
  before_snapshot_hook_version
  warm_profile_hash
  vcpus
  memory_mib
  provider_run_id
  provider_run_attempt
  provider_job_id
  head_sha
  tree_hash
  state
  created_at
  last_used_at
  expires_at
```

`generation_set_hash` is computed from the exact durable generations
referenced by the manifest, including cache name, generation ID, ZFS snapshot
ref, ZFS GUID, drive ID, mount path, bind paths, filesystem type, read-only
flag, and required flag. A restore hit requires every referenced generation and
image snapshot to be present locally or staged before `LoadSnapshot`.

`snapshot_key` is the host/product compatibility digest used to bind the
Firecracker artifacts to the exact disk graph and runtime ABI. It is unique for
the manifest. Provider run ID, run attempt, provider job ID, head SHA, and
tree hash are evidence and checkout metadata, not reusable identity.

`state` values:

```text
candidate
current
retained
invalidated
reapable
reaped
```

### Golden VM Snapshot Generation

```text
golden_vm_snapshot_generation
  golden_vm_snapshot_id
  durable_scope_id
  durable_generation_id
  cache_name
  zfs_snapshot_ref
  zfs_snapshot_guid
  drive_id
  mount_path
  bind_paths_json
  fs_type
  read_only
  required
  sort_order
```

This join table is the retention and debugging boundary for zvol dependencies.
A current golden VM snapshot pins every durable generation named here. The
manifest hash is derived from the canonical ordered rows; retention never
infers dependencies by parsing opaque artifact blobs.

### Golden VM Current Pointer

```text
golden_vm_current_pointer
  org_id
  repository_id
  provider
  provider_repository_id
  scope_kind
  scope_ref
  job_shape_id
  trust_class
  current_golden_vm_snapshot_id
  promoted_by_operation_id
  promoted_at
```

The pointer advances only after the protected provider run's required job set
is green and the candidate manifest was published atomically. Provider run ID,
attempt, job ID, and head SHA are evidence on the candidate, not reusable
identity. Promotion is compare-and-swap against the source
`generation_set_hash` observed before lease acquisition.

### Host Durable Journal

```text
host_durable_journal
  operation_id
  lease_id
  mount_name
  phase
  source_dataset_ref
  working_dataset_ref
  sealed_dataset_ref
  error_message
  recorded_at_unix_nano
```

Every host mutation has an operation ID before the mutation starts and a
journal row after it finishes. This table belongs to vm-orchestrator's local
host state database. The service database records observed durable operations
and generations after terminal host phases. PostgreSQL locks are not held
across ZFS operations.

### Host Golden VM Journal

```text
host_golden_vm_journal
  journal_seq
  operation_id
  checkpoint_id
  lease_id
  phase
  snapshot_key
  root_dataset_ref
  root_snapshot_ref
  state_artifact_ref
  memory_artifact_ref
  error_message
  recorded_at_unix_nano
```

The host golden VM journal records checkpoint phases that span the whole VM.
It belongs to vm-orchestrator's local host state database. It is separate from
`host_durable_journal` because a VM checkpoint couples Firecracker artifacts,
the root disk, and all mounted durable zvols rather than one mount.

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
cache_spec_hash
cache_name
trust_class
```

Matrix values are canonicalized after GitHub expands the job. Jobs with
different Node versions, Python versions, CPU architecture, service topology,
or runner class naturally receive different scopes because their job identity,
matrix key, runner class, platform image, or cache spec hash
differs.

Golden VM snapshots add compatibility dimensions on top of durable scope
identity:

```text
exact durable generation set
root substrate snapshot ref/GUID
drive manifest hash
mount manifest hash
Firecracker ABI hash
vm-bridge/vmproto version
after-restore hook version
before-snapshot hook version
warm profile hash
vCPU count
memory size
```

Provider run ID, run attempt, provider job ID, lease ID, TAP name, guest
address, and PR head SHA are not reusable identity. They are replaced or
reconciled after restore.

### Platform Version Invalidation

Breaking changes to the root substrate image, Firecracker device model,
toolchain image graph, vm-bridge protocol, guest agent, restore hooks, or clock
restore semantics advance a platform VM snapshot epoch. The control plane treats
all existing golden VM snapshots from the prior epoch as misses and cold boots
from the upgraded platform image and guest agent. Durable zvol generations keep
their normal scope and retention rules; only Firecracker vmstate, memory, and
root-snapshot reuse are globally invalidated.

The product surface should expose this as a planned platform upgrade: warm VM
cache misses use the upgraded version, and performance returns after the next
successful protected-branch run publishes a compatible golden VM snapshot. Old
Firecracker VM artifacts remain subject to the organization snapshot ring and
are reaped unless explicitly pinned by an operator.

### Golden VM Retention

Golden VM snapshots are retained as an organization-level ring buffer. Free
organizations retain one physical snapshot. Paid organizations retain two
physical snapshots. The ring is intentionally not per job shape; it is a
capacity product policy for the customer's rebuildable warm-state artifacts.

Invalidated or tombstoned snapshots are eligible for immediate physical
reaping. Reaping destroys the Firecracker vmstate and memory artifacts and the
root zvol generation through vm-orchestrator. The service keeps the manifest
row as historical metadata with `state = 'reaped'` and `reaped_at` set so
Describe-style reads can report that the snapshot existed, was invalidated or
fell out of retention, and no longer has restorable host artifacts.

## CPU Architecture

The supported hosted runner architecture for this design is Linux `x86_64` on
Firecracker. Linux `aarch64` is design-compatible because the durable cache
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

The product does not support Windows or macOS durable caches in this design.
Those operating systems need separate mount, filesystem, and runner lifecycle
contracts.

## Concurrency And Races

### Concurrent Jobs For The Same Scope

Two jobs may start from the same current generation. Each records the observed
source generation before lease acquisition. Both may seal candidate generations.
Only one CAS promotion can advance the current pointer. The loser remains a
retained generation or becomes reapable.

### Workflow-Level Promotion

A protected branch pointer advances only after the provider run's required job
set is observed green. The promotion batch is derived from GitHub workflow run
and job state. Each job's durable caches still promote independently by durable
scope CAS. If a job has three caches and only two seal cleanly, the two sealed
caches may promote and the ambiguous cache remains on its previous current
generation. A golden VM current pointer advances only when the VM checkpoint
manifest was published and all durable generations referenced by the manifest
are committed.

### Pull Requests

Pull request jobs use the target branch's current secretless cache generation
as their source when policy allows. PR candidate writes are isolated to PR or
retained candidate generations and cannot promote the target branch pointer.

For untrusted PRs, the cache declaration is read from the trusted base branch,
not from PR head. Manifest edits from PR head are ignored for host mount
planning. A PR cannot introduce new host mount paths for code that has not
been merged into the trusted branch.

### Declaration Changes

A compatibility-affecting change to a cache's name, path set, mount policy, or
reconcile policy changes that cache's durable spec hash and creates a new
durable scope. Existing current pointers remain available for older scopes
until retention reaps them. Adding, removing, or changing one manifest cache
does not invalidate `workspace` or unrelated manifest caches.

### Lease Cancellation

A provider cancellation, timeout, or control-plane cancellation can terminate
customer execution after durable caches were mounted. The operation records the
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
published VM manifest with missing artifact -> platform invariant violation
checkpoint artifact without service row -> retain orphan for recovery or destroy by policy
```

Destroying orphan working datasets is allowed only after journal reconciliation
proves they are not referenced by a live lease or a committed generation.

### Retention Race

Retention never destroys a generation referenced by `durable_current_pointer`,
`golden_vm_snapshot_generation`, a running `durable_operation.source_generation_id`,
a retained debug pin, or a sealed generation whose promotion decision is still
pending. Retention reads references and destroys through vm-orchestrator-owned
host mutation, not by service-side shell commands. Firecracker vmstate and
memory artifacts follow the same root rule: a current golden VM manifest pins
its artifacts.

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
Verself treats this as a mount miss for the affected cache. The job
continues cold and the mount miss is recorded. Customers choose clean cache
paths for reliable hit rates.

### Clock And Idempotency

Operation IDs and generation IDs are service-generated before host mutation.
Host mutation APIs are idempotent by operation ID. Wall clock is metadata; it
is not used to order promotions. Promotion ordering is by observed source
identity and CAS.

## Security Model

Generic CI jobs are secretless. Durable caches are readable by later jobs in
compatible scopes, including PR jobs when policy allows reading the target
branch's secretless current generation. Customers must not store secrets in
durable caches or golden VM memory.

The durable-cache and VM-snapshot design does not implement content-based
secret tainting. The security model relies on lane separation and explicit
snapshot hooks:

- Generic build/test CI does not receive repository, organization, or
  environment secrets.
- Jobs with staging or production authority run in a trusted lane and are not
  reusable by lower-trust cache scopes.
- OIDC or JWT credential exchange for trusted lanes produces separate trust
  scopes.
- Fork PR jobs cannot promote target branch cache generations.
- `BeforeGoldenSnapshot` and customer expunge hooks provide the supported
  surface for deleting state before a warm VM is published.
- `AfterRestore` replaces per-run bootstrap material before customer work is
  admitted to the restored VM.

Mount hardening:

- Customer zvol datasets inherit encryption from the organization storage
  namespace. vm-orchestrator must fail lease preparation if the namespace
  encryption key is unavailable or the target dataset is not encrypted.
- Cache filesystems are guest block devices, not host bind mounts.
- Guest mounts use `nodev` and `nosuid`.
- Cache paths under system roots are rejected.
- Product services never receive host ZFS authority.
- vm-orchestrator is the only runtime process that mutates ZFS, jailer state,
  Firecracker devices, TAP networking, or `/dev/zvol` devices.

## ZFS Layout

The host stores durable cache generations under the golden root:

```text
vspool/orgs/<org_id>/goldens/<durable_scope_id>/generations/<durable_generation_id>
vspool/orgs/<org_id>/goldens/<durable_scope_id>/generations/<durable_generation_id>@sealed
```

Lease working datasets live under the workload root:

```text
vspool/orgs/<org_id>/workloads/<lease_id>/mounts/<index>-<mount_name>
```

The same ZFS lifecycle applies to `workspace` and manifest caches:

```text
clone source snapshot or create empty zvol
attach as Firecracker block device
mount in guest
restore or boot VM
run workload
checkpoint VM while mounts are still present
snapshot root/workspace/durable zvols for golden VM manifest
seal in guest
flush host block device
select checkpoint snapshot or snapshot working dataset
clone selected snapshot into generation namespace
promote clone
snapshot @sealed
record service generation
CAS promote durable and golden VM pointers
```

Snapshots and clones are local lifecycle artifacts. Retention and reaping
destroy unreferenced datasets through vm-orchestrator; they do not enqueue,
upload, or catalog backup copies of customer zvols.

Firecracker vmstate and memory artifacts are stored outside ZFS dataset
lineage and referenced by the golden VM manifest. Retention treats the manifest
as the root: it must not delete a VM artifact or any zvol generation named by a
current manifest.

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
durable.cache.prepare
durable.cache.select
durable.cache.mount
durable.cache.bind
durable.cache.seal
durable.cache.commit
durable.cache.promote
durable.cache.retain
durable.cache.reap
durable.cache.reconcile
golden.vm.lookup
golden.vm.restore
golden.vm.after_restore
golden.vm.before_snapshot
golden.vm.checkpoint
golden.vm.publish
golden.vm.promote
golden.vm.reap
```

Expected durable-cache sequence for a mounted successful protected-branch run:

```text
durable.declaration.resolve  declaration  manifest|none  succeeded
durable.cache.prepare        workspace    succeeded
durable.cache.select         workspace    hit|miss
durable.cache.mount          workspace    mounted
durable.cache.seal           workspace    succeeded
durable.cache.commit         workspace    succeeded
durable.cache.promote        workspace    succeeded|already_current|conflicted
durable.cache.prepare        <name>       succeeded
durable.cache.select         <name>       hit|miss
durable.cache.mount          <name>       mounted
durable.cache.bind           <name>       mounted
durable.cache.seal           <name>       succeeded
durable.cache.commit         <name>       succeeded
durable.cache.promote        <name>       succeeded|already_current|conflicted
```

Mount misses remain visible and non-terminal after declaration acceptance:

```text
durable.cache.mount          <name>       skipped
durable.cache.bind           <name>       skipped
durable.cache.seal           <name>       skipped
```

Failed, cancelled, and lease-expired jobs skip seal and commit:

```text
durable.cache.seal           <name>       skipped
```

Successful non-promotable contexts close their durable operations without
publishing filesystem mutations:

```text
durable.cache.seal           <name>       skipped      non_promotable_scope
```

The enclosing `sandbox.durable.seal` phase is informational and succeeds with
`commit_allowed=false` and `commit_skip_reason=non_promotable_scope`.

Required row fields for debugging and verification:

```text
event_name
result
reason
cache_name
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

Golden VM events additionally carry:

```text
golden_vm_snapshot_id
generation_set_hash
snapshot_key
activation_mode
vmstate_artifact_ref
memory_artifact_ref
state_bytes
memory_bytes
```

Customer debugging surfaces show cache hit or miss, selected source generation,
mount misses, golden VM hit or miss, checkpoint result, seal result, commit
result, promotion result, and retention state. Cache misses are expected
operational states and should not require support access to diagnose.

## References

- Buildkite hosted cache volumes:
  <https://buildkite.com/docs/agent/buildkite-hosted/cache-volumes>
- Linux bind mounts and mount flags:
  <https://man7.org/linux/man-pages/man2/mount.2.html>
- Firecracker drive composition is implemented in repo-owned vm-orchestrator:
  `src/substrate/vm-orchestrator/AGENTS.md`,
  `src/substrate/vm-orchestrator/docs/zfs-volume-lifecycle.md`, and
  `src/substrate/vm-orchestrator/proto/v1/vm_service.proto`.
- Firecracker snapshot behavior:
  <https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md>
- GitHub Actions variables and `GITHUB_WORKSPACE`:
  <https://docs.github.com/en/actions/reference/workflows-and-actions/variables>

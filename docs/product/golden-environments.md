# Golden Environments

Verself hosted runners accelerate CI by starting each job from a sealed
golden environment generation selected from prior successful jobs. The public
surface is the Verself runner label plus the Verself checkout action. The
customer workflow remains ordinary GitHub Actions YAML.

The golden environment is a bundle of durable volumes mounted into a fresh VM
before the runner job starts. The first required component is the GitHub
workspace under the runner `_work` tree. Later components cover Docker layers,
language caches, database data directories, toolchain output roots, and other
customer-declared paths.

## Product Contract

1. A customer installs the GitHub app, switches jobs to Verself runners, and
   replaces `actions/checkout` with the Verself checkout action.
2. A job starts from the newest eligible golden generation for the caller repo,
   target branch, workflow job shape, trust class, runner class, and platform
   image.
3. The Verself checkout action updates the pre-mounted working copy to the
   GitHub event SHA while preserving warmed build state according to the
   checkout policy.
4. The workflow steps execute normally.
5. If the job succeeds and the execution is promotable, Verself seals the
   durable volumes and records a committed generation.
6. Branch and workflow promotion gates decide when committed job generations
   become current for their scopes.
7. Promotion updates the current pointer only with a compare-and-swap against
   the observed source generation.
8. Failed jobs, cancelled jobs, secret-tainted jobs, and non-promotable trust
   contexts do not advance reusable goldens.
9. Losing a promotion race produces a retained committed generation. It is not
   a job failure.

Branch behavior:

- Target branch jobs promote that branch after all required jobs for the commit
  are green.
- The dogfood implementation only promotes `refs/heads/main`. Non-main jobs may
  read the main golden and commit retained candidates, but they do not advance
  the reusable pointer.
- Pull request jobs read from the target branch golden selected by ancestry and
  may produce same-branch or same-PR generations when the trust policy allows.
- A push to a branch promotes that branch's own golden when the job succeeds
  and the branch trust policy allows.
- A merge has no special promotion API. The post-merge target branch workflow
  run promotes the target branch if it succeeds.

## Compatibility Scope

The system preserves GitHub Actions job boundaries. Each job receives a fresh
VM, fresh runner runtime, and its own durable mounts. Durable data crosses jobs
only through a sealed generation selected for the next job.

GitHub-managed service containers remain per-job resources. If a customer uses
`services: postgres`, GitHub starts that service for the job as usual. Verself
can accelerate customer-owned database setup when the database data directory
is mounted as a durable component controlled by the customer workflow or a
future Verself service component.

## Logical Data Model

The records below describe the product model. Physical table names can differ,
but each identity and invariant should remain explicit in the implementation.

### Organization

```text
organization
  id
  github_enterprise_id
  github_organization_id
  slug
  policy_version
  created_at
```

Owns GitHub installations, repositories, runner policies, billing, retention,
and authorization for debugging or export operations.

### Repository

```text
repository
  id
  organization_id
  github_installation_id
  github_repository_id
  full_name
  default_branch
  visibility
  created_at
```

`github_repository_id` is the stable identity. `full_name` is mutable display
metadata.

### Workflow Invocation

```text
workflow_invocation
  id
  repository_id
  github_run_id
  github_run_attempt
  github_event_name
  github_actor_id
  github_triggering_actor_id
  head_sha
  head_ref
  base_sha
  base_ref
  pull_request_number
  trust_class
  secret_policy
  received_at
```

Represents the GitHub event and run attempt. This record is derived from the
persisted webhook payload and GitHub API reads performed by the control plane.
Runner-side environment variables are telemetry. They are not authorization
inputs.

### Job Shape

```text
job_shape
  id
  repository_id
  workflow_identity
  called_workflow_identity
  job_id
  job_name
  matrix_key
  runner_class
  platform_image_id
  durable_manifest_hash
  checkout_policy_hash
  created_at
```

The job shape identifies disk compatibility. Distinct jobs usually need
distinct goldens because their setup differs materially.

Fields:

- `workflow_identity`: the caller workflow file and ref identity.
- `called_workflow_identity`: empty for normal jobs; populated for
  `workflow_call` jobs.
- `job_id`: the stable YAML job key where available.
- `matrix_key`: canonical JSON or a stable hash of matrix values.
- `runner_class`: CPU, memory, architecture, virtualization class, and runner
  label class.
- `platform_image_id`: guest OS image, kernel, runner base image, tool catalog,
  and Verself guest agent version.
- `durable_manifest_hash`: set of durable components and mount policies.
- `checkout_policy_hash`: checkout behavior that affects retained workspace
  state.

### Golden Scope

```text
golden_scope
  id
  repository_id
  scope_kind
  scope_ref
  job_shape_id
  trust_class
  created_at
```

`golden_scope` is the namespace for the current pointer. Common scopes:

- `branch`: `refs/heads/main`, `refs/heads/feature-x`
- `pull_request`: repository-local PR scope when same-PR reuse is allowed
- `debug`: explicit customer debugging scope

Branch goldens should be selected by ancestry. A PR targeting `main` should use
the exact base SHA generation when available, then the nearest green ancestor,
then a cold start.

### Golden Environment Generation

```text
golden_generation
  id
  golden_scope_id
  operation_id
  source_generation_id
  head_sha
  tree_hash
  workflow_invocation_id
  github_run_id
  github_run_attempt
  github_job_id
  result
  taint_class
  promotion_eligible
  state
  sealed_at
  committed_at
  last_used_at
  expires_at
```

Represents an immutable sealed result of one job execution.

States:

- `creating`: operation authorized, host mutation not yet sealed.
- `committed`: durable volumes exist and metadata is recorded.
- `superseded`: committed generation no longer current.
- `retained`: committed generation kept for debugging, ancestry, or rollback.
- `prunable`: generation is outside retention and has no protected reference.
- `pruned`: durable data has been destroyed.
- `failed`: operation did not produce a sealed generation.

Currentness lives in `golden_current_pointer`; generation rows are immutable
after commit except for retention metadata.

`state='committed'` requires durable storage to exist. The database must never
claim a generation is committed before the host has sealed it.

### Golden Current Pointer

```text
golden_current_pointer
  golden_scope_id
  current_generation_id
  expected_source_generation_id
  promoted_by_operation_id
  promoted_at
```

Promotion is a compare-and-swap:

```sql
update golden_current_pointer
set current_generation_id = :new_generation_id,
    promoted_by_operation_id = :operation_id,
    promoted_at = now()
where golden_scope_id = :scope_id
  and current_generation_id is not distinct from :observed_source_generation_id;
```

If the update affects zero rows, another operation won the race. The new
generation remains committed and retention decides its lifetime.

### Promotion Batch

```text
promotion_batch
  id
  repository_id
  scope_kind
  scope_ref
  head_sha
  workflow_invocation_id
  required_job_set_hash
  trust_class
  taint_policy
  state
  created_at
  closed_at
```

```text
promotion_batch_member
  promotion_batch_id
  job_shape_id
  golden_scope_id
  operation_id
  generation_id
  job_result
  promotion_result
  recorded_at
```

The batch prevents a green job from advancing its branch golden when another
required job for the same commit is red. For a target branch, every required
promotable job for the commit must have a successful committed generation
before any job scope in the batch advances. Each member still promotes with its
own CAS against its observed source generation.

States:

- `collecting`: waiting for required jobs.
- `green`: all required promotable jobs succeeded.
- `red`: at least one required job failed, cancelled, or became non-promotable.
- `promoting`: CAS promotion is running for member generations.
- `promoted`: all eligible member promotions completed or lost benign races.
- `failed`: the batch encountered a platform error while recording promotion
  results.

### Durable Component

```text
durable_component
  id
  golden_generation_id
  component_kind
  component_key
  guest_mount_path
  host_dataset_ref
  sealed_snapshot_ref
  filesystem_kind
  size_bytes_logical
  size_bytes_referenced
  scrub_policy_hash
  created_at
```

Component kinds:

- `github_workspace`: runner `_work` tree containing `GITHUB_WORKSPACE`.
- `docker_graph`: Docker or containerd storage root.
- `service_data`: database, queue, search, or other local service data.
- `tool_cache`: language package caches and compiler output roots.
- `custom_path`: customer-declared durable path.

Components are mounted before the runner starts. Component compatibility is
part of `job_shape` through `durable_manifest_hash`.

### Workspace Operation

```text
workspace_operation
  id
  workflow_invocation_id
  job_shape_id
  golden_scope_id
  source_generation_id
  observed_current_generation_id
  attempt_id
  trust_class
  taint_policy
  requested_at
  authorized_at
  host_accepted_at
  mounted_at
  runner_started_at
  runner_completed_at
  sealed_at
  result_recorded_at
  promotion_result
  final_state
```

This record authorizes the operation and joins GitHub job execution to host
storage mutation. It does not hold a PostgreSQL lock while ZFS operations run.

### Host Journal Entry

```text
host_workspace_journal
  operation_id
  host_id
  phase
  source_dataset_ref
  working_dataset_ref
  sealed_dataset_ref
  error_code
  error_message
  recorded_at
```

The vm-orchestrator owns this journal on the host. Every ZFS mutation must be
associated with an operation ID before the mutation starts and after it
finishes. The service database observes the result after the host journal has a
terminal phase.

### Taint Observation

```text
taint_observation
  id
  operation_id
  source
  taint_class
  evidence_kind
  observed_at
```

Records why a job became non-promotable or restricted. Taint is monotonic
within an operation. Once observed, later scrubbers cannot convert the operation
back to untainted.

## Golden Selection

Inputs:

```text
repository_id
github_event_name
head_sha
head_ref
base_sha
base_ref
pull_request_number
workflow_identity
called_workflow_identity
job_id
matrix_key
runner_class
platform_image_id
trust_class
durable_manifest_hash
checkout_policy_hash
```

Selection procedure:

1. Derive the caller repository, ref, commit, and trust class from persisted
   webhook state.
2. Derive the job shape from the actual job GitHub scheduled.
3. Resolve the target golden scope.
4. For pull requests, prefer a branch generation whose `head_sha` equals the
   PR base SHA.
5. If the exact base generation is unavailable, select the nearest green
   ancestor compatible with the job shape and trust class.
6. If no compatible generation exists, create an empty durable environment.
7. Record the selected source generation in the operation before lease/exec
   starts.

Compatibility dimensions:

- Repository stable ID.
- Scope kind and ref.
- Workflow and called workflow identity.
- Job ID and matrix key.
- Runner class and architecture.
- Platform image ID.
- Durable manifest hash.
- Checkout policy hash.
- Trust class and taint policy.

## `workflow_call`

Reusable workflows are modeled as jobs in the caller's run. The golden belongs
to the caller repository, caller commit, caller event, and caller trust class.
The called workflow contributes to the job shape.

Keying:

```text
repository_id = caller repository
head_sha = caller SHA
base_sha = caller base SHA, for PR events
trust_class = caller event trust class
workflow_identity = caller workflow
called_workflow_identity = called workflow repo/path/ref/SHA
job_id = job key inside the called workflow
matrix_key = caller and callee matrix values after expansion
```

Security:

- Secrets explicitly passed to a reusable workflow taint the called jobs.
- `secrets: inherit` taints the called jobs.
- Permissions on `GITHUB_TOKEN` are part of the job authority and can taint the
  operation when they provide write authority or downstream secret exchange.
- A called workflow cannot make an untrusted caller promotable.

DX rule:

- Any job that checks out the repository should use the Verself checkout action,
  including jobs defined inside reusable workflows.

## Host Storage Model

The storage model uses generation-per-dataset sealing. A mutable working clone
is snapshotted, cloned into the golden generation namespace, promoted so it no
longer depends on the ephemeral lease dataset, and sealed with `@sealed`.

Example layout:

```text
vspool/tenants/<org_id>/goldens/<golden_scope_id>/
  generations/<generation_id>/
    github_workspace
    docker_graph
    tool_cache/<component_key>
    service_data/<component_key>
```

Each component has a sealed snapshot:

```text
vspool/tenants/<org_id>/goldens/<scope>/generations/<generation>/github_workspace@sealed
```

Host lifecycle:

1. The service records and authorizes `workspace_operation`.
2. The vm-orchestrator journals `accepted`.
3. The vm-orchestrator clones source component snapshots or creates empty
   component datasets.
4. The vm-orchestrator mounts components into the guest before the runner
   starts.
5. The job runs.
6. The vm-orchestrator stops consumers, flushes, snapshots the working datasets,
   clones them into the generation namespace, promotes the clones, and seals the
   generation snapshots.
7. The vm-orchestrator journals the terminal result.
8. The service records the committed generation and attempts CAS promotion.

Storage invariants:

- No committed generation row exists before host storage is sealed.
- No ZFS mutation occurs without an operation ID in the host journal.
- No PostgreSQL row lock is held across ZFS mutation.
- No stable destination dataset receives concurrent generations.
- ZFS rollback flags such as receive-force are not used to resolve promotion
  conflicts.
- Promotion loss is recorded as metadata rather than storage failure.
- Retention destroys only generations that are not current and have no protected
  reference.

## Checkout Semantics

The Verself checkout action updates `GITHUB_WORKSPACE` from a pre-mounted
workspace generation to the event commit.

The default policy should:

- Avoid persisting Git credentials.
- Avoid cleaning warmed build artifacts unless the checkout policy explicitly
  requests it.
- Make the working tree match the requested commit for tracked files.
- Remove or quarantine files that conflict with checkout safety.
- Record preexisting HEAD, target SHA, tree hash, bytes fetched, and duration.

The checkout action is the customer-visible boundary because GitHub Actions
does not provide a pre-job repository checkout hook with the semantics Verself
needs.

Dogfood note: the current implementation persists the GitHub `_work` tree. The
Verself repository's Bazel output root and disk cache currently live under the
runner user's home directory via `.bazelrc`, outside that durable component.
The golden workspace path therefore proves instant workspace selection and
checkout, but it does not by itself make `bazelisk build //...` warm. Bazel
speedup requires either a durable `tool_cache` component for the Bazel output
root and disk cache or a CI policy that places those paths under a durable
mount.

## Security Model

The security boundary combines trust classes, taint classification, and
redaction. Customer redaction reduces accidental persistence. Verself trust
policy prevents secret-bearing executions from producing goldens that later run
in lower-trust contexts.

The dogfood implementation relies on the repository-wide secretless CI invariant
and does not inspect workflow secret usage. Taint classification becomes an
enforced data model when trusted deployment lanes and customer secret surfaces
are admitted into golden generation.

### Trust Classes

Suggested classes:

- `fork_pull_request`: code from a fork or untrusted actor.
- `same_repo_pull_request`: PR branch in the same repository.
- `unprotected_branch`: branch push without protected-branch guarantees.
- `protected_branch`: protected branch push after repository policy checks.
- `environment_protected`: job gated by GitHub Environment protection.
- `debug_session`: customer-requested debugging VM or export operation.

Trust ordering is monotonic. A generation can be reused only by a job whose
trust class is allowed to read from the source generation's class.

Default read policy:

- Untrusted PRs may read from trusted base branch secretless goldens.
- Trusted branch jobs may read from their own branch goldens.
- Secret-tainted goldens are restricted to debugging or same-authority reuse
  when an explicit policy enables it.

Default write policy:

- Secretless protected branch jobs can promote branch goldens.
- Secretless same-repo branch jobs can promote branch-scoped goldens when
  repository policy allows.
- Untrusted PR jobs cannot promote target branch goldens.
- Secret-tainted jobs cannot promote reusable goldens.

### Taint Sources

An operation becomes tainted when the job receives or can mint sensitive
authority.

Sources:

- GitHub Actions secrets.
- `workflow_call` explicit secrets.
- `workflow_call` `secrets: inherit`.
- GitHub Environments with secrets.
- OIDC tokens or cloud credential exchanges.
- Deploy keys, SSH agents, or private Git credentials.
- Package registry tokens.
- Writable or elevated `GITHUB_TOKEN` permissions.
- Customer-provided secret mounts.
- Verself debug credentials or SSH session material.
- Detected credential files in durable paths.

Taint applies to the operation and all durable components produced by it. A
component scrubber can remove files before seal, but it cannot remove the taint
classification from the operation.

### Promotion Policy

Default policy:

```text
if job_result != success:
  reject promotion
if taint_class != secretless:
  reject reusable promotion
if trust_class cannot write target scope:
  reject promotion
if platform image or durable manifest changed:
  create a new compatibility lineage
otherwise:
  commit generation and CAS-promote current pointer
```

Explicit opt-in policy for tainted generations should be narrow:

- Same repository.
- Same ref or explicit debug scope.
- Same secret authority.
- Short retention.
- No default download/export.
- Visible taint marker in API and UI.
- No reuse by fork PRs or lower-trust jobs.

### Redaction And Scrubbing

Customer durable manifests may include exclusions:

```yaml
persist:
  include:
    - "$GITHUB_WORKSPACE"
    - "/var/lib/docker"
    - "/home/runner/.cache/bazel"
  exclude:
    - "**/.env"
    - "**/.npmrc"
    - "**/.ssh/**"
    - "**/.aws/**"
    - "**/.docker/config.json"
```

Platform scrubbers should run regardless of customer policy:

- Exclude runner runtime directories.
- Exclude action temp directories.
- Exclude OIDC token files.
- Exclude runner credentials and registration material.
- Exclude SSH agent sockets.
- Exclude known Git credential helpers and credential URLs.
- Exclude package-manager auth files when recognized.
- Mark the operation tainted when credential-like material is detected in a
  durable component.

Redaction failures are security events when they involve platform-owned
credential material.

## Customer APIs

Initial public surface:

- Runner labels.
- Verself checkout action.
- Web UI status for golden hit/miss, source generation, and promotion result.

Later surfaces:

- List golden scopes and generations.
- Show generation metadata, size, commit, workflow, job, matrix, runner image,
  taint class, and retention status.
- Delete or pin generations.
- Open a debug VM from a generation.
- Download an export when policy allows.
- Declare durable components and scrub policies.
- Configure promotion rules.

API responses must expose taint, trust, and compatibility metadata. Hidden
policy decisions make golden selection behavior impossible to debug.

## Observability

Every job should produce ClickHouse evidence for:

- Golden selection input.
- Selected source generation.
- Cache hit or miss.
- Durable component mount plan.
- Checkout duration and result.
- Host journal phases.
- Seal result.
- Generation commit result.
- CAS promotion result.
- Taint observations.
- Retention decisions.

Recommended span names:

```text
github.workspace.select
github.workspace.prepare
github.checkout.update
golden.operation.accept
golden.component.mount
golden.component.seal
golden.generation.commit
golden.generation.promote
golden.generation.retain
golden.generation.prune
```

Completion evidence for implementation changes should include GitHub run IDs,
operation IDs, generation IDs, selected source generation IDs, and promotion
results.

## Failure Semantics

Job failure:

- The GitHub job result remains the customer-visible CI result.
- The source golden remains current.
- A failed generation is not committed.

Persistence failure after job success:

- The CI result can remain success for compatibility.
- Verself records a platform degradation event.
- No current pointer advances.
- The UI and API show that no new golden was produced.

Promotion race:

- The job can remain success.
- The generation remains committed and retained or pruned by policy.
- The current pointer remains on the winner.

Host crash during mutation:

- The host journal drives recovery.
- Operations without sealed durable storage become failed.
- Orphan working datasets are destroyed only after journal reconciliation.

## Open Design Decisions

- Whether default CI success should fail when golden persistence fails in a
  customer opt-in "golden required" mode.
- Exact ancestry algorithm for nearest green ancestor selection.
- Durable component manifest syntax and defaults.
- Service quiescing protocol for customer-managed databases.
- Policy for branch-scoped goldens on unprotected branches.
- Export format and encryption model for downloadable generations.

## References

- GitHub reusable workflows:
  <https://docs.github.com/en/actions/how-tos/sharing-automations/reusing-workflows>
- GitHub Actions variables and `GITHUB_WORKSPACE`:
  <https://docs.github.com/en/actions/reference/workflows-and-actions/variables>
- GitHub self-hosted runner security hardening:
  <https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions>
- GitHub runner job hooks:
  <https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/running-scripts-before-or-after-a-job>
- OpenZFS receive semantics:
  <https://openzfs.github.io/openzfs-docs/man/master/8/zfs-receive.8.html>
- OpenZFS promote semantics:
  <https://openzfs.github.io/openzfs-docs/man/master/8/zfs-promote.8.html>

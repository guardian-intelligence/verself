# VM Execution Control Plane

sandbox-rental-service owns customer semantics: org policy, IAM, billing
reservation/activation/settlement, GitHub demand records, execution state,
logs, and public API DTOs. vm-orchestrator owns only host facts: VM leases,
execs inside leases, ZFS, TAP slots, Firecracker, vm-bridge control, and guest
telemetry.

Code pointers:

- `internal/jobs/` - River workers, execution attempts, billing windows,
  GitHub demand records, and reconciliation.
- `internal/api/` - secured Huma routes and the sandbox operation catalog.
- `migrations/` - PostgreSQL tables for executions, attempts, billing windows,
  logs, GitHub workflow jobs, runner allocations, and assignment bindings.
- `../../vm-orchestrator/proto/v1/` - host lease/exec gRPC API. This is V1 of
  the rewritten orchestrator contract; the old Run API is gone.
- `../../apiwire/sandbox.go` - service wire DTOs.
- `../openapi/` - generated OpenAPI contracts.

State model:

- `executions` are customer-visible workload submissions.
- `execution_attempts` are durable River/reconciliation units. Attempts store
  host-assigned `lease_id` and `exec_id` only after the host returns them.
- `execution_billing_windows` are control-plane billing records. The host never
  receives billing, org, customer, attempt, or quota vocabulary.
- `github_workflow_jobs` are GitHub demand facts from webhooks and polling.
- `github_runner_allocations` are Forge Metal capacity records.
- `github_runner_job_bindings` are the only authoritative job-to-runner
  assignment records.

Direct execution flow:

1. `POST /api/v1/executions` inserts `executions` + `execution_attempts` and
   enqueues `execution.advance`.
2. The worker reserves billing, acquires a vm-orchestrator lease, starts an exec,
   activates billing at `exec.started_at`, and waits for the exec result.
3. The worker releases the lease, settles or voids billing with detached bounded
   cleanup contexts, writes logs, and writes a ClickHouse `job_events` row.
4. Reconciliation repairs stale reserved/launching attempts by voiding reserved
   windows, releasing any lease ID already recorded, and terminalizing the
   attempt.

Single-node VM concurrency budget:

- `SANDBOX_EXECUTION_MAX_WORKERS=16` is the production default for the current
  single-node bare-metal profile: 24 logical CPUs, 93 GiB RAM, no swap, and the
  `metal-4vcpu-ubuntu-2404` runner class at 4 vCPU / 4096 MiB / 8 GiB rootfs.
- Treat the worker count as the customer VM admission limit. One River execution
  worker can hold one active vm-orchestrator lease, so raising this value raises
  the maximum number of simultaneous Firecracker VMs, TAP slots, ZFS clones,
  guest bridges, and billing windows.
- The default is based on public API proof runs, not theoretical vCPU math. On
  2026-04-15, `sandbox-mixedcpu4-16w-2048m-128d-3cpu-20260415T075732Z` ran
  200 submissions at 16 workers with each VM touching 2 GiB RAM, running four
  CPU-bound workers for 3 seconds, and writing/fsyncing/reading 128 MiB. It
  passed with exec p50 7.53s, exec p99 8.97s, max 9.21s, max observed 1-minute
  load 35.07, and minimum observed `MemAvailable` 46.07 GiB.
- 20 and 24 workers are proven burst settings for smaller CI-shaped jobs, not
  defaults for arbitrary customer workloads. The same four-worker mixed profile
  passed at 20 workers in
  `sandbox-mixedcpu4-20w-2048m-128d-3cpu-20260415T075342Z`, but exec p99 grew
  to 11.45s and load peaked at 43.79. It passed at 24 workers in
  `sandbox-mixedcpu4-24w-2048m-128d-3cpu-20260415T080126Z`, but exec p99 grew
  to 11.97s, max execution duration to 13.50s, boot p99 to 3.32s, load peaked
  at 52.13, and minimum observed `MemAvailable` fell to 26.59 GiB.
- Do not set the global default above 16 until admission control distinguishes
  full-memory arbitrary workloads from constrained CI runner workloads. A future
  CI runner class may use a higher class-specific limit after it has explicit
  memory admission, class-local proof runs, and tail-latency SLOs.
- The current boot-time bottleneck is guest readiness, not Firecracker launch.
  Across these runs `vmorchestrator.firecracker.instance_start` stayed in the
  tens of milliseconds, while `vmorchestrator.guest.control_connect` dominated
  lease boot at roughly 1.0-1.4s p50 and worsened under CPU pressure. Direct
  netlink can still improve TAP correctness and p99 setup, but it is not the
  lever that removes the current ~1s boot floor.

Expected proof surface:

- PostgreSQL shows each attempt moving through
  `queued -> reserved -> launching -> running -> finalizing -> succeeded`.
- ClickHouse `forge_metal.job_events` has one succeeded row per execution.
- ClickHouse `forge_metal.vm_lease_evidence` has `lease_ready`,
  `exec_started`, and `lease_cleanup` rows for each host lease.
- OTel traces include `sandbox-rental.execution.submit`,
  `sandbox-rental.execution.run`, `rpc.AcquireLease`, `rpc.StartExec`,
  `rpc.WaitExec`, `rpc.ReleaseLease`, and `vmorchestrator.lease.boot`.

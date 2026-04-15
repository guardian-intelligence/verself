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

Expected proof surface:

- PostgreSQL shows each attempt moving through
  `queued -> reserved -> launching -> running -> finalizing -> succeeded`.
- ClickHouse `forge_metal.job_events` has one succeeded row per execution.
- ClickHouse `forge_metal.vm_lease_evidence` has `lease_ready`,
  `exec_started`, and `lease_cleanup` rows for each host lease.
- OTel traces include `sandbox-rental.execution.submit`,
  `sandbox-rental.execution.run`, `rpc.AcquireLease`, `rpc.StartExec`,
  `rpc.WaitExec`, `rpc.ReleaseLease`, and `vmorchestrator.lease.boot`.

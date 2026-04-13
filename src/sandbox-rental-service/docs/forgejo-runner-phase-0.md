# Forgejo Runner Phase 0: Guest Workflow Engine

Status: Implemented tracer bullet; live closure requires a fresh guest-rootfs and single-node proof
Owner: `sandbox-rental-service`
Last Updated: 2026-04-13

## Scope

Phase 0 closes the runner-engine decision before adding the Forgejo north face.
It proves that a Forgejo Actions workflow can run through the existing
`sandbox-rental-service -> vm-orchestrator -> vm-bridge -> Firecracker` path
without adding a new host service, a new vsock protocol, or a new bespoke guest
helper.

The implementation uses the pinned Forgejo runner CLI inside the guest:
`forgejo-runner exec --event push --workflows /workspace/.forgejo/workflows/job.yml --no-recurse --image -self-hosted`.
That CLI is the supported way to exercise the Forgejo `act` fork after the fork
became library-only. Importing the fork directly into `vm-bridge` was rejected
for this tracer bullet because it couples PID 1 to a large workflow engine and
skips the runner behavior Forgejo itself relies on.

Phase 0 does not implement runner registration, `FetchTask`, live `UpdateLog`,
customer-facing Forgejo workflow submission, per-org runners, cache, artifacts,
or the `services:` workflow keyword.

## Current Contract

- Public runner label: `metal-4vcpu-ubuntu-2404`.
- Execution source: `source_kind='forgejo_actions'`.
- Workload kind: `workload_kind='forgejo_workflow'`.
- Guest rootfs: Ubuntu 24.04 base image with pinned GitHub Actions runner,
  Forgejo runner, Go, Node.js, git, and the Forge Metal guest agents.
- VM protocol: `vmproto.ProtocolVersion == 2` with workload discriminator fields
  on `RunRequest`; direct shell execution remains `workload_kind='direct'`.
- Database correlation: `executions.runner_class`,
  `executions.external_provider`, `executions.external_task_id`,
  `execution_workload_specs`, `execution_workload_secrets`, and ClickHouse
  `forge_metal.job_events.runner_class`.

## Verification Gate

Fail-first protocol:

1. Run the proof against `main` before this branch: no `forgejo_workflow`
   workload kind, no pinned Forgejo runner in the guest rootfs, and no
   runner-class projection exist.
2. The workflow proof must fail before any production code is considered green.

Green protocol:

1. Rebuild the guest rootfs and deploy the single node:
   `ansible-playbook playbooks/guest-rootfs.yml` followed by
   `ansible-playbook playbooks/dev-single-node.yml`.
2. Submit a `forgejo_workflow` execution that writes a workflow with
   `runs-on: metal-4vcpu-ubuntu-2404`, includes a checkout-using case when a repo
   is present, runs a Node-backed step, and prints `phase-0-forgejo-act`.
3. Assert the execution terminalizes through River-backed
   `execution.advance`, settles billing, writes logs, and projects one
   ClickHouse job event.

PostgreSQL:

```sql
SELECT e.source_kind, e.workload_kind, e.runner_class, e.external_provider, a.state
FROM executions e
JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
WHERE e.execution_id = $1;
```

Expected row:

- `source_kind='forgejo_actions'`
- `workload_kind='forgejo_workflow'`
- `runner_class='metal-4vcpu-ubuntu-2404'`
- `external_provider='forgejo'`
- terminal attempt state `succeeded`

ClickHouse:

```sql
SELECT count()
FROM forge_metal.job_events
WHERE execution_id = $1
  AND source_kind = 'forgejo_actions'
  AND workload_kind = 'forgejo_workflow'
  AND runner_class = 'metal-4vcpu-ubuntu-2404'
  AND status = 'succeeded'
  AND exit_code = 0;
```

```sql
SELECT count()
FROM forge_metal.job_logs
WHERE attempt_id = $1
  AND positionCaseInsensitive(toString(chunk), 'phase-0-forgejo-act') > 0;
```

Required trace order:

1. `sandbox-rental.execution.submit`
2. `river.insert_many`
3. `river.work/execution.advance`
4. `sandbox-rental.runner_class.resolve`
5. `vm-orchestrator.EnsureRun`
6. `vmorchestrator.guest.run_request`
7. `vmorchestrator.guest.phase_start`
8. `vmorchestrator.guest.phase_end`
9. `vm-orchestrator.WaitRun`
10. `sandbox-rental.execution.finalize`

The same trace must also include the vm-orchestrator lifecycle spine introduced
for direct execution proof: `rpc.EnsureRun`, `vmorchestrator.managed_run`,
`vmorchestrator.Run`, `vmorchestrator.zfs.clone`,
`vmorchestrator.runDataset`, `vmorchestrator.network.setup`,
`vmorchestrator.firecracker.instance_start`, `vmorchestrator.guest.hello`,
`vmorchestrator.vm.exit_wait`, `vmorchestrator.zfs.written`, and
`vmorchestrator.zfs.volsize`.

Billing:

```sql
SELECT state, actual_quantity
FROM execution_billing_windows
WHERE attempt_id = $1
ORDER BY window_seq DESC
LIMIT 1;
```

Expected row: `state='settled'` and `actual_quantity > 0`.

## River Handoff

River is the durable scheduler runtime. The workflow tracer must enter through
the same `Submit -> execution.advance` path as direct executions. Runner
adapters should enqueue provider events or execution advances through River; the
VM execution substrate remains vm-orchestrator and vm-bridge.

## Primary Sources

- Forgejo code hosting docs: https://forgejo.org/docs/latest/contributor/code-forgejo-org/
- Forgejo runner source: https://code.forgejo.org/forgejo/runner
- Forgejo runner protocol package: https://pkg.go.dev/code.forgejo.org/forgejo/actions-proto/runner/v1
- Forgejo runner ConnectRPC client package: https://pkg.go.dev/code.forgejo.org/forgejo/actions-proto/runner/v1/runnerv1connect
- Forgejo `act` fork source and README: https://code.forgejo.org/forgejo/act

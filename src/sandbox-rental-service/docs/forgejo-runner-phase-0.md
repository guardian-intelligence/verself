# Forgejo Runner Phase 0: Act Tracer Bullet

Status: Proposed
Owner: `sandbox-rental-service`
Last Updated: 2026-04-13

## Scope

Phase 0 exists to close the runner-engine decision before adding the Forgejo
runner north face. It proves that a Forgejo Actions task can be reduced to a
deterministic workflow run using the Forgejo `act` fork and the existing
vm-orchestrator/Firecracker execution path.

Phase 0 does not implement runner registration, `FetchTask`, live `UpdateLog`,
customer-facing workflow submissions, per-org runners, cache, or artifacts. The
River scheduler runtime is now part of `sandbox-rental-service`; production
runner work must attach to `execution.advance` instead of adding a private
runner scheduler.

## Locked Amendments

- Use Forgejo `act`, not upstream `nektos/act` and not the `act` CLI. The Forgejo fork is hosted at `code.forgejo.org/forgejo/act` and its README states that it is no longer usable as a command-line tool, only as a library for Forgejo runner.
- Treat the current tag `v1.37.0` as the Phase 0 pin candidate. It resolves to git hash `a48f2a44d9275a2edcef5e1df6b0a5ebca0d61dc` from `https://code.forgejo.org/forgejo/act.git`.
- The fork currently declares `module github.com/nektos/act`. Go code should import `github.com/nektos/act/...` and use a module `replace` to `code.forgejo.org/forgejo/act v1.37.0` unless the fork changes its module path before implementation.
- Do not import `code.forgejo.org/forgejo/runner/v12` or its vendored `act` tree into Forge Metal code. Use Forgejo runner as a protocol and behavior reference; use the MIT-licensed Forgejo `act` fork as the engine dependency.
- Do not add a separate scheduler path for runner work before River. The tracer
  bullet may use a direct harness path, but the production runner path must
  queue through River and advance the Postgres execution state machine described
  in `durable-execution-workflow-plan.md`.
- Correct the plan against current repo state: sandbox PostgreSQL migrations live in `src/sandbox-rental-service/migrations/`; ClickHouse job evidence tables live in `src/platform/migrations/`; `job_logs` uses `chunk` and `created_at`; billing windows use `actual_quantity`; Forgejo Actions are currently disabled in the Forgejo Ansible template; `.forgejo/workflows/ci.yml` already exists as a tracer workflow.

## Phase 0 Closure Definition

Phase 0 is closed only when all of the following are true against the real single-node environment:

1. A small Forgejo-task-shaped fixture is executed through the same VM path that direct executions use today.
2. The fixture uses the Forgejo `act` library with a host-mode platform picker that maps the Forge Metal runner label to `-self-hosted`.
3. The workflow includes `actions/checkout@v4`, at least one JavaScript action or Node-backed step, and a marker step that prints `phase-0-forgejo-act`.
4. The run produces ClickHouse proof in `forge_metal.job_events`, `forge_metal.job_logs`, and `default.otel_traces`.
5. The proof is reproduced after:
   - `ansible-playbook playbooks/guest-rootfs.yml`
   - `ansible-playbook playbooks/dev-single-node.yml --tags forgejo,firecracker,sandbox_rental_service,caddy`
6. The implementation records the selected guest packaging decision:
   - preferred: compile the workflow runner into `vm-bridge` so there is no new in-guest binary;
   - fallback: create one dedicated guest helper binary only if importing `act` into `vm-bridge` creates an unacceptable PID 1 dependency or binary-size footprint.

## Implementation Plan

### 1. Source Contract Probe

Create a build-only probe that imports the Forgejo `act` library from the pinned fork, constructs a `runner.Config`, parses a one-job workflow, and executes it with a host-mode platform picker. This probe does not touch Forgejo, vm-orchestrator, or River. Its job is to fail fast on module-path, API, or dependency drift.

The probe should record the fork URL, tag, git hash, and Go module path in a small package-level constant so traces and errors show exactly which engine was exercised.

The probe should also compare the direct fork API against the behavior Forgejo runner relies on, especially fields around default action URLs, server version handling, token use for action checkout, event JSON, vars, and ID-token request fields. If the direct fork lacks a runner-required knob, Phase 0 must expose the missing behavior through our adapter or fail the gate; it must not silently fall back to upstream `nektos/act` behavior.

### 2. Task Context Compiler

Create an internal package that converts a minimal `runnerv1.Task` fixture into an execution-ready workflow run spec:

- workflow payload and single job ID;
- event name and event JSON;
- `github`/`forge` context values from task context;
- `ACTIONS_RUNTIME_TOKEN`, ID-token request fields, secrets, vars, inputs, and default actions URL;
- platform picker for `forge-metal-2vcpu-4gb:host` and future Forge Metal labels;
- explicit unsupported diagnostics for `services:`, cache, and artifacts.

Do not import Forgejo runner `internal/` packages. Port only the small mapping we need and back it with fixtures derived from upstream Forgejo runner behavior.

### 3. Guest Packaging Decision

Run the probe both outside and inside the guest build target:

- If `vm-bridge` can import the library without unacceptable bloat or PID 1 lifecycle risk, add a `workflow_runner` module beside `agent.go`.
- If not, document the exception and compile a single dedicated guest helper. The helper remains implementation detail behind vm-bridge and does not create a new host service or protocol.

The fork is library-only, so baking an `act` binary into the rootfs is not a valid Phase 0 implementation.

### 4. VM Tracer Bullet

Add a private verification path that submits one synthetic Forgejo-task-shaped
workflow through the existing direct execution control-plane path. The workflow:

```yaml
name: phase-0-forgejo-act
on: push
jobs:
  probe:
    runs-on: forge-metal-2vcpu-4gb
    steps:
      - uses: actions/checkout@v4
      - run: echo "phase-0-forgejo-act $(git rev-parse --short HEAD)"
      - run: node --version
```

The tracer must run in Firecracker, not in a local Docker or host-only substitute.

### 5. Evidence Gate

The fail-first gate should fail on current `main` because there is no Forgejo `act` tracer path and no matching spans.

The green gate asserts:

```sql
SELECT count()
FROM forge_metal.job_events
WHERE execution_id = $1
  AND kind = 'forgejo_workflow'
  AND status = 'succeeded'
  AND exit_code = 0;
```

```sql
SELECT count()
FROM forge_metal.job_logs
WHERE attempt_id = $1
  AND positionCaseInsensitive(toString(chunk), 'phase-0-forgejo-act') > 0;
```

```sql
SELECT SpanName
FROM default.otel_traces
WHERE TraceId = $1
ORDER BY Timestamp;
```

Required span names, in order:

- `sandbox-rental.execution.submit`
- `sandbox-rental.execution.run`
- `vm-orchestrator.EnsureRun`
- `vm-orchestrator.WaitRun`
- `vm-bridge.run_phase`
- `vm-bridge.workflow_runner.act_prepare`
- `vm-bridge.workflow_runner.act_execute`

The same `TraceId` must include the vm-orchestrator latency spine introduced
for the direct execution path: at minimum `rpc.EnsureRun`,
`vmorchestrator.managed_run`, `vmorchestrator.Run`,
`vmorchestrator.zfs.clone`, `vmorchestrator.jail.setup`,
`vmorchestrator.network.setup`, `vmorchestrator.firecracker.instance_start`,
`vmorchestrator.guest.hello`, and `vmorchestrator.vm.exit_wait`. The matching
`forge_metal.vm_run_evidence` rows for `execution_attempts.orchestrator_run_id`
must carry that trace ID for the `run_state_transition` evidence through
`pending`, `running`, and `succeeded`. Guest telemetry evidence is
opportunistic for short runs; when `telemetry_hello` or `telemetry_diagnostic`
rows exist for the run, they must carry the same trace ID.

The matching billing assertion uses `execution_billing_windows.actual_quantity > 0` and terminal window state `settled`.

## River Handoff

The River runtime tracer bullet is already landed in `sandbox-rental-service`:
`scheduler.probe` proves `riverpgxv5`, queue registration, and OTel middleware.
The next handoff is the execution state-machine cutover that runner work will
reuse:

1. Replace API-launched goroutines with transactional enqueue of
   `execution.advance`.
2. Move execution state changes into compare-and-swap transition helpers.
3. Store submit/fetch trace context on the execution/attempt and link worker
   traces back to it.
4. Re-run the Phase 0 tracer queued through River instead of the private direct
   harness.

Only after the same tracer produces identical ClickHouse evidence when queued
through River should Phase A resume.

## Primary Sources

- Forgejo code hosting docs: https://forgejo.org/docs/latest/contributor/code-forgejo-org/
- Forgejo `act` fork source and README: https://code.forgejo.org/forgejo/act
- Forgejo runner source: https://code.forgejo.org/forgejo/runner
- Forgejo runner protocol package: https://pkg.go.dev/code.forgejo.org/forgejo/actions-proto/runner/v1
- Runner ConnectRPC client package: https://pkg.go.dev/code.forgejo.org/forgejo/actions-proto/runner/v1/runnerv1connect

# Disaster Recovery Reference

Every deployable unit converges to its desired state through a level-triggered
reconciler that a Nomad allocation runs as a long-lived sidecar. Disaster
recovery is that control loop operating from a degraded starting point. A
component that converges from zero converges identically during deployment and
during steady-state drift; recovery spends more time closing a larger gap. This
reference defines the recovery lifecycle contract, the canonical `nomad.hcl`
topology, and the reasoning behind each decision.

The reference implementations on this contract are `spire` (`task "registrar"`)
and `postgresql` (`task "reconcile"`), both of which run a reconcile loop as a
`poststart` sidecar. `openbao` carries the most developed reconciler binary in
`openbao-recover` and is the worked example used throughout.

## The reconciler

A recovery reconciler is a function of observed state. It reads the live state of
the resource, classifies it into one of a closed set of states, and runs the
idempotent action that closes the gap to desired. It never branches on assumed
history, so re-running it from any starting point converges to the same result.

`openbao-recover`'s `recoverOnce` is the canonical body: read `Status`,
`classify` into `Uninitialized` / `InitializedSealed` / `InitializedUnsealed`,
and dispatch the action that advances that state — restore or fresh-init when
uninitialized, unseal when sealed, reconcile baseline when unsealed, no-op when
already converged. The choice between restore and fresh-init is made by observing
whether an offsite snapshot is present, so a component prefers offsite restore
when backups exist and bootstraps clean when they do not.

The reconciler emits its result as a set of conditions written to a nonsecret
report path. Conditions carry a `type`, a `status` of `True` or `False`, a
`reason`, and the resource name. The terminal condition is
`<Resource>RecoveryComplete`; intermediate conditions such as `OpenBaoUnsealed`
and `OpenBaoBaselineReconciled` record progress and the precise reason a gap
remains open.

## Canonical topology

```hcl
job "<component>" {
  group "<component>" {
    count = 1

    # Idempotent materialization of the runtime tree and rendered config from the
    # resource graph. Runs to completion before the process starts.
    task "prepare" {
      lifecycle { hook = "prestart"; sidecar = false }
      config {
        command = "<component>-recover"
        args    = ["prepare", "--resource-graph=${var.repo_root}/workspace/.guardian/fly/document.json", "--resource-name=<component>"]
      }
    }

    # The long-running process. On-node restart is enabled so "process running" is
    # a maintained invariant; a restart is safe because the unseal/bootstrap actuator
    # is autonomous (auto-unseal, idempotent bootstrap), not human-gated.
    task "serve" {
      driver = "raw_exec"
      restart { attempts = 3; delay = "15s"; interval = "300s"; mode = "delay" }
      config { command = "<component>"; args = ["serve", ...] }
    }

    # The reconciler. Runs for the allocation's whole life, re-sampling observed
    # state on an interval and re-converging. Authenticates with a scoped Nomad
    # workload identity rather than operator authority.
    task "reconcile" {
      lifecycle { hook = "poststart"; sidecar = true }
      identity  { name = "vault_default"; aud = ["vault.io"]; ttl = "1h"; file = true }
      config {
        command = "<component>-recover"
        args = [
          "reconcile", "--loop",
          "--resource-graph=${var.repo_root}/workspace/.guardian/fly/document.json",
          "--resource-name=<component>",
          "--report=/run/verself/recovery/<component>/report.json",
        ]
      }
    }

    # Placement convergence for a single global writer stays with fly, not Nomad,
    # to avoid split-brain. Stateless components may reschedule freely.
    reschedule { attempts = 0; unlimited = false }

    # A revert target that auto-promotes cannot stall at a manual gate.
    update { canary = 1; auto_revert = true; auto_promote = true; health_check = "checks" }
  }
}
```

## Decisions

### Reconcile as a `poststart sidecar = true` loop

A controller closes the gap between desired and observed continuously. A task
that runs once at allocation start samples a single point in time and is blind to
drift that appears afterward — a process that re-seals after a transient restart,
baseline configuration that diverges, an authority that becomes available a few
seconds later. Running the reconciler as a sidecar that re-samples on an interval
makes the converged state a maintained invariant for the whole life of the
allocation. `spire`'s `registrar` and `postgresql`'s `reconcile` already run this
shape via `<component>-recover reconcile --loop`.

### Observe, classify, act; idempotent and keyed on measured state

The action is selected from the current observed state, never from a record of
what previous runs did. This makes every reconcile re-entrant: a half-completed
recovery, a fresh node, and a fully converged node all run the same code and
arrive at the same place. Transient failures retry within a bounded window before
the reconciler reports a `False` condition and waits for the next tick.

### Autonomous unseal actuator; Shamir is breakglass

A level-triggered loop can only self-heal when its actuator runs without a human.
Unseal driven by an external seal or equivalent root-of-trust mechanism lets the
process unseal itself on every restart, which is what makes on-node restart of the
`serve` task safe. Threshold Shamir material is breakglass authority presented
through ephemeral operator paths, and the generate-root path is breakglass only.
Recovery that depends on a human piping unseal shares into stdin forecloses the
sidecar loop and turns every seal event into a manual intervention.

### `serve` restarts on-node; placement convergence for singletons stays with `fly`

"Process running" is maintained on the node through a bounded `restart` policy.
"Allocation placed on a healthy node" is a separate invariant; for a single global
writer (`count = 1`) it is held by `fly` rather than by Nomad rescheduling, so a
node fault does not trigger a reschedule that could produce a second writer. The
reconciliation authority for placement is relocated to `fly`, recorded explicitly,
rather than dropped. Stateless components reschedule freely.

### Conditions are a feedback signal

The report's conditions are read, not only written. The `fly` procedure gates
promotion on `<Resource>RecoveryComplete` being `True` and treats a `False`
condition as the reason to wait or fail. Components also surface recovery health
at `/recoveryz`, and components across the platform emit conditions to a report
path. A reconciler whose conditions are written but never consumed is an open loop with
a sensor and no wire back to the controller.

### Separate actuator authorities; model unavailability as a condition

Unseal authority and baseline-reconcile authority differ, and a component's own
auth method must exist before the scoped workload identity that depends on it can
be used. The reconciler handles this staging inside one loop: when the authority
for a step is not yet available it emits a descriptive `False` condition
(`BaselineDeferred`, `WaitingForOpenBao`) and retries on the next tick, rather
than splitting the work across separate one-shot tasks. Authentication uses a
scoped Nomad workload identity; a transient operator token used during fresh
bootstrap is revoked before recovery reports complete. How that transient token
is obtained, scoped, and audited is defined in
`disaster-recovery-bootstrap-trust.md`.

### Restore versus fresh-init is decided by observed storage

An uninitialized resource with an offsite snapshot present restores from the
snapshot after verifying its digest against the manifest; an uninitialized
resource with no snapshot bootstraps a clean instance. Both paths converge to the
same desired state. Customer data excluded from offsite backup by design is not
recoverable by this path and is bootstrapped empty.

## Anti-patterns

- A one-shot `prestart`/`poststart` recovery task in place of a sidecar loop.
  It cannot observe or correct post-start drift. `openbao` currently splits
  recovery across `recover` and `baseline` `poststart` tasks and converges to the
  sidecar shape defined here.
- Shamir unseal as the steady-state restart path. It makes autonomous restart
  impossible and is reserved for breakglass.
- A report that nothing consumes. Conditions exist to gate `fly` and to populate
  `/recoveryz`.
- A `serve` task with restart disabled and no autonomous unseal actuator. A single
  process fault then requires a human to recover a running process.

## Conformance

A component conforms when its `nomad.hcl` runs a `<component>-recover reconcile
--loop` sidecar, the binary classifies observed state and acts idempotently, the
reconciler emits `<Resource>RecoveryComplete` and intermediate conditions to its
report path, `fly` gates on those conditions, and `/recoveryz` surfaces recovery
health. The unseal or bootstrap actuator runs without operator interaction in the
steady state, with operator material reserved for breakglass.

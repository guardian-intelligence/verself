# Fly

`fly` runs Guardian boarding and then submits the configured Nomad job from the
boarded workspace.

```sh
guardian fly gamma.cue --dry-run -o yaml
guardian fly gamma.cue
```

The command loads the same resource graph used by `board`, writes the graph to
`.guardian/fly/document.json`, runs the boarding phase, and verifies the
boarded repo tree and kernel prerequisites. Re-running `fly` is the normal way
to refresh the boarded workspace before Nomad jobs run their owner-defined
lifecycle tasks.

## Config Shape

```yaml
entrypoint:
  apiVersion: guardian.guardianintelligence.org/v1alpha1
  kind: FlyProcedure
  name: gamma

resources:
  - apiVersion: guardian.guardianintelligence.org/v1alpha1
    kind: FlyProcedure
    metadata:
      name: gamma
    spec:
      substrateRef:
        apiVersion: substrate.guardianintelligence.org/v1alpha1
        kind: Substrate
        name: gamma-primary
      nomad:
        run:
          argv:
            - ssh
            - -T
            - ubuntu@206.223.228.87
            - nomad job run -detach /home/ubuntu/.local/state/guardian/repo/current/workspace/src/infrastructure-components/openbao/nomad.hcl

  - apiVersion: networking.guardianintelligence.org/v1alpha1
    kind: PublicOrigin
    metadata:
      name: product
    spec:
      url: https://gamma.verself.sh
```

The base graph describes shared inputs. Runtime execution details live in
component-owned Nomad job files. OpenBao defines how to install, restore,
unseal, and reconcile the root-of-trust store. Edge components define how
public origins become listener, certificate, and backend state.

The graph contains component CRDs. If a field describes an action such as
`restore`, `init`, `migrate`, `wait`, `submit`, `unseal`, or `import`, the
owning Nomad lifecycle task or owner-local binary implements that action and
reads static inputs from its CRD.

## Dry Run

`guardian fly --dry-run` runs non-mutating checks: resource validation and
upload bundle preparation. It does not run kernel hooks or submit Nomad jobs.

## Live Run

`guardian fly` performs boarding, then runs `FlyProcedure.spec.nomad.run`. In
this repo that hook uses the boarded Nomad binary to submit and monitor a
Nomad job. Owner job files use lifecycle tasks to install runtime artifacts,
restore or initialize state, reconcile configuration, and block loudly when
external authority is missing.

On a wiped node, boarding prepares OpenBao before Nomad starts. The
`openbao-recover prepare` hook installs the repo-built OpenBao runtime, writes
host-local config, and creates the CA file Nomad needs for Vault integration
without initializing or unsealing OpenBao. Nomad recovery then starts the
single-node agent once with Vault integration already configured. After that,
`fly` submits the OpenBao Nomad job and later Vault-backed jobs can validate
against the same stable Nomad agent.

OpenBao recovery reports concrete component blockers when it cannot continue.
Examples include missing Shamir unseal quorum, unavailable auto-unseal backing
key, missing PGP recipient identities for fresh initialization, and missing
provider parent credential during re-import.

Point-in-time recovery, snapshot restore, backup catalog selection, offsite
object-store reads, and provider token import are component concerns. They live
in owner-local binaries and Nomad lifecycle tasks.

## Nomad Job Convention

Each component job may define a prestart recovery task:

```hcl
task "recover" {
  lifecycle {
    hook    = "prestart"
    sidecar = false
  }

  config {
    command = "component-recover"
    args = ["recover"]
  }
}
```

The recovery task reads the boarded graph, selects its component CRD, installs
repo-built artifacts, reconciles static configuration, restores durable state
when configured, and reports conditions when external authority is missing.
Nomad handles retries and health-driven scheduling. A healthy component treats
the recovery task as a no-op.

## Repeatability

`guardian fly` is safely repeatable. Each run boards the substrate and refreshes
the graph available to component Nomad jobs. Components that are already healthy
perform no-op recovery. Components that are degraded attempt to repair or block
loudly with stable conditions.

The second consecutive successful `fly` run for the same config should produce
the same upload digest and no unexpected allocation churn after component Nomad
jobs are run. This is the primary steady-state regression signal.

## Component Progress

Component readiness for `fly` is measured in levels:

- the component has an owner-defined Nomad job with a recovery prestart task;
- the job starts on an empty boarded host;
- recovery detects absent, initialized/sealed, initialized/unsealed, and
  degraded states;
- prestart tasks bootstrap empty state;
- prestart tasks no-op on healthy existing state;
- prestart tasks repair or block loudly on degraded state;
- stateful components declare restore behavior from offsite backups;
- the component exposes recovery health through service checks or `/recoveryz`;
- a wipe drill passes;
- a second `fly` run is stable.

Site progress is the aggregate of component levels plus live convergence
evidence from component recovery reports, Nomad scheduler events, service health
checks, and runtime telemetry.

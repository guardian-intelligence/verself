# Fly

`fly` runs Guardian preflight and then submits the configured Nomad job from
the materialized workspace.

```sh
guardian fly gamma --dry-run -o yaml
guardian fly gamma
guardian fly -f gamma.cue
```

The command resolves a profile, writes the graph to
`.guardian/fly/document.json`, runs the preflight phase, and verifies the
materialized repo tree and kernel prerequisites. Re-running `fly` is the normal
way to refresh the materialized workspace before Nomad jobs run their
owner-defined lifecycle tasks.

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
            - nomad job run -detach /home/ubuntu/.local/state/guardian/repo/current/workspace/src/integrations/cloudflare/control-plane/nomad.hcl

  - apiVersion: networking.guardianintelligence.org/v1alpha1
    kind: PublicOrigin
    metadata:
      name: product
    spec:
      url: https://gamma.verself.sh
```

The base graph describes shared inputs. Runtime execution details live in
preflight for root services and in component-owned Nomad job files for
post-root services. OpenBao defines how to install, restore, unseal, and
reconcile the root-of-trust store during preflight. Edge components define how
public origins become listener, certificate, and backend state.

The graph contains component CRDs. If a field describes an action such as
`restore`, `init`, `migrate`, `wait`, `submit`, `unseal`, or `import`, the
owning Nomad lifecycle task or owner-local binary implements that action and
reads static inputs from its CRD.

## Dry Run

`guardian fly --dry-run` runs non-mutating checks: resource validation and
local preflight input validation. It does not run kernel hooks or submit Nomad
jobs.

## Live Run

`guardian fly` performs preflight, then runs `FlyProcedure.spec.nomad.run`. In
this repo that hook uses the materialized Nomad binary to submit and monitor a
Nomad job. Owner job files use lifecycle tasks to install runtime artifacts,
restore or initialize state, reconcile configuration, and block loudly when
external authority is missing.

On a wiped node, preflight prepares and starts OpenBao before Nomad starts. The
`openbao-recover prepare` hook installs the repo-built OpenBao runtime, writes
host-local config, and creates the CA file Nomad needs for Vault integration.
Preflight then starts OpenBao as a systemd root service, runs one bounded
OpenBao recovery pass, starts the single-node Nomad agent, and verifies the
Podman driver. After that, `fly` submits post-root Nomad jobs.

OpenBao recovery reports concrete component blockers when it cannot continue.
Examples include missing Shamir unseal quorum, unavailable auto-unseal backing
key, missing PGP recipient identities for fresh initialization, and missing
provider parent credential during re-import.

When a component has exhausted autonomous recovery sources and needs an
operator-held credential, it reports reason `RootTrustMaterialRequired`. For
Cloudflare, that means neither an account-admin credential nor bucket-scoped R2
recovery credentials are available in the deployed OpenBao state. Manual import
is allowed in that branch; the import material must enter through the
component-owned import path and must not be committed, logged, passed through
argv, or persisted as plaintext. The preferred import path decrypts the scoped
OpenBao token from `init-material.json` with an operator-held PGP key and reads
the Cloudflare account-admin token from an operator-only local file; stdin JSON
import is only for tightly controlled operator sessions.

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

The recovery task reads the materialized graph, selects its component CRD,
installs repo-built artifacts, reconciles static configuration, restores
durable state when configured, and reports conditions when external authority
is missing. Nomad handles retries and health-driven scheduling. A healthy
component treats the recovery task as a no-op.

## Repeatability

`guardian fly` is safely repeatable. Each run preflights the substrate and
refreshes the graph available to component Nomad jobs. Components that are
already healthy perform no-op recovery. Components that are degraded attempt to
repair or block loudly with stable conditions.

The second consecutive successful `fly` run for the same config should produce
the same upload digest and no unexpected allocation churn after component Nomad
jobs are run. This is the primary steady-state regression signal.

## Component Progress

Component readiness for `fly` is measured in levels:

- the component has an owner-defined Nomad job with a recovery prestart task;
- the job starts on an empty materialized host;
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

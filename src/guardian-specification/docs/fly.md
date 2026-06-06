# Fly

`fly` runs Guardian boarding and prepares the boarded workspace for
component-owned Nomad recovery.

```sh
guardian fly gamma.cue --dry-run -o yaml
guardian fly gamma.cue
```

The command loads the same resource graph used by `board`, writes the graph to
`.guardian/fly/document.json`, runs the boarding phase, and verifies the
boarded repo tree. Re-running `fly` is the normal way to refresh the boarded
workspace before Nomad jobs run their owner-defined recovery tasks.

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

## Dry Run

`guardian fly --dry-run` runs non-mutating checks: resource validation and
upload bundle preparation.

## Live Run

`guardian fly` performs boarding and leaves component recovery to Nomad. In
this repo, owner job files use prestart recovery tasks to install runtime
artifacts, restore or initialize state, reconcile configuration, and block
loudly when external authority is missing.

OpenBao recovery is expected to report `RootTrustMaterialAvailable=False` when it
cannot continue without operator-held or externally-held authority. Examples
include missing Shamir unseal quorum, unavailable auto-unseal backing key,
missing PGP recipient identities for fresh initialization, and missing provider
parent credential during re-import.

Point-in-time recovery, snapshot restore, backup catalog selection, offsite
object-store reads, and provider token import are component concerns. They live
in owner-local recovery binaries and Nomad lifecycle tasks.

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
- root trust gates report `RootTrustMaterialAvailable`;
- the component exposes recovery health through service checks or `/recoveryz`;
- a wipe drill passes;
- a second `fly` run is stable.

Site progress is the aggregate of component levels plus live convergence
evidence from component recovery reports, Nomad scheduler events, service health
checks, and runtime telemetry.

# Fly

`fly` wraps Nomad for disaster-recovery and deployment.

```sh
guardian fly gamma.cue --dry-run -o yaml
guardian fly gamma.cue
```

The command loads the same config document used by `board`, verifies boarding
readiness, resolves Nomad job files, plans or submits those jobs, and emits a
structured command result.

## Config Shape

```yaml
kind: FlyProcedure

nomad:
  address: http://127.0.0.1:4646
  namespace: default
  jobs:
    - path: src/infrastructure-components/openbao/nomad.hcl
      requiredFor: [recovery]
    - path: src/services/deployment-service/nomad.hcl
      requiredFor: [deploy]
```

Guardian does not model the full Nomad HCL schema. Nomad job files remain the
runtime contract for task groups, lifecycle hooks, update strategies, services,
volumes, and component-local recovery tasks.

## Dry Run

`guardian fly --dry-run` verifies boarding readiness, resolves job paths, and
emits the Nomad job set without submitting jobs.

## Live Run

`guardian fly` performs the dry-run checks, submits planned Nomad jobs, records
Nomad evaluation IDs, and emits conditions for submitted jobs.

Components expose recovery tasks in their Nomad job files. Guardian invokes
Nomad; component jobs perform component-specific bootstrap, restore, and
stabilization behavior.

Point-in-time recovery, snapshot restore, backup catalog selection, and
offsite object-store reads are component concerns. They belong in owner-local
Nomad prestart tasks and component recovery binaries, not in the Guardian
config document schema.

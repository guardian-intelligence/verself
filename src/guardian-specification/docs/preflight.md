# Preflight

`guardian preflight` resolves one Guardian CRD graph, writes the generated fly
document into the workspace, verifies required local build artifacts are
present, and runs the declared Ansible playbook.

```sh
guardian preflight gamma -o yaml
guardian preflight -f gamma.cue -o json
```

The Guardian CLI stays narrow. It parses CUE/YAML/JSON/TOML/TOON, derives an
ephemeral Ansible inventory from `Substrate.spec.remote.ssh`, feeds the CRD
graph to Ansible as private extra vars, and formats the command response. The
playbook owns the remote procedure.

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
      preflight:
        ansible:
          playbook: src/guardian-specification/ansible/preflight.yml
      nomad:
        run:
          argv: [ssh, -T, ubuntu@206.223.228.87, nomad, job, run, /path/to/job.hcl]

  - apiVersion: substrate.guardianintelligence.org/v1alpha1
    kind: Substrate
    metadata:
      name: gamma-primary
    spec:
      remote:
        repoRoot: /home/ubuntu/.local/state/guardian/repo/current
        guardian: /home/ubuntu/.local/state/guardian/repo/current/bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian
        ssh: [ssh, -T, -o, BatchMode=yes, ubuntu@206.223.228.87]
```

## Contract

The preflight playbook must fail loudly when the target is not ready for
`guardian fly`. For gamma, the playbook:

- first runs a fast health probe and exits immediately when the previously
  verified repo tree, OpenBao service, Nomad agent, and Podman driver are
  already healthy,
- uploads the workspace and required Bazel artifacts with the repo-pinned rsync
  when repair or refresh is needed,
- verifies workspace and artifact checksum deltas with rsync dry runs,
- runs `openbao-recover prepare` to materialize OpenBao host integration inputs,
- starts OpenBao as a systemd root service and runs one bounded
  recovery-or-verification pass,
- installs the pinned Nomad runtime artifact,
- starts the Nomad agent and waits for the Podman driver,
- validates the first post-root recovery Nomad jobs.

`ready_to_fly: yes` means the playbook completed successfully. It does not mean
component runtime convergence has completed. `guardian fly` performs preflight
again before submitting or monitoring Nomad work.

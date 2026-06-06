# Boarding

Boarding is the first stop point in the Guardian convergence state machine.

```sh
guardian board gamma.cue -o yaml
```

The command loads a resource graph, verifies local build artifacts are present,
runs the entrypoint's referenced `Substrate` access hook, runs the `Substrate`
upload hooks, verifies the repo on the target, prepares the fixed kernel
executor, and stops before fly submits a Nomad job.

Before upload hooks run, Guardian writes `.guardian/fly/document.json` in the
workspace. Upload hooks are expected to materialize that generated graph on the
target so Nomad tasks can consume static configuration from the boarded repo.

## Config Shape

```yaml
entrypoint:
  apiVersion: guardian.guardianintelligence.org/v1alpha1
  kind: FlyProcedure
  name: gamma

resources:
  - apiVersion: substrate.guardianintelligence.org/v1alpha1
    kind: Substrate
    metadata:
      name: gamma-primary
    spec:
      access:
        argv: [ssh, -T, ubuntu@206.223.228.87, true]
      upload:
        run:
          argv: [bazel-bin/src/guardian-specification/tools/rsync, -a, --delete, ./, ubuntu@206.223.228.87:/tmp/repo-next/workspace/]
        extract:
          argv: [ssh, -T, ubuntu@206.223.228.87, mv -Tf /tmp/repo-next /tmp/repo]
        verify:
          argv: [ssh, -T, ubuntu@206.223.228.87, cd /tmp/repo && find workspace bazel-bin -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum]
      kernel:
        openbaoPrepare:
          argv: [ssh, -T, ubuntu@206.223.228.87, sudo /tmp/repo/bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover prepare]
        nomad:
          argv: [ssh, -T, ubuntu@206.223.228.87, sudo /tmp/repo/bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover]
        verify:
          argv: [ssh, -T, ubuntu@206.223.228.87, nomad job validate /tmp/repo/workspace/src/infrastructure-components/postgresql/nomad.hcl]
```

Lifecycle hooks are self-contained commands. `Substrate.spec.access` proves the
target can be reached. Upload hooks can use SSH, WireGuard, rsync, AWS SSM, or
another operator-provided mechanism. The upload phase leaves the boarded
substrate with a materialized repo tree where component Nomad jobs can run
repo-bundled artifacts.

Kernel hooks recover the fixed host runtime prerequisites for this
implementation: OpenBao host integration inputs and the Nomad agent. The
sequence prepares OpenBao runtime and CA material, starts Nomad once with Vault
integration available, and verifies the resulting executor can validate
Vault-backed jobs. OpenBao initialization, unseal, baseline reconciliation, and
health reporting are handled by the OpenBao component's owner-local recovery
logic.

## Upload Contract

Lifecycle hooks run from the workspace root. Guardian does not inject upload
information through environment variables and does not package the repo. The
site graph chooses the upload primitive. For gamma, the primitive is rsync: use
the Bazel-pinned controller rsync at
`bazel-bin/src/guardian-specification/tools/rsync`, copy the workspace, copy the
explicit built artifacts needed by the current graph with symlinks dereferenced,
atomically promote the uploaded tree, then use `rsync --dry-run --checksum` to
prove the remote tree still matches the local tree. This first cut still
requires remote rsync to be available on the substrate; absence is a boarding
failure, not an implicit install step.

`board.upload.verify` must verify the boarded tree and print a sha256 digest.
JSON output with a `digest`, `observed_digest`, `upload_digest`, or `sha256`
field is accepted. Plain `sha256sum` output is also accepted.

## Command Result

`board` emits `ready_to_fly`, `resource_digest`, access status,
`upload.digest`, kernel hook status, and stable conditions. Command results
never contain secret values.

`ready_to_fly: yes` means the repo tree is present on the target, the upload
verify hook proved it matches the local tree, and Nomad can run component-owned
recovery jobs with OpenBao integration inputs already present. It does not mean
component runtime convergence has completed. `guardian fly` performs boarding
again before component convergence, so it does not depend on a stored report
from an earlier `guardian board` invocation.

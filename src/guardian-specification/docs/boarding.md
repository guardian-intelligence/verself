# Boarding

Boarding is the first stop point in the Guardian convergence state machine.

```sh
guardian board gamma.cue -o yaml
```

The command loads a resource graph, verifies required local build artifacts,
creates a deterministic upload bundle, runs the entrypoint's referenced
`Substrate` access hook, runs the `Substrate` upload hooks, extracts the repo
on the target, verifies the extracted tree, and stops before component
recovery/deployment.

Before packaging, Guardian writes `.guardian/fly/document.json` in the
workspace. The upload bundle includes that generated graph and excludes
`.guardian/board` artifacts, so Nomad tasks can consume static configuration
without recursively uploading prior bundles.

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
        bundlePath: .guardian/board/upload.tar.gz
        manifestPath: .guardian/board/upload-manifest.json
        digestPath: .guardian/board/upload.sha256
        run:
          argv: [rsync, .guardian/board/upload.tar.gz, ubuntu@206.223.228.87:/tmp/upload.tar.gz]
        extract:
          argv: [ssh, -T, ubuntu@206.223.228.87, tar -xzf /tmp/upload.tar.gz -C /tmp/repo]
        verify:
          argv: [ssh, -T, ubuntu@206.223.228.87, cd /tmp/repo && sha256sum -c guardian-upload-sha256sums.txt && sha256sum /tmp/upload.tar.gz]
```

Lifecycle hooks are self-contained commands. `Substrate.spec.access` proves the
target can be reached. Upload hooks can use SSH, WireGuard, Ansible, rsync, AWS
SSM, or another operator-provided mechanism. The upload phase only has to leave
the boarded substrate with a materialized repo tree where the next phase can run
repo-bundled recovery artifacts.

## Upload Paths

Guardian writes the local upload bundle, manifest, and digest to repo-relative
paths declared on `Substrate.spec.upload`. If omitted, the defaults are:

- `.guardian/board/upload.tar.gz`
- `.guardian/board/upload-manifest.json`
- `.guardian/board/upload.sha256`

Lifecycle hooks run from the workspace root and reference these paths directly.
Guardian does not inject upload information through environment variables.

`board.upload.verify` must verify the extracted tree and print the observed
upload digest. JSON output with a `digest`, `observed_digest`,
`upload_digest`, or `sha256` field is accepted. Plain `sha256sum` output is
also accepted.

## Command Result

`board` emits `ready_to_fly`, `resource_digest`, access status,
`upload.digest`, `upload.observed_digest`, hook status, and stable conditions.
Command results never contain secret values.

`ready_to_fly: yes` means the repo tree is present on the target and byte-for-
byte verified against the local upload bundle. It does not mean component
recovery has completed or root trust material is available. `guardian fly`
performs boarding again before component convergence, so it does not depend on a
stored report from an earlier `guardian board` invocation.

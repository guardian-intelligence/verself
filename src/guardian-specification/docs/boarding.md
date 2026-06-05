# Boarding

Boarding prepares the bytes required for `fly`.

```sh
guardian board gamma.cue -o yaml
```

The command loads a config document, verifies required local build artifacts,
creates a deterministic upload bundle, runs `board.access`, runs
`board.upload.run`, runs `board.upload.verify`, and compares the observed
digest with the local digest.

## Config Shape

```yaml
kind: FlyProcedure

staticConfig:
  baseURL: https://gamma.guardianintelligence.org
  credentialsRef: gamma-credentials

board:
  access:
    argv: [ssh, -T, ubuntu@206.223.228.87, true]
  upload:
    run:
      argv: [ansible-playbook, src/sites/gamma/board.yml]
    verify:
      argv: [ssh, -T, ubuntu@206.223.228.87, sha256sum /path/to/upload.tar.zst]
```

Lifecycle hooks are self-contained commands. `board.access` proves the target
can be reached. Upload hooks can use SSH, WireGuard, Ansible, rsync, AWS SSM,
or another operator-provided mechanism. Guardian does not compose connection
primitives with upload behavior.

## Hook Environment

Guardian sets these environment variables for each hook:

- `GUARDIAN_REPO_ROOT`
- `GUARDIAN_UPLOAD_BUNDLE`
- `GUARDIAN_UPLOAD_FORMAT`
- `GUARDIAN_EXPECTED_DIGEST`
- `GUARDIAN_UPLOAD_DIGEST`
- `GUARDIAN_UPLOAD_COMPRESSED_BYTES`
- `GUARDIAN_UPLOAD_UNCOMPRESSED_BYTES`

`board.upload.verify` must print the observed upload digest. JSON output with a
`digest`, `observed_digest`, `upload_digest`, or `sha256` field is accepted.
Plain `sha256sum` output is also accepted.

## Command Result

`board` emits `ready_to_fly`, `static_config_digest`, access status,
`upload.digest`, `upload.observed_digest`, hook status, and stable conditions.
Command results never contain secret values.

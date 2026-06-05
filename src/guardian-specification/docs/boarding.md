# Boarding

Boarding prepares the inputs required for `fly`.

```sh
guardian board gamma.cue -o yaml
```

The command loads a config document, checks SSH access configuration, computes
a deterministic seed manifest, verifies local seed sources, and prints a
structured command result.

## Config Shape

```yaml
kind: FlyProcedure

staticConfig:
  baseURL: https://gamma.guardianintelligence.org
  credentialsRef: gamma-credentials

board:
  substrate:
    stateDir: /var/lib/guardian
  access:
    ssh:
      host: 206.223.228.87
      port: 22
      user: ubuntu
      knownHostsFile: ~/.ssh/known_hosts
      wireguardFallback:
        host: 10.66.67.1
        port: 2222
        interface: wg-ops
  seed:
    targetRoot: /var/lib/guardian/seeds
    paths:
      - source: bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian
        target: bin/guardian
        mode: "0755"
```

`source` is resolved relative to `--repo-root` when it is not absolute.
`target` is relative to the seed root and must not escape it. Seed sources must
be regular files.

## Seed Identity

The seed digest is computed from static config, declared seed files, file
digests, target paths, file modes, and declared Nomad jobs. The remote seed root
is:

```text
<targetRoot>/sha256-<seedDigest>
```

## Command Result

`board` emits `ready_to_fly`, SSH access details, `static_config_digest`,
`seed.digest`, `seed.root`, per-source status, and stable conditions. Command
results never contain secret values.

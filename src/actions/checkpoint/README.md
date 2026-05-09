# Verself Checkpoint

Mounts a Verself Checkpoint volume during a GitHub Actions job and saves the mounted generation during the post step.

```yaml
- uses: guardian-intelligence/verself-checkpoint@v0
  with:
    key: npm-${{ runner.os }}-${{ hashFiles('package-lock.json') }}
    path: ~/.npm
    size-gb: '5'
```

## Inputs

| Input | Required | Default | Description |
| --- | --- | --- | --- |
| `key` | yes | | Stable checkpoint key evaluated by the workflow before the action runs. |
| `path` | yes | | Absolute path, `~` path, or path relative to `GITHUB_WORKSPACE`. |
| `size-gb` | no | `5` | Cold-start zvol size in GiB. Existing committed generations keep their source size. |

## Outputs

| Output | Description |
| --- | --- |
| `cache-hit` | `true` when an existing checkpoint generation was restored. |
| `mount-id` | Verself checkpoint mount identifier. |
| `generation` | Source generation restored into the mount. |

## Runner Contract

This action is intentionally small. It depends on the Verself runner injecting the host-service endpoint, attempt identity, and a short-lived checkpoint bearer token into the job environment. On a non-Verself runner the action fails fast because those environment variables and `vm-bridge` are unavailable.

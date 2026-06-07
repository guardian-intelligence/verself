# CLI

`guardian` resolves repo-owned tools and Guardian profiles, then runs the
convergence state machine to the requested stop point.

```sh
guardian run bazel -- test //src/guardian-specification/...
guardian preflight gamma -o json
guardian fly gamma --dry-run -o yaml
guardian fly run gamma -- nomad status
```

The stable command response is written to stdout. State transitions and
diagnostics are written to stderr, so machine callers can collect the final
response without parsing progress logs.

## Response Formats

`-o yaml` is the default. `json`, `toml`, and `toon` are supported for
automation and language-specific tooling. `--output` is the long form, and
`--format` is accepted as an alias.

JSON uses Go's standard `encoding/json` package. YAML uses `gopkg.in/yaml.v3`.
TOML uses `github.com/BurntSushi/toml`. TOON uses
`github.com/toon-format/toon-go`.

## Input Formats

CUE is the preferred authoring format. YAML, JSON, TOML, and TOON are accepted
as runtime input formats when they encode the same entrypoint and resource
graph.

Normal usage discovers `.config/guardian/guardian.cue` from the workspace root
and selects a named profile. `-f <config>` is the file override for explicit
config files or standalone Guardian documents.

## Tool Execution

`guardian run <tool> -- <args...>` resolves a repo-declared catalog tool,
verifies its admitted digest, materializes it in the Guardian cache, and
executes it without consulting `PATH`.

Tool inspection and shim installation stay under `run`:

```sh
guardian run --list
guardian run bazel --which
guardian run bazel --verify
guardian run --install-shims --bin-dir "$HOME/.local/bin"
```

## Profiles

`guardian profiles list` shows available profiles and the repo default.
`guardian profiles show [profile]` shows the resolved Guardian document.

Profiles are named contexts. There is no mutable local profile selection.

## Preflight

`preflight` runs through substrate readiness and stops. It resolves the profile
and referenced `Substrate`, verifies local build artifacts exist, runs the
access and upload lifecycle hooks, materializes the repo tree on the target,
prepares OpenBao integration inputs, and starts the Nomad executor. Preflight
writes `.guardian/fly/document.json` before upload hooks run so component-owned
Nomad jobs can read the graph from the materialized workspace.

`ready_to_fly: yes` means the access hook completed, the upload was extracted,
the verify hook proved the materialized tree and printed a sha256 digest, and
Nomad can run component-owned recovery jobs. Missing build artifacts, failed
hooks, missing verify digests, or kernel blockers produce `ready_to_fly: no`
with stable condition reasons.

## Fly

`fly` starts with the same preflight phase. `fly --dry-run` validates the graph
and verifies local preflight inputs without mutating the target.

Live `fly` prepares the target and runs the configured Nomad job hook.
Components own provider reconciliation, backup restore, health waiting, and
other runtime behavior through their job files and owner-local binaries.

`fly run <profile> -- <tool> <args...>` runs `fly` first, verifies the remote
Guardian and remote catalog tool, then streams the remote tool output.

## Observability

State transitions are emitted by default for `preflight`, `fly`, and `fly run`.
Use [Command Observability](command-observability.md) for verbosity, filters,
tee-friendly logs, and structured event formats.

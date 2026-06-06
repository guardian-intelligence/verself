# CLI

`guardian` reads one resource graph document and runs the Guardian convergence
state machine to the requested stop point.

```sh
guardian board src/guardian-specification/examples/gamma/gamma.cue -o json
guardian fly src/guardian-specification/examples/gamma/gamma.cue --dry-run -o yaml
```

The stable command response is written to stdout. Progress events are written
to stderr when `--stream` is set, so machine callers can collect the final
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

The CLI exposes only `board` and `fly`. Format conversion belongs in
development tooling, not in the runtime command surface.

## Boarding

`board` runs through substrate readiness and stops. It resolves the entrypoint
and referenced `Substrate`, verifies local build artifacts exist, runs the
access and upload lifecycle hooks, materializes the repo tree on the target,
prepares OpenBao integration inputs, and starts the Nomad executor. Boarding
writes `.guardian/fly/document.json` before upload hooks run so component-owned
Nomad jobs can read the graph from the boarded workspace.

`ready_to_fly: yes` means the access hook completed, the upload was extracted,
the verify hook proved the boarded tree and printed a sha256 digest, and Nomad
can run component-owned recovery jobs. Missing build artifacts, failed hooks,
missing verify digests, or kernel blockers produce `ready_to_fly: no` with
stable condition reasons.

## Fly

`fly` starts with the same boarding phase. `fly --dry-run` validates the graph
and verifies local boarding inputs without mutating the target.

Live `fly` boards the target and runs the configured Nomad job hook. Components
own provider reconciliation, backup restore, health waiting, and other runtime
behavior through their job files and owner-local binaries.

# CLI

`guardian` reads one config document and executes one command.

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
as runtime input formats when they encode the same config document fields.

The CLI exposes only `board` and `fly`. Format conversion belongs in
development tooling, not in the runtime command surface.

## Boarding

`board` checks the `board` section of the config document, computes the seed
manifest digest, and reports access and seed status.

`ready_to_fly: yes` means the loaded config document has enough local evidence
for `guardian fly --dry-run` to plan the Nomad jobs. Missing seed sources,
invalid remote seed targets, invalid modes, or invalid access settings produce
`ready_to_fly: no` with stable condition reasons.

## Fly

`fly --dry-run` evaluates the same boarding inputs, resolves the declared Nomad
job files, and reports the job set without submitting to Nomad.

Live `fly` submission is outside the first CLI tracer bullet. A live run must
perform the same checks before submitting jobs to Nomad and recording Nomad
evaluation IDs in the command result.

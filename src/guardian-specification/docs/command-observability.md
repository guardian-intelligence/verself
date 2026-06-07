# Command Observability

Guardian commands keep data and observation separate: command results and remote
tool output go to stdout, while state transitions and diagnostics go to stderr.
`preflight`, `fly`, and `fly run` emit state transitions by default.

## Usage

```sh
guardian preflight gamma
guardian fly gamma
guardian fly run gamma -- bazel test //...
guardian -v fly run -f src/guardian-specification/examples/gamma/gamma.cue -- bazel info release
guardian --quiet fly gamma
```

`-v`, `-vv`, and `-vvv` add command metadata, hook detail, and redacted line
output; `--quiet` prints only final errors.

## Tee and Files

```sh
guardian fly run gamma -- bazel test //... >bazel.out 2> >(tee .guardian/logs/gamma.flyrun.log >&2)
guardian --log-file .guardian/logs/gamma.flyrun.jsonl --event-format jsonl fly run gamma -- bazel test //...
```

`--log-file` mirrors the event stream to a file and keeps stderr active for the
operator terminal.

## Event Formats

```sh
guardian --event-format text fly gamma
guardian --event-format jsonl fly gamma 2>.guardian/logs/gamma.events.jsonl
guardian --event-format yaml fly gamma 2>.guardian/logs/gamma.events.yaml
guardian --event-format toml fly gamma 2>.guardian/logs/gamma.events.toml
guardian --event-format toon fly gamma 2>.guardian/logs/gamma.events.toon
```

`text` is for terminals. `jsonl`, `yaml`, `toml`, and `toon` encode the same
event schema as one record per event.

## Event Schema

```json
{"time":"2026-06-07T00:00:00Z","run_id":"01JY0000000000000000000000","command":"guardian fly run","profile":"gamma","level":"state","kind":"transition","phase":"preflight.upload","state":"RemoteTreeVerified","status":"ok","message":"remote tree verified","duration_ms":1183,"attrs":{"digest":"sha256:..."}}
```

Every event includes `time`, `run_id`, `command`, `level`, `kind`, `phase`,
`status`, and `message`; `attrs` carries stable details such as resource names,
tool names, digests, exit codes, and remote paths.

## Filters

```sh
guardian --log-level debug fly gamma
guardian --log-kind state,condition,hook fly gamma
guardian --log-status blocked,error fly gamma
guardian --log-phase preflight.upload,fly.nomad fly gamma
guardian --log-filter 'phase =~ "preflight.*" && status != "ok"' fly gamma
```

Filters select events before terminal rendering and file mirroring. The filter
expression uses event field names and never matches secret values.

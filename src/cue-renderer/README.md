# cue-renderer

A Go tool that compiles the CUE topology in this module into the
artefacts the rest of the platform consumes. Eventually replaces the Python
implementation at `src/platform/scripts/topology.py` one renderer at a time.

This is the renderer side of a deliberate split: the CUE *is* the program;
this tool is just how Go projects that program onto disk.

## Outputs (planned)

- `group_vars/all/generated/*.yml` — Ansible projections of the CUE facts.
- `etc/nftables.d/<component>.nft` — per-component firewall snippets driven
  by the topology edge graph.
- `etc/systemd/system/*.service` — per-process unit files driven by
  `topology_processes`.
- OTel pipeline spans into ClickHouse, correlated with each deploy's
  `verself.deploy_run_key`.

## Why Go (not Python)

The CUE Go SDK (`cuelang.org/go/cue`) gives typed access to the graph.
`cue exp gengotypes` produces `cue_types_*_gen.go` next to the schema, and
`cue.Value.Decode(&Loaded)` hydrates the whole graph in a single walk —
cheaper than the `cue export | json.loads` round-trip the Python tool does
today, and the resulting Go code reaches into the schema with field access
rather than `dict["foo"]["bar"]`. Single static binary, builds and ships
through Bazel like every other Go tool in the repo.

## Layout

    cmd/cue-renderer/      # CLI binary
    internal/load/         # cuelang.org/go SDK wrapper; one Decode per run
    internal/render/       # Renderer interface + WritableFS, one pkg per artefact
    internal/spans/        # OTel pipeline tracing — wraps every run

`internal/spans` lives outside `internal/render/` because tracing is
cross-cutting; renderers don't emit spans of their own, the pipeline does.

The Go types come from `cue exp gengotypes` over `schema/` in this module —
`internal/model/` does not exist on purpose. As the schema tightens, gengotypes produces real structs and
renderers get more typed access for free.

## Status

Scaffold. The framework is in place — load, spans, the Renderer interface,
and `WritableFS` with OS + in-memory implementations — but no renderers
ship with this commit. `topology.py` remains the source of truth for every
generated artefact in `group_vars/all/generated/` until specific renderers
land here and the corresponding Python branches are deleted.

The first renderer to land is the per-component nftables snippet
generator (Phase A from the Ansible cleanup plan): it's greenfield,
exercises the "one renderer produces N files" path, and has no parity
trap because there's no existing PyYAML output to byte-diff against.

## Usage

    bazel run //src/cue-renderer/cmd/cue-renderer -- list
    bazel run //src/cue-renderer/cmd/cue-renderer -- generate
    bazel run //src/cue-renderer/cmd/cue-renderer -- check
    bazel run //src/cue-renderer/cmd/cue-renderer -- render <name>

`generate` and `check` are no-ops while the renderer list is empty;
`list` returns nothing.

## Adding a renderer

1. Add the CUE shape to `src/cue-renderer/` (CUE is the source of
   truth — every fact a renderer needs must be a CUE field, not a Go
   constant).
2. If the renderer wants typed access to a field that's currently
   `map[string]any` from gengotypes, tighten that field's CUE
   definition (close the record, drop `...`, add `@go(...)` for naming)
   and rerun `cue exp gengotypes ./...` from `src/cue-renderer/`.
3. If the renderer wants a typed projection on `load.Loaded`, add the
   field there and the matching `Decode` call in `internal/load/`.
4. Implement `render.Renderer` in `internal/render/<artefact>/`.
5. Register it in `cmd/cue-renderer/main.go`'s `renderers()`.
6. Once the Go output is stable, delete the Python branch that produced
   it and remove the artefact from `topology.py`'s `ARTIFACTS`.

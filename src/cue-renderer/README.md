# cue-renderer

A Go tool that compiles the CUE topology in this module into the
artefacts the rest of the platform consumes.

This is the renderer side of a deliberate split: the CUE *is* the program;
this tool is just how Go projects that program onto disk.

## Outputs

- `src/platform/ansible/group_vars/all/generated/*.yml` — legacy Ansible
  projections for roles that have not been collapsed to final-file copies yet.
- `src/platform/ansible/share/rendered/etc/nftables.d/<component>.nft` —
  per-component firewall snippets driven by CUE `topology.nftables.rulesets`
  facts.
- `src/platform/ansible/share/rendered/etc/nftables.conf` and
  `src/platform/ansible/share/rendered/etc/systemd/system/verself-firewall.target`
  — host firewall final files copied directly by the nftables role.
- OTel pipeline spans into ClickHouse, correlated with each deploy's
  `verself.deploy_run_key`.

## Why Go (not Python)

The CUE Go SDK (`cuelang.org/go/cue`) gives typed access to the graph.
`cue exp gengotypes` produces `cue_types_*_gen.go` next to the schema, and
`cue.Value.Decode(&Loaded)` hydrates the whole graph in a single walk, and
the resulting Go code reaches into the schema with field access rather than
`dict["foo"]["bar"]`. Single static binary, builds and ships through Bazel
like every other Go tool in the repo.

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

## Usage

    bazel run //src/cue-renderer/cmd/cue-renderer -- list
    bazel run //src/cue-renderer/cmd/cue-renderer -- generate
    bazel run //src/cue-renderer/cmd/cue-renderer -- check
    bazel run //src/cue-renderer/cmd/cue-renderer -- render <name>

`generate` writes every registered renderer to disk; `check` fails when any
registered renderer's output differs from the checked-in file. The operator
entry points are `aspect codegen run --kind=topology` and
`aspect codegen check --kind=topology`, backed by `write_source_files`
freshness targets.

## Adding a renderer

1. Add the CUE shape to `src/cue-renderer/` (CUE is the source of
   truth — every fact a renderer needs must be a CUE field, not a Go
   constant).
2. If the renderer wants typed access to a field that's currently
   `map[string]any` from gengotypes, tighten that field's CUE
   definition (close the record, drop `...`, add `@go(...)` for naming)
   and rerun `cue exp gengotypes ./...` from `src/cue-renderer/`.
   Service-specific firewall policy belongs in CUE; Go renderers transform
   typed policy facts into syntax and must not own allowlists.
3. If the renderer wants a typed projection on `load.Loaded`, add the
   field there and the matching `Decode` call in `internal/load/`.
4. Implement `render.Renderer` in `internal/render/<artefact>/`.
5. Register it in `cmd/cue-renderer/main.go`'s `renderers()`.
6. Add the renderer's generated path to `src/cue-renderer/BUILD.bazel` so
   `aspect codegen check --kind=topology` catches drift. The nftables ruleset list is the
   exception: adding a `topology.nftables.rulesets.<name>` entry in CUE
   regenerates `nftables_files.bzl` (a fan-out renderer) and the
   per-component genrule + write_source_files mapping pick up the new
   entry on next build, no BUILD.bazel edit required.

## Boundaries that intentionally stay outside CUE

- `roles/firecracker/templates/firecracker-network.nft.j2` — mixed input:
  guest CIDR comes from CUE (`config.firecracker.guest_pool_cidr`), but
  the uplink interface is host-discovered at apply time. The Jinja
  template is the canonical "this stays as Ansible" example.

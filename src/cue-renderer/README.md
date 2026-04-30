# cue-renderer

A Go tool that compiles the CUE topology in this module into the
artefacts the rest of the platform consumes.

This is the renderer side of a deliberate split: CUE holds declarative
platform facts; Go and shell code project those facts onto deployable files.

## Outputs

The renderer's `generate` subcommand requires `--output-dir` and writes
every artefact under that root. `aspect render --site=<site>` calls it
with `--output-dir=.cache/render/<site>/`, producing:

- `inventory/group_vars/all/generated/*.yml` — Ansible group_vars
  projections (catalog, ops, postgres, spire, endpoints, routes, …).
- `inventory/group_vars/all/main.yml`, `secrets.sops.yml`, `spire.yml`,
  `inventory/hosts.ini` — staged from the authored `src/platform/ansible/`
  tree by the render task so Ansible's host_group_vars + community.sops
  plugins find both the generated and authored vars from one
  `inventory_dir`.
- `share/rendered/etc/nftables.d/<component>.nft` — per-component firewall
  snippets driven by CUE `topology.nftables.rulesets`.
- `share/rendered/etc/nftables.conf`, `share/rendered/etc/systemd/system/verself-firewall.target`
  — host firewall final files copied directly by the nftables role.
- `jobs/<component>.nomad.json`, `jobs/index.json` — Nomad job specs and
  the controller-side submit/build enumeration for application components.

Bazel-input artefacts (`binaries/{server,dev,substrate}_tools.MODULE.bazel`,
`binaries/{server,dev,substrate}_tools.bzl`, `vm-orchestrator/guest-images/guest_images.MODULE.bazel`)
are still tracked in git via `write_source_files` because Bazel evaluates them at load time.

The renderer also emits OTel pipeline spans (`cue_renderer.run`,
`topology.cue.export_*`, `topology.generated.render_artifact`) into
ClickHouse, correlated with each deploy's `verself.deploy_run_key`.

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

    aspect render --site=prod                                 # canonical
    bazel run //src/cue-renderer/cmd/cue-renderer -- list
    bazel run //src/cue-renderer/cmd/cue-renderer -- generate --output-dir=<path>
    bazel run //src/cue-renderer/cmd/cue-renderer -- render <name>

`generate` materialises every registered renderer under `--output-dir`;
the operator entry point is `aspect render --site=<site>` which sets it
to `.cache/render/<site>/` and stages the inventory + hand-written
group_vars alongside. `bazelisk run //src/cue-renderer:dev_update` refreshes
the Bazel-input artefacts via `write_source_files`; `bazelisk test
//src/cue-renderer:dev_check` fails on drift.

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
6. If the renderer's output is a Bazel input (a `*.MODULE.bazel`,
   `*.bzl`, or other file Bazel evaluates at load time), add a genrule
   in `src/cue-renderer/BUILD.bazel` and register it in `_RENDERED_FILES`
   under `write_source_files(name = "freshness")`. Per-component
   nftables fragments and Nomad job specs are not tracked through
   write_source_files; they flow through `aspect render --site=<site>`
   into `.cache/render/<site>/` along with the rest of the deploy cache.

## Boundaries that intentionally stay outside CUE

- `roles/firecracker/templates/firecracker-network.nft.j2` — mixed input:
  guest CIDR comes from CUE (`config.firecracker.guest_pool_cidr`), but
  the uplink interface is host-discovered at apply time. The Jinja
  template is the canonical "this stays as Ansible" example.

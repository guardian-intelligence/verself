# cue-renderer — agent notes

The renderer is a pure projection of CUE onto disk. CUE owns truth; this
tool emits files. If you find yourself adding a new `case item.Name == "..."`
branch or hardcoding an install path, stop and put the fact in CUE first.

## Tier model for catalog binaries

Every binary the platform provisions belongs to one of these tiers. The
tier names are CUE values (`#DevToolTier`, `#ServerToolTier` later) and
control which delivery channel ships the binary.

| Tier                  | Delivery                                                                                              | Pin granularity        |
|-----------------------|-------------------------------------------------------------------------------------------------------|------------------------|
| `pinned_http_file`    | Bazel `http_file` rule → packed into a `*_tools.tar.zst` → unpacked by Ansible at `/`                | url + sha256           |
| `source_built_go`     | Bazel `go_binary` from vendored `go_deps` → packed into the same tarball as `pinned_http_file`        | go.sum (per-module)    |
| `lockfile_uv`         | `uv sync --frozen` against a committed `uv.lock` per tool project, then symlink the entrypoint        | per-wheel sha256       |
| `system_apt`          | `apt-get install` of an unpinned upstream package; the only tier without content pinning              | version label only     |
| `bootstrap_pivot`     | Direct download from `scripts/bootstrap` before Bazel can run; the chicken-and-egg exception          | url + sha256 (in shell)|
| `legacy_install_plan` | The pre-Bazel install pipeline driven by `catalog.go`'s switch + `roles/dev_tools/tasks/main.yml`     | varies per strategy    |

`legacy_install_plan` is the migration sentinel: every entry currently
on it is on the path to one of the other tiers. The end state has no
`legacy_install_plan` entries, the `catalog.go` install-plan loops are
deleted, and `roles/dev_tools/` reduces to (a) one `bazel build` + untar
for the dev tools tarball, (b) one `uv sync` loop, (c) one `apt` task.

## Tier inventory (current state)

`bootstrap_pivot` (2):
- bazelisk — fetched and verified by `scripts/bootstrap`; the only manual
  download. Bazelisk in turn fetches Bazel itself, governed by the pinned
  `bazel` version in `versions.cue`.
- bazel — version pin only; bazelisk reads `.bazelversion` and downloads.

`pinned_http_file` (16): go, zig, tofu, protoc, cue, buf, buildifier,
shellcheck, jq, sops, age, uv, clickhouse, osv-scanner, stripe,
agent-browser. These flow through `dev_tools.tar.zst`.

`legacy_install_plan` (11): ansible (+ 4 OTel companions + ansible-lint),
pre-commit, golangci-lint, gosec, gofumpt, sqlc, protoc-gen-go,
protoc-gen-go-grpc, guarddog. To be migrated to `lockfile_uv` (Python)
or `source_built_go` (Go-installable) in later phases.

`system_apt` (3): build-essential, crun, debootstrap. Will be moved out
of `devTools` into a separate `systemPackages` block with an explicit
risk acknowledgement that they are unpinned.

## Canonical pattern: CUE → http_file → pkg_tar → Ansible-untar

Worked example for server tools — the dev-tools side mirrors this
exactly. Read these files together:

1. `catalog/versions.cue` declares the truth: `serverToolDownloads` (one
   entry per binary with name/url/sha256/downloaded_file_path) and
   `serverToolPackaging` (the layout: tar_single, zip_single, raw,
   archive_dir, symlinks).
2. `internal/render/bazelmodule/bazelmodule.go` reads `serverToolDownloads`
   and emits `binaries/server_tools.MODULE.bazel` (one `http_file()` per
   entry).
3. `internal/render/bazelservertools/bazelservertools.go` reads
   `serverToolPackaging` and emits `binaries/server_tools.bzl` (data
   constants plus a Starlark `server_tools_archive()` macro).
4. Root `MODULE.bazel` includes `server_tools.MODULE.bazel`. Root
   `binaries/BUILD.bazel` calls `server_tools_archive()`. Bazel
   produces `:server_tools.tar.zst`.
5. `roles/deploy_profile/tasks/static_binaries.yml` runs
   `bazelisk build`, cqueries the output path, stats the sha256,
   uploads, untars to `/`, and emits a `server_tools.artifact.publish`
   smoke span carrying `bazel_label`, `version`, `archive_sha256`.

The dev-tools mirror uses `devToolDownloads` + `devToolPackaging`,
emits `dev_tools.MODULE.bazel` + `dev_tools.bzl`, and produces
`:dev_tools.tar.zst`. The Ansible bridge for dev tools is the next
PR; this PR ends after step 4.

## Renderer integration recipe

Adding any new generated file:

1. New CUE values in `catalog/versions.cue` or `instances/local/*.cue`.
   If a renderer needs typed access, tighten the schema in
   `schema/schema.cue` and rerun `cue exp gengotypes ./...` from
   `src/cue-renderer/`.
2. New Renderer in `internal/render/<name>/`. Implement `Render`,
   register the package in `cmd/cue-renderer/main.go`'s `renderers()`.
3. Extend `internal/load/load.go` (`Catalog` struct + `decodeCatalog`)
   only if the renderer wants typed access to a new top-level catalog
   field. Map-typed access via `loaded.Catalog.Raw` is fine for one-off
   reads.
4. Add a `genrule(...)` to `src/cue-renderer/BUILD.bazel` invoking
   `cue-renderer render <name>` and add the output to `_RENDERED_FILES`.
   `make topology-check` then catches drift.
5. If the rendered file is a Bazel input (`*.MODULE.bazel`, `*.bzl`),
   add an `include()` line to root `MODULE.bazel` or a `load()` to the
   consuming `BUILD.bazel`.

The nftables ruleset list is the one fan-out exception:
`bazel_nftables` emits `nftables_files.bzl` containing one tuple per
ruleset, and `BUILD.bazel` comprehends over that list to generate one
genrule per ruleset. Adding a ruleset to topology.cue regenerates the
manifest and the per-ruleset genrules appear automatically.

## Boundaries that intentionally stay outside CUE

- `roles/firecracker/templates/firecracker-network.nft.j2` — the
  uplink interface is host-discovered at apply time. Mixed input,
  Jinja stays.
- `scripts/bootstrap` — the bazelisk pin must match
  `versions.development.bazelisk` in `versions.cue` by hand. The
  bootstrap script cannot read CUE because CUE evaluation requires
  Bazel which requires bazelisk. When the version bumps, both move
  together; an integration check (Phase 4-ish) will assert equality.

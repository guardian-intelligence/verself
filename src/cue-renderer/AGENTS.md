# cue-renderer — agent notes

The renderer is a pure projection of CUE onto disk. CUE owns truth; this
tool emits files. If you find yourself adding a new `case item.Name == "..."`
branch or hardcoding an install path, stop and put the fact in CUE first.

## Tier model for catalog binaries

Every binary the platform provisions belongs to one of these tiers. The
tier names are CUE values (`#DevToolTier`, `#ServerToolTier` later) and
control which delivery channel ships the binary.

| Tier               | Delivery                                                                                       | Pin granularity         |
|--------------------|------------------------------------------------------------------------------------------------|-------------------------|
| `pinned_http_file` | Bazel `http_file` rule → packed into a `*_tools.tar.zst` → unpacked by Ansible at `/`          | url + sha256            |
| `source_built_go`  | Bazel `go_binary` from vendored `go_deps` → packed into the same tarball as `pinned_http_file` | go.sum (per-module)     |
| `lockfile_uv`      | `uv sync --frozen` against a committed `uv.lock` per tool project, then symlink the entrypoint | per-wheel sha256        |
| `bootstrap_pivot`  | Direct download from `scripts/bootstrap` before Bazel can run; the chicken-and-egg exception   | url + sha256 (in shell) |

`systemPackages` is a separate top-level CUE block (not a devTools tier)
for apt-managed packages. Each entry must declare `risk_acknowledgement`
containing the substring "upstream" so the schema rejects empty or
boilerplate strings; this is the only delivery channel intentionally
without content pinning.

The `legacy_install_plan` tier no longer exists; every dev tool now
lands via Bazel, uv lockfile, or `scripts/bootstrap`. New tools must
fit one of those tiers — adding a fifth one needs a renderer change,
not a CUE-only addition.

## Tier inventory (current state)

`bootstrap_pivot` (2):
- bazelisk — fetched and verified by `scripts/bootstrap`; the only manual
  download. Bazelisk in turn fetches Bazel itself, governed by the pinned
  `bazel` version in `versions.cue`.
- bazel — version pin only; bazelisk reads `.bazelversion` and downloads.

`pinned_http_file` (16): go, zig, tofu, protoc, cue, buf, buildifier,
shellcheck, jq, sops, age, uv, clickhouse, osv-scanner, stripe,
agent-browser. These flow through `dev_tools.tar.zst` via Bazel
`http_file` rules.

`source_built_go` (6): golangci-lint, gosec, gofumpt, protoc-gen-go,
protoc-gen-go-grpc, sqlc. Compiled from vendored sources via rules_go's
`go_binary`, packed into the same `dev_tools.tar.zst`. Pinned via
`go.sum` (per-module). Adding a new entry: add a `tool` and `require`
directive to `src/devtools/go.mod`, run `go mod tidy` + `bazelisk mod
tidy`, verify the auto-generated `@<repo>//cmd/<tool>:<tool>` label
resolves, then add a `sourceBuiltGoTools` entry and a tier=source_built_go
devTools entry. Runtime `--version` reporting is unreliable for some
binaries (no upstream -ldflags stamp); we don't gate deploys on it.

sqlc carried two upstream packaging quirks worth knowing about for
future updates: (1) `github.com/pingcap/tidb/pkg/parser` is a Go
submodule whose on-disk layout doesn't match its import path; resolved
via per-subpackage `gazelle:resolve` directives in MODULE.bazel.
(2) `github.com/pganalyze/pg_query_go/v6` vendors PostgreSQL's parser
as ~80 .c files plus 400+ headers under `parser/include/`; gazelle's
auto-generated BUILD missed the headers and concatenated `#cgo CFLAGS`
into one mangled list entry. Resolved by disabling BUILD generation
for that module and shipping a hand-written BUILD via
`bazel/patches/pg-query-go-bazel-cgo.patch`.

`lockfile_uv` (4): ansible, ansible-lint, pre-commit, guarddog. Each
maps via `project:` to a top-level `lockfileUvTools` entry, which holds
the project_dir, version pin, and full set of entrypoints to symlink
into /usr/local/bin/. The OpenTelemetry companions are gone — they're
transitive deps of ansible-core's pyproject, not separate tools.

`systemPackages` (3): build-essential, crun, debootstrap. Apt-managed,
intentionally not content-pinned; each entry carries a
`risk_acknowledgement` string explaining why upstream archive integrity
is acceptable for that package.

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
`:dev_tools.tar.zst`. `devToolsArchive: { bazel_label, version }`
exposes the consumer surface (parallel to `serverTools`); Ansible's
`roles/dev_tools/tasks/dev_tools_archive.yml` reads those two fields
to request the artefact, marker-gates the re-unpack, and emits
`dev_tools.artifact.publish` + per-tool `dev_tool.version_check`
spans (one per tool tagged `tier: pinned_http_file`).

Adding a new pinned_http_file tool is now exactly:
1. Add a `versions.development.<name>` pin.
2. Add a `devTools.<name>: { tier: "pinned_http_file", version, sha256, url, install_path, version_cmd }` entry.
3. Add a matching `devToolDownloads.<name>` entry referencing the
   above (sha/url come from `devTools.<name>` to keep one source of
   truth).
4. Add the layout row in the appropriate `devToolPackaging` list
   (`raw` for prebuilt single binaries, `tar_single` for one binary
   from an archive, etc.).
5. `aspect codegen run --kind=topology`.

`MODULE.bazel`, `dev_tools.bzl`, and `catalog.yml` regenerate; the
`TestDevToolPinnedHTTPFileTriangle` invariant test keeps the three
blocks coherent. No Go edits, no Ansible edits.

A new packaging *shape* (e.g. dev tools want a `.deb` member install
the way server tools do) is the rare case where the renderer extends
— wire a new field through `bazeldevtools.go` and add a Starlark
helper.

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
   `aspect codegen check --kind=topology` then catches drift.
5. If the rendered file is a Bazel input (`*.MODULE.bazel`, `*.bzl`),
   add an `include()` line to root `MODULE.bazel` or a `load()` to the
   consuming `BUILD.bazel`.

The nftables ruleset list is the one fan-out exception:
`bazel_nftables` emits `nftables_files.bzl` containing one tuple per
ruleset, and `BUILD.bazel` comprehends over that list to generate one
genrule per ruleset. Adding a ruleset to topology.cue regenerates the
manifest and the per-ruleset genrules appear automatically.

## Verification: Ansible trusts the render, canaries verify out-of-band

Ansible roles do not emit ClickHouse spans, do not re-validate the shape
of generated topology, and do not re-introspect what Bazel/uv just laid
down. That pattern duplicated work CUE schemas and module-level pins
already enforce, drifted span-attribute schemas across emitters of the
same span name, and coupled deploy progress to the observability stack.

Two distinct surfaces, one role each:

- **Ansible** owns idempotent host mutation. It trusts the rendered
  topology and the Bazel artefact, runs the apt/copy/unarchive/symlink
  steps, and exits non-zero on real OS failures (apt failure, file
  copy permission, etc.). It does not emit observability spans and does
  not re-assert facts the renderer or Bazel already enforce.
- **e2e canaries** under `src/platform/scripts/verify-*-live.sh` own
  continuous verification. Each canary opens an SSH tunnel to OTLP,
  emits a smoke span via `go run //src/otel/cmd/smoke-span`, then polls
  `default.otel_traces` for the row. They run from CI and on a loop;
  see `verify-bazel-live.sh` as the reference shape.

When adding a new role or extending one: write `assert` tasks for
in-band gates, and (if continuous external verification is meaningful)
add or extend a `verify-<area>-live.sh` canary. Do not include
`//src/otel/cmd/smoke-span` invocations in role tasks.

## Boundaries that intentionally stay outside CUE

- `roles/firecracker/templates/firecracker-network.nft.j2` — the
  uplink interface is host-discovered at apply time. Mixed input,
  Jinja stays.
- `scripts/bootstrap` — the bazelisk and aspect-cli pins must match
  `versions.development.bazelisk` and `versions.development.aspectCLI` in
  `versions.cue` by hand. The bootstrap script cannot read CUE because CUE
  evaluation requires Bazel which requires bazelisk. When the version bumps,
  both move together; `aspect doctor` reads the rendered catalog yaml and
  asserts both versions match the on-disk binaries.

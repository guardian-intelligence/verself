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
| `bootstrap_pivot`     | Direct download from `scripts/bootstrap` before Bazel can run; the chicken-and-egg exception          | url + sha256 (in shell)|
| `legacy_install_plan` | The pre-Bazel install pipeline driven by `catalog.go`'s switch + `roles/dev_tools/tasks/main.yml`     | varies per strategy    |

`systemPackages` is a separate top-level CUE block (not a devTools tier)
for apt-managed packages. Each entry must declare `risk_acknowledgement`
containing the substring "upstream" so the schema rejects empty or
boilerplate strings; this is the only delivery channel intentionally
without content pinning.

`legacy_install_plan` is the migration sentinel: every entry currently
on it is on the path to one of the other tiers. After the dev-tools
bridge landed, `roles/dev_tools/` is already (a) one `bazel build` +
untar for `dev_tools.tar.zst`, (b) one `apt` task, (c) one `go install`
loop, (d) one `uv tool install` loop. The end state has no
`legacy_install_plan` entries: the `go install` loop becomes a
`source_built_go` Bazel rebuild and the `uv tool install` loop becomes
a `lockfile_uv` `uv sync --frozen` against a committed `uv.lock`.

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

`source_built_go` (5): golangci-lint, gosec, gofumpt, protoc-gen-go,
protoc-gen-go-grpc. Compiled from vendored sources via rules_go's
`go_binary`, packed into the same `dev_tools.tar.zst`. Pinned via
`go.sum` (per-module). Adding a new entry: add a `tool` and `require`
directive to `src/devtools/go.mod`, run `go mod tidy` + `bazelisk mod
tidy`, verify the auto-generated `@<repo>//cmd/<tool>:<tool>` label
resolves, then add a `sourceBuiltGoTools` entry and a tier=source_built_go
devTools entry. Runtime `--version` reporting is unreliable (no
upstream -ldflags stamp); the version_check span only confirms the
binary is reachable on PATH.

`lockfile_uv` (4): ansible, ansible-lint, pre-commit, guarddog. Each
maps via `project:` to a top-level `lockfileUvTools` entry, which holds
the project_dir, version pin, and full set of entrypoints to symlink
into /usr/local/bin/. The OpenTelemetry companions are gone — they're
transitive deps of ansible-core's pyproject, not separate tools.

`legacy_install_plan` (1): sqlc. Stays here until a gazelle override
resolves the `github.com/pingcap/tidb/pkg/parser` Go-submodule layout
mismatch — see the `tool` directive comment in `src/devtools/go.mod`.

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
5. `make topology-generate`.

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
   `make topology-check` then catches drift.
5. If the rendered file is a Bazel input (`*.MODULE.bazel`, `*.bzl`),
   add an `include()` line to root `MODULE.bazel` or a `load()` to the
   consuming `BUILD.bazel`.

The nftables ruleset list is the one fan-out exception:
`bazel_nftables` emits `nftables_files.bzl` containing one tuple per
ruleset, and `BUILD.bazel` comprehends over that list to generate one
genrule per ruleset. Adding a ruleset to topology.cue regenerates the
manifest and the per-ruleset genrules appear automatically.

## Open migration follow-ups

Caddy (Phase 2b, deferred). The plan moves Caddy from xcaddy
(controller-side `go build` over network-fetched modules) to a
rules_go `go_binary` packed into `server_tools.tar.zst`. The Caddy
main is straightforward — `caddy/v2/cmd` + `caddy/v2/modules/standard`
+ `corazawaf/coraza-caddy/v2`. The blocker is a transitive
proto-version mismatch in `dgraph-io/badger/v2` (pulled via
`smallstep/certificates`/`smallstep/nosql` from `caddyserver/certmagic`):
the old `XXX_unrecognized` field is referenced in `table/table.go` but
not generated by the new `protoc-gen-go` runtime that rules_go links.
Resolving it requires either a `go_deps.module_override` patch against
badger v2 to drop the `XXX_unrecognized` references, or a certmagic
upgrade that drops the badger backend. Until then xcaddy stays in
`roles/deploy_profile/tasks/build_caddy.yml` and the `xcaddy` +
`corazaCaddy` version pins remain in `versions.production`.

sqlc (Phase 2a tail, deferred). Stays on `legacy_install_plan` until
a `gazelle_override` resolves the `github.com/pingcap/tidb/pkg/parser`
Go-submodule layout — see the comment at the top of
`src/devtools/go.mod`.

## Verification: Ansible asserts in-band, ClickHouse spans out-of-band

Ansible roles do not emit ClickHouse spans for verification. The pattern
that briefly existed (smoke-span runs threaded into `dev_tools_archive.yml`
and friends) is being removed: it duplicated the role's own `assert`
tasks, drifted span-attribute schemas across emitters of the same span
name, and coupled deploy progress to the observability stack.

Two distinct surfaces, one role each:

- **Ansible** owns idempotent host mutation with loud failures.
  `ansible.builtin.assert` is the gate inside a play. If a tool's
  version doesn't match the catalog pin, the play fails right there.
  No span, no ClickHouse query, no out-of-band wait.
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
- `scripts/bootstrap` — the bazelisk pin must match
  `versions.development.bazelisk` in `versions.cue` by hand. The
  bootstrap script cannot read CUE because CUE evaluation requires
  Bazel which requires bazelisk. When the version bumps, both move
  together; an integration check (Phase 4-ish) will assert equality.

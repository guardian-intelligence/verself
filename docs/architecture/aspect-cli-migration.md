# Make -> Aspect CLI cutover

Replace the root `Makefile` as the canonical founder/agent task runner with
[Aspect CLI](https://github.com/aspect-build/aspect-cli). This is not a
one-for-one port of every Make target. Durable workflows move to a small Aspect
surface; temporary smoke-test scaffolding and convenience aliases are deleted or
left as directly callable scripts until the structured canary work lands.

Bazelisk + Bazel keep owning the build graph. Aspect is the typed task layer for
repo workflows that need orchestration, flags, help text, or Bazel graph access
from AXL.

Single cutover: the `Makefile` is deleted in the same PR that adds `.aspect/`,
updates docs, and removes or updates any retained CI/workflow callers. The
cutover gate is `rg '\bmake\b' .github README.md AGENTS.md .claude docs src/platform/ansible`
with intentional prose-only exceptions reviewed manually.

## Sources

- Aspect install docs: `aspect` can be installed as a launcher that runs the
  repo-configured CLI version, or as a manually installed release binary.
  <https://docs.aspect.build/cli/install>
- Aspect task docs: tasks live in `.aspect/*.axl`; `MODULE.aspect` declares AXL
  extension dependencies; AXL tasks can call `ctx.bazel.build(...)` and consume
  build events. <https://docs.aspect.build/cli/usage/basic>
- Aspect module docs: external AXL dependencies are declared in `MODULE.aspect`.
  <https://docs.aspect.build/cli/usage/module>
- Bazel build docs: `build`, `test`, and `query` consume target patterns, and
  Bazel is the layer that owns incremental graph rebuilds and build outputs.
  <https://bazel.build/docs/build>
- Local sources of truth:
  - `Makefile`
  - `scripts/bootstrap`
  - `src/cue-renderer/catalog/versions.cue`
  - `src/cue-renderer/instances/local/{topology,config}.cue`
  - `docs/architecture/directory-structure.md`

## Decision

Use Aspect CLI, not `just`, `mise`, or a forest of `bazel run :sh_binary`
targets.

| | Aspect CLI | `just` | `mise` | bare `bazel run :sh_binary` |
|---|---|---|---|---|
| Bazel graph access | First-class AXL `ctx.bazel.*` APIs | Shell out | Shell out | You are already inside one target |
| Typed flags/help | Native task args and generated help | Mostly positional | Env-oriented | `--` passthrough |
| Repo-local tasks | `.aspect/*.axl` + `MODULE.aspect` | `justfile` | `mise.toml` tasks | BUILD targets only |
| Fit for deploy/debug workflows | Good | Good shell runner, no graph model | Tool/env manager first | Poor ergonomics for multi-step ops |
| Maturity | Early Preview in Apr 2026 | Stable | Stable | Stable |

Aspect is still preview-quality. That is acceptable because the AXL surface is
kept thin and local: typed command routing, argument validation, calls into
Bazel, and calls into existing Ansible/platform scripts. Durable artifact
generation should move toward Bazel targets/tests over time instead of growing
large AXL programs.

## Toolchain Pin

Aspect is declared in CUE as part of the development toolchain, but it is a
bootstrap pivot like Bazelisk, not an ordinary tool installed only by
`aspect platform setup-dev`.

Add a development version:

```cue
versions: development: {
    aspectCLI: "2026.17.17"
}
```

Add a `devTools.aspect` entry next to `devTools.bazelisk`:

```cue
aspect: {
    // scripts/bootstrap installs Aspect before Aspect can run. This is the
    // version-of-record for that bootstrap pin and is intentionally excluded
    // from dev_tools.tar.zst packaging.
    tier:         #DevToolTier & "bootstrap_pivot"
    version:      versions.development.aspectCLI
    strategy:     "binary"
    url:          "https://github.com/aspect-build/aspect-cli/releases/download/v\(version)/aspect-cli-x86_64-unknown-linux-musl"
    sha256:       "<pinned sha256>"
    install_path: "/usr/local/bin/aspect"
    version_cmd:  "aspect --version"
}
```

`scripts/bootstrap` installs both Bazelisk and Aspect from mirrored constants.
The CUE catalog remains the version-of-record; `aspect doctor` checks that:

- `/usr/local/bin/aspect` reports the CUE-pinned Aspect version.
- Bazelisk reports the CUE-pinned Bazelisk version.
- `.bazelversion` matches `versions.development.bazel`.
- `MODULE.aspect` loads successfully.

The `dev_tools` Ansible role may repair or verify Aspect on already-provisioned
controllers, but it must not be the only path that installs the `aspect` command.
That would create a bootstrap cycle.

## Command Surface

The durable surface is intentionally small. Do not recreate every Make target.

Top-level commands:

```text
aspect doctor
aspect tidy
aspect deploy [--tags=...]
aspect observe [flags...]
```

Grouped commands:

```text
aspect platform setup-dev|setup-sops|provision|deprovision|security-patch|guest-rootfs|edit-secrets|wipe-server|hooks-install
aspect deploy reset identity|billing|verification
aspect db pg list|shell|query --db=... [--query=...]
aspect db ch query|schemas [--database=...] [--query=...]
aspect db tb shell|command [--command=...]
aspect check --kind=go-test|go-vet|go-lint|conversions|scripts|ansible|voice|all
aspect codegen run --kind=topology|sqlc|openapi|clients|all
aspect codegen check --kind=topology|sqlc|openapi|clients|wire|all
aspect dev console|platform|sandbox-inner|sandbox-middle
aspect persona assume <platform-admin|acme-admin|acme-member> [--output=...] [--print]
aspect persona user-state [flags...]
aspect billing clock|state|documents|finalizations|events [flags...]
aspect mail list|accounts|mailboxes|code|read|send|passwords [flags...]
aspect bazel doctor|gazelle|tidy|update
```

Everything else uses raw Bazelisk:

```text
bazelisk build <target-patterns>
bazelisk test <target-patterns>
bazelisk query|cquery|aquery <expr>
```

This keeps Aspect from becoming a second build language. Aspect routes common
workflows; Bazel remains the graph and artifact interface.

## Retired Surface

Do not carry over these Make-era shapes:

- Per-service smoke aliases such as `billing-smoke-test`,
  `firecracker-cutover-smoke-test`, and `company-smoke-test`.
- Database shortcuts such as `billing-pg-query`; use
  `aspect db pg query --db=billing`.
- Persona shortcuts such as `assume-platform-admin`; use
  `aspect persona assume platform-admin`.
- Mail recipient shortcuts such as `mail-send-ceo`; use
  `aspect mail send --to=ceo`.
- Generic build aliases such as `vm-guest-telemetry-build`; use `bazelisk build`
  unless the operation is a higher-level workflow.

Existing smoke scripts under `src/platform/scripts/verify-*-live.sh` may remain
directly callable during the transition, but they are not a durable Aspect API.
Structured canary invocation across PR, staging, and production is a separate
project. When that project lands, it should add a typed `aspect canary ...`
surface instead of preserving the old `*-smoke-test` names.

## AXL Layout

```
.aspect/
├── config.axl                 # root Aspect config
├── helpers.axl                # ws_path, inventory_check, checked_spawn, CUE pin checks
└── tasks/
    ├── bazel.axl              # aspect bazel doctor|gazelle|tidy|update
    ├── billing.axl            # aspect billing ...
    ├── check.axl              # aspect check --kind=...
    ├── codegen.axl            # aspect codegen run|check --kind=...
    ├── db.axl                 # aspect db pg|ch|tb ...
    ├── deploy.axl             # aspect deploy + deploy reset ...
    ├── dev.axl                # aspect dev ...
    ├── doctor.axl             # top-level aspect doctor
    ├── mail.axl               # aspect mail ...
    ├── observe.axl            # top-level aspect observe
    ├── persona.axl            # aspect persona ...
    ├── platform.axl           # aspect platform ...
    └── tidy.axl               # top-level aspect tidy
MODULE.aspect                  # AXL extension deps only
```

`MODULE.aspect` declares AXL dependencies. It is not the version-of-record for
the Aspect binary; CUE is.

Prefer these implementation rules:

- Use `ctx.bazel.build`, `ctx.bazel.test`, and `ctx.bazel.query` instead of
  shelling out to `bazel`. This repo installs `bazelisk`, not necessarily a
  `bazel` binary.
- Keep AXL task bodies small. For complex behavior, call a repo script or,
  preferably, a Bazel-built binary.
- Keep topology/sqlc/openapi checks moving toward Bazel targets/tests. Aspect
  can orchestrate, but Bazel should own generated output freshness.
- Use typed args for every value that used to be a Make variable.
- `inventory_check(ctx)` is a helper, not a user-facing command.

## Codegen Semantics

`aspect codegen` is a single grouped surface, not many leaf commands.

`run` mutates committed generated files:

```text
aspect codegen run --kind=topology
aspect codegen run --kind=sqlc
aspect codegen run --kind=openapi
aspect codegen run --kind=clients
aspect codegen run --kind=all
```

`check` verifies committed generated files:

```text
aspect codegen check --kind=topology
aspect codegen check --kind=sqlc
aspect codegen check --kind=openapi
aspect codegen check --kind=clients
aspect codegen check --kind=wire
aspect codegen check --kind=all
```

Concrete backing:

- `topology` -> `bazelisk run //src/cue-renderer:freshness` for mutation and
  `ctx.bazel.test("//src/cue-renderer:freshness_tests")` for checks.
- `sqlc` -> current `sqlc generate/compile/vet` loop until sqlc generation is
  moved behind Bazel targets.
- `openapi` -> current service OpenAPI generators until those are wrapped as
  Bazel run/check targets.
- `clients` -> current `go generate` client directories until generated clients
  are Bazelized.
- `wire` -> `go run ./src/apiwire/cmd/openapi-wire-check ...` initially, then a
  Bazel test target.

## File Changes

Deleted:

- `Makefile`

Added:

- `.aspect/config.axl`
- `.aspect/helpers.axl`
- `.aspect/tasks/*.axl`
- `MODULE.aspect`

Updated:

- `scripts/bootstrap` installs pinned Bazelisk and pinned Aspect.
- `src/cue-renderer/catalog/versions.cue` adds `versions.development.aspectCLI`
  and `devTools.aspect`.
- Generated catalog projections update after `aspect codegen run --kind=topology`.
- `src/platform/ansible/roles/dev_tools/*` verifies or repairs Aspect using the
  generated catalog, but does not make it the only install path.
- `README.md`, `AGENTS.md`, `.claude/CLAUDE.md`, architecture docs, and Ansible
  comments/error strings replace durable `make` invocations with `aspect` or
  `bazelisk`.
- Temporary CI/workflow scaffolding that still invokes `make` is deleted or
  updated in the same PR.

Not changed:

- `.bazelrc`, `.bazelversion`, `MODULE.bazel`, and `bazel.go.work`
- Existing platform scripts, except where a script itself prints Make-specific
  guidance
- Existing Ansible playbook behavior, except command strings and bootstrap tool
  installation

## Risk Register

| Risk | Severity | Mitigation |
|---|---|---|
| AXL API churn before Aspect 1.0 | Medium | Pin Aspect in CUE, keep AXL thin, centralize helpers, avoid clever Starlark. |
| Aspect bootstrap cycle | High | Treat Aspect as `bootstrap_pivot`; `scripts/bootstrap` installs it before any `aspect` task is required. |
| Recreating Make sprawl under a new binary | High | No one-for-one target mapping; use the command surface in this document as the allowlist. |
| Raw `bazel` calls fail on machines with only Bazelisk | Medium | Use `ctx.bazel.*` in AXL and `bazelisk` in docs/scripts. |
| Temporary smoke scripts disappear during nearby canary work | Low | They are not durable Aspect API. Cutover verification may call existing scripts directly, but no command contract depends on them. |
| Loss of command discoverability from `make help` | Low | `aspect help` and typed task help replace the Make awk help. README/AGENTS carry the short durable surface. |
| No OTel spans for AXL task execution itself | Medium | Existing deploy and verification scripts still emit deploy/correlation spans. A later helper can add task-level spans if useful. |

## Verification Protocol

Write the failing protocol before deleting `Makefile`.

Pre-cutover failure:

```bash
aspect doctor
aspect deploy --tags=company
```

Expected: these fail before `.aspect/` and the bootstrap install path exist.

Cutover evidence:

```bash
./scripts/bootstrap
aspect doctor
aspect codegen check --kind=topology
aspect codegen check --kind=openapi
aspect codegen check --kind=sqlc
aspect deploy --tags=company
```

Then run the existing company verification script directly if it still exists:

```bash
cd src/platform
./scripts/verify-company-live.sh
```

Query ClickHouse through the new DB surface and capture the deploy/smoke
evidence in the PR:

```bash
aspect db ch query --query="
  SELECT
    count() AS spans,
    countIf(SpanStatusCode = 'STATUS_CODE_ERROR') AS errors
  FROM default.otel_traces
  WHERE Timestamp > now() - INTERVAL 30 MINUTE
    AND ServiceName IN ('company', 'company-web')
"
```

Final static gate:

```bash
rg '\bmake\b' .github README.md AGENTS.md .claude docs src/platform/ansible
```

Every remaining match must be either historical prose in this migration note or
an intentional reference reviewed in the cutover PR.

## Cutover Order

1. Add the CUE Aspect pin and generated catalog projection changes.
2. Update `scripts/bootstrap` to install Bazelisk and Aspect.
3. Add `.aspect/` and `MODULE.aspect`.
4. Implement the durable command surface only.
5. Update docs and command strings.
6. Delete or update temporary CI/workflow callers that still use `make`.
7. Delete `Makefile`.
8. Run the verification protocol and paste the ClickHouse evidence into the PR.

## Explicit Non-Goals

- No structured canary system in this effort. The future canary project should
  introduce `aspect canary ...` on its own terms.
- No Aspect wrapper for arbitrary `bazelisk build/test/query`; use Bazelisk.
- No compatibility aliases for old Make target names.
- No migration of every current smoke script into first-class Aspect tasks.

## Near-Term Follow-Ups (out of cutover scope)

Tracked here so the cutover PR does not grow them, but they should land soon
after.

- Task-invocation telemetry. `helpers.checked_spawn` already wraps every
  subprocess, so the same wrapper can append a row per `aspect <task>`
  invocation to a `default.aspect_task_invocations` ClickHouse table:
  `(invoked_at DateTime64, task_name LowCardinality(String), exit_code Int32,
   duration_ms UInt32, host LowCardinality(String), user LowCardinality(String))`.
  Cheap, ORDER BY `(task_name, invoked_at)`, no spans needed. Answers "how
  often is each task actually invoked" without taking on the full OTel
  task-tracing surface.
- `aspect canary ...` to replace the directly-callable `verify-*-live.sh`
  scripts with a typed structured-canary surface.
- Move `topology`, `sqlc`, `openapi`, and `clients` codegen behind Bazel
  `write_source_files` / `genrule` targets so `aspect codegen check --kind=...`
  becomes a thin wrapper around `ctx.bazel.test(...)`.

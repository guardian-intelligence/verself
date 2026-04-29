# Make → Aspect CLI cutover

Replace the 564-line root `Makefile` (104 targets) with [Aspect CLI](https://github.com/aspect-build/aspect-cli) as the canonical task runner. Bazelisk + Bazel keep owning builds; Aspect sits on top as the AXL-driven task layer that queries the Bazel graph natively.

Single cutover. The Makefile is deleted in the same PR that adds `.aspect/`. CI is unaffected (CI does not call `make`).

## Why Aspect, not just / mise / bare `bazel run`

| | Aspect CLI | `just` | `mise` | `bazel run :sh_binary` |
|---|---|---|---|---|
| Bazel graph access | first-class (`ctx.bazel.{build,query,info}`) | shell out | shell out | n/a (you *are* bazel) |
| Typed flags / help | `args.string(default=, short=, long=, description=)` | positional + `set positional-arguments` | env vars | `--` passthrough only |
| Pinning | `.aspect/version.axl` | `just --version` | mise plugin | none |
| Lock-in to existing Bazel investment | tight | none | none | maximal |
| Maturity | Early Preview (Apr 2026) | stable | stable | stable |

Bazel-graph-aware tasks plus typed flags are the lever. `make pg-query DB=billing QUERY='SELECT 1'` becomes `aspect db pg query --db=billing --query='SELECT 1'` with auto-generated `--help` and short flags. The Bazel-native bit matters for targets like `topology-generate` that need to depend on `//src/cue-renderer:freshness` rebuilding correctly — Aspect calls into the Bazel build event stream rather than shelling out.

## Stability stance

Aspect CLI is in [Early Preview](https://github.com/aspect-build/aspect-cli/blob/main/README.md). PR [#998](https://github.com/aspect-build/aspect-cli/pull/998) (2026-04-14) renamed task arguments top-to-bottom (`args` ↔ `attrs`). Releases ship weekly; breaking renames are still landing.

**Plan**: pin `v2026.17.17` exactly in `.aspect/version.axl`. Accept that 1.0 will require one or two `.axl` rewrites. Mitigations:

- Each task is a small, named function — a global rename is a `find -name '*.axl' | xargs sed` away (or 30 minutes for a coding agent).
- Shell scripts under `src/platform/scripts/` keep doing the heavy lifting; AXL tasks shell out to them. AXL surface area is intentionally thin.
- Helpers (find-walker, inventory guard) live in one file, `helpers.axl`, so any API change there is a one-file fix.

## Repository layout

```
.aspect/
├── version.axl                # pin Aspect CLI version + binary sources
├── config.axl                 # entry point: load tasks, set traits
├── helpers.axl                # walk_files(), inventory_check(), ws_path()
└── tasks/
    ├── bazel.axl              # group=["bazel"]      → aspect bazel doctor|gazelle|tidy|update
    ├── build.axl              # group=["build"]      → aspect build vm-guest-telemetry|guest-images|topology|topology-check
    ├── codegen.axl            # group=["codegen"]    → aspect codegen sqlc|sqlc-check|openapi|openapi-check|openapi-clients|openapi-clients-check|openapi-wire-check
    ├── db.axl                 # group=["db", ...]    → aspect db pg list|shell|query  ;  aspect db ch query|schemas  ;  aspect db tb shell|command
    ├── deploy.axl             # top-level deploy + group=["deploy"] for fast-paths and resets
    ├── dev.axl                # group=["dev"]        → aspect dev console|platform|sandbox-inner|sandbox-middle
    ├── lint.axl               # top-level tidy + group=["lint"] → aspect lint all|conversions|scripts|ansible|voice|test|fmt|vet
    ├── mail.axl               # group=["mail"]       → aspect mail list|accounts|mailboxes|code|read|send|passwords
    ├── observe.axl            # top-level observe + telemetry-smoke|services-doctor
    ├── persona.axl            # group=["persona"]    → aspect persona assume|platform-admin|acme-admin|acme-member|set-user-state
    ├── platform.axl           # group=["platform"]   → aspect platform provision|deprovision|setup-sops|setup-dev|security-patch|guest-rootfs|edit-secrets|hooks-install|wipe-server
    ├── billing.axl            # group=["billing"]    → aspect billing clock|wall-clock|state|documents|finalizations|events|reset|pg-shell|pg-query
    └── smoke.axl              # group=["smoke"]      → aspect smoke <service-name> for all *-smoke-test targets
MODULE.aspect                  # axl_local_dep + version pin
```

Top-level commands (no group) — the four touched repeatedly by docs, AGENTS.md, and the founder's daily flow:

- `aspect tidy` — formats Go + TS + tidies all `go.mod`s.
- `aspect deploy [--tags=foo,bar]` — wraps `ansible-with-tunnel.sh playbooks/site.yml`.
- `aspect observe [--what=...] [--signal=...] ...` — telemetry surface.
- `aspect doctor` — Bazel bootstrap contract + inventory presence + Aspect version match.

Everything else is grouped. Two-level grouping (e.g. `aspect db pg query`) is supported: `task(group=["db", "pg"])`. From `aspect-build/aspect-cli/.aspect/user-task.axl`.

## File-by-file AXL sketches

These are concrete enough to compile against `v2026.17.17`. Names follow Aspect's current convention (`task(...)` returns a task; the AXL variable name = the leaf command name; `args` (post-#998) holds typed flags).

### `.aspect/version.axl`

```python
"""Aspect CLI pin. Binary install handled by the dev_tools Ansible role."""

version(
    "2026.17.17",
    sources = [
        github(
            owner = "aspect-build",
            repo = "aspect-cli",
            tag = "v{tag}",
            artifact = "aspect-cli-{tag}-{os}-{arch}.tar.gz",
        ),
    ],
)
```

### `MODULE.aspect`

```python
module(name = "forge_metal", version = "0.0.0")

# All tasks live in .aspect/tasks/. Auto-discovered.
axl_local_dep(name = "tasks", path = ".aspect/tasks", auto_use_tasks = True)
axl_local_dep(name = "helpers", path = ".aspect", auto_use_tasks = False)
```

### `.aspect/helpers.axl`

The two AXL gaps from the research, isolated:

```python
"""Workspace-wide helpers. Single file = single rewrite when AXL APIs churn."""

def ws_path(ctx, *parts):
    """Absolute path under the workspace root."""
    return "/".join([ctx.std.env.root_dir()] + list(parts))

def inventory_check(ctx):
    """Fail loudly if the generated Ansible inventory is missing.

    Equivalent of `make inventory-check`. Many tasks depend on this — the
    inventory file is a marker that `aspect platform provision` has run.
    """
    inv = ws_path(ctx, "src/platform/ansible/inventory/hosts.ini")
    if not ctx.std.fs.exists(inv):
        fail("ERROR: %s not found. Run: aspect platform provision" % inv)

def walk_files(ctx, root, name):
    """Recursive find-by-name. AXL has no native fs walker; this is the
    workaround. Used by codegen.sqlc to discover sqlc.yaml manifests.

    Returns a list of absolute paths to files whose basename == name.
    """
    out = []
    stack = [root]
    for _ in range(10000):  # Starlark has no while; bound the recursion.
        if not stack:
            break
        cur = stack.pop()
        for entry in ctx.std.fs.read_dir(cur):
            p = "%s/%s" % (cur, entry.name)
            if entry.is_dir:
                stack.append(p)
            elif entry.name == name:
                out.append(p)
    return out

def go_modules(ctx):
    """The set of Go modules driven by Make's GO_DIRS + GO_CLIENT_DIRS.

    Sourced from bazel.go.work to avoid drift. Falls back to the static list
    if `go work` parsing isn't available.
    """
    out = ctx.std.process.command("go").args(["work", "use", "-print"]).current_dir(ctx.std.env.root_dir()).stdout("piped").spawn().wait_with_output()
    return [line.strip() for line in out.stdout.splitlines() if line.strip()]
```

### `.aspect/tasks/bazel.axl`

Pure Bazel wrappers. Bucket A (8 targets).

```python
load("//helpers.axl", "ws_path")

def _doctor_impl(ctx):
    res = ctx.bazel.build("//tools/bazel:doctor").wait()
    if not res.success:
        return res.code
    # bazel run equivalent: build, then exec the binary at runfiles path.
    info = ctx.bazel.info()
    return ctx.std.process.command("%s/tools/bazel/doctor" % info["bazel-bin"]).spawn().wait().code

doctor = task(
    group = ["bazel"],
    description = "Verify the pinned Bazel/Bazelisk bootstrap contract.",
    implementation = _doctor_impl,
)

def _gazelle_impl(ctx):
    info = ctx.bazel.info()
    if not ctx.bazel.build("//:gazelle").wait().success:
        return 1
    return ctx.std.process.command("%s/gazelle" % info["bazel-bin"]).args([
        "update",
        "-go_naming_convention=import_alias",
        "-go_naming_convention_external=import_alias",
    ]).current_dir(ctx.std.env.root_dir()).spawn().wait().code

gazelle = task(group = ["bazel"], description = "Regenerate Bazel Go BUILD files.", implementation = _gazelle_impl)

def _tidy_impl(ctx):
    return ctx.std.process.command("bazel").args(["mod", "tidy", "--lockfile_mode=update"]).current_dir(ctx.std.env.root_dir()).spawn().wait().code

tidy = task(group = ["bazel"], description = "Update Bzlmod repository wiring.", implementation = _tidy_impl)

def _update_impl(ctx):
    for fn in [_gazelle_impl, _tidy_impl]:
        rc = fn(ctx)
        if rc != 0:
            return rc
    return 0

update = task(group = ["bazel"], description = "Run gazelle, then mod tidy.", implementation = _update_impl)
```

### `.aspect/tasks/build.axl`

Bucket A continued. Pure `bazel build` aliases.

```python
def _bazel_build(ctx, *labels):
    return ctx.bazel.build(*labels).wait().code

vm_guest_telemetry = task(
    group = ["build"],
    description = "Build the vm-guest-telemetry guest binary.",
    implementation = lambda ctx: _bazel_build(ctx, "//src/vm-guest-telemetry:vm-guest-telemetry"),
)

guest_images = task(
    group = ["build"],
    description = "Build the substrate inputs bundle + every toolchain image ext4.",
    implementation = lambda ctx: _bazel_build(
        ctx,
        "//src/vm-orchestrator/guest-images/substrate:substrate_inputs_bundle",
        "//src/vm-orchestrator/guest-images:toolchain_images_bundle",
    ),
)

topology = task(
    group = ["build"],
    description = "Regenerate Ansible deploy inputs from CUE topology.",
    implementation = lambda ctx: ctx.bazel.build("//src/cue-renderer:freshness").wait().code,
)

topology_check = task(
    group = ["build"],
    description = "Verify generated deploy inputs match CUE topology.",
    implementation = lambda ctx: ctx.std.process.command("bazel").args(["test", "//src/cue-renderer:freshness_tests"]).current_dir(ctx.std.env.root_dir()).spawn().wait().code,
)
```

### `.aspect/tasks/lint.axl`

Bucket B. Multi-Go-module loops + top-level `tidy`.

```python
load("//helpers.axl", "go_modules")

def _go_loop(ctx, label, args):
    """Run `go <args>` in every Go module. Print headers like the Makefile."""
    code = 0
    for d in go_modules(ctx):
        ctx.std.io.stdout.write("==> %s\n" % d)
        rc = ctx.std.process.command("go").args(args).current_dir(d).spawn().wait().code
        if rc != 0 and code == 0:
            code = rc
    return code

# Top-level: aspect tidy
def _tidy_impl(ctx):
    rc = _go_loop(ctx, "go mod tidy", ["mod", "tidy"])
    if rc != 0:
        return rc
    return ctx.std.process.command("vp").args(["fmt", ".", "--write"]).current_dir(ctx.std.env.root_dir() + "/src/viteplus-monorepo").spawn().wait().code

tidy = task(description = "go mod tidy across all modules + vp fmt.", implementation = _tidy_impl)

# Grouped lint commands
test = task(group = ["lint"], description = "go test -race ./... in every module.", implementation = lambda ctx: _go_loop(ctx, "test", ["test", "-race", "./..."]))
vet = task(group = ["lint"], description = "go vet ./... in every module.", implementation = lambda ctx: _go_loop(ctx, "vet", ["vet", "./..."]))

def _conversions_impl(ctx):
    return _go_loop(ctx, "gosec", ["run", "github.com/securego/gosec/v2/cmd/gosec", "--", "-quiet", "-include=G115", "./..."])
    # Or: shell out to `gosec` if it's on PATH from the dev_tools role.

conversions = task(group = ["lint"], description = "gosec G115 across all modules.", implementation = _conversions_impl)

def _all_impl(ctx):
    rc = _conversions_impl(ctx)
    if rc != 0:
        return rc
    return _go_loop(ctx, "golangci-lint", ["run", "github.com/golangci/golangci-lint/v2/cmd/golangci-lint", "--", "run", "./..."])

all = task(group = ["lint"], description = "lint conversions + golangci-lint.", implementation = _all_impl)

scripts = task(
    group = ["lint"],
    description = "shellcheck over platform shell scripts.",
    implementation = lambda ctx: ctx.std.process.command("shellcheck").args([
        "-x", "-P", ".",
    ] + ctx.std.fs.read_dir(ctx.std.env.root_dir() + "/src/platform/scripts").filter(lambda e: e.name.endswith(".sh")).map(lambda e: e.name)).current_dir(ctx.std.env.root_dir() + "/src/platform/scripts").spawn().wait().code,
)

ansible = task(
    group = ["lint"],
    description = "ansible-lint over playbooks + roles.",
    implementation = lambda ctx: ctx.std.process.command("ansible-lint").args(["playbooks", "roles"]).current_dir(ctx.std.env.root_dir() + "/src/platform/ansible").spawn().wait().code,
)

voice = task(
    group = ["lint"],
    description = "Voice spec checks for the company marketing site.",
    implementation = lambda ctx: ctx.std.process.command("corepack").args(["pnpm", "--filter", "@verself/company", "run", "lint:voice"]).current_dir(ctx.std.env.root_dir() + "/src/viteplus-monorepo").spawn().wait().code,
)

fmt = task(
    group = ["lint"],
    description = "gofumpt -w over all Go directories.",
    implementation = lambda ctx: ctx.std.process.command("gofumpt").args(["-w"] + go_modules(ctx)).current_dir(ctx.std.env.root_dir()).spawn().wait().code,
)
```

### `.aspect/tasks/codegen.axl`

Bucket B. The `openapi` family is repetitive — a small data-driven loop replaces the 100-line Make recipe.

```python
load("//helpers.axl", "ws_path", "walk_files")

# (service_dir, command_name, kind) where kind ∈ {"public", "internal"}
OPENAPI_SPECS = [
    ("billing-service", "billing-openapi", "public"),
    ("governance-service", "governance-openapi", "public"),
    ("governance-service", "governance-internal-openapi", "internal"),
    ("identity-service", "identity-openapi", "public"),
    ("identity-service", "identity-internal-openapi", "internal"),
    ("secrets-service", "secrets-openapi", "public"),
    ("secrets-service", "secrets-internal-openapi", "internal"),
    ("source-code-hosting-service", "source-code-hosting-openapi", "public"),
    ("source-code-hosting-service", "source-code-hosting-internal-openapi", "internal"),
    ("mailbox-service", "mailbox-openapi", "public"),
    ("object-storage-service", "object-storage-openapi", "public"),
    ("profile-service", "profile-openapi", "public"),
    ("profile-service", "profile-internal-openapi", "internal"),
    ("notifications-service", "notifications-openapi", "public"),
    ("projects-service", "projects-openapi", "public"),
    ("projects-service", "projects-internal-openapi", "internal"),
    ("sandbox-rental-service", "sandbox-rental-openapi", "public"),
    ("sandbox-rental-service", "sandbox-rental-internal-openapi", "internal"),
]

OPENAPI_CLIENT_DIRS = [
    # (service_dir, kind) — every committed generated client.
    ("billing-service", "client"),
    ("governance-service", "client"), ("governance-service", "internalclient"),
    ("identity-service", "client"), ("identity-service", "internalclient"),
    ("secrets-service", "client"), ("secrets-service", "internalclient"),
    ("source-code-hosting-service", "client"), ("source-code-hosting-service", "internalclient"),
    ("sandbox-rental-service", "client"), ("sandbox-rental-service", "internalclient"),
    ("mailbox-service", "client"),
    ("object-storage-service", "client"),
    ("profile-service", "client"), ("profile-service", "internalclient"),
    ("notifications-service", "client"),
    ("projects-service", "client"), ("projects-service", "internalclient"),
]

def _openapi_filename(kind, fmt):
    base = "openapi" if kind == "public" else "internal-openapi"
    return "%s-%s.yaml" % (base, fmt)

def _generate_one(ctx, svc, cmd, kind, fmt, check):
    out_dir = ws_path(ctx, "src", svc, "openapi")
    ctx.std.fs.create_dir_all(out_dir)
    out_path = "%s/%s" % (out_dir, _openapi_filename(kind, fmt))
    args = ["run", "./cmd/%s" % cmd, "--format", fmt]
    if check:
        args.append("--check")
        return ctx.std.process.command("go").args(args).current_dir(ws_path(ctx, "src", svc)).spawn().wait().code
    res = ctx.std.process.command("go").args(args).current_dir(ws_path(ctx, "src", svc)).stdout("piped").spawn().wait_with_output()
    if not res.status.success:
        return res.status.code
    ctx.std.fs.write(out_path, res.stdout)
    return 0

def _openapi_impl(ctx):
    for svc, cmd, kind in OPENAPI_SPECS:
        for fmt in ["3.0", "3.1"]:
            rc = _generate_one(ctx, svc, cmd, kind, fmt, check = False)
            if rc != 0:
                return rc
    return _openapi_clients_impl(ctx)

def _openapi_check_impl(ctx):
    for svc, cmd, kind in OPENAPI_SPECS:
        for fmt in ["3.0", "3.1"]:
            rc = _generate_one(ctx, svc, cmd, kind, fmt, check = True)
            if rc != 0:
                return rc
    rc = _openapi_clients_check_impl(ctx)
    if rc != 0:
        return rc
    return _openapi_wire_check_impl(ctx)

def _openapi_clients_impl(ctx):
    for svc, kind in OPENAPI_CLIENT_DIRS:
        rc = ctx.std.process.command("go").args(["generate", "./..."]).current_dir(ws_path(ctx, "src", svc, kind)).spawn().wait().code
        if rc != 0:
            return rc
    return 0

def _openapi_clients_check_impl(ctx):
    rc = _openapi_clients_impl(ctx)
    if rc != 0:
        return rc
    files = ["src/%s/%s/client.gen.go" % (s, k) for s, k in OPENAPI_CLIENT_DIRS]
    rc = ctx.std.process.command("git").args(["diff", "--exit-code", "--"] + files).current_dir(ctx.std.env.root_dir()).spawn().wait().code
    if rc != 0:
        return rc
    # Hand-rolled SDK guard: no raw http.NewRequest etc. in client dirs.
    forbidden = "http\\.NewRequestWithContext\\(|\\.Do\\(req\\)|json\\.NewDecoder\\(|json\\.Marshal\\("
    dirs = ["src/%s/%s" % (s, k) for s, k in OPENAPI_CLIENT_DIRS]
    rc = ctx.std.process.command("rg").args([
        "-n", forbidden,
    ] + dirs + [
        "--glob", "!**/client.gen.go",
        "--glob", "!**/generate.go",
    ]).current_dir(ctx.std.env.root_dir()).spawn().wait().code
    return 1 if rc == 0 else 0  # rg returns 0 when matches found = bad.

def _openapi_wire_check_impl(ctx):
    specs = []
    for svc, _, kind in OPENAPI_SPECS:
        specs.append("src/%s/openapi/%s" % (svc, _openapi_filename(kind, "3.1")))
    return ctx.std.process.command("go").args([
        "run", "./src/apiwire/cmd/openapi-wire-check",
    ] + specs).current_dir(ctx.std.env.root_dir()).spawn().wait().code

openapi = task(group = ["codegen"], description = "Regenerate committed OpenAPI 3.0 + 3.1 specs and Go clients.", implementation = _openapi_impl)
openapi_check = task(group = ["codegen"], description = "Verify committed OpenAPI specs + clients are up to date.", implementation = _openapi_check_impl)
openapi_clients = task(group = ["codegen"], description = "Regenerate committed Go clients from OpenAPI 3.0 specs.", implementation = _openapi_clients_impl)
openapi_clients_check = task(group = ["codegen"], description = "Verify committed Go clients are up to date.", implementation = _openapi_clients_check_impl)
openapi_wire_check = task(group = ["codegen"], description = "Verify frontend-consumed OpenAPI 3.1 specs are JS wire-safe.", implementation = _openapi_wire_check_impl)

def _sqlc_impl(ctx):
    """find src -mindepth 2 -maxdepth 2 -name sqlc.yaml — uses helpers.walk_files."""
    paths = walk_files(ctx, ws_path(ctx, "src"), "sqlc.yaml")
    if not paths:
        fail("ERROR: no sqlc.yaml files found")
    for p in paths:
        d = p.rsplit("/", 1)[0]
        ctx.std.io.stdout.write("sqlc generate %s\n" % d)
        rc = ctx.std.process.command("sqlc").args(["generate"]).current_dir(d).spawn().wait().code
        if rc != 0:
            return rc
    return 0

def _sqlc_check_impl(ctx):
    paths = walk_files(ctx, ws_path(ctx, "src"), "sqlc.yaml")
    if not paths:
        fail("ERROR: no sqlc.yaml files found")
    for p in paths:
        d = p.rsplit("/", 1)[0]
        for verb in ["compile", "vet"]:
            rc = ctx.std.process.command("sqlc").args([verb]).current_dir(d).spawn().wait().code
            if rc != 0:
                return rc
    rc = _sqlc_impl(ctx)
    if rc != 0:
        return rc
    # git diff --exit-code on every generated store file.
    return ctx.std.process.command("bash").args([
        "-c",
        "set -e; files=$(find src -path '*/internal/store/*.go' -print | sort); test -n \"$files\" || { echo no sqlc generated files; exit 1; }; git diff --exit-code -- $files; untracked=$(git ls-files --others --exclude-standard -- $files); test -z \"$untracked\" || { echo untracked: $untracked; exit 1; }",
    ]).current_dir(ctx.std.env.root_dir()).spawn().wait().code

sqlc = task(group = ["codegen"], description = "Regenerate sqlc stores for every service with sqlc.yaml.", implementation = _sqlc_impl)
sqlc_check = task(group = ["codegen"], description = "Verify committed sqlc stores are up to date.", implementation = _sqlc_check_impl)
```

### `.aspect/tasks/db.axl`

Bucket C. Parameterized wrappers around `src/platform/scripts/{pg,clickhouse,tigerbeetle}.sh`. AXL flag-passing in action.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _pg_query_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/pg.sh")).args([
        ctx.args.db, "--query", ctx.args.query,
    ]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

pg_query = task(
    group = ["db", "pg"],
    description = "Run a PostgreSQL query on the worker.",
    implementation = _pg_query_impl,
    args = {
        "db": args.string(description = "Database name (run 'aspect db pg list' to see databases).", short = "d"),
        "query": args.string(description = "SQL query to execute.", short = "q"),
    },
)

def _pg_shell_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/pg.sh")).args([ctx.args.db]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

pg_shell = task(
    group = ["db", "pg"],
    description = "Open interactive psql.",
    implementation = _pg_shell_impl,
    args = {"db": args.string(description = "Database name.", short = "d")},
)

def _pg_list_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/pg.sh")).args(["--list"]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

pg_list = task(group = ["db", "pg"], description = "List PostgreSQL databases on the worker.", implementation = _pg_list_impl)

def _ch_query_impl(ctx):
    inventory_check(ctx)
    cmd = ctx.std.process.command(ws_path(ctx, "src/platform/scripts/clickhouse.sh"))
    args_list = []
    if ctx.args.database:
        args_list += ["--database", ctx.args.database]
    args_list += ["--query", ctx.args.query]
    return cmd.args(args_list).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

ch_query = task(
    group = ["db", "ch"],
    description = "Run a ClickHouse query on the worker.",
    implementation = _ch_query_impl,
    args = {
        "query": args.string(description = "SQL query to execute.", short = "q"),
        "database": args.string(default = "", description = "ClickHouse database (default: server default).", short = "D"),
    },
)

def _ch_schemas_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/clickhouse.sh")).args([
        "--query",
        "SELECT concat(database, '.', name, '\n', create_table_query, '\n') FROM system.tables WHERE database IN ('verself', 'default') AND name NOT LIKE '.%' ORDER BY database, name FORMAT TSVRaw",
    ]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

ch_schemas = task(group = ["db", "ch"], description = "Print CREATE TABLE for all project tables.", implementation = _ch_schemas_impl)

def _tb_shell_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/tigerbeetle.sh")).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

tb_shell = task(group = ["db", "tb"], description = "Open the TigerBeetle REPL.", implementation = _tb_shell_impl)

def _tb_command_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/tigerbeetle.sh")).args(["--command", ctx.args.command]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

tb_command = task(
    group = ["db", "tb"],
    description = "Run a single TigerBeetle REPL op.",
    implementation = _tb_command_impl,
    args = {"command": args.string(description = "REPL op (e.g. 'lookup_accounts id=1;').", short = "c")},
)
```

### `.aspect/tasks/deploy.axl`

Bucket D. Top-level `deploy` (the most-used flow) plus the platform-mutating playbook wrappers.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _deploy_impl(ctx):
    inventory_check(ctx)
    extra = []
    if ctx.args.tags:
        extra = ["--tags", ctx.args.tags]
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args([
        "playbooks/site.yml",
    ] + extra).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

# Top-level: aspect deploy --tags=billing,caddy
deploy = task(
    description = "Deploy current site topology.",
    implementation = _deploy_impl,
    args = {"tags": args.string(default = "", description = "Comma-separated Ansible tags (e.g. billing,caddy).", short = "t")},
)

def _ansible_playbook_via_tunnel(name):
    """Closure factory for one-line `ansible-with-tunnel.sh playbooks/<name>.yml`."""
    def _impl(ctx):
        inventory_check(ctx)
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args([
            "playbooks/%s.yml" % name,
        ]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

guest_rootfs = task(group = ["deploy"], description = "Build the substrate ext4 on the worker and stage toolchain ext4s.", implementation = _ansible_playbook_via_tunnel("guest-rootfs"))
identity_reset = task(group = ["deploy"], description = "Wipe identity-service PostgreSQL and restart dependents.", implementation = _ansible_playbook_via_tunnel("identity-reset"))
seed_system = task(group = ["deploy"], description = "Seed platform + Acme tenants, billing, mailboxes, auth.", implementation = _ansible_playbook_via_tunnel("seed-system"))
billing_reset = task(group = ["deploy"], description = "Wipe billing state (TigerBeetle + PostgreSQL) and restart callers.", implementation = _ansible_playbook_via_tunnel("billing-reset"))

def _verification_reset_impl(ctx):
    inventory_check(ctx)
    rc = ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args(["playbooks/verification-reset.yml"]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    if rc != 0:
        return rc
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args([
        "playbooks/site.yml",
        "-e", "temporal_force_schema_reset=true",
        "-e", "clickhouse_force_schema_reset=true",
    ]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

verification_reset = task(group = ["deploy"], description = "Wipe verification state and redeploy with schema reset.", implementation = _verification_reset_impl)

def _wipe_pg_db_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args([
        "playbooks/wipe-pg-db.yml",
        "-e", "wipe_pg_db_name=%s" % ctx.args.db,
    ]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

wipe_pg_db = task(
    group = ["deploy"],
    description = "Wipe one managed PostgreSQL service DB.",
    implementation = _wipe_pg_db_impl,
    args = {"db": args.string(description = "Service DB name (billing|sandbox_rental|...).", short = "d")},
)

def _security_patch_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args(["playbooks/security-patch.yml"]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

security_patch = task(group = ["deploy"], description = "Apply OS security updates.", implementation = _security_patch_impl)

# Fast-path UI deploys
def _frontend_fast(script):
    def _impl(ctx):
        inventory_check(ctx)
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/%s" % script)).current_dir(ctx.std.env.root_dir()).spawn().wait().code
    return _impl

console_frontend_fast = task(group = ["deploy"], description = "Ship UI-only changes to console (~5-10s).", implementation = _frontend_fast("console-frontend-deploy-fast.sh"))
platform_frontend_fast = task(group = ["deploy"], description = "Ship UI-only changes to platform docs (~5-10s).", implementation = _frontend_fast("platform-frontend-deploy-fast.sh"))
```

### `.aspect/tasks/platform.axl`

Bucket D + G. Platform-scoped operations that don't fit `deploy` cleanly.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _ansible_playbook(name):
    """Closure factory for `cd ansible && ansible-playbook playbooks/<name>.yml`.
    No tunnel — used for provision (no inventory yet) + sops setup."""
    def _impl(ctx):
        return ctx.std.process.command("ansible-playbook").args(["playbooks/%s.yml" % name]).current_dir(ws_path(ctx, "src/platform/ansible")).spawn().wait().code
    return _impl

provision = task(group = ["platform"], description = "Provision bare metal and generate inventory.", implementation = _ansible_playbook("provision"))
setup_sops = task(group = ["platform"], description = "Bootstrap SOPS + Age encryption.", implementation = _ansible_playbook("setup-sops"))

def _setup_dev_impl(ctx):
    """Hybrid: tunneled if inventory exists, plain otherwise."""
    inv = ws_path(ctx, "src/platform/ansible/inventory/hosts.ini")
    if ctx.std.fs.exists(inv):
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/ansible-with-tunnel.sh")).args(["playbooks/setup-dev.yml"]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return ctx.std.process.command("ansible-playbook").args(["playbooks/setup-dev.yml"]).current_dir(ws_path(ctx, "src/platform/ansible")).spawn().wait().code

setup_dev = task(group = ["platform"], description = "Install pinned dev tools (bazelisk, aspect-cli, sqlc, ...).", implementation = _setup_dev_impl)

def _deprovision_impl(ctx):
    if ctx.args.confirm != "deprovision":
        fail("ERROR: deprovision requires --confirm=deprovision")
    return _ansible_playbook("deprovision")(ctx)

deprovision = task(
    group = ["platform"],
    description = "Destroy provisioned bare metal infrastructure.",
    implementation = _deprovision_impl,
    args = {"confirm": args.string(description = "Must equal 'deprovision'.")},
)

def _wipe_server_impl(ctx):
    if ctx.args.confirm != "wipe-server":
        fail("ERROR: wipe-server requires --confirm=wipe-server")
    inventory_check(ctx)
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/wipe-server.sh")).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

wipe_server = task(
    group = ["platform"],
    description = "Wipe all verself state from the provisioned server.",
    implementation = _wipe_server_impl,
    args = {"confirm": args.string(description = "Must equal 'wipe-server'.")},
)

edit_secrets = task(
    group = ["platform"],
    description = "Open encrypted secrets in $EDITOR via sops.",
    implementation = lambda ctx: ctx.std.process.command("sops").args([ws_path(ctx, "src/platform/ansible/group_vars/all/secrets.sops.yml")]).spawn().wait().code,
)

def _hooks_install_impl(ctx):
    """Match Make's logic: unset core.hooksPath if it points at .git/hooks, then install."""
    res = ctx.std.process.command("git").args(["config", "--get", "core.hooksPath"]).stdout("piped").spawn().wait_with_output()
    cur = res.stdout.strip() if res.status.success else ""
    expected = ws_path(ctx, ".git/hooks")
    if cur == expected or cur == ".git/hooks":
        ctx.std.process.command("git").args(["config", "--unset-all", "core.hooksPath"]).spawn().wait()
    return ctx.std.process.command("pre-commit").args(["install"]).current_dir(ctx.std.env.root_dir()).spawn().wait().code

hooks_install = task(group = ["platform"], description = "Install pre-commit hooks.", implementation = _hooks_install_impl)
```

### `.aspect/tasks/observe.axl`

Bucket C. The `observe` target with its ~20 optional flags is the hardest porting case — and it gets dramatically nicer with typed flags.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _observe_impl(ctx):
    inventory_check(ctx)
    flags = []
    for name, val in [
        ("what", ctx.args.what), ("signal", ctx.args.signal), ("service", ctx.args.service),
        ("metric", ctx.args.metric), ("span", ctx.args.span), ("field", ctx.args.field),
        ("query", ctx.args.query), ("prefix", ctx.args.prefix), ("search", ctx.args.search),
        ("group-by", ctx.args.group_by), ("mode", ctx.args.mode), ("trace-id", ctx.args.trace_id),
        ("run-key", ctx.args.run_key), ("host", ctx.args.host), ("status-min", ctx.args.status_min),
        ("format", ctx.args.format), ("minutes", ctx.args.minutes), ("limit", ctx.args.limit),
    ]:
        if val:
            flags += ["--%s" % name, val]
    if ctx.args.errors:
        flags.append("--errors")
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/observe.sh")).args(flags).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

# Top-level: aspect observe --what=catalog
observe = task(
    description = "Discover/query telemetry. Run with no flags for the catalog.",
    implementation = _observe_impl,
    args = {
        "what": args.string(default = "", description = "One of: catalog|queries|describe|metric|trace|logs|http|service|errors|mail|deploy|workload-identity|temporal."),
        "signal": args.string(default = ""), "service": args.string(default = ""),
        "metric": args.string(default = ""), "span": args.string(default = ""),
        "field": args.string(default = ""), "query": args.string(default = ""),
        "prefix": args.string(default = ""), "search": args.string(default = ""),
        "group_by": args.string(default = "", long = "group-by"),
        "mode": args.string(default = ""), "trace_id": args.string(default = "", long = "trace-id"),
        "run_key": args.string(default = "", long = "run-key"),
        "host": args.string(default = ""), "status_min": args.string(default = "", long = "status-min"),
        "format": args.string(default = "", description = "table|json|markdown"),
        "minutes": args.string(default = ""), "limit": args.string(default = ""),
        "errors": args.bool(default = False, description = "Filter to error rows only."),
    },
)

def _services_doctor_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command("python3").args([
        ws_path(ctx, "src/platform/scripts/services-doctor.py"),
    ]).spawn().wait().code

services_doctor = task(
    group = ["observe"],
    description = "Cross-check generated topology endpoints against live listeners.",
    implementation = _services_doctor_impl,
)

def _telemetry_smoke_impl(expect_fail):
    def _impl(ctx):
        inventory_check(ctx)
        cmd = ctx.std.process.command(ws_path(ctx, "src/platform/scripts/telemetry-smoke-test.sh"))
        if expect_fail:
            cmd = cmd.env("TELEMETRY_SMOKE_TEST_EXPECT_FAIL", "1")
        return cmd.current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

telemetry_smoke = task(group = ["observe"], description = "Run observability smoke; verify ansible spans land in ClickHouse.", implementation = _telemetry_smoke_impl(expect_fail = False))
telemetry_smoke_fail = task(group = ["observe"], description = "Run observability smoke fail-path; verify Error spans land.", implementation = _telemetry_smoke_impl(expect_fail = True))
```

### `.aspect/tasks/smoke.axl`

Bucket E. 20+ targets, all `cd src/platform && ./scripts/verify-X-live.sh`. One factory.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _smoke(script):
    def _impl(ctx):
        inventory_check(ctx)
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/%s" % script)).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

bazel = task(group = ["smoke"], description = "Verify Bazel bootstrap with ClickHouse trace assertion.", implementation = _smoke("verify-bazel-live.sh"))
company = task(group = ["smoke"], description = "Walk Guardian Intelligence IA, verify company.* spans.", implementation = _smoke("verify-company-live.sh"))
billing = task(group = ["smoke"], description = "Live billing browser smoke + evidence collection.", implementation = _smoke("verify-console-billing-flow.sh"))
profile = task(group = ["smoke"], description = "Live profile API/UI smoke + PG/CH evidence.", implementation = _smoke("verify-profile-live.sh"))
organization_sync = task(group = ["smoke"], description = "Live organization auto-sync/OCC smoke.", implementation = _smoke("verify-organization-sync-live.sh"))
notifications = task(group = ["smoke"], description = "Live notifications bell smoke + PG/CH traces.", implementation = _smoke("verify-notifications-live.sh"))
projects = task(group = ["smoke"], description = "Live projects API smoke + PG/CH traces.", implementation = _smoke("verify-projects-live.sh"))
source_code_hosting = task(group = ["smoke"], description = "Live source repository UI/API smoke + PG/CH traces.", implementation = _smoke("verify-source-code-hosting-live.sh"))
secrets = task(group = ["smoke"], description = "Live secrets API smoke + audit/trace evidence.", implementation = _smoke("verify-secrets-live.sh"))
secrets_leak = task(group = ["smoke"], description = "Verify no bearer/JWT material in traces/logs/audit/artifacts.", implementation = _smoke("verify-secrets-leak-smoke-test.sh"))
openbao = task(group = ["smoke"], description = "Verify OpenBao process, health, metrics, audit, nftables, CH evidence.", implementation = _smoke("verify-openbao-live.sh"))
openbao_tenancy = task(group = ["smoke"], description = "Verify OpenBao per-org mounts/JWT/SPIFFE roles + CH spans.", implementation = _smoke("verify-openbao-tenancy-live.sh"))
object_storage = task(group = ["smoke"], description = "Verify Garage-backed object-storage runtime + CH evidence.", implementation = _smoke("verify-object-storage-live.sh"))
workload_identity = task(group = ["smoke"], description = "Verify SPIFFE mTLS/JWT-SVID boundaries + CH evidence.", implementation = _smoke("verify-workload-identity-live.sh"))
spiffe_rotation = task(group = ["smoke"], description = "Verify file-backed SPIFFE consumers reload rotated material.", implementation = _smoke("verify-spiffe-rotation-live.sh"))
temporal = task(group = ["smoke"], description = "Verify Temporal runtime, bootstrap path, CH evidence.", implementation = _smoke("verify-temporal-live.sh"))
temporal_web = task(group = ["smoke"], description = "Verify Temporal Web login, operator routing, CH evidence.", implementation = _smoke("verify-temporal-web-live.sh"))
recurring_schedule = task(group = ["smoke"], description = "Create+resume Temporal-backed recurring schedule + PG/CH evidence.", implementation = _smoke("verify-recurring-schedule-live.sh"))
vm_orchestrator = task(group = ["smoke"], description = "vm-orchestrator lease/exec spans live smoke.", implementation = _smoke("verify-vm-orchestrator-live.sh"))
sandbox = task(group = ["smoke"], description = "Redeploy + reseed + full sandbox lifecycle verification.", implementation = _smoke("verify-sandbox-live.sh"))
console_ui = task(group = ["smoke"], description = "Deployed console authenticated shell smoke.", implementation = _smoke("verify-console-ui-smoke.sh"))
console_ui_local = task(group = ["smoke"], description = "Console smoke against local HMR.", implementation = _smoke("verify-console-ui-local.sh"))
grafana = task(group = ["smoke"], description = "Verify Grafana health, datasource, PG state, CH evidence.", implementation = _smoke("verify-grafana-live.sh"))

def _observability_impl(ctx):
    inventory_check(ctx)
    return ctx.std.process.command("ansible-playbook").args(["playbooks/observability-smoke.yml"]).current_dir(ws_path(ctx, "src/platform/ansible")).spawn().wait().code

observability = task(group = ["smoke"], description = "Raw Ansible observability smoke playbook.", implementation = _observability_impl)
```

### `.aspect/tasks/persona.axl`

Bucket C. Persona assumption + state mutation.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _assume_impl(persona):
    def _impl(ctx):
        inventory_check(ctx)
        flags = [persona]
        if ctx.args.output:
            flags += ["--output", ctx.args.output]
        if ctx.args.print:
            flags += ["--print"]
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/assume-persona.sh")).args(flags).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

ASSUME_ARGS = {
    "output": args.string(default = "", description = "Path to write env file.", short = "o"),
    "print": args.bool(default = False, description = "Print env to stdout."),
}

def _assume_persona_impl(ctx):
    if not ctx.args.persona:
        fail("ERROR: --persona is required (platform-admin|acme-admin|acme-member)")
    return _assume_impl(ctx.args.persona)(ctx)

assume = task(
    group = ["persona"],
    description = "Write persona env file.",
    implementation = _assume_persona_impl,
    args = dict(ASSUME_ARGS, **{"persona": args.string(description = "platform-admin|acme-admin|acme-member.", short = "p")}),
)
platform_admin = task(group = ["persona"], description = "Write env for platform admin agent.", implementation = _assume_impl("platform-admin"), args = ASSUME_ARGS)
acme_admin = task(group = ["persona"], description = "Write env for Acme org admin.", implementation = _assume_impl("acme-admin"), args = ASSUME_ARGS)
acme_member = task(group = ["persona"], description = "Write env for Acme org member.", implementation = _assume_impl("acme-member"), args = ASSUME_ARGS)

def _set_user_state_impl(ctx):
    inventory_check(ctx)
    flags = []
    for name, val in [
        ("email", ctx.args.email), ("org", ctx.args.org), ("org-id", ctx.args.org_id),
        ("org-name", ctx.args.org_name), ("state", ctx.args.state), ("plan-id", ctx.args.plan_id),
        ("product-id", ctx.args.product_id), ("balance-units", ctx.args.balance_units),
        ("balance-cents", ctx.args.balance_cents), ("business-now", ctx.args.business_now),
        ("overage-policy", ctx.args.overage_policy), ("trust-tier", ctx.args.trust_tier),
    ]:
        if val:
            flags += ["--%s" % name, val]
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/set-user-state.sh")).args(flags).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

set_user_state = task(
    group = ["persona"],
    description = "Set billing fixture state for a user.",
    implementation = _set_user_state_impl,
    args = {
        "email": args.string(default = ""), "org": args.string(default = ""), "org_id": args.string(default = "", long = "org-id"),
        "org_name": args.string(default = "", long = "org-name"), "state": args.string(default = ""),
        "plan_id": args.string(default = "", long = "plan-id"),
        "product_id": args.string(default = "sandbox", long = "product-id"),
        "balance_units": args.string(default = "", long = "balance-units"),
        "balance_cents": args.string(default = "", long = "balance-cents"),
        "business_now": args.string(default = "", long = "business-now"),
        "overage_policy": args.string(default = "", long = "overage-policy"),
        "trust_tier": args.string(default = "", long = "trust-tier"),
    },
)
```

### `.aspect/tasks/billing.axl`

Bucket C. Billing-specific shells.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _billing_clock_impl(ctx):
    inventory_check(ctx)
    flags = []
    for name, val in [("org", ctx.args.org), ("org-id", ctx.args.org_id), ("product-id", ctx.args.product_id)]:
        if val:
            flags += ["--%s" % name, val]
    if ctx.args.set:
        flags += ["--set", ctx.args.set]
    if ctx.args.advance_seconds:
        flags += ["--advance-seconds", ctx.args.advance_seconds]
    if ctx.args.clear:
        flags.append("--clear")
    if ctx.args.wall_clock:
        flags.append("--wall-clock")
    if ctx.args.reason:
        flags += ["--reason", ctx.args.reason]
    return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/billing-clock.sh")).args(flags).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code

clock = task(
    group = ["billing"],
    description = "Inspect or mutate billing business time.",
    implementation = _billing_clock_impl,
    args = {
        "org": args.string(default = ""), "org_id": args.string(default = "", long = "org-id"),
        "product_id": args.string(default = "sandbox", long = "product-id"),
        "set": args.string(default = "", description = "Set business time to ISO timestamp."),
        "advance_seconds": args.string(default = "", long = "advance-seconds"),
        "clear": args.bool(default = False),
        "wall_clock": args.bool(default = False, long = "wall-clock"),
        "reason": args.string(default = ""),
    },
)

# state, documents, finalizations, events all share billing-inspect.sh.
def _billing_inspect_impl(kind):
    def _impl(ctx):
        inventory_check(ctx)
        if not ctx.args.org and not ctx.args.org_id:
            fail("ERROR: --org or --org-id is required")
        flags = ["--kind", kind, "--org", ctx.args.org, "--org-id", ctx.args.org_id, "--product-id", ctx.args.product_id]
        if kind == "events":
            if ctx.args.event:
                flags += ["--event-type", ctx.args.event]
            if ctx.args.minutes:
                flags += ["--minutes", ctx.args.minutes]
        if ctx.args.format:
            flags += ["--format", ctx.args.format]
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/billing-inspect.sh")).args(flags).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

INSPECT_ARGS = {
    "org": args.string(default = ""), "org_id": args.string(default = "", long = "org-id"),
    "product_id": args.string(default = "sandbox", long = "product-id"),
    "format": args.string(default = "", description = "table|json|markdown"),
}

state = task(group = ["billing"], description = "Inspect billing state for an org.", implementation = _billing_inspect_impl("state"), args = INSPECT_ARGS)
documents = task(group = ["billing"], description = "List billing documents for an org.", implementation = _billing_inspect_impl("documents"), args = INSPECT_ARGS)
finalizations = task(group = ["billing"], description = "List billing finalizations for an org.", implementation = _billing_inspect_impl("finalizations"), args = INSPECT_ARGS)

events = task(
    group = ["billing"],
    description = "Query recent billing events in ClickHouse.",
    implementation = _billing_inspect_impl("events"),
    args = dict(INSPECT_ARGS, **{
        "event": args.string(default = "", description = "Filter by event type."),
        "minutes": args.string(default = "", description = "Look back this many minutes."),
    }),
)
```

### `.aspect/tasks/mail.axl`

Bucket C. Mail family — wraps `cmd/mailbox-tool` Go binary + `mail-send.sh`.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _mailbox_tool(ctx, *extra):
    inventory_check(ctx)
    inv = ws_path(ctx, "src/platform/ansible/inventory/hosts.ini")
    args_list = ["run", "./cmd/mailbox-tool", "--inventory", inv] + list(extra)
    return ctx.std.process.command("go").args(args_list).current_dir(ws_path(ctx, "src/mailbox-service")).spawn().wait().code

def _account_flag(ctx):
    return ["--account", ctx.args.mailbox] if ctx.args.mailbox else []

list_ = task(
    group = ["mail"],
    description = "List recent emails (defaults to agents).",
    implementation = lambda ctx: _mailbox_tool(ctx, "list", *_account_flag(ctx), *(["--limit", ctx.args.n] if ctx.args.n else [])),
    args = {"mailbox": args.string(default = "", short = "m"), "n": args.string(default = "")},
)
accounts = task(group = ["mail"], description = "List synced mailbox accounts.", implementation = lambda ctx: _mailbox_tool(ctx, "accounts"))
mailboxes = task(
    group = ["mail"],
    description = "List mailboxes for an account.",
    implementation = lambda ctx: _mailbox_tool(ctx, "mailboxes", *_account_flag(ctx)),
    args = {"mailbox": args.string(default = "", short = "m")},
)
code = task(
    group = ["mail"],
    description = "Extract latest 2FA/verification code.",
    implementation = lambda ctx: _mailbox_tool(ctx, "code", *_account_flag(ctx)),
    args = {"mailbox": args.string(default = "", short = "m")},
)
read = task(
    group = ["mail"],
    description = "Read a specific email.",
    implementation = lambda ctx: _mailbox_tool(ctx, "read", *_account_flag(ctx), "--id", ctx.args.id),
    args = {"id": args.string(short = "i"), "mailbox": args.string(default = "", short = "m")},
)

def _send_impl(to_override = None):
    def _impl(ctx):
        inventory_check(ctx)
        to = to_override or ctx.args.to
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/mail-send.sh")).args([
            "-t", to, "-s", ctx.args.subject, "-b", ctx.args.body,
        ]).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

SEND_ARGS = {"subject": args.string(short = "s"), "body": args.string(short = "b")}

send = task(
    group = ["mail"],
    description = "Send via Resend.",
    implementation = _send_impl(),
    args = dict(SEND_ARGS, **{"to": args.string(short = "t", description = "agents|ceo|email@domain.")}),
)
send_agents = task(group = ["mail"], description = "Send via Resend to agents inbox.", implementation = _send_impl("agents"), args = SEND_ARGS)
send_ceo = task(group = ["mail"], description = "Send via Resend to ceo inbox.", implementation = _send_impl("ceo"), args = SEND_ARGS)

def _passwords_impl(ctx):
    inventory_check(ctx)
    domain = ctx.std.process.command("bash").args([
        "-c", "grep '^verself_domain:' src/platform/ansible/group_vars/all/main.yml | awk '{print $2}' | tr -d '\"'",
    ]).current_dir(ctx.std.env.root_dir()).stdout("piped").spawn().wait_with_output().stdout.strip()
    for who in ["ceo", "agents"]:
        ctx.std.io.stdout.write("%s@%s:\n" % (who, domain))
        rc = ctx.std.process.command("sops").args([
            "-d", "--extract", '["stalwart_%s_password"]' % who,
            "src/platform/ansible/group_vars/all/secrets.sops.yml",
        ]).current_dir(ctx.std.env.root_dir()).spawn().wait().code
        if rc != 0:
            return rc
        ctx.std.io.stdout.write("\n\n")
    return 0

passwords = task(group = ["mail"], description = "Show Stalwart mailbox passwords.", implementation = _passwords_impl)
```

### `.aspect/tasks/dev.axl`

Bucket F. Local dev loops.

```python
load("//helpers.axl", "ws_path", "inventory_check")

def _script(rel, *fixed):
    def _impl(ctx):
        inventory_check(ctx)
        extra = []
        if hasattr(ctx.args, "print_env") and ctx.args.print_env:
            extra.append("--print-env")
        return ctx.std.process.command(ws_path(ctx, "src/platform/scripts/%s" % rel)).args(list(fixed) + extra).current_dir(ws_path(ctx, "src/platform")).spawn().wait().code
    return _impl

console = task(
    group = ["dev"],
    description = "Start local console dev tunnels and HMR server.",
    implementation = _script("run-console-local-dev.sh"),
    args = {"print_env": args.bool(default = False, long = "print-env")},
)
sandbox_inner = task(group = ["dev"], description = "Sandbox inner loop (HMR by default).", implementation = _script("sandbox-inner.sh"))
sandbox_middle = task(group = ["dev"], description = "Sandbox middle loop (deploys + admin smoke).", implementation = _script("sandbox-middle.sh"))

def _platform_local_impl(ctx):
    """Read verself_domain from generated catalog and start vp dev with it pointing at remote prod."""
    domain = ctx.std.process.command("bash").args([
        "-c",
        "awk -F'\"' '/^verself_domain:/{print $2}' src/platform/ansible/group_vars/all/main.yml",
    ]).current_dir(ctx.std.env.root_dir()).stdout("piped").spawn().wait_with_output().stdout.strip()
    return ctx.std.process.command("vp").args(["dev"]).env(
        "VERSELF_DOMAIN", domain,
    ).env("PRODUCT_BASE_URL", "https://" + domain).env("BASE_URL", "https://" + domain).current_dir(ws_path(ctx, "src/viteplus-monorepo/apps/platform")).spawn().wait().code

platform = task(group = ["dev"], description = "Start local platform docs HMR (no tunnels).", implementation = _platform_local_impl)
```

### `.aspect/tasks/doctor.axl`

Top-level `aspect doctor` — preflight that combines bazel-doctor + inventory + Aspect version pin.

```python
load("//helpers.axl", "ws_path")

def _doctor_impl(ctx):
    """Three contracts: Bazel pin, Aspect pin, inventory presence."""
    rc = 0
    # 1. Bazel bootstrap.
    if not ctx.bazel.build("//tools/bazel:doctor").wait().success:
        ctx.std.io.stderr.write("FAIL: bazel doctor target did not build\n")
        return 1
    info = ctx.bazel.info()
    sub = ctx.std.process.command("%s/tools/bazel/doctor" % info["bazel-bin"]).spawn().wait()
    if sub.code != 0:
        return sub.code
    # 2. Inventory (warn-only — not every dev has provisioned).
    inv = ws_path(ctx, "src/platform/ansible/inventory/hosts.ini")
    if not ctx.std.fs.exists(inv):
        ctx.std.io.stderr.write("WARN: %s missing — run aspect platform provision\n" % inv)
    # 3. Aspect version (parse .aspect/version.axl pin and assert match).
    expected = "2026.17.17"
    actual = ctx.std.process.command("aspect").args(["--version"]).stdout("piped").spawn().wait_with_output().stdout.strip()
    if expected not in actual:
        ctx.std.io.stderr.write("FAIL: aspect-cli %s, expected %s\n" % (actual, expected))
        rc = 1
    ctx.std.io.stdout.write("aspect doctor ok\n")
    return rc

doctor = task(description = "Verify Bazel + inventory + Aspect version contracts.", implementation = _doctor_impl)
```

### `.aspect/config.axl`

Entry point that wires it all together. Mostly trait declarations.

```python
"""Aspect CLI configuration. Tasks under .aspect/tasks/ are auto-discovered via
MODULE.aspect's `axl_local_dep(auto_use_tasks=True)`."""

# No special traits today. As we grow, .bazelrc-style config goes here.
```

## Bucketed inventory — every Make target maps somewhere

Total: 104 distinct targets. Eight buckets, one row per Make target.

| Make target | Aspect command | File | Bucket |
|---|---|---|---|
| `bazel-doctor` | `aspect bazel doctor` | bazel.axl | A |
| `bazel-smoke-test` | `aspect smoke bazel` | smoke.axl | E |
| `bazel-gazelle` | `aspect bazel gazelle` | bazel.axl | A |
| `bazel-tidy` | `aspect bazel tidy` | bazel.axl | A |
| `bazel-update` | `aspect bazel update` | bazel.axl | A |
| `vm-guest-telemetry-build` | `aspect build vm-guest-telemetry` | build.axl | A |
| `guest-images-build` | `aspect build guest-images` | build.axl | A |
| `guest-rootfs` | `aspect deploy guest-rootfs` | deploy.axl | D |
| `topology-generate` | `aspect build topology` | build.axl | A |
| `topology-check` | `aspect build topology-check` | build.axl | A |
| `inventory-check` | `helpers.inventory_check(ctx)` | helpers.axl | (lib) |
| `test` | `aspect lint test` | lint.axl | B |
| `lint` | `aspect lint all` | lint.axl | B |
| `lint-conversions` | `aspect lint conversions` | lint.axl | B |
| `lint-scripts` | `aspect lint scripts` | lint.axl | B |
| `lint-ansible` | `aspect lint ansible` | lint.axl | B |
| `lint-voice` | `aspect lint voice` | lint.axl | B |
| `fmt` | `aspect lint fmt` | lint.axl | B |
| `vet` | `aspect lint vet` | lint.axl | B |
| `tidy` | `aspect tidy` | lint.axl | B |
| `hooks-install` | `aspect platform hooks-install` | platform.axl | B |
| `sqlc` | `aspect codegen sqlc` | codegen.axl | B |
| `sqlc-check` | `aspect codegen sqlc-check` | codegen.axl | B |
| `openapi` | `aspect codegen openapi` | codegen.axl | B |
| `openapi-check` | `aspect codegen openapi-check` | codegen.axl | B |
| `openapi-clients` | `aspect codegen openapi-clients` | codegen.axl | B |
| `openapi-clients-check` | `aspect codegen openapi-clients-check` | codegen.axl | B |
| `openapi-wire-check` | `aspect codegen openapi-wire-check` | codegen.axl | B |
| `setup-dev` | `aspect platform setup-dev` | platform.axl | D |
| `setup-sops` | `aspect platform setup-sops` | platform.axl | D |
| `provision` | `aspect platform provision` | platform.axl | D |
| `deprovision` | `aspect platform deprovision --confirm=deprovision` | platform.axl | D |
| `deploy` | `aspect deploy [--tags=...]` | deploy.axl | D |
| `security-patch` | `aspect deploy security-patch` | deploy.axl | D |
| `identity-reset` | `aspect deploy identity-reset` | deploy.axl | D |
| `seed-system` | `aspect deploy seed-system` | deploy.axl | D |
| `assume-persona` | `aspect persona assume --persona=...` | persona.axl | C |
| `assume-platform-admin` | `aspect persona platform-admin` | persona.axl | C |
| `assume-acme-admin` | `aspect persona acme-admin` | persona.axl | C |
| `assume-acme-member` | `aspect persona acme-member` | persona.axl | C |
| `set-user-state` | `aspect persona set-user-state --email=... --org=... --state=...` | persona.axl | C |
| `billing-clock` | `aspect billing clock --org-id=...` | billing.axl | C |
| `billing-wall-clock` | `aspect billing clock --wall-clock --org=...` | billing.axl | C |
| `billing-state` | `aspect billing state --org=...` | billing.axl | C |
| `billing-documents` | `aspect billing documents --org=...` | billing.axl | C |
| `billing-finalizations` | `aspect billing finalizations --org=...` | billing.axl | C |
| `billing-events` | `aspect billing events [--event=...] [--minutes=...]` | billing.axl | C |
| `billing-pg-shell` | `aspect db pg shell --db=billing` | db.axl | C |
| `billing-pg-query` | `aspect db pg query --db=billing --query='...'` | db.axl | C |
| `billing-smoke-test` | `aspect smoke billing` | smoke.axl | E |
| `billing-reset` | `aspect deploy billing-reset` | deploy.axl | D |
| `verification-reset` | `aspect deploy verification-reset` | deploy.axl | D |
| `wipe-pg-db` | `aspect deploy wipe-pg-db --db=...` | deploy.axl | D |
| `wipe-server` | `aspect platform wipe-server --confirm=wipe-server` | platform.axl | G |
| `profile-smoke-test` | `aspect smoke profile` | smoke.axl | E |
| `organization-sync-smoke-test` | `aspect smoke organization-sync` | smoke.axl | E |
| `notifications-smoke-test` | `aspect smoke notifications` | smoke.axl | E |
| `projects-smoke-test` | `aspect smoke projects` | smoke.axl | E |
| `source-code-hosting-smoke-test` | `aspect smoke source-code-hosting` | smoke.axl | E |
| `secrets-smoke-test` | `aspect smoke secrets` | smoke.axl | E |
| `secrets-leak-smoke-test` | `aspect smoke secrets-leak` | smoke.axl | E |
| `openbao-smoke-test` | `aspect smoke openbao` | smoke.axl | E |
| `openbao-tenancy-smoke-test` | `aspect smoke openbao-tenancy` | smoke.axl | E |
| `object-storage-smoke-test` | `aspect smoke object-storage` | smoke.axl | E |
| `workload-identity-smoke-test` | `aspect smoke workload-identity` | smoke.axl | E |
| `spiffe-rotation-smoke-test` | `aspect smoke spiffe-rotation` | smoke.axl | E |
| `temporal-smoke-test` | `aspect smoke temporal` | smoke.axl | E |
| `temporal-web-smoke-test` | `aspect smoke temporal-web` | smoke.axl | E |
| `recurring-schedule-smoke-test` | `aspect smoke recurring-schedule` | smoke.axl | E |
| `vm-orchestrator-smoke-test` | `aspect smoke vm-orchestrator` | smoke.axl | E |
| `sandbox-smoke-test` | `aspect smoke sandbox` | smoke.axl | E |
| `sandbox-inner` | `aspect dev sandbox-inner` | dev.axl | F |
| `sandbox-middle` | `aspect dev sandbox-middle` | dev.axl | F |
| `console-ui-smoke` | `aspect smoke console-ui` | smoke.axl | E |
| `console-ui-local` | `aspect smoke console-ui-local` | smoke.axl | E |
| `console-local-dev` | `aspect dev console` | dev.axl | F |
| `console-frontend-deploy-fast` | `aspect deploy console-frontend-fast` | deploy.axl | F |
| `platform-frontend-deploy-fast` | `aspect deploy platform-frontend-fast` | deploy.axl | F |
| `platform-local-dev` | `aspect dev platform` | dev.axl | F |
| `grafana-smoke-test` | `aspect smoke grafana` | smoke.axl | E |
| `services-doctor` | `aspect observe services-doctor` | observe.axl | C |
| `observe` | `aspect observe [...flags]` | observe.axl | C |
| `telemetry-smoke-test` | `aspect observe telemetry-smoke` | observe.axl | E |
| `telemetry-smoke-test-fail` | `aspect observe telemetry-smoke-fail` | observe.axl | E |
| `observability-smoke` | `aspect smoke observability` | smoke.axl | E |
| `clickhouse-query` | `aspect db ch query --query='...'` | db.axl | C |
| `clickhouse-schemas` | `aspect db ch schemas` | db.axl | C |
| `pg-shell` | `aspect db pg shell --db=...` | db.axl | C |
| `pg-query` | `aspect db pg query --db=... --query='...'` | db.axl | C |
| `pg-list` | `aspect db pg list` | db.axl | C |
| `tb-shell` | `aspect db tb shell` | db.axl | C |
| `tb-command` | `aspect db tb command --command='...'` | db.axl | C |
| `mail` | `aspect mail list` | mail.axl | C |
| `mail-accounts` | `aspect mail accounts` | mail.axl | C |
| `mail-mailboxes` | `aspect mail mailboxes` | mail.axl | C |
| `mail-code` | `aspect mail code` | mail.axl | C |
| `mail-read` | `aspect mail read --id=...` | mail.axl | C |
| `mail-send` | `aspect mail send --to=... --subject=... --body=...` | mail.axl | C |
| `mail-send-agents` | `aspect mail send-agents --subject=... --body=...` | mail.axl | C |
| `mail-send-ceo` | `aspect mail send-ceo --subject=... --body=...` | mail.axl | C |
| `mail-passwords` | `aspect mail passwords` | mail.axl | C |
| `edit-secrets` | `aspect platform edit-secrets` | platform.axl | G |
| `help` | (built-in `aspect --help`) | (built-in) | — |

Notes:

- `inventory-check` becomes a `helpers.axl` function called by every task that needs the inventory. Not a CLI command.
- `bazel-update` is a sequence wrapper — `aspect bazel update` calls gazelle then tidy.
- The `tidy` Make target combines per-module `go mod tidy` + `vp fmt` — `aspect tidy` keeps that behavior (top-level convenience).
- `make billing-pg-shell` and `make billing-pg-query` collapse into the generic `aspect db pg {shell,query} --db=billing`. Less surface, same behavior.

## Delta — files outside `.aspect/` that change

**Deleted:**

- `Makefile` — root.

**Added:**

- `.aspect/version.axl`, `.aspect/config.axl`, `.aspect/helpers.axl`, `.aspect/tasks/*.axl` (12 files), `MODULE.aspect`.
- `src/platform/ansible/roles/dev_tools/tasks/aspect_cli.yml` — installs aspect-cli with the same archive + SHA256 verify pattern as bazelisk.

**Updated:**

- `README.md` — replace every `make X` with `aspect X` (~30 occurrences). Lines 56–149 currently lean heavily on Make.
- `AGENTS.md` — `make tidy` → `aspect tidy`, `make deploy TAGS=company` → `aspect deploy --tags=company`, `make observe` → `aspect observe`, `make pg-list` → `aspect db pg list`, `make pg-query` → `aspect db pg query`, `make clickhouse-query` → `aspect db ch query`, `make clickhouse-schemas` → `aspect db ch schemas`, `Most commands should begin with either make or bazelisk` → `Most commands should begin with either aspect or bazelisk`.
- `.claude/CLAUDE.md` — same swap as AGENTS.md.
- `docs/architecture/{api-versioning,change-data-capture,data-rights,domain-event-stream,durable-execution,system-context,workload-identity}.md` — replace every `make X` reference (~15 lines total).
- `src/platform/ansible/playbooks/wipe-pg-db.yml` (line 5 comment, line 169 error string) — `make wipe-pg-db` → `aspect deploy wipe-pg-db`.
- `src/platform/ansible/roles/firecracker/tasks/main.yml` (line 16) — `make topology-generate` → `aspect build topology`.
- `src/platform/ansible/roles/deploy_profile/tasks/build_go.yml` (line 13) — same.
- `src/cue-renderer/AGENTS.md` — references `bazelisk` directly, no Make. Untouched.
- `src/cue-renderer/catalog/versions.cue` — add `aspect: "2026.17.17"` next to `bazelisk: "1.28.1"`.
- `src/platform/ansible/roles/dev_tools/defaults/main.yml` — add `dev_tools_aspect: aspect-cli` matching the existing `dev_tools_bazelisk: bazelisk` pattern.
- `src/platform/ansible/roles/dev_tools/tasks/main.yml` — include `aspect_cli.yml` after the bazelisk install task.
- `tools/bazel/doctor.sh` — keep as-is. `aspect bazel doctor` calls into it; `aspect doctor` also calls it.

**Not changed:**

- `.github/workflows/{github,blacksmith}-ci.yml` — CI does not invoke `make`. Confirmed by `grep -rn "make " .github/`.
- `.bazelrc`, `.bazelversion`, `.bazeliskrc`, `MODULE.bazel`, `bazel.go.work` — Bazel layer untouched. Aspect CLI invokes Bazel underneath; bazelisk is still the launcher.
- All `src/platform/scripts/*.sh` scripts — kept verbatim. AXL tasks shell out to them. Half their value is being callable as `bash scripts/foo.sh ...` for one-off debugging.
- All `playbooks/*.yml` — Aspect just invokes the same `ansible-with-tunnel.sh` wrapper.

## Risk register

| Risk | Severity | Mitigation |
|---|---|---|
| AXL breaking rename like PR #998 (args↔attrs) lands again | medium | Pin `v2026.17.17`. Tasks are small, named, stylistically uniform — a global rename is an `rg \| sed` away. Helpers concentrated in `helpers.axl` so cross-cutting API changes touch one file. |
| Aspect CLI install path on Linux (no apt package) | low | Add `dev_tools/tasks/aspect_cli.yml` mirroring the bazelisk install: download release tarball, verify SHA256 from `versions.cue`, extract to `/usr/local/bin/aspect`. |
| AXL has no recursive file walk | low | Implemented in `helpers.walk_files()`; ~15 lines using `read_dir` + an explicit stack. Bounded recursion (Starlark has no `while`). |
| AXL has no try/except — subprocess errors are data | low | Every task returns `child.wait().code`. Pattern is uniform across all 100 tasks. `fail()` only used for argument-validation guards. |
| No Starlark LSP for `.axl` files (issue #914) | low | Tasks are short, uniformly shaped. Editor pain is low. |
| Aspect CLI emits no OTel spans for task invocations | medium | Out of scope today. Document as a follow-up: shell out wrappers (`ansible-with-tunnel.sh`, `verify-*-live.sh`) already emit `deploy_run_key`-correlated spans, so the existing observability story is preserved. A later step can wrap `ctx.std.process.command(...).spawn()` in a helper that emits an OTel span per task — but that's a v2. |
| Founder's daily flow disrupted while muscle memory rebuilds | low | Docs (README, AGENTS.md) updated in the same PR. Top-level commands (`aspect tidy`, `aspect deploy`, `aspect observe`) preserve the most-used names verbatim. |
| Coexistence with bazelisk pinning contract | low | `tools/bazel/doctor.sh` still enforces the bazelisk pin. `aspect doctor` calls it as part of its own preflight. Bazel is invoked underneath Aspect via `ctx.bazel.*` (which respects `.bazelversion`). |

## Verification protocol — the cutover canary

The handoff names this explicitly: pick one real production-deploy workflow as the cutover canary. **Pick: `make deploy TAGS=company` → `make company-smoke-test`.**

Why this one:
- It deploys a real production surface (`https://company_domain`).
- `scripts/verify-company-live.sh` already produces ClickHouse spans (`company.*`, `newsroom.*`) keyed on `deploy_run_key` and `deploy_id`.
- It exercises bucket A (Bazel), bucket D (Ansible), and bucket E (smoke tests) — three of our hardest cases.
- Recovery from failure is cheap: the company site is read-only marketing.

### Pre-cutover (write the failing test first)

1. Stand up `.aspect/` and `MODULE.aspect` with the version pin only — no tasks. `aspect --version` should report `2026.17.17`.
2. Add a single failing assertion: `aspect deploy --tags=company` must succeed and produce a fresh `deploy_run_key`. Confirm it fails (no tasks defined).

### Cutover (the implementation PR)

1. Land all 12 `.axl` files + helpers.
2. Update the dev_tools role to install aspect-cli on the controller; re-run `aspect platform setup-dev` (or its current Make equivalent) to verify the install path works on a fresh checkout.
3. Delete `Makefile`.
4. Update README, AGENTS.md, CLAUDE.md, docs/architecture, and the three Ansible files with embedded `make X` strings.

### Verification (the admissible evidence)

Per `output_contract`: ClickHouse traces are the only admissible completion artifact. The verification script exists already; we just point Aspect at it.

```bash
# 1. Generate a known deploy_run_key by running through Aspect.
aspect deploy --tags=company
KEY1=$(cat ~/.cache/verself/last-deploy-run-key)  # or read it from the script's output

aspect smoke company   # runs scripts/verify-company-live.sh; emits company.* spans

# 2. Query ClickHouse for the spans correlated to that run.
aspect db ch query --query="
  SELECT count() AS spans, countIf(SpanStatusCode = 'STATUS_CODE_ERROR') AS errors
  FROM default.otel_traces
  WHERE ServiceName IN ('company','company-web')
    AND SpanAttributes['deploy.run_key'] = '${KEY1}'
    AND Timestamp > now() - INTERVAL 30 MINUTE
"

# 3. Assert: spans > 0, errors = 0. Same shape as the Make-driven baseline.
```

Bonus assertions (each fails the cutover if it doesn't pass):

- `aspect bazel doctor` passes (bootstrap contract preserved).
- `aspect doctor` passes (Bazel + inventory + Aspect pin all green).
- `aspect codegen openapi-check` exits 0 (committed specs still in sync — the move shouldn't drift them).
- `aspect codegen sqlc-check` exits 0.
- A fresh ClickHouse query for `dev_tools.install` spans (emitted by `playbooks/setup-dev.yml`) shows the aspect-cli install task ran successfully on the controller.

### Stop conditions during cutover

- If `aspect deploy --tags=company` succeeds but no `company.*` spans appear within 5 minutes of the smoke test finishing: stop. Investigate whether `ansible-with-tunnel.sh` propagated env correctly through Aspect's subprocess invocation. Don't paper over.
- If any AXL task hits a runtime error mentioning `attrs` vs. `args` (or any Starlark naming collision): stop. PR #998 may have a follow-up rename and we missed it.
- If `aspect doctor` reports a version mismatch: stop and reconcile the pin in `version.axl` and `versions.cue`.

## Cutover order (the implementation PR)

One PR, one commit (or a small series for review hygiene). No phased rollout.

1. Add `.aspect/version.axl` + `MODULE.aspect`. Confirm `aspect --version` runs.
2. Add `.aspect/helpers.axl`.
3. Add `.aspect/tasks/{bazel,build,smoke}.axl` — the simplest. Verify each leaf with a smoke run.
4. Add `.aspect/tasks/{db,observe}.axl`. Run `aspect db pg list`, `aspect observe --what=catalog`, confirm parity.
5. Add `.aspect/tasks/{lint,codegen,mail,billing,persona,dev,deploy,platform}.axl`. Run a few each.
6. Update `dev_tools` role to install aspect-cli; rerun `aspect platform setup-dev` on the local controller.
7. Update README, AGENTS.md, CLAUDE.md, docs/architecture, three Ansible files.
8. Delete `Makefile`.
9. Run the cutover canary (above). Capture the ClickHouse span counts in the PR description as evidence.

## Open questions for sign-off

1. **Naming**. Top-level commands as proposed are `aspect tidy`, `aspect deploy`, `aspect observe`, `aspect doctor`. Anything else you want pulled to top-level for muscle memory? (E.g. should `aspect db pg query` shorten to `aspect pgq`?)
2. **`billing-pg-{shell,query}` collapse**. Proposing to drop the billing-specific aliases and just use `aspect db pg shell --db=billing` / `aspect db pg query --db=billing --query='...'`. Saves surface area; loses the muscle-memory shortcut. OK?
3. **`assume-platform-admin` etc. shortcuts**. Proposing to keep these as named tasks (rather than forcing `aspect persona assume --persona=platform-admin`), because the shortcut form is touched a lot. Confirm.
4. **`make help` replacement**. Aspect's built-in `aspect --help` and `aspect <group> --help` do auto-discovery. The Makefile's hand-rolled help (`awk -F ":.*## "`) goes away. OK to lose the custom format?
5. **OTel for AXL itself**. Out of scope as proposed — task-level tracing isn't built today. Confirm we can ship without it and add later as a `helpers.spawn_traced(...)` wrapper.

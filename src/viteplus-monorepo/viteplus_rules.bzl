load("@aspect_rules_js//js:defs.bzl", "js_run_binary")
load("@aspect_rules_js//npm:defs.bzl", "npm_package")
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@npm//:defs.bzl", "npm_link_all_packages")

_INSTRUMENTATION_BUNDLER = "//src/viteplus-monorepo/scripts:bundle_instrumentation"
_NODEJS_ARCHIVE = "@server_tool_nodejs//file"

def viteplus_source_package(npm_name, srcs):
    """Source-only workspace package linked into the rules_js npm graph.

    Apps consume the package's `.ts` source directly (per `package.json#exports`
    pointing at `./src/<entry>.ts` under the `node` condition); there is no
    pre-build step. The `npm_package` target exists so rules_js's
    `npm_link_all_packages()` can resolve `@verself/<name>` from any
    consumer's node_modules.
    """
    npm_link_all_packages()

    native.filegroup(
        name = "sources",
        srcs = native.glob(srcs),
    )

    npm_package(
        name = "pkg",
        srcs = [":sources"],
        package = npm_name,
        version = "0.0.0",
    )

def viteplus_app(npm_name, srcs):
    """Bazel surface for a viteplus app.

    `:node_app_nomad_artifact` runs `vp build` from the source tree as a local,
    unsandboxed action. Deploy preflights materialize the Vite+ workspace with
    `vp install --frozen-lockfile`; Bazel declares the app/workspace input set
    and decides when the packaging action reruns.

    Putting Vite/Rolldown inside a Bazel action sandbox breaks TanStack Start's
    route-level code splitting: the compiler plugin computes route filenames
    via `import.meta.url`, and the `node_modules/.aspect_rules_js/...` layout
    in the sandbox produces a 12-level-deep relative path that the plugin's
    chunk-split heuristic rejects, collapsing every route into a single 1.6 MB
    monolithic main bundle (vs. ~148 route-split chunks the source-tree build
    produces).

    The base app macro builds the small hermetic OTel preload bundle
    (`:instrumentation_bundle`) — `instrumentation.mts` plus its
    `@verself/nitro-plugins` deps, bundled by Rolldown. That action is
    genuinely sandbox-safe (no filesystem-walking plugin involved).

    `srcs` is kept as a filegroup so a future Bazel target (e.g. a typecheck
    test, an e2e harness) can reference the app's input set without
    re-globbing.
    """
    npm_link_all_packages()

    native.filegroup(
        name = "sources",
        srcs = native.glob(
            srcs,
            allow_empty = True,
        ),
    )

    # Bundle `instrumentation.mts` (the OTel preload) into a self-contained
    # `instrumentation.mjs` that runs without a node_modules tree on the host.
    # The driver runs Rolldown (same bundler `vp build` uses for the server
    # output) with `platform=node`, leaving Node built-ins external and
    # inlining everything else. The Nomad launcher loads it via Node's
    # `--import` flag.
    js_run_binary(
        name = "instrumentation_bundle",
        srcs = [
            "//src/viteplus-monorepo:workspace_config",
            ":node_modules",
            ":sources",
            "instrumentation.mts",
        ],
        outs = ["instrumentation.mjs"],
        args = [
            "--entry=instrumentation.mts",
            "--outfile=instrumentation.mjs",
        ],
        chdir = native.package_name(),
        progress_message = "Bundling %s OTel preload" % npm_name,
        tool = _INSTRUMENTATION_BUNDLER,
    )

def viteplus_node_runtime_artifact(name):
    native.genrule(
        name = name,
        srcs = [_NODEJS_ARCHIVE],
        outs = ["nodejs-runtime.tar"],
        cmd = """
set -euo pipefail
tmp="$$(mktemp -d)"
trap 'rm -rf "$$tmp"' EXIT
mkdir -p "$$tmp/runtime/nodejs"
tar -tf "$(location {nodejs_archive})" > "$$tmp/nodejs-members.txt"
node_member="$$(awk '/\\/bin\\/node$$/ {{ print; exit }}' "$$tmp/nodejs-members.txt")"
test -n "$$node_member"
tar -xJf "$(location {nodejs_archive})" -C "$$tmp" "$$node_member"
mkdir -p "$$tmp/runtime/nodejs/bin"
mv "$$tmp/$$node_member" "$$tmp/runtime/nodejs/bin/node"
rm -rf "$$tmp/$${{node_member%%/*}}"
rm -f "$$tmp/nodejs-members.txt"
chmod 0755 "$$tmp/runtime/nodejs/bin/node"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='UTC 2000-01-01' -cf "$@" -C "$$tmp" .
""".format(nodejs_archive = _NODEJS_ARCHIVE),
    )

def viteplus_node_app_artifact(name, output, migration_entry = None, migration_data = {}):
    package_dir = native.package_name()
    migration_bundle = "_" + name + "_migration_bundle"
    srcs = [
        ":instrumentation_bundle",
        ":sources",
        "//src/viteplus-monorepo:workspace_sources",
    ]
    migration_cmds = ""
    if migration_entry:
        js_run_binary(
            name = migration_bundle,
            srcs = [
                "//src/viteplus-monorepo:workspace_config",
                ":node_modules",
                ":sources",
                migration_entry,
            ],
            outs = [output + "-migration.mjs"],
            args = [
                "--entry=" + migration_entry,
                "--outfile=" + output + "-migration.mjs",
            ],
            chdir = native.package_name(),
            progress_message = "Bundling %s Nomad migration" % output,
            tool = _INSTRUMENTATION_BUNDLER,
        )
        srcs.append(":" + migration_bundle)
        srcs.extend(migration_data.keys())
        migration_data_copies = []
        for label, dest in migration_data.items():
            migration_data_copies.append("""
mkdir -p "$$tmp/app/$$(dirname "{dest}")"
cp "$$execroot/$(location {label})" "$$tmp/app/{dest}"
""".format(label = label, dest = dest))
        migration_cmds = """
cp "$$execroot/$(location :{migration_bundle})" "$$tmp/app/{output}-migration.mjs"
{migration_data_copies}
cat > "$$tmp/bin/{output}-migrate" <<'EOF'
#!/usr/bin/env sh
set -eu
script_dir=$$(CDPATH= cd -- "$$(dirname -- "$$0")" && pwd)
root=$$(CDPATH= cd -- "$$script_dir/.." && pwd)
exec "$$root/runtime/nodejs/bin/node" --import "$$root/app/instrumentation.mjs" "$$root/app/{output}-migration.mjs" "$$@"
EOF
chmod 0755 "$$tmp/bin/{output}-migrate"
""".format(
            migration_bundle = migration_bundle,
            migration_data_copies = "\n".join(migration_data_copies),
            output = output,
        )
    native.genrule(
        name = name,
        srcs = srcs,
        outs = [output + ".tar"],
        cmd = """
set -euo pipefail
execroot="$$(pwd)"
out="$$execroot/$@"
home="$$(getent passwd "$$(id -un)" | cut -d: -f6)"
test -n "$$home"
vp="$$home/.vite-plus/bin/vp"
test -x "$$vp"
tmp="$$(mktemp -d)"
trap 'rm -rf "$$tmp"' EXIT

cd "{package_dir}"
rm -rf .output
"$$vp" build

mkdir -p "$$tmp/app" "$$tmp/bin"
cp -a .output "$$tmp/app/.output"
cp "$$execroot/$(location :instrumentation_bundle)" "$$tmp/app/instrumentation.mjs"
cat > "$$tmp/bin/{output}" <<'EOF'
#!/usr/bin/env sh
set -eu
script_dir=$$(CDPATH= cd -- "$$(dirname -- "$$0")" && pwd)
root=$$(CDPATH= cd -- "$$script_dir/.." && pwd)
exec "$$root/runtime/nodejs/bin/node" --import "$$root/app/instrumentation.mjs" "$$root/app/.output/server/index.mjs" "$$@"
EOF
chmod 0755 "$$tmp/bin/{output}"
{migration_cmds}

tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='UTC 2000-01-01' -cf "$$out" -C "$$tmp" .
""".format(
            output = output,
            package_dir = package_dir,
            migration_cmds = migration_cmds,
        ),
        local = True,
        tags = [
            "local",
            "no-remote",
            "no-sandbox",
        ],
    )

def viteplus_openapi_clients(name, specs, openapi_ts_bin, plugin_packages = None):
    """Generates TypeScript SDKs from a set of OpenAPI specs via @hey-api/openapi-ts.

    Each entry in `specs` is a (sdk_dir, spec_label) pair. `sdk_dir` is the
    subdirectory under `src/__generated/` (e.g. "sandbox-rental-api"); `spec_label`
    is the Bazel label of the openapi-3.1.yaml file owned by the service.

    Outputs are emitted under bazel-out and then projected onto disk under
    `src/__generated/<sdk_dir>/` via write_source_files. The on-disk copies are
    the source of truth for IDE typecheck and the vite build glob; CI runs
    `bazel test //src/viteplus-monorepo/apps/<app>:<name>_check` to enforce
    that committed snapshots match the generator output.

    `openapi_ts_bin` must be the `bin` struct loaded from the consuming
    package's `@npm//<pkg>:@hey-api/openapi-ts/package_json.bzl`. `plugin_packages`
    are extra `:node_modules/<pkg>` labels openapi-ts plugins resolve at runtime
    (e.g. valibot). @hey-api/typescript and @hey-api/sdk are openapi-ts internals,
    not separate npm packages.
    """
    if plugin_packages == None:
        plugin_packages = []

    file_map = {}
    for sdk_dir, spec_label in specs:
        sdk_token = sdk_dir.replace("-", "_")
        raw_target = "%s_%s_raw" % (name, sdk_token)
        gen_target = "%s_%s_gen" % (name, sdk_token)
        out_dir = "src/__generated/%s" % sdk_dir
        raw_dir = "_openapi_raw/%s" % sdk_dir

        # js_binary chdir's to BAZEL_BINDIR at runtime, so the spec path must
        # be BAZEL_BINDIR-relative ($(rootpath ...) returns the workspace-rooted
        # source path of a copy_to_bin'd file, which lives directly under
        # BAZEL_BINDIR). The output path is BAZEL_BINDIR-relative too, so we
        # prefix it with the consuming package and the declared out_dir.
        openapi_ts_bin.openapi_ts(
            name = raw_target,
            srcs = [spec_label] + plugin_packages,
            out_dirs = [raw_dir],
            args = [
                "--input",
                "$(rootpath %s)" % spec_label,
                "--output",
                "%s/%s" % (native.package_name(), raw_dir),
                "--plugins",
                "@hey-api/typescript",
                "@hey-api/sdk",
                "valibot",
            ],
            mnemonic = "OpenapiTsGen",
            progress_message = "Generating %s SDK (raw)" % sdk_dir,
        )

        # Post-process: prepend `// @ts-nocheck` to every emitted .ts file so
        # `vp run typecheck` doesn't choke on generated code.
        js_run_binary(
            name = gen_target,
            srcs = [":" + raw_target],
            out_dirs = [out_dir],
            args = [
                "%s/%s" % (native.package_name(), raw_dir),
                "%s/%s" % (native.package_name(), out_dir),
            ],
            tool = "//src/viteplus-monorepo/scripts:openapi_postprocess",
            mnemonic = "OpenapiTsPostprocess",
            progress_message = "Stamping ts-nocheck on %s SDK" % sdk_dir,
        )
        file_map[out_dir] = ":" + gen_target

    write_source_files(
        name = name,
        files = file_map,
    )

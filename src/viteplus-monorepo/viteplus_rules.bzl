load("@aspect_rules_js//js:defs.bzl", "js_run_binary")
load("@aspect_rules_js//npm:defs.bzl", "npm_package")
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@npm//:defs.bzl", "npm_link_all_packages")

_VITEPLUS_TOOL = "//src/viteplus-monorepo:vp"
_APP_ENTRY_BUNDLER = "//src/viteplus-monorepo/scripts:bundle_app_entry"

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
    """Bazel surface for a viteplus app — deliberately narrow.

    Bazel does NOT run `vp build` for the app. Putting Vite/Rolldown inside a
    Bazel action sandbox breaks TanStack Start's route-level code splitting:
    the compiler plugin computes route filenames via `import.meta.url`, and
    the `node_modules/.aspect_rules_js/...` layout in the sandbox produces a
    12-level-deep relative path that the plugin's chunk-split heuristic
    rejects, collapsing every route into a single 1.6 MB monolithic main
    bundle (vs. ~148 route-split chunks the source-tree build produces).

    Bazel's job here is to build the small hermetic OTel preload bundle
    (`:app_entry_bundle`) — `app-entry.mts` plus its `@verself/nitro-plugins`
    deps, bundled by Rolldown. That action is genuinely sandbox-safe (no
    filesystem-walking plugin involved).

    The full deploy tarball (`.output/{public,server}/` + `app-entry.mjs`)
    is composed by the Ansible verself_web/company roles: the role runs
    `vp build` in the source tree, then `bazelisk build :app_entry_bundle`,
    then `tar -cf` over both, then ships the tarball to the host.

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

    # Bundle the single app entry (`app-entry.mts` — OTel preload + dynamic
    # import of the Nitro server bundle) so the deploy tarball runs without a
    # node_modules tree. The driver runs Rolldown (same bundler `vp build`
    # uses for the server output) with `platform=node`, leaving Node built-ins
    # external and inlining everything else.
    #
    # The entry is named `app-entry.mts` (not `server.mts`) deliberately —
    # TanStack Start auto-discovers root-level `server.{ts,mts}` as an SSR
    # hook and pulls it into the Nitro server bundle, which would defeat the
    # preload-then-server ordering and trip Nitro's dependency tracer over
    # OTel's `require-in-the-middle`.
    js_run_binary(
        name = "app_entry_bundle",
        srcs = [
            ":node_modules",
            "app-entry.mts",
        ],
        outs = ["app-entry.mjs"],
        args = [
            "--entry=app-entry.mts",
            "--outfile=app-entry.mjs",
        ],
        chdir = native.package_name(),
        progress_message = "Bundling %s app entry" % npm_name,
        tool = _APP_ENTRY_BUNDLER,
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

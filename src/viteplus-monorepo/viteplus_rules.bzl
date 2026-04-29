load("@aspect_rules_js//js:defs.bzl", "js_run_binary")
load("@aspect_rules_js//npm:defs.bzl", "npm_package")
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@npm//:defs.bzl", "npm_link_all_packages")
load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

_APP_EXCLUDES = [
    ".output/**",
    "node_modules/**",
    "playwright-report/**",
    "test-results/**",
]

_VITEPLUS_WORKSPACE_CONFIG = "//src/viteplus-monorepo:workspace_config"
_VITEPLUS_TOOL = "//src/viteplus-monorepo:vp"
_APP_ENTRY_BUNDLER = "//src/viteplus-monorepo/scripts:bundle_app_entry"

def viteplus_source_package(npm_name, srcs):
    """Links a source-first workspace package into the rules_js npm graph."""
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

def viteplus_packed_package(npm_name, srcs, entries, dts = False):
    """Builds a dist-first workspace package and exposes it to npm_link_all_packages()."""
    npm_link_all_packages()

    native.filegroup(
        name = "sources",
        srcs = native.glob(srcs),
    )

    js_run_binary(
        name = "dist",
        srcs = [
            ":node_modules",
            ":sources",
            _VITEPLUS_WORKSPACE_CONFIG,
        ],
        out_dirs = ["dist"],
        args = ["pack"] + entries + (["--dts"] if dts else []),
        chdir = native.package_name(),
        progress_message = "Packing %s" % npm_name,
        tool = _VITEPLUS_TOOL,
    )

    # rules_js does not peer-resolve first-party npm_package outputs; packed
    # packages must list runtime imports in package.json dependencies.
    npm_package(
        name = "pkg",
        srcs = [
            ":dist",
            ":sources",
        ],
        package = npm_name,
        version = "0.0.0",
    )

def viteplus_app(npm_name, srcs):
    """Builds a TanStack Start app .output tree and deployable tarball."""
    npm_link_all_packages()

    native.filegroup(
        name = "sources",
        srcs = native.glob(
            srcs,
            exclude = _APP_EXCLUDES,
            allow_empty = True,
        ),
    )

    js_run_binary(
        name = "output",
        srcs = [
            ":node_modules",
            ":sources",
            _VITEPLUS_WORKSPACE_CONFIG,
        ],
        out_dirs = [".output"],
        args = ["build"],
        chdir = native.package_name(),
        progress_message = "Building %s" % npm_name,
        tool = _VITEPLUS_TOOL,
    )

    # Bundle the single app entry (`app-entry.mts` — OTel preload + dynamic
    # import of the Nitro server bundle) as a sibling file so the deploy
    # tarball runs without a node_modules tree. systemd runs `node ./app-entry.mjs`
    # and nothing else; the OTel-preload-then-server ordering lives in JS via
    # top-level await. The driver runs Rolldown (same bundler `vp build` uses
    # for the server output) with `platform=node`, leaving Node built-ins
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

    pkg_tar(
        name = "deploy_bundle",
        srcs = [
            ":output",
            ":app_entry_bundle",
        ],
        package_dir = ".",
        strip_prefix = ".",
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

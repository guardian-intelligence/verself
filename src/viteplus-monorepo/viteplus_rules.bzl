load("@aspect_rules_js//js:defs.bzl", "js_run_binary")
load("@aspect_rules_js//npm:defs.bzl", "npm_package")
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

    pkg_tar(
        name = "deploy_bundle",
        srcs = [
            ":output",
            "instrumentation.mts",
        ],
        package_dir = ".",
        strip_prefix = ".",
    )

"""Catalog of dev-tools binaries packaged into the controller dev_tools archive.

Each spec list maps an upstream artifact (`@dev_tool_*//file`) to the install
location inside the archive's tar layout. `dev_tools_archive()` fans these
specs out into per-tool genrules and a single `pkg_tar` whose output gets
unpacked into the controller image at provisioning time.
"""

load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

TAR_SINGLE_BINARIES = [
    ("shellcheck", "@dev_tool_shellcheck//file", "J", "shellcheck-v0.11.0/shellcheck", "usr/local/bin/shellcheck"),
    ("stripe", "@dev_tool_stripe//file", "z", "stripe", "usr/local/bin/stripe"),
    ("clickhouse", "@dev_tool_clickhouse//file", "z", "clickhouse-common-static-26.3.2.3/usr/bin/clickhouse", "usr/local/bin/clickhouse"),
    ("grype", "@dev_tool_grype//file", "z", "grype", "usr/local/bin/grype"),
    ("syft", "@dev_tool_syft//file", "z", "syft", "usr/local/bin/syft"),
]

ZIP_SINGLE_BINARIES = [
    ("tofu", "@dev_tool_tofu//file", "tofu", "usr/local/bin/tofu"),
    ("protoc_bin", "@dev_tool_protoc//file", "bin/protoc", "usr/local/bin/protoc"),
]

ZIP_DIRECTORY_INSTALLS = [
    ("protoc_include", "@dev_tool_protoc//file", "include", "usr/local/include"),
]

TAR_MULTI_BINARIES = [
    ("age", "@dev_tool_age//file", "z", 1, [("age", "usr/local/bin/age"), ("age-keygen", "usr/local/bin/age-keygen")]),
    ("uv", "@dev_tool_uv//file", "z", 1, [("uv", "usr/local/bin/uv"), ("uvx", "usr/local/bin/uvx")]),
]

ARCHIVE_DIRECTORIES = [
    ("go_install", "@dev_tool_go//file", "z", "usr/local/go-1.25.8", 1),
    ("zig_install", "@dev_tool_zig//file", "J", "usr/local/zig-0.15.2", 1),
]

RAW_BINARY_SPECS = [
    ("buf", "@dev_tool_buf//file", "usr/local/bin/buf"),
    ("buildifier", "@dev_tool_buildifier//file", "usr/local/bin/buildifier"),
    ("cdxgen", "@dev_tool_cdxgen//file", "usr/local/bin/cdxgen"),
    ("jq", "@dev_tool_jq//file", "usr/local/bin/jq"),
    ("sops", "@dev_tool_sops//file", "usr/local/bin/sops"),
    ("osv_scanner", "@dev_tool_osv_scanner//file", "usr/local/bin/osv-scanner"),
    ("agent_browser", "@dev_tool_agent_browser//file", "usr/local/bin/agent-browser"),
]

SOURCE_BUILT_GO_BINARIES = [
    ("go_gofumpt", "@cc_mvdan_gofumpt//:gofumpt", "usr/local/bin/gofumpt"),
    ("go_golangci_lint", "@com_github_golangci_golangci_lint_v2//cmd/golangci-lint:golangci-lint", "usr/local/bin/golangci-lint"),
    ("go_gosec", "@com_github_securego_gosec_v2//cmd/gosec:gosec", "usr/local/bin/gosec"),
    ("go_protoc_gen_go", "@org_golang_google_protobuf//cmd/protoc-gen-go:protoc-gen-go", "usr/local/bin/protoc-gen-go"),
    ("go_protoc_gen_go_grpc", "@org_golang_google_grpc_cmd_protoc_gen_go_grpc//:protoc-gen-go-grpc", "usr/local/bin/protoc-gen-go-grpc"),
    ("go_sqlc", "@com_github_sqlc_dev_sqlc//cmd/sqlc:sqlc", "usr/local/bin/sqlc"),
]

DEV_TOOL_DEPS = [
    ":dev_tools_age",
    ":dev_tools_agent_browser",
    ":dev_tools_buf",
    ":dev_tools_buildifier",
    ":dev_tools_cdxgen",
    ":dev_tools_clickhouse",
    ":dev_tools_go_gofumpt",
    ":dev_tools_go_golangci_lint",
    ":dev_tools_go_gosec",
    ":dev_tools_grype",
    ":dev_tools_go_install",
    ":dev_tools_go_protoc_gen_go",
    ":dev_tools_go_protoc_gen_go_grpc",
    ":dev_tools_go_sqlc",
    ":dev_tools_jq",
    ":dev_tools_osv_scanner",
    ":dev_tools_protoc_bin",
    ":dev_tools_protoc_include",
    ":dev_tools_shellcheck",
    ":dev_tools_sops",
    ":dev_tools_stripe",
    ":dev_tools_syft",
    ":dev_tools_tofu",
    ":dev_tools_uv",
    ":dev_tools_zig_install",
]

DEV_TOOL_SYMLINKS = {
    "usr/local/bin/zig": "/usr/local/zig/zig",
    "usr/local/go": "/usr/local/go-1.25.8",
    "usr/local/zig": "/usr/local/zig-0.15.2",
}

_DEV_TOOLS_PREFIX = "dev_tools_"

def _tar_fragment(name, src, cmd):
    native.genrule(
        name = _DEV_TOOLS_PREFIX + name,
        srcs = [src],
        outs = [_DEV_TOOLS_PREFIX + name + ".tar"],
        cmd = """
set -euo pipefail
tmp="$$(mktemp -d)"
trap 'rm -rf "$$tmp"' EXIT
{cmd}
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='UTC 2000-01-01' -cf "$@" -C "$$tmp" .
""".format(cmd = cmd),
    )

def _tar_single_binary(name, src, tar_flag, binary, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract"
tar -x{tar_flag}f "$(location {src})" -C "$$tmp/extract"
install -D -m 0755 "$$tmp/extract"/{binary} "$$tmp/{dest}"
rm -rf "$$tmp/extract"
""".format(
            binary = binary,
            dest = dest,
            src = src,
            tar_flag = tar_flag,
        ),
    )

def _zip_single_binary(name, src, binary, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract"
unzip -q "$(location {src})" -d "$$tmp/extract"
install -D -m 0755 "$$tmp/extract"/{binary} "$$tmp/{dest}"
rm -rf "$$tmp/extract"
""".format(
            binary = binary,
            dest = dest,
            src = src,
        ),
    )

def _zip_directory_install(name, src, src_sub, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract" "$$tmp/{dest}"
unzip -q "$(location {src})" -d "$$tmp/extract"
cp -r "$$tmp/extract/{src_sub}/." "$$tmp/{dest}/"
rm -rf "$$tmp/extract"
""".format(
            dest = dest,
            src = src,
            src_sub = src_sub,
        ),
    )

def _tar_multi_binary(name, src, tar_flag, strip_components, binaries):
    install_lines = []
    for member, dest in binaries:
        install_lines.append('install -D -m 0755 "$$tmp/extract/{member}" "$$tmp/{dest}"'.format(
            dest = dest,
            member = member,
        ))
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract"
tar -x{tar_flag}f "$(location {src})" -C "$$tmp/extract" --strip-components={strip_components}
{installs}
rm -rf "$$tmp/extract"
""".format(
            installs = "\n".join(install_lines),
            src = src,
            strip_components = strip_components,
            tar_flag = tar_flag,
        ),
    )

def _archive_directory(name, src, tar_flag, dest, strip_components):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/{dest}"
tar -x{tar_flag}f "$(location {src})" -C "$$tmp/{dest}" --strip-components={strip_components}
""".format(
            dest = dest,
            src = src,
            strip_components = strip_components,
            tar_flag = tar_flag,
        ),
    )

def _raw_binary(name, src, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
install -D -m 0755 "$(location {src})" "$$tmp/{dest}"
""".format(
            dest = dest,
            src = src,
        ),
    )

def _source_built_go_binary(name, src, dest):
    # Source-built Go binaries (rules_go go_binary outputs) are dropped
    # into the tar in exactly the same shape as raw http_files: install
    # the single executable at tmp/dest and let pkg_tar collect it. The
    # src here is a Bazel label such as @<repo>//cmd/<tool>:<tool> rather
    # than @<repo>//file, so the genrule srcs attribute resolves the
    # go_binary compiled output instead of an http_file payload.
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
install -D -m 0755 "$(location {src})" "$$tmp/{dest}"
""".format(
            dest = dest,
            src = src,
        ),
    )

def dev_tools_archive(name = "dev_tools_archive"):
    """Fan out the dev-tools spec lists into per-tool genrules and the top-level pkg_tar.

    Args:
      name: name of the top-level `pkg_tar` target. Defaults to `dev_tools_archive`.
    """
    for tool, src, tar_flag, binary, dest in TAR_SINGLE_BINARIES:
        _tar_single_binary(
            name = tool,
            src = src,
            tar_flag = tar_flag,
            binary = binary,
            dest = dest,
        )

    for tool, src, binary, dest in ZIP_SINGLE_BINARIES:
        _zip_single_binary(
            name = tool,
            src = src,
            binary = binary,
            dest = dest,
        )

    for tool, src, src_sub, dest in ZIP_DIRECTORY_INSTALLS:
        _zip_directory_install(
            name = tool,
            src = src,
            src_sub = src_sub,
            dest = dest,
        )

    for tool, src, tar_flag, strip_components, binaries in TAR_MULTI_BINARIES:
        _tar_multi_binary(
            name = tool,
            src = src,
            tar_flag = tar_flag,
            strip_components = strip_components,
            binaries = binaries,
        )

    for tool, src, tar_flag, dest, strip_components in ARCHIVE_DIRECTORIES:
        _archive_directory(
            name = tool,
            src = src,
            tar_flag = tar_flag,
            dest = dest,
            strip_components = strip_components,
        )

    for tool, src, dest in RAW_BINARY_SPECS:
        _raw_binary(
            name = tool,
            src = src,
            dest = dest,
        )

    for tool, src, dest in SOURCE_BUILT_GO_BINARIES:
        _source_built_go_binary(
            name = tool,
            src = src,
            dest = dest,
        )

    pkg_tar(
        name = name,
        out = "dev_tools.tar.zst",
        compressor = "//src/tools/dev/cmd/zstd-compressor:zstd-compressor",
        deps = DEV_TOOL_DEPS,
        extension = "tar.zst",
        symlinks = DEV_TOOL_SYMLINKS,
    )

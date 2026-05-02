load("@rules_pkg//pkg:tar.bzl", "pkg_tar")
load("@rules_shell//shell:sh_binary.bzl", "sh_binary")

PROFILE_BIN = "opt/verself/profile/bin"
GRAFANA_CLICKHOUSE_DATASOURCE_VERSION = "4.14.1"

TAR_SINGLE_BINARIES = [
    ("clickhouse", "@server_tool_clickhouse//file", "z", "clickhouse-common-static-*/usr/bin/clickhouse", "clickhouse"),
    ("zitadel", "@server_tool_zitadel//file", "z", "zitadel-linux-amd64/zitadel", "zitadel"),
    ("spire_server", "@server_tool_spire//file", "z", "spire-*/bin/spire-server", "spire-server"),
    ("spire_agent", "@server_tool_spire//file", "z", "spire-*/bin/spire-agent", "spire-agent"),
    ("spiffe_helper", "@server_tool_spiffe_helper//file", "z", "spiffe-helper", "spiffe-helper"),
    ("nats_server", "@server_tool_nats_server//file", "z", "nats-server-*-linux-amd64/nats-server", "nats-server"),
    ("otelcol_contrib", "@server_tool_otelcol_contrib//file", "z", "otelcol-contrib", "otelcol-contrib"),
    ("temporal_server", "@server_tool_temporal//file", "z", "temporal-server", "temporal-server"),
    ("temporal_sql_tool", "@server_tool_temporal//file", "z", "temporal-sql-tool", "temporal-sql-tool"),
    ("tdbg", "@server_tool_temporal//file", "z", "tdbg", "tdbg"),
    ("stalwart", "@server_tool_stalwart//file", "z", "stalwart", "stalwart"),
    ("stalwart_cli", "@server_tool_stalwart_cli//file", "z", "stalwart-cli", "stalwart-cli"),
    ("containerd", "@server_tool_containerd//file", "z", "bin/containerd", "containerd"),
    ("caddy", "@server_tool_caddy//file", "z", "caddy", "caddy"),
]

ZIP_SINGLE_BINARIES = [
    ("tigerbeetle", "@server_tool_tigerbeetle//file", "tigerbeetle", "tigerbeetle"),
    ("nomad", "@server_tool_nomad//file", "nomad", "nomad"),
]

DEB_BINARY_SPECS = [
    ("openbao", "@server_tool_openbao//file", "usr/bin/bao", "bao"),
]

RAW_BINARY_SPECS = [
    ("garage", "@server_tool_garage//file", "garage"),
    ("forgejo", "@server_tool_forgejo//file", "forgejo"),
    ("bazel_remote", "@server_tool_bazel_remote//file", "bazel-remote"),
]

ARCHIVE_DIRECTORIES = [
    ("grafana", "@server_tool_grafana//file", "z", "opt/verself/grafana"),
    ("nodejs", "@server_tool_nodejs//file", "J", "opt/verself/nodejs"),
]

SERVER_TOOL_DEPS = [
    ":bazel_remote",
    ":caddy",
    ":clickhouse",
    ":containerd",
    ":forgejo",
    ":garage",
    ":grafana",
    ":grafana_clickhouse_datasource",
    ":grafana_clickhouse_datasource_version",
    ":nats_server",
    ":nodejs",
    ":nomad",
    ":openbao",
    ":otelcol_contrib",
    ":spiffe_helper",
    ":spire_agent",
    ":spire_server",
    ":stalwart",
    ":stalwart_cli",
    ":tdbg",
    ":temporal_server",
    ":temporal_sql_tool",
    ":tigerbeetle",
    ":zitadel",
]

HOST_GO_TOOLS = [
    ("//src/temporal-platform/cmd/temporal-bootstrap:temporal-bootstrap", "temporal-bootstrap"),
    ("//src/temporal-platform/cmd/temporal-schema:temporal-schema", "temporal-schema"),
    ("//src/temporal-platform/cmd/verself-temporal-server:verself-temporal-server", "verself-temporal-server"),
    ("//src/vm-orchestrator/cmd/vm-orchestrator:vm-orchestrator", "vm-orchestrator"),
    ("//src/vm-orchestrator/cmd/vm-orchestrator-cli:vm-orchestrator-cli", "vm-orchestrator-cli"),
]

SERVER_TOOL_SYMLINKS = {
    "opt/verself/profile/bin/clickhouse-benchmark": "/opt/verself/profile/bin/clickhouse",
    "opt/verself/profile/bin/clickhouse-client": "/opt/verself/profile/bin/clickhouse",
    "opt/verself/profile/bin/clickhouse-keeper": "/opt/verself/profile/bin/clickhouse",
    "opt/verself/profile/bin/clickhouse-local": "/opt/verself/profile/bin/clickhouse",
    "opt/verself/profile/bin/clickhouse-server": "/opt/verself/profile/bin/clickhouse",
    "opt/verself/profile/bin/corepack": "/opt/verself/nodejs/bin/corepack",
    "opt/verself/profile/bin/grafana": "/opt/verself/grafana/bin/grafana",
    "opt/verself/profile/bin/node": "/opt/verself/nodejs/bin/node",
    "opt/verself/profile/bin/npm": "/opt/verself/nodejs/bin/npm",
    "opt/verself/profile/bin/npx": "/opt/verself/nodejs/bin/npx",
}

def _tar_fragment(name, src, cmd):
    native.genrule(
        name = name,
        srcs = [src],
        outs = [name + ".tar"],
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
mkdir -p "$$tmp/extract" "$$tmp/{profile_bin}"
tar -x{tar_flag}f "$(location {src})" -C "$$tmp/extract"
install -m 0755 "$$tmp/extract"/{binary} "$$tmp/{profile_bin}/{dest}"
rm -rf "$$tmp/extract"
""".format(
            binary = binary,
            dest = dest,
            profile_bin = PROFILE_BIN,
            src = src,
            tar_flag = tar_flag,
        ),
    )

def _zip_single_binary(name, src, binary, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract" "$$tmp/{profile_bin}"
unzip -q "$(location {src})" -d "$$tmp/extract"
install -m 0755 "$$tmp/extract"/{binary} "$$tmp/{profile_bin}/{dest}"
rm -rf "$$tmp/extract"
""".format(
            binary = binary,
            dest = dest,
            profile_bin = PROFILE_BIN,
            src = src,
        ),
    )

def _package_deb_member(name, src, binary, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract" "$$tmp/{profile_bin}"
dpkg-deb -x "$(location {src})" "$$tmp/extract"
install -m 0755 "$$tmp/extract"/{binary} "$$tmp/{profile_bin}/{dest}"
rm -rf "$$tmp/extract"
""".format(
            binary = binary,
            dest = dest,
            profile_bin = PROFILE_BIN,
            src = src,
        ),
    )

def _package_raw_file(name, src, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
install -D -m 0755 "$(location {src})" "$$tmp/{profile_bin}/{dest}"
""".format(
            dest = dest,
            profile_bin = PROFILE_BIN,
            src = src,
        ),
    )

def _archive_directory(name, src, tar_flag, dest, strip_components = 1):
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

def _zip_directory(name, src, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/{dest}"
unzip -q "$(location {src})" -d "$$tmp/{dest}"
""".format(
            dest = dest,
            src = src,
        ),
    )

def _grafana_clickhouse_datasource_version():
    _tar_fragment(
        name = "grafana_clickhouse_datasource_version",
        src = "@server_tool_grafana_clickhouse_datasource//file",
        cmd = """
mkdir -p "$$tmp/var/lib/grafana/plugins"
printf '{version}\\n' > "$$tmp/var/lib/grafana/plugins/.grafana-clickhouse-datasource-version"
""".format(version = GRAFANA_CLICKHOUSE_DATASOURCE_VERSION),
    )

def server_tools_archive():
    sh_binary(
        name = "zstd_compressor",
        srcs = ["//bazel/tools:zstd-compressor.sh"],
    )

    for name, src, tar_flag, binary, dest in TAR_SINGLE_BINARIES:
        _tar_single_binary(
            name = name,
            src = src,
            tar_flag = tar_flag,
            binary = binary,
            dest = dest,
        )

    for name, src, binary, dest in ZIP_SINGLE_BINARIES:
        _zip_single_binary(
            name = name,
            src = src,
            binary = binary,
            dest = dest,
        )

    for name, src, binary, dest in DEB_BINARY_SPECS:
        _package_deb_member(
            name = name,
            src = src,
            binary = binary,
            dest = dest,
        )

    for name, src, dest in RAW_BINARY_SPECS:
        _package_raw_file(
            name = name,
            src = src,
            dest = dest,
        )

    for name, src, tar_flag, dest in ARCHIVE_DIRECTORIES:
        _archive_directory(
            name = name,
            src = src,
            tar_flag = tar_flag,
            dest = dest,
        )

    _zip_directory(
        name = "grafana_clickhouse_datasource",
        src = "@server_tool_grafana_clickhouse_datasource//file",
        dest = "var/lib/grafana/plugins",
    )
    _grafana_clickhouse_datasource_version()

    pkg_tar(
        name = "server_tools_archive",
        out = "server_tools.tar.zst",
        compressor = ":zstd_compressor",
        deps = SERVER_TOOL_DEPS,
        extension = "tar.zst",
        symlinks = SERVER_TOOL_SYMLINKS,
    )

def substrate_go_tools_archive():
    files = {}
    modes = {}
    for label, output in HOST_GO_TOOLS:
        dest = "opt/verself/profile/bin/" + output
        files[label] = dest
        modes[dest] = "0755"

    pkg_tar(
        name = "substrate_go_tools",
        out = "substrate_go_tools.tar",
        files = files,
        modes = modes,
        visibility = ["//visibility:public"],
    )

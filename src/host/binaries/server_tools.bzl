"""Catalog of server-tool binaries packaged into the bare-metal node archive.

Each spec list maps an upstream artifact (`@server_tool_*//file`) to the
install location under `opt/verself/profile/bin/`. `server_tools_archive()`
fans the specs out into per-tool genrules and one `pkg_tar` that the host
configuration playbook unpacks during convergence.
"""

load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

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
    ("pomerium", "@server_tool_pomerium//file", "z", "pomerium", "pomerium"),
    ("spicedb", "@server_tool_spicedb//file", "z", "spicedb", "spicedb"),
    ("zed", "@server_tool_zed//file", "z", "zed", "zed"),
    ("containerd", "@server_tool_containerd//file", "z", "bin/containerd", "containerd"),
    # Electric runs OCI images through `ctr --runtime io.containerd.runc.v2`.
    # containerd starts this shim per container; systemd never manages it.
    ("containerd_shim_runc_v2", "@server_tool_containerd//file", "z", "bin/containerd-shim-runc-v2", "containerd-shim-runc-v2"),
    ("ctr", "@server_tool_containerd//file", "z", "bin/ctr", "ctr"),
    ("lego", "@server_tool_lego//file", "z", "lego", "lego"),
]

ZIP_SINGLE_BINARIES = [
    ("tigerbeetle", "@server_tool_tigerbeetle//file", "tigerbeetle", "tigerbeetle"),
    ("nomad", "@server_tool_nomad//file", "nomad", "nomad"),
]

DEB_BINARY_SPECS = [
    ("haproxy", "@server_tool_haproxy_awslc//file", "usr/sbin/haproxy", "haproxy"),
    ("openbao", "@server_tool_openbao//file", "usr/bin/bao", "bao"),
]

DEB_DIRECTORY_SPECS = [
    ("haproxy_awslc_libs", "@server_tool_libssl_awslc//file", "opt/aws-lc", "opt/aws-lc"),
]

RAW_BINARY_SPECS = [
    ("garage", "@server_tool_garage//file", "garage"),
    ("forgejo", "@server_tool_forgejo//file", "forgejo"),
    ("zot", "@server_tool_zot//file", "zot"),
]

RAW_FILE_SPECS = [
    ("stalwart_webadmin_resource", "@server_tool_stalwart_webadmin//file", "var/lib/stalwart/webadmin.zip"),
    ("stalwart_spam_filter_resource", "@server_tool_stalwart_spam_filter//file", "var/lib/stalwart/spam-filter.toml"),
]

ARCHIVE_DIRECTORIES = [
    ("grafana", "@server_tool_grafana//file", "z", "opt/verself/grafana"),
    ("nodejs", "@server_tool_nodejs//file", "J", "opt/verself/nodejs"),
]

SERVER_TOOL_DEPS = [
    ":clickhouse",
    ":containerd",
    ":containerd_shim_runc_v2",
    ":ctr",
    ":forgejo",
    ":garage",
    ":grafana",
    ":grafana_clickhouse_datasource",
    ":grafana_clickhouse_datasource_version",
    ":haproxy",
    ":haproxy_awslc_libs",
    ":lego",
    ":nats_server",
    ":nodejs",
    ":nomad",
    ":openbao",
    ":otelcol_contrib",
    ":pomerium",
    ":spicedb",
    ":spiffe_helper",
    ":spire_agent",
    ":spire_server",
    ":stalwart",
    ":stalwart_cli",
    ":stalwart_spam_filter_resource",
    ":stalwart_webadmin_resource",
    ":tdbg",
    ":temporal_server",
    ":temporal_sql_tool",
    ":tigerbeetle",
    "//src/components/verdaccio:verdaccio_runtime",
    ":zed",
    ":zitadel",
    ":zot",
]

PROFILE_GO_TOOLS = [
    ("//src/components/haproxy/cmd/haproxy-lego-renew:haproxy-lego-renew", "haproxy-lego-renew"),
    ("//src/components/haproxy/cmd/haproxy-upstreams-apply:haproxy-upstreams-apply", "haproxy-upstreams-apply"),
    ("//src/components/zot/cmd/zot-htpasswd:zot-htpasswd", "zot-htpasswd"),
    ("//src/components/temporal-platform/cmd/temporal-bootstrap:temporal-bootstrap", "temporal-bootstrap"),
    ("//src/components/temporal-platform/cmd/temporal-schema:temporal-schema", "temporal-schema"),
    ("//src/components/temporal-platform/cmd/verself-temporal-server:verself-temporal-server", "verself-temporal-server"),
    ("//src/substrate/vm-orchestrator/cmd/vm-orchestrator:vm-orchestrator", "vm-orchestrator"),
    ("//src/substrate/vm-orchestrator/cmd/vm-orchestrator-cli:vm-orchestrator-cli", "vm-orchestrator-cli"),
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

def _package_deb_directory(name, src, source_dir, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
mkdir -p "$$tmp/extract" "$$tmp/{dest}"
dpkg-deb -x "$(location {src})" "$$tmp/extract"
cp -a "$$tmp/extract"/{source_dir}/. "$$tmp/{dest}/"
rm -rf "$$tmp/extract"
""".format(
            dest = dest,
            source_dir = source_dir,
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

def _package_data_file(name, src, dest):
    _tar_fragment(
        name = name,
        src = src,
        cmd = """
install -D -m 0644 "$(location {src})" "$$tmp/{dest}"
""".format(
            dest = dest,
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

def server_tools_archive(name = "server_tools_archive"):
    """Fan out the server-tool spec lists into per-tool genrules and the top-level pkg_tar.

    Args:
      name: name of the top-level `pkg_tar` target. Defaults to `server_tools_archive`.
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

    for tool, src, binary, dest in DEB_BINARY_SPECS:
        _package_deb_member(
            name = tool,
            src = src,
            binary = binary,
            dest = dest,
        )

    for tool, src, source_dir, dest in DEB_DIRECTORY_SPECS:
        _package_deb_directory(
            name = tool,
            src = src,
            source_dir = source_dir,
            dest = dest,
        )

    for tool, src, dest in RAW_BINARY_SPECS:
        _package_raw_file(
            name = tool,
            src = src,
            dest = dest,
        )

    for tool, src, dest in RAW_FILE_SPECS:
        _package_data_file(
            name = tool,
            src = src,
            dest = dest,
        )

    for tool, src, tar_flag, dest in ARCHIVE_DIRECTORIES:
        _archive_directory(
            name = tool,
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
        name = name,
        out = "server_tools.tar.zst",
        compressor = "//src/tools/dev/cmd/zstd-compressor:zstd-compressor",
        deps = SERVER_TOOL_DEPS,
        extension = "tar.zst",
        symlinks = SERVER_TOOL_SYMLINKS,
    )

def substrate_go_tools_archive(name = "substrate_go_tools"):
    """Bundle Go binaries installed into the server profile for Ansible installation.

    Args:
      name: name of the produced `pkg_tar` target. Defaults to `substrate_go_tools`.
    """
    files = {}
    modes = {}
    for label, output in PROFILE_GO_TOOLS:
        dest = "opt/verself/profile/bin/" + output
        files[label] = dest
        modes[dest] = "0755"

    pkg_tar(
        name = name,
        out = "substrate_go_tools.tar.zst",
        compressor = "//src/tools/dev/cmd/zstd-compressor:zstd-compressor",
        extension = "tar.zst",
        files = files,
        modes = modes,
        visibility = ["//visibility:public"],
    )

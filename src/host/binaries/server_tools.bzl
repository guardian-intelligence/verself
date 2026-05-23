"""Host tool archives assembled from the rules_multitool catalog."""

load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

PROFILE_BIN = "opt/verself/profile/bin"
GRAFANA_CLICKHOUSE_DATASOURCE_VERSION = "4.14.1"
POSTGRESQL_MAJOR_VERSION = "16"
POSTGRESQL_RUNTIME_ROOT = "opt/verself/postgresql"

SERVER_TOOL_BINARY_RENAMES = {
    "@server_tools//tools/clickhouse": PROFILE_BIN + "/clickhouse",
    "@server_tools//tools/containerd": PROFILE_BIN + "/containerd",
    "@server_tools//tools/containerd-shim-runc-v2": PROFILE_BIN + "/containerd-shim-runc-v2",
    "@server_tools//tools/ctr": PROFILE_BIN + "/ctr",
    "@server_tools//tools/forgejo": PROFILE_BIN + "/forgejo",
    "@server_tools//tools/garage": PROFILE_BIN + "/garage",
    "@server_tools//tools/lego": PROFILE_BIN + "/lego",
    "@server_tools//tools/nats-server": PROFILE_BIN + "/nats-server",
    "@server_tools//tools/nomad": PROFILE_BIN + "/nomad",
    "@server_tools//tools/otelcol-contrib": PROFILE_BIN + "/otelcol-contrib",
    "@server_tools//tools/pomerium": PROFILE_BIN + "/pomerium",
    "@server_tools//tools/runc": PROFILE_BIN + "/runc",
    "@server_tools//tools/spicedb": PROFILE_BIN + "/spicedb",
    "@server_tools//tools/spiffe-helper": PROFILE_BIN + "/spiffe-helper",
    "@server_tools//tools/spire-agent": PROFILE_BIN + "/spire-agent",
    "@server_tools//tools/spire-server": PROFILE_BIN + "/spire-server",
    "@server_tools//tools/stalwart": PROFILE_BIN + "/stalwart",
    "@server_tools//tools/stalwart-cli": PROFILE_BIN + "/stalwart-cli",
    "@server_tools//tools/tdbg": PROFILE_BIN + "/tdbg",
    "@server_tools//tools/temporal-schema": PROFILE_BIN + "/temporal-sql-tool",
    "@server_tools//tools/temporal-server": PROFILE_BIN + "/temporal-server",
    "@server_tools//tools/tigerbeetle": PROFILE_BIN + "/tigerbeetle",
    "@server_tools//tools/zed": PROFILE_BIN + "/zed",
    "@server_tools//tools/zitadel": PROFILE_BIN + "/zitadel",
    "@server_tools//tools/zot": PROFILE_BIN + "/zot",
}

SERVER_TOOL_DATA_RENAMES = {
    "@server_tools//tools/stalwart-spam-filter": "var/lib/stalwart/spam-filter.toml",
    "@server_tools//tools/stalwart-webadmin": "var/lib/stalwart/webadmin.zip",
}

POSTGRESQL_RUNTIME_DEBS = [
    "@server_tools//tools/postgresql-runtime-libaudit1-deb",
    "@server_tools//tools/postgresql-runtime-libcap-ng0-deb",
    "@server_tools//tools/postgresql-runtime-libcap2-deb",
    "@server_tools//tools/postgresql-runtime-libcom-err2-deb",
    "@server_tools//tools/postgresql-runtime-libffi8-deb",
    "@server_tools//tools/postgresql-runtime-libgcc-s1-deb",
    "@server_tools//tools/postgresql-runtime-libgcrypt20-deb",
    "@server_tools//tools/postgresql-runtime-libgmp10-deb",
    "@server_tools//tools/postgresql-runtime-libgnutls30t64-deb",
    "@server_tools//tools/postgresql-runtime-libgpg-error0-deb",
    "@server_tools//tools/postgresql-runtime-libgssapi-krb5-2-deb",
    "@server_tools//tools/postgresql-runtime-libhogweed6t64-deb",
    "@server_tools//tools/postgresql-runtime-libicu74-deb",
    "@server_tools//tools/postgresql-runtime-libidn2-0-deb",
    "@server_tools//tools/postgresql-runtime-libk5crypto3-deb",
    "@server_tools//tools/postgresql-runtime-libkeyutils1-deb",
    "@server_tools//tools/postgresql-runtime-libkrb5-3-deb",
    "@server_tools//tools/postgresql-runtime-libkrb5support0-deb",
    "@server_tools//tools/postgresql-runtime-libldap2-deb",
    "@server_tools//tools/postgresql-runtime-liblz4-1-deb",
    "@server_tools//tools/postgresql-runtime-liblzma5-deb",
    "@server_tools//tools/postgresql-runtime-libnettle8t64-deb",
    "@server_tools//tools/postgresql-runtime-libp11-kit0-deb",
    "@server_tools//tools/postgresql-runtime-libpam0g-deb",
    "@server_tools//tools/postgresql-runtime-libpq5-deb",
    "@server_tools//tools/postgresql-runtime-libreadline8t64-deb",
    "@server_tools//tools/postgresql-runtime-libsasl2-2-deb",
    "@server_tools//tools/postgresql-runtime-libssl3t64-deb",
    "@server_tools//tools/postgresql-runtime-libstdcpp6-deb",
    "@server_tools//tools/postgresql-runtime-libsystemd0-deb",
    "@server_tools//tools/postgresql-runtime-libtasn1-6-deb",
    "@server_tools//tools/postgresql-runtime-libtinfo6-deb",
    "@server_tools//tools/postgresql-runtime-libunistring5-deb",
    "@server_tools//tools/postgresql-runtime-libxml2-deb",
    "@server_tools//tools/postgresql-runtime-libzstd1-deb",
    "@server_tools//tools/postgresql-runtime-postgresql-16-deb",
    "@server_tools//tools/postgresql-runtime-postgresql-client-16-deb",
    "@server_tools//tools/postgresql-runtime-zlib1g-deb",
]

SERVER_TOOL_RUNTIME_DEPS = [
    ":grafana",
    ":grafana_clickhouse_datasource",
    ":grafana_clickhouse_datasource_version",
    ":haproxy",
    ":haproxy_awslc_libs",
    ":nodejs",
    ":openbao",
    ":postgresql_runtime",
    ":wireguard_tools",
    "//src/infrastructure-components/verdaccio:verdaccio_runtime",
]

PROFILE_GO_TOOLS = {
    "//src/infrastructure-components/clickhouse/cmd/clickhouse-spiffe-bundle-reload:clickhouse-spiffe-bundle-reload": PROFILE_BIN + "/clickhouse-spiffe-bundle-reload",
    "//src/infrastructure-components/haproxy/cmd/haproxy-lego-renew:haproxy-lego-renew": PROFILE_BIN + "/haproxy-lego-renew",
    "//src/infrastructure-components/haproxy/cmd/haproxy-upstreams-apply:haproxy-upstreams-apply": PROFILE_BIN + "/haproxy-upstreams-apply",
    "//src/infrastructure-components/temporal-platform/cmd/temporal-bootstrap:temporal-bootstrap": PROFILE_BIN + "/temporal-bootstrap",
    "//src/infrastructure-components/temporal-platform/cmd/temporal-schema:temporal-schema": PROFILE_BIN + "/temporal-schema",
    "//src/infrastructure-components/temporal-platform/cmd/verself-temporal-server:verself-temporal-server": PROFILE_BIN + "/verself-temporal-server",
    "//src/infrastructure-components/zot/cmd/zot-htpasswd:zot-htpasswd": PROFILE_BIN + "/zot-htpasswd",
}

SERVER_TOOL_SYMLINKS = {
    PROFILE_BIN + "/clickhouse-benchmark": "/opt/verself/profile/bin/clickhouse",
    PROFILE_BIN + "/clickhouse-client": "/opt/verself/profile/bin/clickhouse",
    PROFILE_BIN + "/clickhouse-keeper": "/opt/verself/profile/bin/clickhouse",
    PROFILE_BIN + "/clickhouse-local": "/opt/verself/profile/bin/clickhouse",
    PROFILE_BIN + "/clickhouse-server": "/opt/verself/profile/bin/clickhouse",
    PROFILE_BIN + "/corepack": "/opt/verself/nodejs/bin/corepack",
    PROFILE_BIN + "/grafana": "/opt/verself/grafana/bin/grafana",
    PROFILE_BIN + "/initdb": "/opt/verself/postgresql/usr/lib/postgresql/16/bin/initdb",
    PROFILE_BIN + "/node": "/opt/verself/nodejs/bin/node",
    PROFILE_BIN + "/npm": "/opt/verself/nodejs/bin/npm",
    PROFILE_BIN + "/npx": "/opt/verself/nodejs/bin/npx",
    PROFILE_BIN + "/pg_dump": "/opt/verself/postgresql/usr/lib/postgresql/16/bin/pg_dump",
    PROFILE_BIN + "/pg_isready": "/opt/verself/postgresql/usr/lib/postgresql/16/bin/pg_isready",
    PROFILE_BIN + "/pg_restore": "/opt/verself/postgresql/usr/lib/postgresql/16/bin/pg_restore",
    PROFILE_BIN + "/postgres": "/opt/verself/postgresql/usr/lib/postgresql/16/bin/postgres",
    PROFILE_BIN + "/psql": "/opt/verself/postgresql/usr/lib/postgresql/16/bin/psql",
}

def _mode_map(paths, mode):
    return {path: mode for path in paths}

def _merged(left, right):
    out = dict(left)
    out.update(right)
    return out

def _tar_fragment(name, srcs, cmd):
    native.genrule(
        name = name,
        srcs = srcs,
        outs = [name + ".tar"],
        cmd = """
set -euo pipefail
tmp="$$(mktemp -d)"
trap 'rm -rf "$$tmp"' EXIT
{cmd}
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='UTC 2000-01-01' -cf "$@" -C "$$tmp" .
""".format(cmd = cmd),
        target_compatible_with = [
            "@platforms//cpu:x86_64",
            "@platforms//os:linux",
        ],
    )

def _archive_directory(name, src, tar_flag, dest, strip_components = 1):
    _tar_fragment(
        name = name,
        srcs = [src],
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
        srcs = [src],
        cmd = """
mkdir -p "$$tmp/{dest}"
unzip -q "$(location {src})" -d "$$tmp/{dest}"
""".format(
            dest = dest,
            src = src,
        ),
    )

def _deb_directory(name, src, source_dir, dest):
    _tar_fragment(
        name = name,
        srcs = [src],
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

def _deb_member(name, src, member, dest):
    _tar_fragment(
        name = name,
        srcs = [src],
        cmd = """
mkdir -p "$$tmp/extract" "$$tmp/$$(dirname {dest})"
dpkg-deb -x "$(location {src})" "$$tmp/extract"
install -m 0755 "$$tmp/extract"/{member} "$$tmp/{dest}"
rm -rf "$$tmp/extract"
""".format(
            dest = dest,
            member = member,
            src = src,
        ),
    )

def _postgresql_runtime():
    extract_cmds = [
        'dpkg-deb -x "$(location {src})" "$$tmp/extract"'.format(src = src)
        for src in POSTGRESQL_RUNTIME_DEBS
    ]
    _tar_fragment(
        name = "postgresql_runtime",
        srcs = POSTGRESQL_RUNTIME_DEBS,
        cmd = """
mkdir -p "$$tmp/extract"
{extract_cmds}
mkdir -p \
  "$$tmp/{root}/usr/lib/postgresql" \
  "$$tmp/{root}/usr/share/postgresql" \
  "$$tmp/{root}/usr/lib/x86_64-linux-gnu"
cp -a "$$tmp/extract/usr/lib/postgresql/{major}" "$$tmp/{root}/usr/lib/postgresql/{major}"
cp -a "$$tmp/extract/usr/share/postgresql/{major}" "$$tmp/{root}/usr/share/postgresql/{major}"
cp -a "$$tmp/extract/usr/lib/x86_64-linux-gnu"/. "$$tmp/{root}/usr/lib/x86_64-linux-gnu/"
rm -rf "$$tmp/extract"
""".format(
            extract_cmds = "\n".join(extract_cmds),
            major = POSTGRESQL_MAJOR_VERSION,
            root = POSTGRESQL_RUNTIME_ROOT,
        ),
    )

def _runtime_archives():
    _archive_directory(
        name = "grafana",
        src = "@server_tools//tools/grafana-runtime",
        tar_flag = "z",
        dest = "opt/verself/grafana",
    )
    _zip_directory(
        name = "grafana_clickhouse_datasource",
        src = "@server_tools//tools/grafana-clickhouse-datasource",
        dest = "var/lib/grafana/plugins",
    )
    _tar_fragment(
        name = "grafana_clickhouse_datasource_version",
        srcs = [],
        cmd = """
mkdir -p "$$tmp/var/lib/grafana/plugins"
printf '{version}\\n' > "$$tmp/var/lib/grafana/plugins/.grafana-clickhouse-datasource-version"
""".format(version = GRAFANA_CLICKHOUSE_DATASOURCE_VERSION),
    )
    _deb_directory(
        name = "haproxy_awslc_libs",
        src = "@server_tools//tools/haproxy-awslc-libs-deb",
        source_dir = "opt/aws-lc",
        dest = "opt/aws-lc",
    )
    _deb_member(
        name = "haproxy",
        src = "@server_tools//tools/haproxy-awslc-deb",
        member = "usr/sbin/haproxy",
        dest = PROFILE_BIN + "/haproxy",
    )
    _deb_member(
        name = "openbao",
        src = "@server_tools//tools/openbao-deb",
        member = "usr/bin/bao",
        dest = PROFILE_BIN + "/bao",
    )
    _deb_member(
        name = "wireguard_tools",
        src = "@server_tools//tools/wireguard-tools-deb",
        member = "usr/bin/wg",
        dest = PROFILE_BIN + "/wg",
    )
    _archive_directory(
        name = "nodejs",
        src = "@server_tools//tools/nodejs-runtime",
        tar_flag = "J",
        dest = "opt/verself/nodejs",
    )
    _postgresql_runtime()

def server_tools_archive(name = "server_tools_archive"):
    """Build the Bazel-owned host tools archive.

    Args:
      name: Repository-local target name for the generated server tool archive.
    """
    _runtime_archives()

    modes = _mode_map(SERVER_TOOL_BINARY_RENAMES.values(), "0755")
    modes.update(_mode_map(SERVER_TOOL_DATA_RENAMES.values(), "0644"))

    pkg_tar(
        name = name,
        out = "server_tools.tar.zst",
        compressor = "//src/tools/dev/cmd/zstd-compressor:zstd-compressor",
        deps = SERVER_TOOL_RUNTIME_DEPS,
        extension = "tar.zst",
        files = _merged(SERVER_TOOL_BINARY_RENAMES, SERVER_TOOL_DATA_RENAMES),
        modes = modes,
        symlinks = SERVER_TOOL_SYMLINKS,
    )

def substrate_go_tools_archive(name = "substrate_go_tools"):
    """Bundle repo-built Go helpers into the host profile.

    Args:
      name: Repository-local target name for the generated Go helper archive.
    """
    pkg_tar(
        name = name,
        out = "substrate_go_tools.tar.zst",
        compressor = "//src/tools/dev/cmd/zstd-compressor:zstd-compressor",
        extension = "tar.zst",
        files = PROFILE_GO_TOOLS,
        modes = _mode_map(PROFILE_GO_TOOLS.values(), "0755"),
        visibility = ["//visibility:public"],
    )

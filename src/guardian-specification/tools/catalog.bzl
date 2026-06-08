"""Single source of truth for the Guardian tool catalog.

`guardian run <tool>` resolves <root>/<name>/bin/<exe> against the golden image
tool root ($GUARDIAN_TOOLS_ROOT) or the Bazel-materialized dev root
(.verself/tools/root). This list drives BOTH:

  * //src/guardian-specification/tools:tools_root  — the pkg_tar that lays each
    tool's bytes at <name>/bin/<exe>, and
  * //src/guardian-specification/tools:tools_catalog  — the generated
    .config/guardian/tools.cue the resolver loads.

Because both are projected from this one list, the committed catalog cannot drift
from what is actually materialized.

Each entry is a struct:

  name       tool name; the <name> dir under the tool root and the catalog key.
  exe        executable basename; the resolver execs <root>/<name>/bin/<exe>.
  platforms  os/arch strings the catalog advertises for this tool.
  file       (single-binary tools) a label producing exactly the executable
             file; placed at <name>/bin/<exe>.
  tree       (tree tools, e.g. go/ruby) a label producing a .tar already rooted
             at <name>/... ; composed into the tool root via pkg_tar `deps`.

Exactly one of `file` / `tree` is set per entry.

Platform reality: controller dev tools are dual-platform (linux/amd64 +
darwin/arm64); the rules_multitool runtime binaries are pinned linux/x86_64 only
(they run on the bare-metal fleet), so their catalog entries advertise
linux/amd64 alone.
"""

_LINUX = ["linux/amd64"]
_DUAL = ["darwin/arm64", "linux/amd64"]

def _file(name, exe, platforms, file):
    return struct(name = name, exe = exe, platforms = platforms, file = file, tree = None)

def _tree(name, exe, platforms, tree):
    return struct(name = name, exe = exe, platforms = platforms, file = None, tree = tree)

# Controller dev + build tooling (host-selected per platform by
# controller_http_file / controller_http_archive). go and ruby carry full trees.
_DEV_TOOLS = [
    _tree("go", "go", _DUAL, "//src/tools/dev/binaries:go_root_tar"),
    # ruby is deferred: its portable bottle is an external archive whose lib tree
    # pkg_tar flattens to basenames (glob("**") + path stripping); it needs a
    # genrule re-tar (like go_root_tar) before it can be materialized at
    # ruby/bin/ruby with its load path intact. It stays a dev shim
    # (//src/tools/dev/binaries:ruby) until then.
    _file("tofu", "tofu", _DUAL, "//src/tools/dev/binaries:tofu_bin"),
    _file("buildifier", "buildifier", _DUAL, "//src/tools/dev/binaries:buildifier_bin"),
    _file("gofumpt", "gofumpt", _DUAL, "//src/tools/dev/binaries:gofumpt_bin"),
    _file("jq", "jq", _DUAL, "//src/tools/dev/binaries:jq_bin"),
    _file("cdxgen", "cdxgen", _DUAL, "//src/tools/dev/binaries:cdxgen_bin"),
    _file("syft", "syft", _DUAL, "//src/tools/dev/binaries:syft_bin"),
    _file("uv", "uv", _DUAL, "//src/tools/dev/binaries:uv"),
    _file("uvx", "uvx", _DUAL, "//src/tools/dev/binaries:uvx"),
    _file("stripe", "stripe", _DUAL, "//src/tools/dev/binaries:stripe_cli_bin"),
    _file("stripe-cli-projects", "stripe-cli-projects", _DUAL, "//src/tools/dev/binaries:stripe_projects_plugin_bin"),
]

# Guardian-specification-owned tools (the bazel pin, rsync) and the lone
# benchmark tool from the guardian_specification multitool hub.
_GUARDIAN_TOOLS = [
    _file("bazel", "bazel", _DUAL, "//src/guardian-specification/tools:bazel_bin"),
    _file("rsync", "rsync", _LINUX, "//src/guardian-specification/tools:rsync_bin"),
    _file("hyperfine", "hyperfine", _LINUX, "@guardian_specification_tools//tools/hyperfine"),
]

# rules_multitool runtime binaries. Each is exposed by its hub as
# @<hub>//tools/<tool> (a single executable file). linux/x86_64 only.
_MULTITOOL = [
    _file("clickhouse", "clickhouse", _LINUX, "@clickhouse_tools//tools/clickhouse"),
    _file("spiffe-helper", "spiffe-helper", _LINUX, "@clickhouse_tools//tools/spiffe-helper"),
    _file("zitadel", "zitadel", _LINUX, "@zitadel_tools//tools/zitadel"),
    _file("zot", "zot", _LINUX, "@zot_tools//tools/zot"),
    _file("otelcol-contrib", "otelcol-contrib", _LINUX, "@otelcol_tools//tools/otelcol-contrib"),
    _file("forgejo", "forgejo", _LINUX, "@forgejo_tools//tools/forgejo"),
    _file("nats-server", "nats-server", _LINUX, "@nats_tools//tools/nats-server"),
    _file("spicedb", "spicedb", _LINUX, "@spicedb_tools//tools/spicedb"),
    _file("zed", "zed", _LINUX, "@spicedb_tools//tools/zed"),
    _file("stalwart", "stalwart", _LINUX, "@stalwart_tools//tools/stalwart"),
    _file("stalwart-cli", "stalwart-cli", _LINUX, "@stalwart_tools//tools/stalwart-cli"),
    _file("tigerbeetle", "tigerbeetle", _LINUX, "@tigerbeetle_tools//tools/tigerbeetle"),
    _file("pomerium", "pomerium", _LINUX, "@pomerium_tools//tools/pomerium"),
    _file("temporal-server", "temporal-server", _LINUX, "@temporal_tools//tools/temporal-server"),
    _file("temporal-sql-tool", "temporal-sql-tool", _LINUX, "@temporal_tools//tools/temporal-sql-tool"),
    _file("tdbg", "tdbg", _LINUX, "@temporal_tools//tools/tdbg"),
    _file("containerd", "containerd", _LINUX, "@electric_tools//tools/containerd"),
    _file("containerd-shim-runc-v2", "containerd-shim-runc-v2", _LINUX, "@electric_tools//tools/containerd-shim-runc-v2"),
    _file("ctr", "ctr", _LINUX, "@electric_tools//tools/ctr"),
    _file("runc", "runc", _LINUX, "@electric_tools//tools/runc"),
]

GUARDIAN_TOOLS = _DEV_TOOLS + _GUARDIAN_TOOLS + _MULTITOOL

def tools_root_files():
    """The pkg_tar `files` map for single-binary catalog entries.

    Returns:
        A dict mapping each single-file tool label to its <name>/bin/<exe> path.
    """
    files = {}
    for tool in GUARDIAN_TOOLS:
        if tool.file == None:
            continue
        files[tool.file] = tool.name + "/bin/" + tool.exe
    return files

def tools_root_modes():
    """The pkg_tar `modes` map (exec bit on every materialized binary).

    Returns:
        A dict mapping each <name>/bin/<exe> path to mode "0755".
    """
    modes = {}
    for tool in GUARDIAN_TOOLS:
        if tool.file == None:
            continue
        modes[tool.name + "/bin/" + tool.exe] = "0755"
    return modes

def tools_root_tree_deps():
    """Return the pkg_tar `deps` list for tree catalog entries (go, ruby)."""
    return [tool.tree for tool in GUARDIAN_TOOLS if tool.tree != None]

def catalog_projection():
    """The catalog projection consumed by the tools.cue generator.

    Returns:
        A dict {name: {platform: executable}} over every catalog tool.
    """
    out = {}
    for tool in GUARDIAN_TOOLS:
        platforms = {}
        for platform in tool.platforms:
            platforms[platform] = tool.exe
        out[tool.name] = platforms
    return out

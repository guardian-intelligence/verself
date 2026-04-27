package bazelservertools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const outputPath = "src/cue-renderer/binaries/server_tools.bzl"

type Renderer struct{}

func (Renderer) Name() string { return "bazel_server_tools" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	packaging := loaded.Catalog.ServerToolPackaging
	var b strings.Builder
	b.WriteString(projection.HeaderFor("src/cue-renderer/catalog/versions.cue"))
	b.WriteString("load(\"@rules_pkg//pkg:tar.bzl\", \"pkg_tar\")\n")
	b.WriteString("load(\"@rules_shell//shell:sh_binary.bzl\", \"sh_binary\")\n\n")

	profileBin, err := projection.String(packaging, "serverToolPackaging", "profile_bin")
	if err != nil {
		return err
	}
	grafanaVersion, err := projection.String(packaging, "serverToolPackaging", "grafana_clickhouse_datasource_version")
	if err != nil {
		return err
	}
	fmt.Fprintf(&b, "PROFILE_BIN = %q\n", profileBin)
	fmt.Fprintf(&b, "GRAFANA_CLICKHOUSE_DATASOURCE_VERSION = %q\n\n", grafanaVersion)

	if err := writeTupleList(&b, "TAR_SINGLE_BINARIES", packaging, "tar_single", []string{"name", "repo", "tar_flag", "binary", "dest"}); err != nil {
		return err
	}
	if err := writeTupleList(&b, "ZIP_SINGLE_BINARIES", packaging, "zip_single", []string{"name", "repo", "binary", "dest"}); err != nil {
		return err
	}
	if err := writeTupleList(&b, "DEB_BINARY_SPECS", packaging, "deb_member", []string{"name", "repo", "binary", "dest"}); err != nil {
		return err
	}
	if err := writeTupleList(&b, "RAW_BINARY_SPECS", packaging, "raw", []string{"name", "repo", "dest"}); err != nil {
		return err
	}
	if err := writeTupleList(&b, "ARCHIVE_DIRECTORIES", packaging, "archive_dir", []string{"name", "repo", "tar_flag", "dest"}); err != nil {
		return err
	}

	deps, err := serverToolDeps(packaging)
	if err != nil {
		return err
	}
	b.WriteString("SERVER_TOOL_DEPS = [\n")
	for _, dep := range deps {
		fmt.Fprintf(&b, "    %q,\n", ":"+dep)
	}
	b.WriteString("]\n\n")

	symlinks, err := projection.Map(packaging, "serverToolPackaging", "symlinks")
	if err != nil {
		return err
	}
	b.WriteString("SERVER_TOOL_SYMLINKS = {\n")
	for _, key := range projection.SortedKeys(symlinks) {
		value, err := projection.String(symlinks, "serverToolPackaging.symlinks", key)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "    %q: %q,\n", key, value)
	}
	b.WriteString("}\n\n")
	b.WriteString(starlarkTemplate)

	return out.WriteFile(outputPath, []byte(b.String()))
}

func writeTupleList(b *strings.Builder, name string, packaging map[string]any, key string, fields []string) error {
	items, err := mapSlice(packaging, "serverToolPackaging", key)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "%s = [\n", name)
	for _, item := range items {
		b.WriteString("    (")
		for i, field := range fields {
			if i > 0 {
				b.WriteString(", ")
			}
			value, err := projection.String(item, key, field)
			if err != nil {
				return err
			}
			if field == "repo" {
				value = "@" + value + "//file"
			}
			fmt.Fprintf(b, "%q", value)
		}
		b.WriteString("),\n")
	}
	b.WriteString("]\n\n")
	return nil
}

func serverToolDeps(packaging map[string]any) ([]string, error) {
	deps := map[string]struct{}{
		"grafana_clickhouse_datasource_version": {},
	}
	for _, key := range []string{"tar_single", "zip_single", "deb_member", "raw", "archive_dir", "zip_dir"} {
		items, err := mapSlice(packaging, "serverToolPackaging", key)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			name, err := projection.String(item, key, "name")
			if err != nil {
				return nil, err
			}
			deps[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(deps))
	for dep := range deps {
		out = append(out, dep)
	}
	sort.Strings(out)
	return out, nil
}

func mapSlice(parent map[string]any, path, key string) ([]map[string]any, error) {
	value, ok := parent[key]
	if !ok {
		return nil, fmt.Errorf("%s.%s: missing", path, key)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s: expected list, got %T", path, key, value)
	}
	out := make([]map[string]any, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.%s[%d]: expected map, got %T", path, key, i, item)
		}
		out = append(out, m)
	}
	return out, nil
}

const starlarkTemplate = `def _tar_fragment(name, src, cmd):
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
        srcs = ["zstd-compressor.sh"],
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
`

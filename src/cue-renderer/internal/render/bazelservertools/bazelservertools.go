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

type hostGoTool struct {
	Label  string
	Output string
}

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

	if err := projection.StarlarkRepoTupleList(&b, "TAR_SINGLE_BINARIES", packaging, "serverToolPackaging", "tar_single", []string{"name", "repo", "tar_flag", "binary", "dest"}); err != nil {
		return err
	}
	if err := projection.StarlarkRepoTupleList(&b, "ZIP_SINGLE_BINARIES", packaging, "serverToolPackaging", "zip_single", []string{"name", "repo", "binary", "dest"}); err != nil {
		return err
	}
	if err := projection.StarlarkRepoTupleList(&b, "DEB_BINARY_SPECS", packaging, "serverToolPackaging", "deb_member", []string{"name", "repo", "binary", "dest"}); err != nil {
		return err
	}
	if err := projection.StarlarkRepoTupleList(&b, "RAW_BINARY_SPECS", packaging, "serverToolPackaging", "raw", []string{"name", "repo", "dest"}); err != nil {
		return err
	}
	if err := projection.StarlarkRepoTupleList(&b, "ARCHIVE_DIRECTORIES", packaging, "serverToolPackaging", "archive_dir", []string{"name", "repo", "tar_flag", "dest"}); err != nil {
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

	hostTools, err := hostGoTools(loaded)
	if err != nil {
		return err
	}
	b.WriteString("HOST_GO_TOOLS = [\n")
	for _, item := range hostTools {
		fmt.Fprintf(&b, "    (%q, %q),\n", item.Label, item.Output)
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

func hostGoTools(loaded load.Loaded) ([]hostGoTool, error) {
	components, err := projection.Components(loaded)
	if err != nil {
		return nil, err
	}

	seen := map[string]hostGoTool{}
	for _, component := range components {
		if isHostRuntime(component.Value) {
			artifact, _ := component.Value["artifact"].(map[string]any)
			if err := appendHostGoTool(seen, component.Name+".artifact", artifact); err != nil {
				return nil, err
			}
		}

		rawTools, ok := component.Value["tools"].(map[string]any)
		if !ok {
			continue
		}
		for name, raw := range rawTools {
			artifact, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("topology.components.%s.tools.%s: expected map, got %T", component.Name, name, raw)
			}
			if err := appendHostGoTool(seen, component.Name+".tools."+name, artifact); err != nil {
				return nil, err
			}
		}
	}

	out := make([]hostGoTool, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Output != out[j].Output {
			return out[i].Output < out[j].Output
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

func isHostRuntime(component map[string]any) bool {
	kind, _ := component["kind"].(string)
	if kind != "resource" && kind != "privileged_daemon" {
		return false
	}
	deployment, _ := component["deployment"].(map[string]any)
	supervisor, _ := deployment["supervisor"].(string)
	return supervisor != "nomad"
}

func appendHostGoTool(seen map[string]hostGoTool, path string, artifact map[string]any) error {
	kind, _ := artifact["kind"].(string)
	if kind != "go_binary" {
		return nil
	}
	label, _ := artifact["bazel_label"].(string)
	output, _ := artifact["output"].(string)
	if label == "" || output == "" {
		return fmt.Errorf("topology.components.%s: go_binary artifact requires bazel_label and output", path)
	}
	if existing, ok := seen[output]; ok && existing.Label != label {
		return fmt.Errorf("host Go tool output %q is declared by both %s and %s", output, existing.Label, label)
	}
	seen[output] = hostGoTool{Label: label, Output: output}
	return nil
}

func serverToolDeps(packaging map[string]any) ([]string, error) {
	deps := map[string]struct{}{
		"grafana_clickhouse_datasource_version": {},
	}
	for _, key := range []string{"tar_single", "zip_single", "deb_member", "raw", "archive_dir", "zip_dir"} {
		items, err := projection.MapSlice(packaging, "serverToolPackaging", key)
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
`

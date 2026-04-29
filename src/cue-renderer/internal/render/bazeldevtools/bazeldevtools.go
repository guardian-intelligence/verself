// Package bazeldevtools emits the Starlark manifest the cue-renderer
// `binaries/BUILD.bazel` reads to produce `:dev_tools.tar.zst`.
//
// The output is the dev-tools twin of `binaries/server_tools.bzl`: data
// lists projected from `devToolPackaging` plus a `dev_tools_archive()`
// macro that invokes the local helpers for each row, then assembles
// every intermediate tar fragment via `pkg_tar`.
//
// The renderer holds no policy: layout (tar paths, archive members,
// symlink targets, strip-components, install destinations) is owned by
// the CUE catalog. Adding a tool to `pinned_http_file` means adding
// `devToolDownloads` + a row in one of the `devToolPackaging` lists.
// Adding a new packaging *shape* (e.g. a `.deb` member install for a
// dev tool) requires extending CUE, this renderer, and the Starlark
// template together.
package bazeldevtools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const (
	outputPath = "src/cue-renderer/binaries/dev_tools.bzl"

	// devToolsPrefix namespaces every emitted genrule + dep label so the
	// dev-tools archive can coexist with the server-tools archive in
	// the same Bazel package without colliding on bare names like
	// `clickhouse` (which both archives ship).
	devToolsPrefix = "dev_tools_"
)

type Renderer struct{}

func (Renderer) Name() string { return "bazel_dev_tools" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	packaging := loaded.Catalog.DevToolPackaging
	var b strings.Builder
	b.WriteString(projection.HeaderFor("src/cue-renderer/catalog/versions.cue"))
	b.WriteString("load(\"@rules_pkg//pkg:tar.bzl\", \"pkg_tar\")\n")
	b.WriteString("load(\"@rules_shell//shell:sh_binary.bzl\", \"sh_binary\")\n\n")

	if err := writeTupleList(&b, "TAR_SINGLE_BINARIES", packaging, "tar_single", []string{"name", "repo", "tar_flag", "binary", "dest"}); err != nil {
		return err
	}
	if err := writeTupleList(&b, "ZIP_SINGLE_BINARIES", packaging, "zip_single", []string{"name", "repo", "binary", "dest"}); err != nil {
		return err
	}
	if err := writeTupleList(&b, "ZIP_DIRECTORY_INSTALLS", packaging, "zip_directory", []string{"name", "repo", "src_sub", "dest"}); err != nil {
		return err
	}
	if err := writeTarMultiList(&b, packaging); err != nil {
		return err
	}
	if err := writeArchiveDirList(&b, packaging); err != nil {
		return err
	}
	if err := writeTupleList(&b, "RAW_BINARY_SPECS", packaging, "raw", []string{"name", "repo", "dest"}); err != nil {
		return err
	}

	deps, err := devToolDeps(packaging)
	if err != nil {
		return err
	}
	b.WriteString("DEV_TOOL_DEPS = [\n")
	for _, dep := range deps {
		fmt.Fprintf(&b, "    %q,\n", ":"+devToolsPrefix+dep)
	}
	b.WriteString("]\n\n")

	symlinks, err := projection.Map(packaging, "devToolPackaging", "symlinks")
	if err != nil {
		return err
	}
	b.WriteString("DEV_TOOL_SYMLINKS = {\n")
	for _, key := range projection.SortedKeys(symlinks) {
		value, err := projection.String(symlinks, "devToolPackaging.symlinks", key)
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
	items, err := mapSlice(packaging, "devToolPackaging", key)
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

// writeTarMultiList emits TAR_MULTI_BINARIES rows where the last element
// is a Starlark list of (member, dest) tuples.
func writeTarMultiList(b *strings.Builder, packaging map[string]any) error {
	items, err := mapSlice(packaging, "devToolPackaging", "tar_multi")
	if err != nil {
		return err
	}
	b.WriteString("TAR_MULTI_BINARIES = [\n")
	for _, item := range items {
		name, err := projection.String(item, "tar_multi", "name")
		if err != nil {
			return err
		}
		repo, err := projection.String(item, "tar_multi", "repo")
		if err != nil {
			return err
		}
		tarFlag, err := projection.String(item, "tar_multi", "tar_flag")
		if err != nil {
			return err
		}
		strip, err := projection.Int(item, "tar_multi", "strip_components")
		if err != nil {
			return err
		}
		binaries, err := mapSlice(item, "tar_multi", "binaries")
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "    (%q, %q, %q, %d, [", name, "@"+repo+"//file", tarFlag, strip)
		for i, binary := range binaries {
			if i > 0 {
				b.WriteString(", ")
			}
			member, err := projection.String(binary, "tar_multi.binaries", "member")
			if err != nil {
				return err
			}
			dest, err := projection.String(binary, "tar_multi.binaries", "dest")
			if err != nil {
				return err
			}
			fmt.Fprintf(b, "(%q, %q)", member, dest)
		}
		b.WriteString("]),\n")
	}
	b.WriteString("]\n\n")
	return nil
}

// writeArchiveDirList is identical in shape to writeTupleList except
// that strip_components is an int, not a string.
func writeArchiveDirList(b *strings.Builder, packaging map[string]any) error {
	items, err := mapSlice(packaging, "devToolPackaging", "archive_dir")
	if err != nil {
		return err
	}
	b.WriteString("ARCHIVE_DIRECTORIES = [\n")
	for _, item := range items {
		name, err := projection.String(item, "archive_dir", "name")
		if err != nil {
			return err
		}
		repo, err := projection.String(item, "archive_dir", "repo")
		if err != nil {
			return err
		}
		tarFlag, err := projection.String(item, "archive_dir", "tar_flag")
		if err != nil {
			return err
		}
		dest, err := projection.String(item, "archive_dir", "dest")
		if err != nil {
			return err
		}
		strip, err := projection.Int(item, "archive_dir", "strip_components")
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "    (%q, %q, %q, %q, %d),\n", name, "@"+repo+"//file", tarFlag, dest, strip)
	}
	b.WriteString("]\n\n")
	return nil
}

// devToolDeps returns the sorted union of every fragment name across
// every packaging list. pkg_tar consumes this list as `deps`.
func devToolDeps(packaging map[string]any) ([]string, error) {
	deps := map[string]struct{}{}
	for _, key := range []string{"tar_single", "zip_single", "zip_directory", "tar_multi", "archive_dir", "raw"} {
		items, err := mapSlice(packaging, "devToolPackaging", key)
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

// starlarkTemplate is the static portion of dev_tools.bzl. It mirrors
// server_tools.bzl's macro shape but covers two extra packaging shapes
// dev tools need (zip_directory_install for protoc-style include trees,
// tar_multi_binary for archives that ship multiple binaries like uv/age).
//
// `dev_tools_archive()` reuses the `:zstd_compressor` sh_binary that
// `server_tools_archive()` declares in the same package; the
// `existing_rule` guard keeps both archives callable in any order.
const starlarkTemplate = `_DEV_TOOLS_PREFIX = "dev_tools_"

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

def dev_tools_archive():
    if not native.existing_rule("zstd_compressor"):
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

    for name, src, src_sub, dest in ZIP_DIRECTORY_INSTALLS:
        _zip_directory_install(
            name = name,
            src = src,
            src_sub = src_sub,
            dest = dest,
        )

    for name, src, tar_flag, strip_components, binaries in TAR_MULTI_BINARIES:
        _tar_multi_binary(
            name = name,
            src = src,
            tar_flag = tar_flag,
            strip_components = strip_components,
            binaries = binaries,
        )

    for name, src, tar_flag, dest, strip_components in ARCHIVE_DIRECTORIES:
        _archive_directory(
            name = name,
            src = src,
            tar_flag = tar_flag,
            dest = dest,
            strip_components = strip_components,
        )

    for name, src, dest in RAW_BINARY_SPECS:
        _raw_binary(
            name = name,
            src = src,
            dest = dest,
        )

    pkg_tar(
        name = "dev_tools_archive",
        out = "dev_tools.tar.zst",
        compressor = ":zstd_compressor",
        deps = DEV_TOOL_DEPS,
        extension = "tar.zst",
        symlinks = DEV_TOOL_SYMLINKS,
    )
`

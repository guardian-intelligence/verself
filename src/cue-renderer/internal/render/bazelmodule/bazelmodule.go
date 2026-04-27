package bazelmodule

import (
	"context"
	"fmt"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const outputPath = "src/cue-renderer/binaries/server_tools.MODULE.bazel"

type Renderer struct{}

func (Renderer) Name() string { return "bazel_module" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	downloads, err := projection.NamedFields(loaded.Catalog.Raw, "serverToolDownloads")
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(projection.HeaderFor("src/cue-renderer/catalog/versions.cue"))
	b.WriteString("\n")
	b.WriteString("http_file = use_repo_rule(\"@bazel_tools//tools/build_defs/repo:http.bzl\", \"http_file\")\n\n")
	for _, item := range downloads {
		name, err := projection.String(item.Value, item.Name, "name")
		if err != nil {
			return err
		}
		downloaded, err := projection.String(item.Value, item.Name, "downloaded_file_path")
		if err != nil {
			return err
		}
		sha, err := projection.String(item.Value, item.Name, "sha256")
		if err != nil {
			return err
		}
		url, err := projection.String(item.Value, item.Name, "url")
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "http_file(\n")
		fmt.Fprintf(&b, "    name = %q,\n", name)
		fmt.Fprintf(&b, "    downloaded_file_path = %q,\n", downloaded)
		fmt.Fprintf(&b, "    sha256 = %q,\n", sha)
		fmt.Fprintf(&b, "    url = %q,\n", url)
		fmt.Fprintf(&b, ")\n\n")
	}
	return out.WriteFile(outputPath, []byte(b.String()))
}

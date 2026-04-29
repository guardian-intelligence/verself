// Package bazeldevtoolsmodule emits the http_file rules that fetch the
// pinned_http_file dev tools. The output is included from the root
// MODULE.bazel and feeds the dev_tools.tar.zst pkg_tar target.
package bazeldevtoolsmodule

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const outputPath = "src/cue-renderer/binaries/dev_tools.MODULE.bazel"

type Renderer struct{}

func (Renderer) Name() string { return "bazel_dev_tools_module" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	data, err := projection.HTTPFileModule(loaded.Catalog.Raw, "devToolDownloads")
	if err != nil {
		return err
	}
	return out.WriteFile(outputPath, data)
}

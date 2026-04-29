// Package bazelmodule emits the http_file rules that fetch the
// pinned third-party server-tool archives. The output is included from
// the root MODULE.bazel and consumed by `server_tools.tar.zst`.
package bazelmodule

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const outputPath = "src/cue-renderer/binaries/server_tools.MODULE.bazel"

type Renderer struct{}

func (Renderer) Name() string { return "bazel_module" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	data, err := projection.HTTPFileModule(loaded.Catalog.Raw, "serverToolDownloads")
	if err != nil {
		return err
	}
	return out.WriteFile(outputPath, data)
}

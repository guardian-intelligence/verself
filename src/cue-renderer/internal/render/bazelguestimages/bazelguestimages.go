// Package bazelguestimages emits the http_file rules that fetch the
// pinned upstream archives for the guest-image build pipeline. The
// output is included from the root MODULE.bazel and consumed by the
// per-image rules under //src/vm-orchestrator/guest-images/.
package bazelguestimages

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const outputPath = "src/vm-orchestrator/guest-images/guest_images.MODULE.bazel"

type Renderer struct{}

func (Renderer) Name() string { return "bazel_guest_images" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	data, err := projection.HTTPFileModule(loaded.Catalog.Raw, "guestImageDownloads")
	if err != nil {
		return err
	}
	return out.WriteFile(outputPath, data)
}

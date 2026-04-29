// Package catalog projects the dev/server tool catalog into Ansible
// group_vars. dev_tools.tar.zst owns every pinned_http_file and
// source_built_go tool; systemPackages owns apt-managed entries;
// lockfileUvTools owns Python tools delivered via committed uv.lock
// files. `bootstrap_pivot` tools are skipped — bazelisk lives in
// scripts/bootstrap and resolves bazel itself.
package catalog

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "catalog" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	return projection.WriteYAML(out, "catalog", map[string]any{
		"topology_versions":             loaded.Catalog.Versions,
		"topology_server_tools":         loaded.Catalog.ServerTools,
		"topology_dev_tools":            loaded.Catalog.DevTools,
		"topology_dev_tools_archive":    loaded.Catalog.DevToolsArchive,
		"topology_lockfile_uv_projects": loaded.Catalog.LockfileUvTools,
		"topology_system_packages":      loaded.Catalog.SystemPackages,
		"topology_guest_versions":       loaded.Catalog.GuestVersions,
	})
}

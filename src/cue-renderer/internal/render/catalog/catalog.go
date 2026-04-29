// Package catalog projects the dev/server tool catalog into Ansible
// group_vars. dev_tools.tar.zst owns every pinned_http_file tool;
// systemPackages owns apt-managed entries; lockfileUvTools owns Python
// tools delivered via committed uv.lock files; the install plan handles
// the one strategy still driven directly by Ansible (`go_install`).
// `bootstrap_pivot` and `pinned_http_file` tools are skipped — bazelisk
// lives in scripts/bootstrap, the rest land via the tarball.
package catalog

import (
	"context"
	"fmt"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

// goPath is the GOPATH used by `go install` strategy entries. Their
// binaries land at goPath/bin and are reached via /etc/profile.d/go.sh
// (written by the dev_tools role, not the install plan).
const goPath = "{{ dev_tools_gopath }}"

type Renderer struct{}

func (Renderer) Name() string { return "catalog" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	installPlan, err := devToolInstallPlan(loaded)
	if err != nil {
		return err
	}
	return projection.WriteYAML(out, "catalog", map[string]any{
		"topology_versions":              loaded.Catalog.Versions,
		"topology_server_tools":          loaded.Catalog.ServerTools,
		"topology_dev_tools":             loaded.Catalog.DevTools,
		"topology_dev_tools_archive":     loaded.Catalog.DevToolsArchive,
		"topology_lockfile_uv_projects":  loaded.Catalog.LockfileUvTools,
		"topology_system_packages":       loaded.Catalog.SystemPackages,
		"topology_guest_versions":        loaded.Catalog.GuestVersions,
		"topology_dev_tool_install_plan": installPlan,
	})
}

type installPlan struct {
	goInstalls []map[string]any
}

func devToolInstallPlan(loaded load.Loaded) (map[string]any, error) {
	tools, err := projection.NamedFields(loaded.Catalog.Raw, "devTools")
	if err != nil {
		return nil, err
	}
	plan := &installPlan{}

	for _, item := range tools {
		tool := item.Value
		tier, err := projection.OptionalString(tool, item.Name, "tier")
		if err != nil {
			return nil, err
		}
		if tier != "legacy_install_plan" {
			continue
		}
		strategy, err := projection.OptionalString(tool, item.Name, "strategy")
		if err != nil {
			return nil, err
		}
		switch strategy {
		case "go_install":
			pkg, err := projection.String(tool, item.Name, "go_package")
			if err != nil {
				return nil, err
			}
			version, err := projection.String(tool, item.Name, "version")
			if err != nil {
				return nil, err
			}
			plan.goInstalls = append(plan.goInstalls, map[string]any{
				"key":    item.Name,
				"argv":   []string{"/usr/local/go/bin/go", "install", pkg + "@v" + version},
				"gopath": goPath,
				"gobin":  goPath + "/bin",
			})
		default:
			return nil, fmt.Errorf("dev tool %s (tier=%s) has unsupported install-plan strategy %q", item.Name, tier, strategy)
		}
	}

	return map[string]any{
		"go_installs": plan.goInstalls,
	}, nil
}

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

const (
	stagingDir = "{{ dev_tools_staging_dir }}"
	goPath     = "{{ dev_tools_gopath }}"
)

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
		"topology_guest_versions":        loaded.Catalog.GuestVersions,
		"topology_dev_tool_install_plan": installPlan,
	})
}

type installPlan struct {
	tools            []map[string]any
	downloads        []map[string]any
	directories      []map[string]any
	extracts         []map[string]any
	copies           []map[string]any
	directoryCopies  []map[string]any
	links            []map[string]any
	goInstalls       []map[string]any
	uvTools          []map[string]any
	aptPackages      []string
	replacementPaths []string
}

func devToolInstallPlan(loaded load.Loaded) (map[string]any, error) {
	tools, err := projection.NamedFields(loaded.Catalog.Raw, "devTools")
	if err != nil {
		return nil, err
	}
	plan := &installPlan{}
	plan.addDirectory(stagingDir, "0755")
	plan.addDirectory(goPath+"/bin", "0755")

	for _, item := range tools {
		tool := item.Value
		strategy, err := projection.OptionalString(tool, item.Name, "strategy")
		if err != nil {
			return nil, err
		}
		plan.toolRecord(item.Name, tool)

		switch {
		case strategy == "binary":
			installPath, err := projection.String(tool, item.Name, "install_path")
			if err != nil {
				return nil, err
			}
			plan.addDownload(item.Name, tool, installPath, "0755")
		case strategy == "apt":
			plan.aptPackages = append(plan.aptPackages, item.Name)
		case strategy == "go_install":
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
		case strategy == "uv_tool":
			pkg, err := projection.String(tool, item.Name, "uv_package")
			if err != nil {
				return nil, err
			}
			version, err := projection.String(tool, item.Name, "version")
			if err != nil {
				return nil, err
			}
			argv := []string{"/usr/local/bin/uv", "tool", "install", "--force", pkg + "==" + version}
			argv = append(argv, withArgs(projection.OptionalStringList(tool, "with"))...)
			plan.uvTools = append(plan.uvTools, map[string]any{"key": item.Name, "argv": argv})
			if item.Name == "ansible" {
				plan.addLink("ansible-lint", "/opt/uv-tools/ansible-core/bin/ansible-lint", "/usr/local/bin/ansible-lint")
			}
		case item.Name == "go":
			version, err := projection.String(tool, item.Name, "version")
			if err != nil {
				return nil, err
			}
			archive, err := archivePath(item.Name, tool)
			if err != nil {
				return nil, err
			}
			installDir := "/usr/local/go-" + version
			plan.addDownload(item.Name, tool, archive, "0644")
			plan.addDirectory(installDir, "0755")
			plan.addExtract(item.Name, archive, installDir, installDir+"/bin/go", []string{"--strip-components=1"})
			plan.replacementPaths = append(plan.replacementPaths, "/usr/local/go")
			plan.addLink(item.Name, installDir, "/usr/local/go")
		case item.Name == "zig":
			version, err := projection.String(tool, item.Name, "version")
			if err != nil {
				return nil, err
			}
			archive, err := archivePath(item.Name, tool)
			if err != nil {
				return nil, err
			}
			installDir := "/usr/local/zig-" + version
			plan.addDownload(item.Name, tool, archive, "0644")
			plan.addDirectory(installDir, "0755")
			plan.addExtract(item.Name, archive, installDir, installDir+"/zig", []string{"--strip-components=1"})
			plan.replacementPaths = append(plan.replacementPaths, "/usr/local/zig")
			plan.addLink(item.Name, installDir, "/usr/local/zig")
			plan.addLink(item.Name, "/usr/local/zig/zig", "/usr/local/bin/zig")
		case item.Name == "tofu":
			installPath, err := projection.String(tool, item.Name, "install_path")
			if err != nil {
				return nil, err
			}
			if err := plan.archiveSingleCopy(item.Name, tool, "tofu", installPath, nil); err != nil {
				return nil, err
			}
		case item.Name == "protoc":
			archive, err := archivePath(item.Name, tool)
			if err != nil {
				return nil, err
			}
			dest, err := extractDir(item.Name, tool)
			if err != nil {
				return nil, err
			}
			plan.addDownload(item.Name, tool, archive, "0644")
			plan.addDirectory(dest, "0755")
			plan.addExtract(item.Name, archive, dest, dest+"/bin/protoc", nil)
			plan.addCopy(item.Name, dest+"/bin/protoc", "/usr/local/bin/protoc", "0755")
			plan.addDirectoryCopy(item.Name, dest+"/include/", "/usr/local/include/", "0755")
		case item.Name == "cue":
			binName, err := projection.String(tool, item.Name, "bin_name")
			if err != nil {
				return nil, err
			}
			installPath, err := projection.String(tool, item.Name, "install_path")
			if err != nil {
				return nil, err
			}
			if err := plan.archiveSingleCopy(item.Name, tool, binName, installPath, nil); err != nil {
				return nil, err
			}
		case item.Name == "shellcheck":
			binName, err := projection.String(tool, item.Name, "bin_name")
			if err != nil {
				return nil, err
			}
			installPath, err := projection.String(tool, item.Name, "install_path")
			if err != nil {
				return nil, err
			}
			if err := plan.archiveSingleCopy(item.Name, tool, binName, installPath, []string{"--strip-components=1"}); err != nil {
				return nil, err
			}
		case item.Name == "age" || item.Name == "uv":
			if err := plan.archiveBins(item.Name, tool); err != nil {
				return nil, err
			}
		case item.Name == "clickhouse":
			archive, err := archivePath(item.Name, tool)
			if err != nil {
				return nil, err
			}
			dest, err := extractDir(item.Name, tool)
			if err != nil {
				return nil, err
			}
			version, err := projection.String(tool, item.Name, "version")
			if err != nil {
				return nil, err
			}
			member := fmt.Sprintf("clickhouse-common-static-%s/usr/bin/clickhouse", version)
			plan.addDownload(item.Name, tool, archive, "0644")
			plan.addDirectory(dest, "0755")
			plan.addExtract(item.Name, archive, dest, dest+"/"+member, nil)
			plan.addCopy(item.Name, dest+"/"+member, "/usr/local/bin/clickhouse", "0755")
			for _, suffix := range []string{"server", "client", "local", "keeper", "benchmark"} {
				plan.addLink(item.Name, "/usr/local/bin/clickhouse", "/usr/local/bin/clickhouse-"+suffix)
			}
		case item.Name == "stripe":
			installPath, err := projection.String(tool, item.Name, "install_path")
			if err != nil {
				return nil, err
			}
			if err := plan.archiveSingleCopy(item.Name, tool, "stripe", installPath, nil); err != nil {
				return nil, err
			}
		case strategy == "uv_tool_companion" || item.Name == "bazel":
			continue
		default:
			return nil, fmt.Errorf("dev tool %s has unsupported install-plan strategy %s", item.Name, strategy)
		}
	}

	return map[string]any{
		"tools":                            plan.tools,
		"downloads":                        plan.downloads,
		"directories":                      plan.directories,
		"extracts":                         plan.extracts,
		"copies":                           plan.copies,
		"directory_copies":                 plan.directoryCopies,
		"links":                            plan.links,
		"go_installs":                      plan.goInstalls,
		"uv_tools":                         plan.uvTools,
		"apt_packages":                     plan.aptPackages,
		"replacement_paths":                plan.replacementPaths,
		"profile_files":                    []map[string]any{{"dest": "/etc/profile.d/go.sh", "mode": "0644", "content": "export PATH=/usr/local/go/bin:" + goPath + "/bin:$PATH\n"}},
		"ansible_collections_requirements": "{{ playbook_dir }}/../requirements.yml",
		"smoke_test_spans":                 smokeTestSpans(plan.tools),
	}, nil
}

func (p *installPlan) toolRecord(key string, tool map[string]any) {
	strategy, _ := projection.OptionalString(tool, key, "strategy")
	version, _ := projection.OptionalString(tool, key, "version")
	sha256, _ := projection.OptionalString(tool, key, "sha256")
	p.tools = append(p.tools, map[string]any{
		"key":      key,
		"strategy": strategy,
		"version":  version,
		"sha256":   sha256,
	})
}

// withArgs flattens an `--with X --with Y` flag list for `uv tool install`.
func withArgs(deps []string) []string {
	if len(deps) == 0 {
		return nil
	}
	out := make([]string, 0, 2*len(deps))
	for _, dep := range deps {
		out = append(out, "--with", dep)
	}
	return out
}

func (p *installPlan) addDirectory(path, mode string) {
	item := map[string]any{"path": path, "mode": mode}
	for _, existing := range p.directories {
		if existing["path"] == path && existing["mode"] == mode {
			return
		}
	}
	p.directories = append(p.directories, item)
}

func (p *installPlan) addDownload(key string, tool map[string]any, dest, mode string) {
	sha256, _ := projection.OptionalString(tool, key, "sha256")
	p.downloads = append(p.downloads, map[string]any{
		"key":      key,
		"url":      tool["url"],
		"dest":     dest,
		"checksum": "sha256:" + sha256,
		"mode":     mode,
	})
}

func (p *installPlan) addExtract(key, src, dest, creates string, extraOpts []string) {
	item := map[string]any{"key": key, "src": src, "dest": dest, "creates": creates}
	if len(extraOpts) > 0 {
		item["extra_opts"] = extraOpts
	}
	p.extracts = append(p.extracts, item)
}

func (p *installPlan) addCopy(key, src, dest, mode string) {
	p.copies = append(p.copies, map[string]any{"key": key, "src": src, "dest": dest, "mode": mode})
}

func (p *installPlan) addDirectoryCopy(key, src, dest, mode string) {
	p.directoryCopies = append(p.directoryCopies, map[string]any{"key": key, "src": src, "dest": dest, "mode": mode})
}

func (p *installPlan) addLink(key, src, dest string) {
	p.links = append(p.links, map[string]any{"key": key, "src": src, "dest": dest, "force": true})
}

func (p *installPlan) archiveSingleCopy(key string, tool map[string]any, member, installPath string, extraOpts []string) error {
	archive, err := archivePath(key, tool)
	if err != nil {
		return err
	}
	dest, err := extractDir(key, tool)
	if err != nil {
		return err
	}
	p.addDownload(key, tool, archive, "0644")
	p.addDirectory(dest, "0755")
	p.addExtract(key, archive, dest, dest+"/"+member, extraOpts)
	p.addCopy(key, dest+"/"+member, installPath, "0755")
	return nil
}

func (p *installPlan) archiveBins(key string, tool map[string]any) error {
	archive, err := archivePath(key, tool)
	if err != nil {
		return err
	}
	dest, err := extractDir(key, tool)
	if err != nil {
		return err
	}
	bins, err := projection.StringList(tool, key, "bins")
	if err != nil {
		return err
	}
	if len(bins) == 0 {
		return fmt.Errorf("%s.bins: expected at least one binary", key)
	}
	p.addDownload(key, tool, archive, "0644")
	p.addDirectory(dest, "0755")
	p.addExtract(key, archive, dest, dest+"/"+bins[0], []string{"--strip-components=1"})
	for _, binary := range bins {
		p.addCopy(key, dest+"/"+binary, "/usr/local/bin/"+binary, "0755")
	}
	return nil
}

func smokeTestSpans(tools []map[string]any) []map[string]any {
	spans := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		spans = append(spans, map[string]any{
			"name": "install_plan.artifact.publish",
			"attributes": map[string]any{
				"install_plan.surface":        "dev_tools",
				"install_plan.tool":           tool["key"],
				"install_plan.strategy":       tool["strategy"],
				"install_plan.version":        tool["version"],
				"install_plan.sha256":         tool["sha256"],
				"install_plan.generated_from": "topology",
			},
		})
	}
	return spans
}

func archivePath(key string, tool map[string]any) (string, error) {
	version, err := projection.String(tool, key, "version")
	if err != nil {
		return "", err
	}
	suffix, err := archiveSuffix(tool)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s-%s.%s", stagingDir, toolSlug(key), version, suffix), nil
}

func extractDir(key string, tool map[string]any) (string, error) {
	version, err := projection.String(tool, key, "version")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s-%s", stagingDir, toolSlug(key), version), nil
}

func archiveSuffix(tool map[string]any) (string, error) {
	url, err := projection.String(tool, "tool", "url")
	if err != nil {
		return "", err
	}
	for _, suffix := range []string{".tar.gz", ".tar.xz", ".tgz", ".zip"} {
		if strings.HasSuffix(url, suffix) {
			return strings.TrimPrefix(suffix, "."), nil
		}
	}
	strategy, _ := projection.OptionalString(tool, "tool", "strategy")
	return archiveExtension(strategy)
}

func archiveExtension(formatName string) (string, error) {
	switch formatName {
	case "zip":
		return "zip", nil
	case "deb":
		return "deb", nil
	case "tarball":
		return "tar.gz", nil
	case "tarball_xz":
		return "tar.xz", nil
	default:
		return "", fmt.Errorf("unsupported tool archive format %s", formatName)
	}
}

func toolSlug(key string) string {
	return strings.ReplaceAll(key, "_", "-")
}

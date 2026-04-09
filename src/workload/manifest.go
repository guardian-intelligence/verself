// Package workload owns the transitional repo workload contract for VM jobs.
//
// Surgical note: a repo may still provide `.forge-metal/ci.toml`, but imported
// repos no longer require it. When the manifest is absent we infer a minimal
// Node-oriented contract from package metadata so repo bootstrap can proceed
// without a forge-metal-specific file. The long-term target remains
// runner-native workflow execution.
package workload

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const ManifestRelPath = ".forge-metal/ci.toml"

type RuntimeProfile string

const (
	RuntimeProfileAuto RuntimeProfile = "auto"
	RuntimeProfileNode RuntimeProfile = "node"
)

var (
	envNamePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	supportedServices = map[string]struct{}{
		"postgres": {},
	}
)

type Manifest struct {
	Version  int            `toml:"version"`
	WorkDir  string         `toml:"workdir"`
	Services []string       `toml:"services"`
	Prepare  []string       `toml:"prepare"`
	Run      []string       `toml:"run"`
	Env      []string       `toml:"env"`
	Profile  RuntimeProfile `toml:"profile"`
}

func LoadManifest(repoRoot string) (*Manifest, error) {
	path := filepath.Join(repoRoot, ManifestRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	if m.Version == 0 {
		m.Version = 1
	}
	if m.WorkDir == "" {
		m.WorkDir = "."
	}
	if m.Profile == "" {
		m.Profile = RuntimeProfileAuto
	}
	m.Services = normalizeStringList(m.Services)
	m.Env = normalizeStringList(m.Env)
	if len(m.Prepare) == 0 {
		m.Prepare = append([]string(nil), m.Run...)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

func InferManifest(repoRoot string, toolchain *Toolchain) (*Manifest, error) {
	pkg, err := loadPackageJSON(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return nil, fmt.Errorf("read package.json for inferred manifest: %w", err)
	}

	run, ok := inferScriptCommand(toolchain, pkg.Scripts)
	if !ok {
		return nil, fmt.Errorf("infer manifest: package.json is missing a ci, test, or build script")
	}

	manifest := &Manifest{
		Version: 1,
		WorkDir: ".",
		Prepare: cloneArgs(run),
		Run:     cloneArgs(run),
		Profile: RuntimeProfileAuto,
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (m *Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if len(m.Run) == 0 {
		return fmt.Errorf("manifest run is required")
	}
	if len(m.Prepare) == 0 {
		return fmt.Errorf("manifest prepare is required")
	}
	switch m.Profile {
	case RuntimeProfileAuto, RuntimeProfileNode:
	default:
		return fmt.Errorf("unsupported manifest profile %q", m.Profile)
	}
	for _, service := range m.Services {
		if !isSupportedService(service) {
			return fmt.Errorf("unsupported service %q", service)
		}
	}
	for _, name := range m.Env {
		if !envNamePattern.MatchString(name) {
			return fmt.Errorf("invalid env name %q", name)
		}
	}
	return nil
}

func (m *Manifest) RepoWorkDir() string {
	if m.WorkDir == "." || m.WorkDir == "" {
		return "/workspace"
	}
	return filepath.ToSlash(filepath.Join("/workspace", m.WorkDir))
}

func (m *Manifest) ResolvedPrepare() []string {
	return append([]string(nil), m.Prepare...)
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSupportedService(name string) bool {
	_, ok := supportedServices[name]
	return ok
}

func inferScriptCommand(toolchain *Toolchain, scripts map[string]string) ([]string, bool) {
	if len(scripts) == 0 {
		return nil, false
	}
	for _, script := range []string{"ci", "test", "build"} {
		if strings.TrimSpace(scripts[script]) == "" {
			continue
		}
		return packageManagerRunCommand(toolchain, script), true
	}
	return nil, false
}

func packageManagerRunCommand(toolchain *Toolchain, script string) []string {
	switch {
	case toolchain != nil && toolchain.PackageManager == PackageManagerPNPM:
		return []string{"pnpm", "run", script}
	case toolchain != nil && toolchain.PackageManager == PackageManagerBun:
		return []string{"bun", "run", script}
	default:
		return []string{"npm", "run", script}
	}
}

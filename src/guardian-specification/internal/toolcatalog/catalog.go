package toolcatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"github.com/verself/guardian-specification/internal/formatio"
)

type Catalog struct {
	Tools map[string]Tool `json:"tools" yaml:"tools" toml:"tools" toon:"tools"`
}

type Tool struct {
	Platforms map[string]PlatformTool `json:"platforms" yaml:"platforms" toml:"platforms" toon:"platforms"`
}

// PlatformTool is the per-platform catalog entry. The resolver needs only the
// executable basename: the tool's bytes are carried by the golden image (or the
// Bazel-materialized dev root) and located by convention at
// <root>/<name>/bin/<executable>. The upstream pin (url + sha256 / OCI digest)
// lives in the Bazel module graph, not here, so a digest is never authored in
// two places.
type PlatformTool struct {
	Executable string `json:"executable" yaml:"executable" toml:"executable" toon:"executable"`
}

type ResolvedTool struct {
	Name       string
	Platform   string
	Executable string
}

func Load(workspaceRoot string) (Catalog, string, error) {
	path, err := discoverCatalogPath(workspaceRoot)
	if err != nil {
		return Catalog{}, "", err
	}
	var catalog Catalog
	if strings.EqualFold(filepath.Ext(path), ".cue") {
		if err := decodeCUEFile(path, &catalog); err != nil {
			return Catalog{}, "", fmt.Errorf("load Guardian tool catalog %s: %w", relTo(workspaceRoot, path), err)
		}
	} else if err := formatio.DecodeFile(path, &catalog); err != nil {
		return Catalog{}, "", fmt.Errorf("load Guardian tool catalog %s: %w", relTo(workspaceRoot, path), err)
	}
	return catalog, path, nil
}

func Resolve(workspaceRoot string, name string) (ResolvedTool, error) {
	catalog, _, err := Load(workspaceRoot)
	if err != nil {
		return ResolvedTool{}, err
	}
	return ResolveFrom(catalog, name, CurrentPlatform())
}

func ResolveAll(workspaceRoot string) ([]ResolvedTool, error) {
	catalog, _, err := Load(workspaceRoot)
	if err != nil {
		return nil, err
	}
	platform := CurrentPlatform()
	names := make([]string, 0, len(catalog.Tools))
	for name := range catalog.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ResolvedTool, 0, len(names))
	for _, name := range names {
		resolved, err := ResolveFrom(catalog, name, platform)
		if err != nil {
			continue
		}
		out = append(out, resolved)
	}
	return out, nil
}

func ResolveFrom(catalog Catalog, name string, platform string) (ResolvedTool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ResolvedTool{}, errors.New("guardian: tool name is required")
	}
	tool, ok := catalog.Tools[name]
	if !ok {
		return ResolvedTool{}, fmt.Errorf("guardian: tool %q not found", name)
	}
	spec, ok := tool.Platforms[platform]
	if !ok {
		keys := make([]string, 0, len(tool.Platforms))
		for key := range tool.Platforms {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return ResolvedTool{}, fmt.Errorf("guardian: tool %q has no platform %q (available: %s)", name, platform, strings.Join(keys, ", "))
	}
	if strings.TrimSpace(spec.Executable) == "" {
		return ResolvedTool{}, fmt.Errorf("guardian: tool %q platform %q executable is required", name, platform)
	}
	return ResolvedTool{
		Name:       name,
		Platform:   platform,
		Executable: strings.TrimSpace(spec.Executable),
	}, nil
}

func CurrentPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func ToolNames(workspaceRoot string) ([]string, error) {
	catalog, _, err := Load(workspaceRoot)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(catalog.Tools))
	for name := range catalog.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func discoverCatalogPath(workspaceRoot string) (string, error) {
	var searched []string
	for _, rootRel := range []string{".config/guardian", ".guardian"} {
		root := filepath.Join(workspaceRoot, filepath.FromSlash(rootRel))
		for _, name := range []string{"tools.cue", "tools.yaml", "tools.toml"} {
			path := filepath.Join(root, name)
			searched = append(searched, path)
			if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
				return path, nil
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("stat %s: %w", relTo(workspaceRoot, path), err)
			}
		}
	}
	return "", fmt.Errorf("guardian: tool catalog not found\nsearched:\n  %s", strings.Join(relList(workspaceRoot, searched), "\n  "))
}

func decodeCUEFile(path string, out any) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve CUE path: %w", err)
	}
	cfg := &load.Config{Dir: filepath.Dir(abs)}
	if moduleRoot, err := findCUEModuleRoot(filepath.Dir(abs)); err != nil {
		return err
	} else if moduleRoot != "" {
		cfg.ModuleRoot = moduleRoot
	}
	instances := load.Instances([]string{abs}, cfg)
	if len(instances) != 1 {
		return fmt.Errorf("load CUE: expected one instance, got %d", len(instances))
	}
	instance := instances[0]
	if instance.Err != nil {
		return fmt.Errorf("load CUE: %w", instance.Err)
	}
	value := cuecontext.New().BuildInstance(instance)
	if err := value.Err(); err != nil {
		return fmt.Errorf("build CUE: %w", err)
	}
	if err := value.Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("CUE document is not concrete: %w", err)
	}
	data, err := value.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal CUE document: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode CUE document: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("multiple CUE documents are not supported")
}

func findCUEModuleRoot(start string) (string, error) {
	dir := start
	for {
		if stat, err := os.Stat(filepath.Join(dir, "cue.mod")); err == nil && stat.IsDir() {
			return dir, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat cue.mod: %w", err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func relList(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, relTo(root, path))
	}
	return out
}

func relTo(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return path
	}
	return filepath.ToSlash(rel)
}

package guardianconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"github.com/verself/guardian-specification/internal/formatio"
)

type RootConfig struct {
	DefaultProfile string                  `json:"defaultProfile" yaml:"defaultProfile" toml:"defaultProfile" toon:"defaultProfile"`
	Profiles       map[string]ProfileEntry `json:"profiles,omitempty" yaml:"profiles,omitempty" toml:"profiles,omitempty" toon:"profiles,omitempty"`
}

type ProfileEntry struct {
	Document string `json:"document,omitempty" yaml:"document,omitempty" toml:"document,omitempty" toon:"document,omitempty"`
}

type ResolveOptions struct {
	CWD        string
	ConfigPath string
	Profile    string
}

type Resolution struct {
	WorkspaceRoot  string
	ConfigRoot     string
	RootConfig     RootConfig
	RootConfigPath string
	ProfileName    string
	DocumentPath   string
	Searched       []string
	Warnings       []string
}

func DiscoverWorkspaceRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	for {
		for _, marker := range []string{"MODULE.bazel", "WORKSPACE", "WORKSPACE.bazel"} {
			if stat, err := os.Stat(filepath.Join(dir, marker)); err == nil && stat.Mode().IsRegular() {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("guardian must be run from inside a Bazel workspace")
		}
		dir = parent
	}
}

func ResolveProfile(opts ResolveOptions) (Resolution, error) {
	cwd := opts.CWD
	if strings.TrimSpace(cwd) == "" {
		current, err := os.Getwd()
		if err != nil {
			return Resolution{}, fmt.Errorf("cwd: %w", err)
		}
		cwd = current
	}
	workspaceRoot, err := DiscoverWorkspaceRoot(cwd)
	if err != nil {
		return Resolution{}, err
	}
	if strings.TrimSpace(opts.ConfigPath) != "" {
		configPath, err := absFrom(cwd, opts.ConfigPath)
		if err != nil {
			return Resolution{}, err
		}
		return resolveExplicitConfigFile(workspaceRoot, configPath, strings.TrimSpace(opts.Profile))
	}
	configRoot, rootPath, warnings, err := discoverConfigRoot(workspaceRoot)
	if err != nil {
		return Resolution{}, err
	}
	return resolveDiscoveredProfile(workspaceRoot, configRoot, rootPath, warnings, strings.TrimSpace(opts.Profile))
}

func resolveExplicitConfigFile(workspaceRoot string, configPath string, profile string) (Resolution, error) {
	stat, err := os.Stat(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Resolution{}, fmt.Errorf("guardian: config file %s not found", relTo(workspaceRoot, configPath))
		}
		return Resolution{}, fmt.Errorf("stat config file %s: %w", relTo(workspaceRoot, configPath), err)
	}
	if !stat.Mode().IsRegular() {
		return Resolution{}, fmt.Errorf("guardian: config file %s is not a regular file", relTo(workspaceRoot, configPath))
	}

	root, rootErr := loadRootConfig(configPath)
	if rootErr == nil && isRootConfig(root) {
		configRoot := filepath.Dir(configPath)
		if profile == "" {
			profile = strings.TrimSpace(root.DefaultProfile)
		}
		if profile == "" {
			return Resolution{}, fmt.Errorf("guardian: profile is required and defaultProfile is empty in %s", relTo(workspaceRoot, configPath))
		}
		documentPath, searched, warnings, err := resolveProfileDocument(workspaceRoot, configRoot, root, profile)
		resolution := Resolution{
			WorkspaceRoot:  workspaceRoot,
			ConfigRoot:     configRoot,
			RootConfig:     root,
			RootConfigPath: configPath,
			ProfileName:    profile,
			DocumentPath:   documentPath,
			Searched:       searched,
			Warnings:       warnings,
		}
		if err != nil {
			return resolution, err
		}
		return resolution, nil
	}

	if profile == "" {
		profile = profileNameFromPath(configPath)
	} else {
		return Resolution{}, fmt.Errorf("guardian: profile %q cannot be used with standalone config file %s", profile, relTo(workspaceRoot, configPath))
	}
	return Resolution{
		WorkspaceRoot: workspaceRoot,
		ProfileName:   profile,
		DocumentPath:  configPath,
	}, nil
}

func isRootConfig(root RootConfig) bool {
	if strings.TrimSpace(root.DefaultProfile) != "" {
		return true
	}
	return len(root.Profiles) > 0
}

func resolveDiscoveredProfile(workspaceRoot string, configRoot string, rootPath string, warnings []string, profile string) (Resolution, error) {
	root, err := loadRootConfig(rootPath)
	if err != nil {
		return Resolution{}, fmt.Errorf("load Guardian root config %s: %w", relTo(workspaceRoot, rootPath), err)
	}
	if profile == "" {
		profile = strings.TrimSpace(root.DefaultProfile)
	}
	if profile == "" {
		return Resolution{}, fmt.Errorf("guardian: profile is required and defaultProfile is empty in %s", relTo(workspaceRoot, rootPath))
	}
	documentPath, searched, profileWarnings, err := resolveProfileDocument(workspaceRoot, configRoot, root, profile)
	warnings = append(warnings, profileWarnings...)
	resolution := Resolution{
		WorkspaceRoot:  workspaceRoot,
		ConfigRoot:     configRoot,
		RootConfig:     root,
		RootConfigPath: rootPath,
		ProfileName:    profile,
		DocumentPath:   documentPath,
		Searched:       searched,
		Warnings:       warnings,
	}
	if err != nil {
		return resolution, err
	}
	return resolution, nil
}

func ListProfiles(resolution Resolution) []string {
	names := map[string]struct{}{}
	for name := range resolution.RootConfig.Profiles {
		names[name] = struct{}{}
	}
	for _, path := range discoverProfileFiles(resolution.ConfigRoot) {
		if name := profileNameFromProfilePath(resolution.ConfigRoot, path); name != "" {
			names[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func discoverConfigRoot(workspaceRoot string) (string, string, []string, error) {
	var searched []string
	var warnings []string
	for _, rel := range []string{".config/guardian", ".guardian"} {
		root := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
		rootPath, rootWarnings, err := selectConfigFile(root)
		warnings = append(warnings, rootWarnings...)
		if err != nil {
			return "", "", nil, err
		}
		for _, name := range configFileNames() {
			searched = append(searched, filepath.Join(root, name))
		}
		if rootPath != "" {
			return root, rootPath, warnings, nil
		}
	}
	return "", "", nil, fmt.Errorf("guardian: root config not found\nsearched:\n  %s", strings.Join(relList(workspaceRoot, searched), "\n  "))
}

func selectConfigFile(root string) (string, []string, error) {
	var found []string
	for _, name := range configFileNames() {
		path := filepath.Join(root, name)
		if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
			found = append(found, path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("stat %s: %w", path, err)
		}
	}
	if len(found) == 0 {
		return "", nil, nil
	}
	var warnings []string
	if len(found) > 1 {
		ignored := relList(root, found[1:])
		warnings = append(warnings, fmt.Sprintf("warning: found multiple Guardian config files in %s; using %s, ignoring %s", relTo(filepath.Dir(root), root), filepath.Base(found[0]), strings.Join(ignored, ", ")))
	}
	return found[0], warnings, nil
}

func configFileNames() []string {
	return []string{"guardian.cue", "guardian.yaml", "guardian.toml"}
}

func loadRootConfig(path string) (RootConfig, error) {
	var root RootConfig
	if strings.EqualFold(filepath.Ext(path), ".cue") {
		if err := decodeCUEFile(path, &root); err != nil {
			return RootConfig{}, err
		}
		return root, nil
	}
	if err := formatio.DecodeFile(path, &root); err != nil {
		return RootConfig{}, err
	}
	return root, nil
}

func resolveProfileDocument(workspaceRoot string, configRoot string, root RootConfig, profile string) (string, []string, []string, error) {
	if entry, ok := root.Profiles[profile]; ok && strings.TrimSpace(entry.Document) != "" {
		path, err := absFrom(configRoot, entry.Document)
		if err != nil {
			return "", nil, nil, err
		}
		if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
			if err := rejectNestedRootConfig(workspaceRoot, profile, path); err != nil {
				return "", []string{path}, nil, err
			}
			return path, nil, nil, nil
		} else if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", []string{path}, nil, profileNotFound(profile, []string{path}, workspaceRoot)
			}
			return "", []string{path}, nil, fmt.Errorf("stat profile document %s: %w", relTo(workspaceRoot, path), err)
		}
		return "", []string{path}, nil, fmt.Errorf("profile document %s is not a regular file", relTo(workspaceRoot, path))
	}
	searched := profileSearchPaths(configRoot, profile)
	var found []string
	for _, path := range searched {
		if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
			found = append(found, path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", searched, nil, fmt.Errorf("stat %s: %w", relTo(workspaceRoot, path), err)
		}
	}
	if len(found) == 0 {
		return "", searched, nil, profileNotFound(profile, searched, workspaceRoot)
	}
	var warnings []string
	if len(found) > 1 {
		ignored := relList(workspaceRoot, found[1:])
		warnings = append(warnings, fmt.Sprintf("warning: found multiple Guardian profile files for %q; using %s, ignoring %s", profile, relTo(workspaceRoot, found[0]), strings.Join(ignored, ", ")))
	}
	if err := rejectNestedRootConfig(workspaceRoot, profile, found[0]); err != nil {
		return "", searched, warnings, err
	}
	return found[0], searched, warnings, nil
}

func rejectNestedRootConfig(workspaceRoot string, profile string, path string) error {
	root, err := loadRootConfig(path)
	if err == nil && isRootConfig(root) {
		return fmt.Errorf("guardian: profile %q resolved to %s, which is a Guardian root config; profiles must resolve to Guardian documents, not another config file", profile, relTo(workspaceRoot, path))
	}
	return nil
}

func profileSearchPaths(configRoot string, profile string) []string {
	return []string{
		filepath.Join(configRoot, "profiles", profile+".cue"),
		filepath.Join(configRoot, "profiles", profile+".yaml"),
		filepath.Join(configRoot, "profiles", profile+".toml"),
		filepath.Join(configRoot, "profiles", profile, "guardian.cue"),
	}
}

func profileNotFound(profile string, searched []string, workspaceRoot string) error {
	return fmt.Errorf("guardian: profile %q not found\nsearched:\n  %s", profile, strings.Join(relList(workspaceRoot, searched), "\n  "))
}

func discoverProfileFiles(configRoot string) []string {
	var files []string
	profilesRoot := filepath.Join(configRoot, "profiles")
	for _, entry := range safeReadDir(profilesRoot) {
		path := filepath.Join(profilesRoot, entry.Name())
		if entry.Type().IsRegular() {
			switch filepath.Ext(entry.Name()) {
			case ".cue", ".yaml", ".toml":
				files = append(files, path)
			}
			continue
		}
		if entry.IsDir() {
			guardian := filepath.Join(path, "guardian.cue")
			if stat, err := os.Stat(guardian); err == nil && stat.Mode().IsRegular() {
				files = append(files, guardian)
			}
		}
	}
	sort.Strings(files)
	return files
}

func profileNameFromProfilePath(configRoot string, path string) string {
	profilesRoot := filepath.Join(configRoot, "profiles")
	rel, err := filepath.Rel(profilesRoot, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 1 {
		return strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
	}
	if len(parts) == 2 && parts[1] == "guardian.cue" {
		return parts[0]
	}
	return ""
}

func safeReadDir(path string) []os.DirEntry {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	return entries
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
	if instance.Incomplete {
		return fmt.Errorf("load CUE: incomplete instance")
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

func absFrom(base string, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Abs(filepath.Join(base, path))
}

func profileNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "guardian" {
		parent := filepath.Base(filepath.Dir(path))
		if parent != "." && parent != string(filepath.Separator) {
			return parent
		}
	}
	return base
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

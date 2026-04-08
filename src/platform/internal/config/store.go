package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Paths defines the config file paths used for git-style scope layering.
type Paths struct {
	System      string
	Global      string
	Local       string
	Credentials string
}

// Scope selects a config file layer.
type Scope string

const (
	ScopeSystem    Scope = "system"
	ScopeGlobal    Scope = "global"
	ScopeLocal     Scope = "local"
	ScopeEffective Scope = "effective"
)

// Entry is a flattened config value plus its winning origin.
type Entry struct {
	Key    string
	Value  string
	Origin string
	Secret bool
}

func (e Entry) DisplayValue() string {
	if e.Secret && e.Value != "" {
		return "***"
	}
	return e.Value
}

type valueKind string

const (
	kindString valueKind = "string"
	kindBool   valueKind = "bool"
	kindInt    valueKind = "int"
)

type keySpec struct {
	Name   string
	Path   []string
	Kind   valueKind
	Secret bool
	EnvVar string
}

var configKeys = []keySpec{
	{Name: "clickhouse.addr", Path: []string{"clickhouse", "addr"}, Kind: kindString},
	{Name: "clickhouse.database", Path: []string{"clickhouse", "database"}, Kind: kindString},
	{Name: "clickhouse.password", Path: []string{"clickhouse", "password"}, Kind: kindString, Secret: true, EnvVar: "FORGE_METAL_CLICKHOUSE_PASSWORD"},
	{Name: "clickhouse.replication.cluster", Path: []string{"clickhouse", "replication", "cluster"}, Kind: kindString},
	{Name: "clickhouse.replication.enabled", Path: []string{"clickhouse", "replication", "enabled"}, Kind: kindBool},
	{Name: "clickhouse.username", Path: []string{"clickhouse", "username"}, Kind: kindString},
	{Name: "latitude.auth_token", Path: []string{"latitude", "auth_token"}, Kind: kindString, Secret: true, EnvVar: "LATITUDESH_AUTH_TOKEN"},
	{Name: "latitude.billing", Path: []string{"latitude", "billing"}, Kind: kindString},
	{Name: "latitude.operating_system", Path: []string{"latitude", "operating_system"}, Kind: kindString},
	{Name: "latitude.plan", Path: []string{"latitude", "plan"}, Kind: kindString},
	{Name: "latitude.project", Path: []string{"latitude", "project"}, Kind: kindString, EnvVar: "LATITUDESH_PROJECT"},
	{Name: "latitude.region", Path: []string{"latitude", "region"}, Kind: kindString},
	{Name: "ssh.private_key_path", Path: []string{"ssh", "private_key_path"}, Kind: kindString},
	{Name: "ssh.public_key_path", Path: []string{"ssh", "public_key_path"}, Kind: kindString},
	{Name: "ssh.user", Path: []string{"ssh", "user"}, Kind: kindString},
	{Name: "wireguard.interface", Path: []string{"wireguard", "interface"}, Kind: kindString},
	{Name: "wireguard.listen_port", Path: []string{"wireguard", "listen_port"}, Kind: kindInt},
	{Name: "wireguard.network", Path: []string{"wireguard", "network"}, Kind: kindString},
	{Name: "zfs.ci_dataset", Path: []string{"zfs", "ci_dataset"}, Kind: kindString},
	{Name: "zfs.golden_dataset", Path: []string{"zfs", "golden_dataset"}, Kind: kindString},
	{Name: "zfs.pool", Path: []string{"zfs", "pool"}, Kind: kindString},
}

var configKeyByName = func() map[string]keySpec {
	byName := make(map[string]keySpec, len(configKeys))
	for _, spec := range configKeys {
		byName[spec.Name] = spec
	}
	return byName
}()

// DefaultPaths returns the default local/global/system config paths.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	return Paths{
		System:      filepath.Join(string(os.PathSeparator), "etc", "forge-metal", "config.toml"),
		Global:      filepath.Join(configHome, "forge-metal", "config.toml"),
		Local:       "forge-metal.toml",
		Credentials: filepath.Join(configHome, "forge-metal", "credentials.toml"),
	}, nil
}

func (p Paths) pathFor(scope Scope) (string, error) {
	switch scope {
	case ScopeSystem:
		return p.System, nil
	case ScopeGlobal:
		return p.Global, nil
	case ScopeLocal:
		return p.Local, nil
	default:
		return "", fmt.Errorf("invalid scope %q", scope)
	}
}

// LookupKey validates a supported config key.
func LookupKey(name string) (string, bool, error) {
	spec, ok := configKeyByName[name]
	if !ok {
		return "", false, fmt.Errorf("unknown config key %q", name)
	}
	return spec.Name, spec.Secret, nil
}

// Get resolves a single key from one scope or from the effective merged config.
func Get(paths Paths, key string, scope Scope) (Entry, error) {
	spec, ok := configKeyByName[key]
	if !ok {
		return Entry{}, fmt.Errorf("unknown config key %q", key)
	}

	if scope == ScopeEffective {
		entry, ok, err := resolveEntry(paths, spec)
		if err != nil {
			return Entry{}, err
		}
		if !ok {
			return Entry{}, fmt.Errorf("config key %q is not set", key)
		}
		return entry, nil
	}

	path, err := paths.pathFor(scope)
	if err != nil {
		return Entry{}, err
	}
	raw, err := readConfigMap(path)
	if err != nil {
		return Entry{}, err
	}
	value, ok := getNested(raw, spec.Path)
	if !ok {
		return Entry{}, fmt.Errorf("config key %q is not set in %s scope", key, scope)
	}
	return Entry{
		Key:    spec.Name,
		Value:  stringifyValue(value),
		Origin: "file:" + path,
		Secret: spec.Secret,
	}, nil
}

// List returns flattened config values from one scope or the effective merged config.
func List(paths Paths, scope Scope) ([]Entry, error) {
	entries := make([]Entry, 0, len(configKeys))

	if scope == ScopeEffective {
		for _, spec := range configKeys {
			entry, ok, err := resolveEntry(paths, spec)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			entries = append(entries, entry)
		}
		sortEntries(entries)
		return entries, nil
	}

	path, err := paths.pathFor(scope)
	if err != nil {
		return nil, err
	}
	raw, err := readConfigMap(path)
	if err != nil {
		return nil, err
	}
	for _, spec := range configKeys {
		value, ok := getNested(raw, spec.Path)
		if !ok {
			continue
		}
		entries = append(entries, Entry{
			Key:    spec.Name,
			Value:  stringifyValue(value),
			Origin: "file:" + path,
			Secret: spec.Secret,
		})
	}
	sortEntries(entries)
	return entries, nil
}

// Set writes a static config key to the selected scope.
func Set(paths Paths, scope Scope, key, value string) error {
	spec, ok := configKeyByName[key]
	if !ok {
		return fmt.Errorf("unknown config key %q", key)
	}
	if spec.Secret {
		if spec.EnvVar != "" {
			return fmt.Errorf("config key %q is secret and cannot be set with `forge-metal config set`; use %s or the interactive workflow instead", key, spec.EnvVar)
		}
		return fmt.Errorf("config key %q is secret and cannot be set with `forge-metal config set`", key)
	}

	path, err := paths.pathFor(scope)
	if err != nil {
		return err
	}

	typed, err := parseValue(spec, value)
	if err != nil {
		return err
	}

	raw, err := readConfigMap(path)
	if err != nil {
		return err
	}
	setNested(raw, spec.Path, typed)
	return writeConfigMap(path, raw)
}

// Unset removes a config key from the selected scope.
func Unset(paths Paths, scope Scope, key string) error {
	spec, ok := configKeyByName[key]
	if !ok {
		return fmt.Errorf("unknown config key %q", key)
	}

	path, err := paths.pathFor(scope)
	if err != nil {
		return err
	}

	raw, err := readConfigMap(path)
	if err != nil {
		return err
	}
	if _, ok := getNested(raw, spec.Path); !ok {
		return fmt.Errorf("config key %q is not set in %s scope", key, scope)
	}

	deleteNested(raw, spec.Path)
	return writeConfigMap(path, raw)
}

func resolveEntry(paths Paths, spec keySpec) (Entry, bool, error) {
	if spec.EnvVar != "" {
		if value, ok := os.LookupEnv(spec.EnvVar); ok {
			return Entry{Key: spec.Name, Value: value, Origin: "env:" + spec.EnvVar, Secret: spec.Secret}, true, nil
		}
	}

	scopePaths := []string{paths.Local, paths.Global, paths.System}
	for _, path := range scopePaths {
		raw, err := readConfigMap(path)
		if err != nil {
			return Entry{}, false, err
		}
		value, ok := getNested(raw, spec.Path)
		if !ok {
			continue
		}
		return Entry{
			Key:    spec.Name,
			Value:  stringifyValue(value),
			Origin: "file:" + path,
			Secret: spec.Secret,
		}, true, nil
	}

	raw, err := defaultConfigMap()
	if err != nil {
		return Entry{}, false, err
	}
	value, ok := getNested(raw, spec.Path)
	if !ok {
		return Entry{}, false, nil
	}

	return Entry{Key: spec.Name, Value: stringifyValue(value), Origin: "default", Secret: spec.Secret}, true, nil
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
}

func parseValue(spec keySpec, value string) (any, error) {
	switch spec.Kind {
	case kindString:
		return value, nil
	case kindBool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("parse %s as bool: %w", spec.Name, err)
		}
		return parsed, nil
	case kindInt:
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("parse %s as int: %w", spec.Name, err)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("unsupported value type for %s", spec.Name)
	}
}

func stringifyValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return fmt.Sprint(v)
	}
}

func readConfigMap(path string) (map[string]any, error) {
	raw := make(map[string]any)
	if path == "" {
		return raw, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return raw, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return raw, nil
}

func defaultConfigMap() (map[string]any, error) {
	raw := make(map[string]any)
	if err := toml.Unmarshal(defaultConfig, &raw); err != nil {
		return nil, fmt.Errorf("parse embedded default config: %w", err)
	}
	return raw, nil
}

func writeConfigMap(path string, raw map[string]any) error {
	if path == "" {
		return fmt.Errorf("config path is empty")
	}

	if len(raw) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove config %s: %w", path, err)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", filepath.Dir(path), err)
	}

	data, err := toml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config %s: %w", path, err)
	}

	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

func getNested(raw map[string]any, path []string) (any, bool) {
	var current any = raw
	for i, part := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := m[part]
		if !ok {
			return nil, false
		}
		if i == len(path)-1 {
			return value, true
		}
		current = value
	}
	return nil, false
}

func setNested(raw map[string]any, path []string, value any) {
	current := raw
	for _, part := range path[:len(path)-1] {
		next, ok := current[part]
		if !ok {
			child := make(map[string]any)
			current[part] = child
			current = child
			continue
		}

		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			current[part] = child
		}
		current = child
	}
	current[path[len(path)-1]] = value
}

func deleteNested(raw map[string]any, path []string) bool {
	current := raw
	for _, part := range path[:len(path)-1] {
		next, ok := current[part]
		if !ok {
			return len(raw) == 0
		}
		child, ok := next.(map[string]any)
		if !ok {
			return len(raw) == 0
		}
		current = child
	}
	delete(current, path[len(path)-1])
	pruneEmptyMaps(raw)
	return len(raw) == 0
}

func pruneEmptyMaps(raw map[string]any) {
	for key, value := range raw {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		pruneEmptyMaps(child)
		if len(child) == 0 {
			delete(raw, key)
		}
	}
}

// ParseScopeFlags resolves git-style scope flags into a single scope.
func ParseScopeFlags(local, global, system bool) (Scope, error) {
	selected := make([]Scope, 0, 3)
	if local {
		selected = append(selected, ScopeLocal)
	}
	if global {
		selected = append(selected, ScopeGlobal)
	}
	if system {
		selected = append(selected, ScopeSystem)
	}
	if len(selected) > 1 {
		names := make([]string, 0, len(selected))
		for _, scope := range selected {
			names = append(names, string(scope))
		}
		slices.Sort(names)
		return "", fmt.Errorf("scope flags are mutually exclusive: %s", strings.Join(names, ", "))
	}
	if len(selected) == 0 {
		return ScopeEffective, nil
	}
	return selected[0], nil
}

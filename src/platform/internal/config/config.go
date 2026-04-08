package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "embed"

	toml "github.com/pelletier/go-toml/v2"
)

//go:embed default.toml
var defaultConfig []byte

// Config is the top-level configuration for forge-metal.
type Config struct {
	ClickHouse  ClickHouseConfig  `toml:"clickhouse" json:"clickhouse"`
	ZFS         ZFSConfig         `toml:"zfs" json:"zfs"`
	WireGuard   WireGuardConfig   `toml:"wireguard" json:"wireguard"`
	Latitude    LatitudeConfig    `toml:"latitude" json:"latitude"`
	SSH         SSHConfig         `toml:"ssh" json:"ssh"`
	SupplyChain SupplyChainConfig `toml:"supplychain" json:"supplychain"`
	Source      ConfigSource      `toml:"-" json:"-"`
}

// SupplyChainConfig controls the Verdaccio mirror scan pipeline.
type SupplyChainConfig struct {
	MinReleaseAgeDays int               `toml:"min_release_age_days" json:"min_release_age_days"`
	OSVDatabasePath   string            `toml:"osv_database_path" json:"osv_database_path"`
	GuardDogExclude   []string          `toml:"guarddog_exclude_rules" json:"guarddog_exclude_rules"`
	Allowlist         map[string]string `toml:"allowlist" json:"allowlist"`
}

// ValueSource records where a config value came from.
type ValueSource string

const (
	SourceUnset   ValueSource = ""
	SourceDefault ValueSource = "default"
	SourceFile    ValueSource = "file"
	SourceEnv     ValueSource = "env"
)

// ConfigSource tracks provenance for the fields that drive CLI UX.
type ConfigSource struct {
	Latitude LatitudeSource `toml:"-" json:"-"`
}

// LatitudeSource tracks provenance for Latitude-specific settings.
type LatitudeSource struct {
	AuthToken ValueSource `toml:"-" json:"-"`
	Region    ValueSource `toml:"-" json:"-"`
	Plan      ValueSource `toml:"-" json:"-"`
	Project   ValueSource `toml:"-" json:"-"`
}

// ClickHouseConfig holds ClickHouse connection settings.
type ClickHouseConfig struct {
	Addr        string            `toml:"addr" json:"addr"`
	Database    string            `toml:"database" json:"database"`
	Username    string            `toml:"username" json:"username"`
	Password    string            `toml:"password" json:"-"`
	Replication ReplicationConfig `toml:"replication" json:"replication"`
}

// ReplicationConfig holds ClickHouse replication settings.
type ReplicationConfig struct {
	Enabled bool   `toml:"enabled" json:"enabled"`
	Cluster string `toml:"cluster" json:"cluster"`
}

// ZFSConfig holds ZFS pool and dataset settings.
type ZFSConfig struct {
	Pool          string `toml:"pool" json:"pool"`
	GoldenDataset string `toml:"golden_dataset" json:"golden_dataset"`
	CIDataset     string `toml:"ci_dataset" json:"ci_dataset"`
}

// WireGuardConfig holds WireGuard mesh networking settings.
type WireGuardConfig struct {
	Interface  string `toml:"interface" json:"interface"`
	ListenPort int    `toml:"listen_port" json:"listen_port"`
	Network    string `toml:"network" json:"network"`
}

// LatitudeConfig holds settings for the Latitude.sh bare-metal provider.
type LatitudeConfig struct {
	AuthToken       string `toml:"auth_token" json:"-"`
	Region          string `toml:"region" json:"region"`
	Plan            string `toml:"plan" json:"plan"`
	OperatingSystem string `toml:"operating_system" json:"operating_system"`
	Project         string `toml:"project" json:"project"`
	Billing         string `toml:"billing" json:"billing"`
}

// SSHConfig holds SSH connection parameters.
type SSHConfig struct {
	PrivateKeyPath string `toml:"private_key_path" json:"private_key_path"`
	PublicKeyPath  string `toml:"public_key_path" json:"public_key_path"`
	User           string `toml:"user" json:"user"`
}

// Load builds a Config by layering: embedded defaults, then forge-metal.toml,
// then explicit --config file, then environment variable overrides.
func Load(path string) (*Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	return LoadWithPaths(paths, path)
}

// LoadWithPaths builds a Config using explicit scope paths.
func LoadWithPaths(paths Paths, path string) (*Config, error) {
	cfg := &Config{}

	// 1. Embedded defaults.
	if err := toml.Unmarshal(defaultConfig, cfg); err != nil {
		return nil, fmt.Errorf("parsing embedded default config: %w", err)
	}
	cfg.markDefaultSources()

	// 2. System/global/local overlays.
	for _, candidate := range []string{paths.System, paths.Global, paths.Local} {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			if err := overlayFile(cfg, candidate, SourceFile); err != nil {
				return nil, fmt.Errorf("loading config from %s: %w", candidate, err)
			}
			slog.Info("loaded config", "path", candidate)
		}
	}

	// 3. Explicit --config file overlay.
	if path != "" {
		if err := overlayFile(cfg, path, SourceFile); err != nil {
			return nil, fmt.Errorf("loading config from %s: %w", path, err)
		}
		slog.Info("loaded config overlay", "path", path)
	}

	// 4. Environment variable overrides (highest precedence).
	applyEnvOverrides(cfg)

	return cfg, nil
}

func overlayFile(cfg *Config, path string, source ValueSource) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	if err := cfg.markOverlaySources(data, source); err != nil {
		return fmt.Errorf("tracking config sources: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing TOML: %w", err)
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LATITUDESH_AUTH_TOKEN"); v != "" {
		cfg.Latitude.AuthToken = v
		cfg.Source.Latitude.AuthToken = SourceEnv
	}
	if v := os.Getenv("LATITUDESH_PROJECT"); v != "" {
		cfg.Latitude.Project = v
		cfg.Source.Latitude.Project = SourceEnv
	}
	if v := os.Getenv("FORGE_METAL_CLICKHOUSE_PASSWORD"); v != "" {
		cfg.ClickHouse.Password = v
	}
}

func (c *Config) markDefaultSources() {
	if c.Latitude.Region != "" {
		c.Source.Latitude.Region = SourceDefault
	}
	if c.Latitude.Plan != "" {
		c.Source.Latitude.Plan = SourceDefault
	}
}

func (c *Config) markOverlaySources(data []byte, source ValueSource) error {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return err
	}

	latitudeRaw, ok := raw["latitude"]
	if !ok {
		return nil
	}

	latitudeMap, ok := latitudeRaw.(map[string]any)
	if !ok {
		return nil
	}

	if _, ok := latitudeMap["auth_token"]; ok {
		c.Source.Latitude.AuthToken = source
	}
	if _, ok := latitudeMap["project"]; ok {
		c.Source.Latitude.Project = source
	}
	if _, ok := latitudeMap["region"]; ok {
		c.Source.Latitude.Region = source
	}
	if _, ok := latitudeMap["plan"]; ok {
		c.Source.Latitude.Plan = source
	}

	return nil
}

// ExpandPaths resolves ~ to the current user's home directory in file paths.
func (c *Config) ExpandPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	c.SSH.PrivateKeyPath = expandTilde(c.SSH.PrivateKeyPath, home)
	c.SSH.PublicKeyPath = expandTilde(c.SSH.PublicKeyPath, home)
	return nil
}

func expandTilde(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	if path == "~" {
		return home
	}
	return path
}

// SaveLatitude persists Latitude.sh provisioning selections to forge-metal.toml.
// Merges with existing file content so other sections are preserved.
// File is written with 0600 permissions since it contains the API token.
func SaveLatitude(token, project, region, plan string) error {
	const localFile = "forge-metal.toml"

	existing := make(map[string]any)
	if data, err := os.ReadFile(localFile); err == nil {
		_ = toml.Unmarshal(data, &existing)
	}

	existing["latitude"] = map[string]any{
		"auth_token": token,
		"project":    project,
		"region":     region,
		"plan":       plan,
	}

	data, err := toml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(localFile, data, 0o600)
}

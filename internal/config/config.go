package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	toml "github.com/pelletier/go-toml/v2"
)

//go:embed default.toml
var defaultConfig []byte

// Config is the top-level configuration for forge-metal.
type Config struct {
	Controller ControllerConfig `toml:"controller" json:"controller"`
	Agent      AgentConfig      `toml:"agent" json:"agent"`
	ClickHouse ClickHouseConfig `toml:"clickhouse" json:"clickhouse"`
	ZFS        ZFSConfig        `toml:"zfs" json:"zfs"`
	WireGuard  WireGuardConfig  `toml:"wireguard" json:"wireguard"`
	Latitude   LatitudeConfig   `toml:"latitude" json:"latitude"`
	SSH        SSHConfig        `toml:"ssh" json:"ssh"`
}

// ControllerConfig holds settings for the job scheduler.
type ControllerConfig struct {
	Listen     string      `toml:"listen" json:"listen"`
	GRPCListen string      `toml:"grpc_listen" json:"grpc_listen"`
	Queue      QueueConfig `toml:"queue" json:"queue"`
}

// QueueConfig holds settings for the SQLite-backed job queue.
type QueueConfig struct {
	DBPath string `toml:"db_path" json:"db_path"`
}

// AgentConfig holds settings for the worker agent.
type AgentConfig struct {
	HeartbeatInterval string        `toml:"heartbeat_interval" json:"heartbeat_interval"`
	ControllerAddr    string        `toml:"controller_addr" json:"controller_addr"`
	Sandbox           SandboxConfig `toml:"sandbox" json:"sandbox"`
}

// HeartbeatDuration parses the HeartbeatInterval string.
func (a AgentConfig) HeartbeatDuration() time.Duration {
	d, err := time.ParseDuration(a.HeartbeatInterval)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

// SandboxConfig holds gVisor sandbox settings.
type SandboxConfig struct {
	Runtime           string `toml:"runtime" json:"runtime"`
	MaxConcurrentJobs int    `toml:"max_concurrent_jobs" json:"max_concurrent_jobs"`
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
	Pool           string `toml:"pool" json:"pool"`
	GoldenDataset  string `toml:"golden_dataset" json:"golden_dataset"`
	CIDataset      string `toml:"ci_dataset" json:"ci_dataset"`
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
	cfg := &Config{}

	// 1. Embedded defaults.
	if err := toml.Unmarshal(defaultConfig, cfg); err != nil {
		return nil, fmt.Errorf("parsing embedded default config: %w", err)
	}

	// 2. Local forge-metal.toml overlay.
	const localFile = "forge-metal.toml"
	if _, err := os.Stat(localFile); err == nil {
		if err := overlayFile(cfg, localFile); err != nil {
			return nil, fmt.Errorf("loading config from %s: %w", localFile, err)
		}
		slog.Info("loaded config", "path", localFile)
	}

	// 3. Explicit --config file overlay.
	if path != "" {
		if err := overlayFile(cfg, path); err != nil {
			return nil, fmt.Errorf("loading config from %s: %w", path, err)
		}
		slog.Info("loaded config overlay", "path", path)
	}

	// 4. Environment variable overrides (highest precedence).
	applyEnvOverrides(cfg)

	return cfg, nil
}

func overlayFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing TOML: %w", err)
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LATITUDESH_AUTH_TOKEN"); v != "" {
		cfg.Latitude.AuthToken = v
	}
	if v := os.Getenv("LATITUDESH_PROJECT"); v != "" {
		cfg.Latitude.Project = v
	}
	if v := os.Getenv("FORGE_METAL_CLICKHOUSE_PASSWORD"); v != "" {
		cfg.ClickHouse.Password = v
	}
	if v := os.Getenv("FORGE_METAL_CONTROLLER_ADDR"); v != "" {
		cfg.Agent.ControllerAddr = v
	}
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

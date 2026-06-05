package specdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Document struct {
	Kind         string       `json:"kind" yaml:"kind" toml:"kind" toon:"kind"`
	Name         string       `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	StaticConfig StaticConfig `json:"staticConfig" yaml:"staticConfig" toml:"staticConfig" toon:"staticConfig"`
	Board        Board        `json:"board" yaml:"board" toml:"board" toon:"board"`
	Nomad        Nomad        `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
}

type StaticConfig struct {
	BaseURL        string `json:"baseURL" yaml:"baseURL" toml:"baseURL" toon:"baseURL"`
	CredentialsRef string `json:"credentialsRef" yaml:"credentialsRef" toml:"credentialsRef" toon:"credentialsRef"`
}

type Board struct {
	Substrate Substrate `json:"substrate" yaml:"substrate" toml:"substrate" toon:"substrate"`
	Access    Access    `json:"access" yaml:"access" toml:"access" toon:"access"`
	Seed      Seed      `json:"seed" yaml:"seed" toml:"seed" toon:"seed"`
}

type Substrate struct {
	StateDir string `json:"stateDir" yaml:"stateDir" toml:"stateDir" toon:"stateDir"`
}

type Access struct {
	SSH SSHAccess `json:"ssh" yaml:"ssh" toml:"ssh" toon:"ssh"`
}

type SSHAccess struct {
	Host              string             `json:"host" yaml:"host" toml:"host" toon:"host"`
	Port              int                `json:"port" yaml:"port" toml:"port" toon:"port"`
	User              string             `json:"user" yaml:"user" toml:"user" toon:"user"`
	KnownHostsFile    string             `json:"knownHostsFile" yaml:"knownHostsFile" toml:"knownHostsFile" toon:"knownHostsFile"`
	IdentityFile      string             `json:"identityFile,omitempty" yaml:"identityFile,omitempty" toml:"identityFile,omitempty" toon:"identityFile,omitempty"`
	ConnectTimeout    string             `json:"connectTimeout,omitempty" yaml:"connectTimeout,omitempty" toml:"connectTimeout,omitempty" toon:"connectTimeout,omitempty"`
	WireGuardFallback *WireGuardFallback `json:"wireguardFallback,omitempty" yaml:"wireguardFallback,omitempty" toml:"wireguardFallback,omitempty" toon:"wireguardFallback,omitempty"`
}

type WireGuardFallback struct {
	Host      string `json:"host" yaml:"host" toml:"host" toon:"host"`
	Port      int    `json:"port" yaml:"port" toml:"port" toon:"port"`
	Interface string `json:"interface,omitempty" yaml:"interface,omitempty" toml:"interface,omitempty" toon:"interface,omitempty"`
}

type Seed struct {
	TargetRoot string     `json:"targetRoot" yaml:"targetRoot" toml:"targetRoot" toon:"targetRoot"`
	Paths      []SeedPath `json:"paths" yaml:"paths" toml:"paths" toon:"paths"`
}

type SeedPath struct {
	Source string `json:"source" yaml:"source" toml:"source" toon:"source"`
	Target string `json:"target" yaml:"target" toml:"target" toon:"target"`
	Mode   string `json:"mode" yaml:"mode" toml:"mode" toon:"mode"`
}

type Nomad struct {
	Address   string     `json:"address" yaml:"address" toml:"address" toon:"address"`
	Namespace string     `json:"namespace" yaml:"namespace" toml:"namespace" toon:"namespace"`
	Jobs      []NomadJob `json:"jobs" yaml:"jobs" toml:"jobs" toon:"jobs"`
}

type NomadJob struct {
	Path        string   `json:"path" yaml:"path" toml:"path" toon:"path"`
	RequiredFor []string `json:"requiredFor,omitempty" yaml:"requiredFor,omitempty" toml:"requiredFor,omitempty" toon:"requiredFor,omitempty"`
}

func Validate(doc Document) error {
	if doc.Kind != "FlyProcedure" {
		return fmt.Errorf("kind must be FlyProcedure, got %q", doc.Kind)
	}
	if strings.TrimSpace(doc.StaticConfig.BaseURL) == "" {
		return errors.New("staticConfig.baseURL is required")
	}
	parsedBaseURL, err := url.Parse(doc.StaticConfig.BaseURL)
	if err != nil {
		return fmt.Errorf("staticConfig.baseURL is invalid: %w", err)
	}
	if parsedBaseURL.Scheme != "https" || parsedBaseURL.Host == "" {
		return errors.New("staticConfig.baseURL must be an https URL with a host")
	}
	if strings.TrimSpace(doc.StaticConfig.CredentialsRef) == "" {
		return errors.New("staticConfig.credentialsRef is required")
	}
	if strings.TrimSpace(doc.Board.Substrate.StateDir) == "" {
		return errors.New("board.substrate.stateDir is required")
	}
	ssh := doc.Board.Access.SSH
	if strings.TrimSpace(ssh.Host) == "" {
		return errors.New("board.access.ssh.host is required")
	}
	if strings.TrimSpace(ssh.User) == "" {
		return errors.New("board.access.ssh.user is required")
	}
	if ssh.Port < 1 || ssh.Port > 65535 {
		return errors.New("board.access.ssh.port must be between 1 and 65535")
	}
	if strings.TrimSpace(ssh.KnownHostsFile) == "" {
		return errors.New("board.access.ssh.knownHostsFile is required")
	}
	if ssh.WireGuardFallback != nil {
		if strings.TrimSpace(ssh.WireGuardFallback.Host) == "" {
			return errors.New("board.access.ssh.wireguardFallback.host is required")
		}
		if ssh.WireGuardFallback.Port < 1 || ssh.WireGuardFallback.Port > 65535 {
			return errors.New("board.access.ssh.wireguardFallback.port must be between 1 and 65535")
		}
	}
	if strings.TrimSpace(doc.Board.Seed.TargetRoot) == "" {
		return errors.New("board.seed.targetRoot is required")
	}
	if len(doc.Board.Seed.Paths) == 0 {
		return errors.New("board.seed.paths is required")
	}
	if strings.TrimSpace(doc.Nomad.Address) == "" {
		return errors.New("nomad.address is required")
	}
	if strings.TrimSpace(doc.Nomad.Namespace) == "" {
		return errors.New("nomad.namespace is required")
	}
	if len(doc.Nomad.Jobs) == 0 {
		return errors.New("nomad.jobs is required")
	}
	return nil
}

func CanonicalJSON(doc Document) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("canonical JSON: %w", err)
	}
	return data, nil
}

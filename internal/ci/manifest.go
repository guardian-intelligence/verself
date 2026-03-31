package ci

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

const ManifestRelPath = ".forge-metal/ci.toml"

type Manifest struct {
	Version         int      `toml:"version"`
	RepoName        string   `toml:"repo_name"`
	Description     string   `toml:"description"`
	DefaultBranch   string   `toml:"default_branch"`
	WorkDir         string   `toml:"workdir"`
	Services        []string `toml:"services"`
	WarmCommand     []string `toml:"warm_command"`
	CICommand       []string `toml:"ci_command"`
	PRBranch        string   `toml:"pr_branch"`
	PRTitle         string   `toml:"pr_title"`
	PRCommitMessage string   `toml:"pr_commit_message"`
	PRChangePath    string   `toml:"pr_change_path"`
	PRChangeFind    string   `toml:"pr_change_find"`
	PRChangeReplace string   `toml:"pr_change_replace"`
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
	if m.DefaultBranch == "" {
		m.DefaultBranch = "main"
	}
	if m.WorkDir == "" {
		m.WorkDir = "."
	}
	if len(m.WarmCommand) == 0 {
		m.WarmCommand = append([]string(nil), m.CICommand...)
	}
	if m.PRBranch == "" {
		m.PRBranch = "test/forge-metal-warm-path"
	}
	if m.PRTitle == "" {
		m.PRTitle = "test: trigger forge-metal warm path"
	}
	if m.PRCommitMessage == "" {
		m.PRCommitMessage = "test: trigger forge-metal warm path"
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

func (m *Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if len(m.CICommand) == 0 {
		return fmt.Errorf("manifest ci_command is required")
	}
	if len(m.WarmCommand) == 0 {
		return fmt.Errorf("manifest warm_command is required")
	}
	if m.PRChangePath == "" {
		return fmt.Errorf("manifest pr_change_path is required")
	}
	if m.PRChangeFind == "" {
		return fmt.Errorf("manifest pr_change_find is required")
	}
	if m.PRChangeReplace == "" {
		return fmt.Errorf("manifest pr_change_replace is required")
	}
	return nil
}

func (m *Manifest) RepoWorkDir() string {
	if m.WorkDir == "." || m.WorkDir == "" {
		return "/workspace"
	}
	return filepath.ToSlash(filepath.Join("/workspace", m.WorkDir))
}


package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// devToolEntry mirrors one entry in dev-tools.json.
type devToolEntry struct {
	Version    string `json:"version"`
	VersionCmd string `json:"version_cmd"`
}

// LoadManifest reads dev-tools.json from the repo root and returns
// the tool specs the doctor should check.
//
// It walks up from the current executable (or working directory) looking
// for dev-tools.json, so it works from any subdirectory of the repo.
func LoadManifest() ([]ToolSpec, error) {
	path, err := findDevToolsJSON()
	if err != nil {
		return nil, err
	}
	return loadManifestFrom(path)
}

func loadManifestFrom(path string) ([]ToolSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dev-tools.json: %w", err)
	}

	var raw map[string]devToolEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse dev-tools.json: %w", err)
	}

	// Stable iteration order: use a fixed ordering so doctor output is deterministic.
	// The order matches the tool categories in the plan: tarballs, go install, pip, apt.
	order := []string{
		"go", "zig", "tofu", "ansible", "protoc", "buf",
		"shellcheck", "jq", "sops", "age", "clickhouse",
		"golangci-lint", "gofumpt", "protoc-gen-go", "protoc-gen-go-grpc",
		"crun", "debootstrap",
	}

	var specs []ToolSpec
	for _, name := range order {
		entry, ok := raw[name]
		if !ok {
			continue
		}
		specs = append(specs, ToolSpec{
			Name:       name,
			VersionCmd: entry.VersionCmd,
			Expected:   entry.Version,
		})
	}

	// Include any tools not in the fixed order (future-proof).
	seen := make(map[string]bool, len(order))
	for _, name := range order {
		seen[name] = true
	}
	for name, entry := range raw {
		if seen[name] {
			continue
		}
		specs = append(specs, ToolSpec{
			Name:       name,
			VersionCmd: entry.VersionCmd,
			Expected:   entry.Version,
		})
	}

	return specs, nil
}

// findDevToolsJSON walks up from cwd looking for dev-tools.json.
func findDevToolsJSON() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "dev-tools.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("dev-tools.json not found (searched from %s to /); run from inside the forge-metal repo", runtime.GOOS)
}

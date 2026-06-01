package controllerstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const RootEnv = "VERSELF_CONTROLLER_STATE_ROOT"

// Root returns the operator-local controller state directory.
func Root(repoRoot string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(RootEnv))
	if raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("%s must be absolute", RootEnv)
		}
		return filepath.Clean(raw), nil
	}
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "", fmt.Errorf("repo root is required when %s is unset", RootEnv)
	}
	if !filepath.IsAbs(repoRoot) {
		abs, err := filepath.Abs(repoRoot)
		if err != nil {
			return "", fmt.Errorf("resolve repo root: %w", err)
		}
		repoRoot = abs
	}
	return filepath.Join(filepath.Clean(repoRoot), ".verself", "controller"), nil
}

// ConfigPath resolves a checked-in controller state path relative to Root.
func ConfigPath(repoRoot, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("controller state path is required")
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("controller state paths in site metadata must be relative to %s", RootEnv)
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("controller state path %q escapes %s", raw, RootEnv)
	}
	if clean == ".verself" || strings.HasPrefix(clean, ".verself"+string(filepath.Separator)) {
		return "", fmt.Errorf("controller state path %q must be relative to .verself/controller", raw)
	}
	root, err := Root(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, clean), nil
}

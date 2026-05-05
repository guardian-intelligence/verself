package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func readYAMLFile(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func siteVarsPath(repoRoot, site string) string {
	site = strings.TrimSpace(site)
	if site == "" {
		site = "prod"
	}
	return filepath.Join(repoRoot, "src", "host", "sites", site, "vars.yml")
}

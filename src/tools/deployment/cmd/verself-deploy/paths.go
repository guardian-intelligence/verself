package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func authoredInventoryPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "host-configuration", "sites", site, "inventory.ini")
}

func deploymentSiteConfigPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "tools", "deployment", "sites", site, "site.json")
}

func resolveRepoRoot(prefix, repoRoot string) (string, bool) {
	if repoRoot != "" {
		return repoRoot, true
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cwd: %v\n", prefix, err)
		return "", false
	}
	return cwd, true
}

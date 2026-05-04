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

func provisioningSiteDir(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "tools", "provisioning", "sites", site)
}

func hostConfigurationSiteSecretsPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "host-configuration", "sites", site, "secrets.sops.yml")
}

func deploymentSiteSecretsPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "tools", "deployment", "sites", site, "secrets.sops.yml")
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

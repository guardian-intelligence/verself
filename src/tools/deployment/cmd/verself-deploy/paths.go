package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func authoredInventoryPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "host-configuration", "ansible", site+".ini")
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

package main

import "path/filepath"

func authoredInventoryPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "substrate", "ansible", "inventory", site+".ini")
}

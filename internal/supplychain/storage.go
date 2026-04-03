package supplychain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findTarballs returns all .tgz files under the Verdaccio storage directory.
func findTarballs(storagePath string) ([]string, error) {
	var tarballs []string
	err := filepath.WalkDir(storagePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".tgz") {
			tarballs = append(tarballs, path)
		}
		return nil
	})
	return tarballs, err
}

// packument is the subset of a Verdaccio-cached package manifest we need.
type packument struct {
	Name string            `json:"name"`
	Time map[string]string `json:"time"` // version → ISO timestamp, plus "created"/"modified"
}

// loadPackument reads the package.json metadata file from a Verdaccio storage directory.
// Verdaccio stores packuments at <storage>/<package>/package.json.
func loadPackument(packageDir string) (*packument, error) {
	data, err := os.ReadFile(filepath.Join(packageDir, "package.json"))
	if err != nil {
		return nil, fmt.Errorf("read packument %s: %w", packageDir, err)
	}
	var p packument
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse packument %s: %w", packageDir, err)
	}
	return &p, nil
}

// findPackageDirs returns directories under storagePath that contain a package.json
// (i.e., are Verdaccio package entries, not just tarball directories).
func findPackageDirs(storagePath string) ([]string, error) {
	entries, err := os.ReadDir(storagePath)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(storagePath, entry.Name())

		// Handle scoped packages (@scope/name → @scope directory contains subdirs).
		if strings.HasPrefix(entry.Name(), "@") {
			scopeEntries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, scopeEntry := range scopeEntries {
				if !scopeEntry.IsDir() {
					continue
				}
				scopeDir := filepath.Join(dir, scopeEntry.Name())
				if hasPackument(scopeDir) {
					dirs = append(dirs, scopeDir)
				}
			}
			continue
		}

		if hasPackument(dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

func hasPackument(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "package.json"))
	return err == nil
}

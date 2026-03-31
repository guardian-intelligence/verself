package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PackageManager string

const (
	PackageManagerNPM  PackageManager = "npm"
	PackageManagerPNPM PackageManager = "pnpm"
	PackageManagerBun  PackageManager = "bun"
)

type Toolchain struct {
	PackageManager        PackageManager
	PackageManagerVersion string
	NodeVersion           string
	LockfileRelPath       string
}

type packageJSON struct {
	PackageManager string `json:"packageManager"`
	Volta          struct {
		Node string `json:"node"`
	} `json:"volta"`
	Engines struct {
		Node string `json:"node"`
	} `json:"engines"`
}

func DetectToolchain(repoRoot string) (*Toolchain, error) {
	tc := &Toolchain{}

	pkg, err := loadPackageJSON(filepath.Join(repoRoot, "package.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if pkg != nil {
		pm, version := parsePackageManager(pkg.PackageManager)
		tc.PackageManager = pm
		tc.PackageManagerVersion = version
		tc.NodeVersion = strings.TrimSpace(pkg.Volta.Node)
		if tc.NodeVersion == "" {
			tc.NodeVersion = strings.TrimSpace(pkg.Engines.Node)
		}
	}

	if tc.NodeVersion == "" {
		if version, err := readFirstLine(filepath.Join(repoRoot, ".nvmrc")); err == nil {
			tc.NodeVersion = version
		} else if version, err := readFirstLine(filepath.Join(repoRoot, ".node-version")); err == nil {
			tc.NodeVersion = version
		}
	}

	switch {
	case fileExists(filepath.Join(repoRoot, "bun.lockb")):
		if tc.PackageManager == "" {
			tc.PackageManager = PackageManagerBun
		}
		tc.LockfileRelPath = "bun.lockb"
	case fileExists(filepath.Join(repoRoot, "bun.lock")):
		if tc.PackageManager == "" {
			tc.PackageManager = PackageManagerBun
		}
		tc.LockfileRelPath = "bun.lock"
	case fileExists(filepath.Join(repoRoot, "pnpm-lock.yaml")):
		if tc.PackageManager == "" {
			tc.PackageManager = PackageManagerPNPM
		}
		tc.LockfileRelPath = "pnpm-lock.yaml"
	case fileExists(filepath.Join(repoRoot, "package-lock.json")):
		if tc.PackageManager == "" {
			tc.PackageManager = PackageManagerNPM
		}
		tc.LockfileRelPath = "package-lock.json"
	case fileExists(filepath.Join(repoRoot, "bunfig.toml")):
		if tc.PackageManager == "" {
			tc.PackageManager = PackageManagerBun
		}
	}

	if tc.PackageManager == "" {
		tc.PackageManager = PackageManagerNPM
	}

	switch tc.PackageManager {
	case PackageManagerNPM, PackageManagerPNPM, PackageManagerBun:
	default:
		return nil, fmt.Errorf("unsupported package manager %q", tc.PackageManager)
	}

	return tc, nil
}

func (tc *Toolchain) InstallCommand() []string {
	switch tc.PackageManager {
	case PackageManagerPNPM:
		if tc.PackageManagerVersion != "" {
			return []string{"bash", "-lc", fmt.Sprintf("npm install -g pnpm@%s && pnpm install --frozen-lockfile", tc.PackageManagerVersion)}
		}
		return []string{"bash", "-lc", "npm install -g pnpm && pnpm install --frozen-lockfile"}
	case PackageManagerBun:
		return []string{"bun", "install", "--frozen-lockfile", "--registry", "http://172.16.0.1:4873"}
	default:
		return []string{"npm", "install"}
	}
}

func (tc *Toolchain) LockfilePath(repoRoot string) string {
	if tc.LockfileRelPath == "" {
		return ""
	}
	return filepath.Join(repoRoot, tc.LockfileRelPath)
}

func ComputeFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func loadPackageJSON(path string) (*packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse package.json %s: %w", path, err)
	}
	return &pkg, nil
}

func parsePackageManager(value string) (PackageManager, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	parts := strings.SplitN(value, "@", 2)
	if len(parts) != 2 {
		return PackageManager(value), ""
	}
	return PackageManager(parts[0]), parts[1]
}

func readFirstLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

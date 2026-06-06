package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const managedHeader = "# Managed by Verself nftables component."

type config struct {
	repoRoot             string
	resourceGraph        string
	resourceName         string
	runtimeArtifact      string
	artifactRoot         string
	nftBin               string
	ldLibraryPath        string
	destRoot             string
	hostRuntimeRoot      string
	configPath           string
	rulesDir             string
	serviceUnitPath      string
	firewallTargetPath   string
	serviceArtifactRoot  string
	serviceNftBin        string
	serviceLDLibraryPath string
	manageSystemd        bool
	systemctlBin         string
}

type guardianDocument struct {
	Resources []guardianResource `json:"resources"`
}

type guardianResource struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   guardianMetadata     `json:"metadata"`
	Spec       nftablesFirewallSpec `json:"spec"`
}

type guardianMetadata struct {
	Name string `json:"name"`
}

type nftablesFirewallSpec struct {
	RuntimeArtifact string              `json:"runtimeArtifact"`
	RuntimeRoot     string              `json:"runtimeRoot"`
	ConfigPath      string              `json:"configPath"`
	RulesDir        string              `json:"rulesDir"`
	ManageSystemd   *bool               `json:"manageSystemd"`
	Systemd         nftablesSystemdSpec `json:"systemd"`
}

type nftablesSystemdSpec struct {
	ServiceUnitPath    string `json:"serviceUnitPath"`
	FirewallTargetPath string `json:"firewallTargetPath"`
}

type fileInstall struct {
	Source string
	Dest   string
	Mode   fs.FileMode
	Body   []byte
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nftables-apply: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if cfg.runtimeArtifact != "" {
		if err := installRepoRuntime(&cfg); err != nil {
			return err
		}
	} else if cfg.manageSystemd {
		if err := installHostRuntime(&cfg); err != nil {
			return err
		}
	}
	if err := installConfig(cfg); err != nil {
		return err
	}
	nftConf := destPath(cfg.destRoot, cfg.configPath)
	if err := runCommand(ctx, cfg.ldLibraryPath, cfg.nftBin, "-c", "-f", nftConf); err != nil {
		return err
	}
	if err := runCommand(ctx, cfg.ldLibraryPath, cfg.nftBin, "-f", nftConf); err != nil {
		return err
	}
	if cfg.destRoot == "/" && cfg.manageSystemd {
		if err := runCommand(ctx, "", cfg.systemctlBin, "daemon-reload"); err != nil {
			return err
		}
		if err := runCommand(ctx, "", cfg.systemctlBin, "enable", "verself-nftables.service", "verself-firewall.target"); err != nil {
			return err
		}
		if err := runCommand(ctx, "", cfg.systemctlBin, "restart", "verself-nftables.service"); err != nil {
			return err
		}
		if err := runCommand(ctx, "", cfg.systemctlBin, "start", "verself-firewall.target"); err != nil {
			return err
		}
	}
	fmt.Println("nftables-apply: host firewall converged")
	return nil
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("nftables-apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := config{
		repoRoot:           "/home/ubuntu/.local/state/guardian/repo/current",
		resourceName:       "nftables",
		artifactRoot:       os.Getenv("VERSELF_NFTABLES_RUNTIME"),
		nftBin:             "nft",
		ldLibraryPath:      os.Getenv("LD_LIBRARY_PATH"),
		destRoot:           "/",
		hostRuntimeRoot:    "/opt/verself/nftables",
		configPath:         "/etc/nftables.conf",
		rulesDir:           "/etc/nftables.d",
		serviceUnitPath:    "/etc/systemd/system/verself-nftables.service",
		firewallTargetPath: "/etc/systemd/system/verself-firewall.target",
		manageSystemd:      true,
		systemctlBin:       "systemctl",
	}
	fs.StringVar(&cfg.repoRoot, "repo-root", cfg.repoRoot, "Boarded repo root.")
	fs.StringVar(&cfg.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&cfg.resourceName, "resource-name", cfg.resourceName, "NftablesFirewall resource name.")
	fs.StringVar(&cfg.runtimeArtifact, "runtime-artifact", "", "Repo-relative nftables runtime tar.")
	fs.StringVar(&cfg.artifactRoot, "artifact-root", cfg.artifactRoot, "Extracted nftables runtime artifact root.")
	fs.StringVar(&cfg.nftBin, "nft-bin", cfg.nftBin, "nft executable.")
	fs.StringVar(&cfg.ldLibraryPath, "ld-library-path", cfg.ldLibraryPath, "LD_LIBRARY_PATH for nft.")
	fs.StringVar(&cfg.destRoot, "dest-root", cfg.destRoot, "Destination root for tests.")
	fs.StringVar(&cfg.hostRuntimeRoot, "host-runtime-root", cfg.hostRuntimeRoot, "Durable host path for the nftables runtime used by systemd.")
	fs.StringVar(&cfg.configPath, "config", cfg.configPath, "Installed nftables config path.")
	fs.StringVar(&cfg.rulesDir, "rules-dir", cfg.rulesDir, "Installed nftables rules directory.")
	fs.StringVar(&cfg.serviceUnitPath, "service-unit", cfg.serviceUnitPath, "Installed systemd service path.")
	fs.StringVar(&cfg.firewallTargetPath, "firewall-target", cfg.firewallTargetPath, "Installed systemd firewall target path.")
	fs.BoolVar(&cfg.manageSystemd, "manage-systemd", cfg.manageSystemd, "Install, enable, and start component-owned systemd units.")
	fs.StringVar(&cfg.systemctlBin, "systemctl-bin", cfg.systemctlBin, "systemctl executable.")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	cfg = normalizeConfig(cfg)
	if cfg.resourceGraph != "" {
		next, err := applyResourceGraphConfig(cfg)
		if err != nil {
			return config{}, err
		}
		cfg = normalizeConfig(next)
	}
	return cfg, nil
}

func normalizeConfig(cfg config) config {
	cfg = defaultConfigPaths(cfg)
	cfg.repoRoot = strings.TrimSpace(cfg.repoRoot)
	cfg.resourceGraph = strings.TrimSpace(cfg.resourceGraph)
	cfg.resourceName = strings.TrimSpace(cfg.resourceName)
	cfg.runtimeArtifact = strings.TrimSpace(cfg.runtimeArtifact)
	cfg.artifactRoot = strings.TrimSpace(cfg.artifactRoot)
	cfg.nftBin = strings.TrimSpace(cfg.nftBin)
	cfg.ldLibraryPath = strings.TrimSpace(cfg.ldLibraryPath)
	cfg.destRoot = strings.TrimSpace(cfg.destRoot)
	cfg.hostRuntimeRoot = strings.TrimSpace(cfg.hostRuntimeRoot)
	cfg.configPath = strings.TrimSpace(cfg.configPath)
	cfg.rulesDir = strings.TrimSpace(cfg.rulesDir)
	cfg.serviceUnitPath = strings.TrimSpace(cfg.serviceUnitPath)
	cfg.firewallTargetPath = strings.TrimSpace(cfg.firewallTargetPath)
	cfg.systemctlBin = strings.TrimSpace(cfg.systemctlBin)
	return cfg
}

func defaultConfigPaths(cfg config) config {
	if cfg.configPath == "" {
		cfg.configPath = "/etc/nftables.conf"
	}
	if cfg.rulesDir == "" {
		cfg.rulesDir = "/etc/nftables.d"
	}
	if cfg.serviceUnitPath == "" {
		cfg.serviceUnitPath = "/etc/systemd/system/verself-nftables.service"
	}
	if cfg.firewallTargetPath == "" {
		cfg.firewallTargetPath = "/etc/systemd/system/verself-firewall.target"
	}
	return cfg
}

func applyResourceGraphConfig(cfg config) (config, error) {
	body, err := os.ReadFile(cfg.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []guardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "nftables.guardianintelligence.org/v1alpha1" &&
			resource.Kind == "NftablesFirewall" &&
			resource.Metadata.Name == cfg.resourceName {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return config{}, fmt.Errorf("expected exactly one NftablesFirewall resource named %q, found %d", cfg.resourceName, len(matches))
	}
	spec := matches[0].Spec
	cfg.runtimeArtifact = spec.RuntimeArtifact
	cfg.hostRuntimeRoot = spec.RuntimeRoot
	cfg.configPath = spec.ConfigPath
	cfg.rulesDir = spec.RulesDir
	if spec.ManageSystemd != nil {
		cfg.manageSystemd = *spec.ManageSystemd
	}
	cfg.serviceUnitPath = spec.Systemd.ServiceUnitPath
	cfg.firewallTargetPath = spec.Systemd.FirewallTargetPath
	return cfg, nil
}

func (cfg *config) validate() error {
	*cfg = defaultConfigPaths(*cfg)
	if cfg.repoRoot == "" && (cfg.resourceGraph != "" || cfg.runtimeArtifact != "") {
		return errors.New("--repo-root is required")
	}
	if cfg.repoRoot != "" {
		repoRoot, err := filepath.Abs(cfg.repoRoot)
		if err != nil {
			return fmt.Errorf("resolve --repo-root: %w", err)
		}
		cfg.repoRoot = repoRoot
	}
	if cfg.runtimeArtifact != "" {
		if err := validateRepoPath(cfg.runtimeArtifact, "--runtime-artifact"); err != nil {
			return err
		}
	}
	if cfg.artifactRoot == "" && cfg.runtimeArtifact == "" {
		return errors.New("--artifact-root or --runtime-artifact is required")
	}
	if cfg.artifactRoot != "" {
		root, err := filepath.Abs(cfg.artifactRoot)
		if err != nil {
			return fmt.Errorf("resolve --artifact-root: %w", err)
		}
		cfg.artifactRoot = root
	}
	if cfg.nftBin == "" && cfg.runtimeArtifact == "" {
		return errors.New("--nft-bin is required")
	}
	if cfg.nftBin != "" && !(cfg.runtimeArtifact != "" && cfg.artifactRoot == "") {
		nftBin, err := resolveExecutablePath(cfg.nftBin)
		if err != nil {
			return fmt.Errorf("resolve --nft-bin: %w", err)
		}
		cfg.nftBin = nftBin
	}
	if cfg.destRoot == "" {
		return errors.New("--dest-root is required")
	}
	destRoot, err := filepath.Abs(cfg.destRoot)
	if err != nil {
		return fmt.Errorf("resolve --dest-root: %w", err)
	}
	cfg.destRoot = destRoot
	if cfg.destRoot == "/" && cfg.manageSystemd && cfg.runtimeArtifact == "" && !filepath.IsAbs(cfg.nftBin) {
		return errors.New("--nft-bin must be absolute when installing systemd units")
	}
	if cfg.manageSystemd || cfg.runtimeArtifact != "" {
		if cfg.hostRuntimeRoot == "" {
			return errors.New("--host-runtime-root is required when managing systemd or installing --runtime-artifact")
		}
		if !filepath.IsAbs(cfg.hostRuntimeRoot) {
			return errors.New("--host-runtime-root must be absolute when managing systemd or installing --runtime-artifact")
		}
	}
	for flag, value := range map[string]string{
		"--config":          cfg.configPath,
		"--rules-dir":       cfg.rulesDir,
		"--service-unit":    cfg.serviceUnitPath,
		"--firewall-target": cfg.firewallTargetPath,
	} {
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", flag)
		}
	}
	return nil
}

func validateRepoPath(value, flagName string) error {
	if filepath.IsAbs(value) {
		return fmt.Errorf("%s must be repo-relative", flagName)
	}
	for _, part := range strings.Split(filepath.ToSlash(value), "/") {
		if part == ".." {
			return fmt.Errorf("%s must not contain '..'", flagName)
		}
	}
	return nil
}

func resolveExecutablePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	if strings.ContainsRune(path, rune(os.PathSeparator)) {
		return filepath.Abs(path)
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func installHostRuntime(cfg *config) error {
	// Nomad allocation paths are ephemeral; systemd must point at a durable host runtime.
	releaseID, err := runtimeTreeHash(cfg.artifactRoot)
	if err != nil {
		return err
	}
	actualBase := destPath(cfg.destRoot, cfg.hostRuntimeRoot)
	actualRelease := filepath.Join(actualBase, "releases", releaseID)
	if _, err := os.Stat(actualRelease); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat nftables runtime release %s: %w", actualRelease, err)
		}
		if err := copyRuntimeRelease(cfg.artifactRoot, actualRelease); err != nil {
			return err
		}
	}
	current := filepath.Join(actualBase, "current")
	tmpLink, err := os.CreateTemp(actualBase, ".current.tmp-*")
	if err != nil {
		return fmt.Errorf("create current symlink placeholder: %w", err)
	}
	tmpLinkName := tmpLink.Name()
	if err := tmpLink.Close(); err != nil {
		return fmt.Errorf("close current symlink placeholder: %w", err)
	}
	if err := os.Remove(tmpLinkName); err != nil {
		return fmt.Errorf("remove current symlink placeholder: %w", err)
	}
	defer func() { _ = os.Remove(tmpLinkName) }()
	if err := os.Symlink(filepath.Join("releases", releaseID), tmpLinkName); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}
	if err := os.Rename(tmpLinkName, current); err != nil {
		return fmt.Errorf("publish current nftables runtime: %w", err)
	}

	serviceRoot := filepath.Join(cfg.hostRuntimeRoot, "current")
	cfg.artifactRoot = current
	cfg.nftBin = filepath.Join(current, "bin", "nft")
	cfg.ldLibraryPath = filepath.Join(current, "lib", "x86_64-linux-gnu")
	cfg.serviceArtifactRoot = serviceRoot
	cfg.serviceNftBin = filepath.Join(serviceRoot, "bin", "nft")
	cfg.serviceLDLibraryPath = filepath.Join(serviceRoot, "lib", "x86_64-linux-gnu")
	return nil
}

func installRepoRuntime(cfg *config) error {
	artifact := filepath.Join(cfg.repoRoot, filepath.FromSlash(cfg.runtimeArtifact))
	releaseID, err := runtimeTarHash(artifact)
	if err != nil {
		return err
	}
	actualBase := destPath(cfg.destRoot, cfg.hostRuntimeRoot)
	actualRelease := filepath.Join(actualBase, "releases", releaseID)
	if _, err := os.Stat(actualRelease); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat nftables runtime release %s: %w", actualRelease, err)
		}
		if err := extractRuntimeTar(artifact, actualRelease); err != nil {
			return err
		}
	}
	if err := promoteCurrentRuntime(actualBase, actualRelease); err != nil {
		return err
	}
	current := filepath.Join(actualBase, "current")
	serviceRoot := filepath.Join(cfg.hostRuntimeRoot, "current")
	cfg.artifactRoot = current
	cfg.nftBin = filepath.Join(current, "bin", "nft")
	cfg.ldLibraryPath = filepath.Join(current, "lib", "x86_64-linux-gnu")
	cfg.serviceArtifactRoot = serviceRoot
	cfg.serviceNftBin = filepath.Join(serviceRoot, "bin", "nft")
	cfg.serviceLDLibraryPath = filepath.Join(serviceRoot, "lib", "x86_64-linux-gnu")
	return nil
}

func runtimeTarHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open nftables runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash nftables runtime artifact: %w", err)
	}
	return "sha256-" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact, releaseRoot string) error {
	if err := os.MkdirAll(filepath.Dir(releaseRoot), 0o755); err != nil {
		return fmt.Errorf("create nftables runtime releases directory: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(releaseRoot), "."+filepath.Base(releaseRoot)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create nftables runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	extracted := filepath.Join(tmp, "opt", "verself", "nftables")
	if _, err := os.Stat(filepath.Join(extracted, "bin", "nftables-apply")); err != nil {
		return fmt.Errorf("nftables runtime artifact missing nftables-apply: %w", err)
	}
	if err := os.Rename(extracted, releaseRoot); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("publish nftables runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open nftables runtime artifact: %w", err)
	}
	defer file.Close()
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(file)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read nftables runtime tar: %w", err)
		}
		target, err := safeTarTarget(destAbs, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, fs.FileMode(header.Mode).Perm()); err != nil {
				return fmt.Errorf("create runtime directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime parent %s: %w", header.Name, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fs.FileMode(header.Mode).Perm())
			if err != nil {
				return fmt.Errorf("create runtime file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write runtime file %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close runtime file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported nftables runtime tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe nftables runtime tar entry %s", name)
	}
	target := filepath.Join(destAbs, name)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe nftables runtime tar entry %s", name)
	}
	return targetAbs, nil
}

func promoteCurrentRuntime(actualBase, actualRelease string) error {
	if err := os.MkdirAll(actualBase, 0o755); err != nil {
		return fmt.Errorf("create nftables runtime root: %w", err)
	}
	current := filepath.Join(actualBase, "current")
	next, err := os.CreateTemp(actualBase, ".current.tmp-*")
	if err != nil {
		return fmt.Errorf("create current symlink placeholder: %w", err)
	}
	nextName := next.Name()
	if err := next.Close(); err != nil {
		return fmt.Errorf("close current symlink placeholder: %w", err)
	}
	if err := os.Remove(nextName); err != nil {
		return fmt.Errorf("remove current symlink placeholder: %w", err)
	}
	defer func() { _ = os.Remove(nextName) }()
	if err := os.Symlink(filepath.Join("releases", filepath.Base(actualRelease)), nextName); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}
	if err := os.Rename(nextName, current); err != nil {
		return fmt.Errorf("publish current nftables runtime: %w", err)
	}
	return nil
}

func runtimeTreeHash(root string) (string, error) {
	h := sha256.New()
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode()); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if _, err := h.Write([]byte(target)); err != nil {
				return err
			}
			_, err = h.Write([]byte{0})
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := io.Copy(h, file); err != nil {
			return err
		}
		_, err = h.Write([]byte{0})
		return err
	}); err != nil {
		return "", fmt.Errorf("hash nftables runtime %s: %w", root, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyRuntimeRelease(source, releaseRoot string) error {
	if err := os.MkdirAll(filepath.Dir(releaseRoot), 0o755); err != nil {
		return fmt.Errorf("create nftables release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(releaseRoot), "."+filepath.Base(releaseRoot)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create nftables release staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := copyTree(source, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, releaseRoot); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("publish nftables runtime release %s: %w", releaseRoot, err)
	}
	return nil
}

func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read symlink %s: %w", path, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
			}
			if err := os.Symlink(linkTarget, target); err != nil {
				return fmt.Errorf("copy symlink %s: %w", path, err)
			}
			return nil
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, mode.Perm()); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			return os.Chmod(target, mode.Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
		}
		return copyFile(path, target, mode.Perm())
	})
}

func copyFile(source, dest string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", source, dest, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dest, err)
	}
	return os.Chmod(dest, mode)
}

func installConfig(cfg config) error {
	cfg = defaultConfigPaths(cfg)
	rulesDir := filepath.Join(cfg.artifactRoot, "etc", "nftables.d")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", rulesDir, err)
	}
	desiredRules := map[string]bool{}
	installs := []fileInstall{
		{
			Source: filepath.Join(cfg.artifactRoot, "etc", "nftables.conf"),
			Dest:   destPath(cfg.destRoot, cfg.configPath),
			Mode:   0o644,
		},
	}
	if cfg.manageSystemd {
		installs = append(installs, fileInstall{
			Source: filepath.Join(cfg.artifactRoot, "systemd", "verself-firewall.target"),
			Dest:   destPath(cfg.destRoot, cfg.firewallTargetPath),
			Mode:   0o644,
		})
		service := fileInstall{
			Dest: destPath(cfg.destRoot, cfg.serviceUnitPath),
			Mode: 0o644,
			Body: nftablesServiceUnit(cfg),
		}
		if err := writeManagedFile(service); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nft") {
			continue
		}
		desiredRules[entry.Name()] = true
		installs = append(installs, fileInstall{
			Source: filepath.Join(rulesDir, entry.Name()),
			Dest:   destPath(cfg.destRoot, cfg.rulesDir, entry.Name()),
			Mode:   0o644,
		})
	}
	sort.Slice(installs, func(i, j int) bool { return installs[i].Dest < installs[j].Dest })
	for _, install := range installs {
		if err := copyManagedFile(install); err != nil {
			return err
		}
	}
	if err := removeStaleManagedRules(destPath(cfg.destRoot, cfg.rulesDir), desiredRules); err != nil {
		return err
	}
	return nil
}

func copyManagedFile(install fileInstall) error {
	body, err := os.ReadFile(install.Source)
	if err != nil {
		return fmt.Errorf("read %s: %w", install.Source, err)
	}
	install.Body = body
	return writeManagedFile(install)
}

func writeManagedFile(install fileInstall) error {
	body := install.Body
	if !bytes.HasPrefix(body, []byte(managedHeader)) {
		return fmt.Errorf("%s must start with %q", install.Dest, managedHeader)
	}
	if err := os.MkdirAll(filepath.Dir(install.Dest), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(install.Dest), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(install.Dest), "."+filepath.Base(install.Dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", install.Dest, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(install.Mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, install.Dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, install.Dest, err)
	}
	return nil
}

func nftablesServiceUnit(cfg config) []byte {
	artifactRoot := cfg.serviceArtifactRoot
	if artifactRoot == "" {
		artifactRoot = cfg.artifactRoot
	}
	nftBin := cfg.serviceNftBin
	if nftBin == "" {
		nftBin = cfg.nftBin
	}
	ldLibraryPath := cfg.serviceLDLibraryPath
	if ldLibraryPath == "" {
		ldLibraryPath = cfg.ldLibraryPath
	}
	return []byte(fmt.Sprintf(`%s
[Unit]
Description=verself nftables apply
Documentation=https://wiki.nftables.org/wiki-nftables/index.php/Main_Page
DefaultDependencies=no
After=local-fs.target
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=LD_LIBRARY_PATH=%s
ExecStart=%s --artifact-root %s --nft-bin %s --ld-library-path %s --manage-systemd=false

[Install]
WantedBy=multi-user.target
`, managedHeader, ldLibraryPath, shellQuoteArg(filepath.Join(artifactRoot, "bin", "nftables-apply")), shellQuoteArg(artifactRoot), shellQuoteArg(nftBin), shellQuoteArg(ldLibraryPath)))
}

func shellQuoteArg(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func removeStaleManagedRules(destDir string, desired map[string]bool) error {
	entries, err := os.ReadDir(destDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", destDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nft") || desired[entry.Name()] {
			continue
		}
		path := filepath.Join(destDir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if bytes.HasPrefix(body, []byte(managedHeader)) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove stale managed rule %s: %w", path, err)
			}
		}
	}
	return nil
}

func runCommand(ctx context.Context, ldLibraryPath, bin string, args ...string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, bin, args...)
	cmd.Env = os.Environ()
	if ldLibraryPath != "" {
		cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+ldLibraryPath)
	}
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func destPath(root, path string, more ...string) string {
	cleanPath := filepath.Clean(path)
	if cleanPath == string(filepath.Separator) {
		return filepath.Clean(root)
	}
	parts := append([]string{filepath.Clean(root), strings.TrimPrefix(cleanPath, string(filepath.Separator))}, more...)
	return filepath.Join(parts...)
}

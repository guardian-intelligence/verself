package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	artifactRoot         string
	nftBin               string
	ldLibraryPath        string
	destRoot             string
	hostRuntimeRoot      string
	serviceArtifactRoot  string
	serviceNftBin        string
	serviceLDLibraryPath string
	manageSystemd        bool
	systemctlBin         string
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
	if cfg.manageSystemd {
		if err := installHostRuntime(&cfg); err != nil {
			return err
		}
	}
	if err := installConfig(cfg); err != nil {
		return err
	}
	nftConf := destPath(cfg.destRoot, "/etc/nftables.conf")
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
		artifactRoot:    os.Getenv("VERSELF_NFTABLES_RUNTIME"),
		nftBin:          "nft",
		ldLibraryPath:   os.Getenv("LD_LIBRARY_PATH"),
		destRoot:        "/",
		hostRuntimeRoot: "/opt/verself/nftables",
		manageSystemd:   true,
		systemctlBin:    "systemctl",
	}
	fs.StringVar(&cfg.artifactRoot, "artifact-root", cfg.artifactRoot, "Extracted nftables runtime artifact root.")
	fs.StringVar(&cfg.nftBin, "nft-bin", cfg.nftBin, "nft executable.")
	fs.StringVar(&cfg.ldLibraryPath, "ld-library-path", cfg.ldLibraryPath, "LD_LIBRARY_PATH for nft.")
	fs.StringVar(&cfg.destRoot, "dest-root", cfg.destRoot, "Destination root for tests.")
	fs.StringVar(&cfg.hostRuntimeRoot, "host-runtime-root", cfg.hostRuntimeRoot, "Durable host path for the nftables runtime used by systemd.")
	fs.BoolVar(&cfg.manageSystemd, "manage-systemd", cfg.manageSystemd, "Install, enable, and start component-owned systemd units.")
	fs.StringVar(&cfg.systemctlBin, "systemctl-bin", cfg.systemctlBin, "systemctl executable.")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return cfg, nil
}

func (cfg *config) validate() error {
	if cfg.artifactRoot == "" {
		return errors.New("--artifact-root is required")
	}
	root, err := filepath.Abs(cfg.artifactRoot)
	if err != nil {
		return fmt.Errorf("resolve --artifact-root: %w", err)
	}
	cfg.artifactRoot = root
	if cfg.nftBin == "" {
		return errors.New("--nft-bin is required")
	}
	nftBin, err := resolveExecutablePath(cfg.nftBin)
	if err != nil {
		return fmt.Errorf("resolve --nft-bin: %w", err)
	}
	cfg.nftBin = nftBin
	if cfg.destRoot == "" {
		return errors.New("--dest-root is required")
	}
	destRoot, err := filepath.Abs(cfg.destRoot)
	if err != nil {
		return fmt.Errorf("resolve --dest-root: %w", err)
	}
	cfg.destRoot = destRoot
	if cfg.destRoot == "/" && cfg.manageSystemd && !filepath.IsAbs(cfg.nftBin) {
		return errors.New("--nft-bin must be absolute when installing systemd units")
	}
	if cfg.manageSystemd {
		if cfg.hostRuntimeRoot == "" {
			return errors.New("--host-runtime-root is required when managing systemd")
		}
		if !filepath.IsAbs(cfg.hostRuntimeRoot) {
			return errors.New("--host-runtime-root must be absolute when managing systemd")
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
	rulesDir := filepath.Join(cfg.artifactRoot, "etc", "nftables.d")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", rulesDir, err)
	}
	desiredRules := map[string]bool{}
	installs := []fileInstall{
		{
			Source: filepath.Join(cfg.artifactRoot, "etc", "nftables.conf"),
			Dest:   destPath(cfg.destRoot, "/etc/nftables.conf"),
			Mode:   0o644,
		},
	}
	if cfg.manageSystemd {
		installs = append(installs, fileInstall{
			Source: filepath.Join(cfg.artifactRoot, "systemd", "verself-firewall.target"),
			Dest:   destPath(cfg.destRoot, "/etc/systemd/system/verself-firewall.target"),
			Mode:   0o644,
		})
		service := fileInstall{
			Dest: destPath(cfg.destRoot, "/etc/systemd/system/verself-nftables.service"),
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
			Dest:   destPath(cfg.destRoot, "/etc/nftables.d", entry.Name()),
			Mode:   0o644,
		})
	}
	sort.Slice(installs, func(i, j int) bool { return installs[i].Dest < installs[j].Dest })
	for _, install := range installs {
		if err := copyManagedFile(install); err != nil {
			return err
		}
	}
	if err := removeStaleManagedRules(destPath(cfg.destRoot, "/etc/nftables.d"), desiredRules); err != nil {
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

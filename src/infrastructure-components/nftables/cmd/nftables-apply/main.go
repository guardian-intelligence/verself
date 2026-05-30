package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const managedHeader = "# Managed by Verself nftables component."
const firecrackerHostServiceCIDR = "10.255.0.1/32"
const firecrackerHostServiceInterface = "verself-host0"

type config struct {
	artifactRoot  string
	nftBin        string
	ipBin         string
	ldLibraryPath string
	destRoot      string
	manageSystemd bool
	systemctlBin  string
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
	if err := installConfig(cfg); err != nil {
		return err
	}
	if err := convergeFirecrackerHostNetwork(ctx, cfg); err != nil {
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
		artifactRoot:  os.Getenv("VERSELF_NFTABLES_RUNTIME"),
		nftBin:        "nft",
		ipBin:         "/usr/sbin/ip",
		ldLibraryPath: os.Getenv("LD_LIBRARY_PATH"),
		destRoot:      "/",
		manageSystemd: true,
		systemctlBin:  "systemctl",
	}
	fs.StringVar(&cfg.artifactRoot, "artifact-root", cfg.artifactRoot, "Extracted nftables runtime artifact root.")
	fs.StringVar(&cfg.nftBin, "nft-bin", cfg.nftBin, "nft executable.")
	fs.StringVar(&cfg.ipBin, "ip-bin", cfg.ipBin, "iproute2 executable.")
	fs.StringVar(&cfg.ldLibraryPath, "ld-library-path", cfg.ldLibraryPath, "LD_LIBRARY_PATH for nft.")
	fs.StringVar(&cfg.destRoot, "dest-root", cfg.destRoot, "Destination root for tests.")
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
	if cfg.ipBin == "" {
		return errors.New("--ip-bin is required")
	}
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
	if cfg.destRoot == "/" && cfg.manageSystemd && !filepath.IsAbs(cfg.ipBin) {
		return errors.New("--ip-bin must be absolute when installing systemd units")
	}
	return nil
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
	installs = append(installs, firecrackerSysctlInstall(cfg))
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
		if install.Source != "" {
			if err := copyManagedFile(install); err != nil {
				return err
			}
			continue
		}
		if err := writeManagedFile(install); err != nil {
			return err
		}
	}
	if err := removeStaleManagedRules(destPath(cfg.destRoot, "/etc/nftables.d"), desiredRules); err != nil {
		return err
	}
	return nil
}

func firecrackerSysctlInstall(cfg config) fileInstall {
	body := []byte(managedHeader + `
# Firecracker guests require host forwarding for NAT egress and must not route host loopback.
net.ipv4.ip_forward = 1
net.ipv4.conf.all.route_localnet = 0
net.ipv4.conf.default.route_localnet = 0
`)
	return fileInstall{
		Dest: destPath(cfg.destRoot, "/etc/sysctl.d/99-verself-firecracker.conf"),
		Mode: 0o644,
		Body: body,
	}
}

func convergeFirecrackerHostNetwork(ctx context.Context, cfg config) error {
	if cfg.destRoot != "/" {
		return nil
	}
	for path, value := range map[string]string{
		"/proc/sys/net/ipv4/ip_forward":                  "1\n",
		"/proc/sys/net/ipv4/conf/all/route_localnet":     "0\n",
		"/proc/sys/net/ipv4/conf/default/route_localnet": "0\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if !commandSucceeds(ctx, "", cfg.ipBin, "link", "show", firecrackerHostServiceInterface) {
		if err := runCommand(ctx, "", cfg.ipBin, "link", "add", firecrackerHostServiceInterface, "type", "dummy"); err != nil {
			return err
		}
	}
	if err := runCommand(ctx, "", cfg.ipBin, "addr", "replace", firecrackerHostServiceCIDR, "dev", firecrackerHostServiceInterface); err != nil {
		return err
	}
	if err := runCommand(ctx, "", cfg.ipBin, "link", "set", firecrackerHostServiceInterface, "up"); err != nil {
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
ExecStart=%s --artifact-root %s --nft-bin %s --ip-bin %s --ld-library-path %s --manage-systemd=false

[Install]
WantedBy=multi-user.target
`, managedHeader, cfg.ldLibraryPath, shellQuoteArg(filepath.Join(cfg.artifactRoot, "bin", "nftables-apply")), shellQuoteArg(cfg.artifactRoot), shellQuoteArg(cfg.nftBin), shellQuoteArg(cfg.ipBin), shellQuoteArg(cfg.ldLibraryPath)))
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

func commandSucceeds(ctx context.Context, ldLibraryPath, bin string, args ...string) bool {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, bin, args...)
	cmd.Env = os.Environ()
	if ldLibraryPath != "" {
		cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+ldLibraryPath)
	}
	return cmd.Run() == nil
}

func destPath(root, path string, more ...string) string {
	cleanPath := filepath.Clean(path)
	if cleanPath == string(filepath.Separator) {
		return filepath.Clean(root)
	}
	parts := append([]string{filepath.Clean(root), strings.TrimPrefix(cleanPath, string(filepath.Separator))}, more...)
	return filepath.Join(parts...)
}

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultOperatorSSHUser = "ubuntu"
	defaultSSHKeyName      = "id_ed25519"
)

type deviceOptions struct {
	repoRoot    string
	site        string
	accessHost  string
	sshRoute    string
	force       bool
	dryRun      bool
	generateKey bool
}

type deviceOpsVars struct {
	BareMetalHostAlias string `yaml:"bare_metal_host_alias"`
	PomeriumDomain     string `yaml:"pomerium_domain"`
	PomeriumSubdomain  string `yaml:"pomerium_subdomain"`
	VerselfDomain      string `yaml:"verself_domain"`
}

type deviceConfig struct {
	Site      string
	Alias     string
	Access    string
	SSHRoute  string
	Inventory string
	KeyPath   string
	PubPath   string
}

func cmdDevice(args []string) error {
	opts := deviceOptions{
		site:        envOr("VERSELF_SITE", "prod"),
		generateKey: true,
	}
	fs := flagSet("device")
	fs.StringVar(&opts.repoRoot, "repo-root", ".", "repository root")
	fs.StringVar(&opts.site, "site", opts.site, "deployment site")
	fs.StringVar(&opts.accessHost, "access-host", "", "Pomerium SSH host; defaults to access.<verself_domain>")
	fs.StringVar(&opts.sshRoute, "ssh-route", "", "Pomerium native-SSH route name; defaults to --site")
	fs.BoolVar(&opts.force, "force", false, "overwrite an existing different local inventory")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print the derived configuration without writing files")
	fs.BoolVar(&opts.generateKey, "generate-key", true, "run ssh-keygen when ~/.ssh/id_ed25519 is missing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("device: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return configureOperatorDevice(opts)
}

func configureOperatorDevice(opts deviceOptions) error {
	repoRoot, err := filepath.Abs(opts.repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}
	if strings.TrimSpace(opts.site) == "" {
		return errors.New("device: --site is required")
	}
	ops, err := loadDeviceOps(repoRoot)
	if err != nil {
		return err
	}
	accessHost := strings.TrimSpace(opts.accessHost)
	if accessHost == "" {
		accessHost, err = derivePomeriumAccessHost(ops)
		if err != nil {
			return err
		}
	}
	route := strings.TrimSpace(opts.sshRoute)
	if route == "" {
		route = opts.site
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	cfg := deviceConfig{
		Site:      opts.site,
		Alias:     strings.TrimSpace(ops.BareMetalHostAlias),
		Access:    accessHost,
		SSHRoute:  route,
		Inventory: filepath.Join(repoRoot, "src", "host-configuration", "ansible", "inventory", opts.site+".ini"),
		KeyPath:   filepath.Join(home, ".ssh", defaultSSHKeyName),
		PubPath:   filepath.Join(home, ".ssh", defaultSSHKeyName+".pub"),
	}
	if cfg.Alias == "" {
		return errors.New("device: ops.yml must define bare_metal_host_alias")
	}
	if strings.ContainsAny(cfg.Alias, " \t\r\n[]") {
		return fmt.Errorf("device: invalid bare_metal_host_alias %q", cfg.Alias)
	}
	if err := ensureDefaultSSHKey(cfg, opts); err != nil {
		return err
	}
	if err := writeDeviceInventory(cfg, opts); err != nil {
		return err
	}
	printDeviceSummary(cfg, opts)
	return nil
}

func loadDeviceOps(repoRoot string) (deviceOpsVars, error) {
	path := filepath.Join(repoRoot, "src", "host-configuration", "ansible", "group_vars", "all", "topology", "ops.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return deviceOpsVars{}, fmt.Errorf("read %s: %w", path, err)
	}
	var out deviceOpsVars
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return deviceOpsVars{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

func derivePomeriumAccessHost(ops deviceOpsVars) (string, error) {
	if domain := strings.TrimSpace(ops.PomeriumDomain); domain != "" && !strings.Contains(domain, "{{") {
		return domain, nil
	}
	subdomain := strings.TrimSpace(ops.PomeriumSubdomain)
	if subdomain == "" {
		subdomain = "access"
	}
	domain := strings.TrimSpace(ops.VerselfDomain)
	if domain == "" {
		return "", errors.New("device: ops.yml must define verself_domain or concrete pomerium_domain")
	}
	return subdomain + "." + domain, nil
}

func ensureDefaultSSHKey(cfg deviceConfig, opts deviceOptions) error {
	if _, err := os.Stat(cfg.PubPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", cfg.PubPath, err)
	}

	if _, err := os.Stat(cfg.KeyPath); err == nil {
		if opts.dryRun {
			return nil
		}
		if err := derivePublicSSHKey(cfg.KeyPath, cfg.PubPath); err != nil {
			return err
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", cfg.KeyPath, err)
	}

	if !opts.generateKey {
		return fmt.Errorf("device: %s is missing; rerun without --generate-key=false or create a default SSH key first", cfg.PubPath)
	}
	if opts.dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.KeyPath), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(cfg.KeyPath), err)
	}
	comment := strings.TrimSpace(envOr("USER", "operator")) + "@" + hostnameForSSHComment() + "-verself"
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", cfg.KeyPath, "-C", comment)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generate SSH key %s: %w", cfg.KeyPath, err)
	}
	return nil
}

func derivePublicSSHKey(keyPath, pubPath string) error {
	cmd := exec.Command("ssh-keygen", "-y", "-f", keyPath)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("derive public key from %s: %w", keyPath, err)
	}
	if err := os.WriteFile(pubPath, bytes.TrimSpace(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", pubPath, err)
	}
	if err := appendFileNewline(pubPath); err != nil {
		return err
	}
	return nil
}

func appendFileNewline(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString("\n"); err != nil {
		return fmt.Errorf("append newline to %s: %w", path, err)
	}
	return nil
}

func writeDeviceInventory(cfg deviceConfig, opts deviceOptions) error {
	rendered := renderDeviceInventory(cfg)
	if opts.dryRun {
		fmt.Print(rendered)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Inventory), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(cfg.Inventory), err)
	}
	existing, err := os.ReadFile(cfg.Inventory)
	if err == nil {
		if string(existing) == rendered {
			return nil
		}
		if !opts.force {
			return fmt.Errorf("device: %s already exists and differs; pass --force to replace the device-local ignored inventory", cfg.Inventory)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", cfg.Inventory, err)
	}
	if err := os.WriteFile(cfg.Inventory, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfg.Inventory, err)
	}
	return nil
}

func renderDeviceInventory(cfg deviceConfig) string {
	return fmt.Sprintf(`[workers]
%s ansible_host=%s

[infra]
%s ansible_host=%s

[all:vars]
ansible_user=%s@%s
ansible_python_interpreter=/usr/bin/python3
ansible_ssh_extra_args=-o IdentitiesOnly=yes -o PreferredAuthentications=publickey -o PubkeyAuthentication=yes
`, cfg.Alias, cfg.Access, cfg.Alias, cfg.Access, defaultOperatorSSHUser, cfg.SSHRoute)
}

func printDeviceSummary(cfg deviceConfig, opts deviceOptions) {
	mode := "configured"
	if opts.dryRun {
		mode = "dry run"
	}
	_, _ = fmt.Fprintf(os.Stdout, `
operator device %s
  inventory: %s
  ssh key:   %s
  login:     ssh %s@%s@%s

Identity model:
  one Zitadel human can use many devices
  each device presents its own SSH key
  Pomerium binds that key to the authenticated human on first SSH login

If the key has a passphrase, load it before running aspect commands:
  ssh-add %s
`, mode, cfg.Inventory, cfg.PubPath, defaultOperatorSSHUser, cfg.SSHRoute, cfg.Access, cfg.KeyPath)
}

func hostnameForSSHComment() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "device"
	}
	return strings.TrimSpace(name)
}

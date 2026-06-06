package main

import (
	"archive/tar"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type config struct {
	repoRoot        string
	runtimeArtifact string
	installRoot     string
	configPath      string
	servicePath     string
	dataDir         string
	datacenter      string
	address         string
	vaultCAFile     string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nomad-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("nomad-recover", flag.ContinueOnError)
	fs.StringVar(&cfg.repoRoot, "repo-root", "/home/ubuntu/.local/state/guardian/repo/current", "Boarded repo artifact root.")
	fs.StringVar(&cfg.runtimeArtifact, "runtime-artifact", "bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar", "Repo-relative Nomad runtime artifact path.")
	fs.StringVar(&cfg.installRoot, "install-root", "/opt/verself/profile", "Nomad runtime install root.")
	fs.StringVar(&cfg.configPath, "config", "/etc/nomad/nomad.hcl", "Nomad agent config path.")
	fs.StringVar(&cfg.servicePath, "service", "/etc/systemd/system/nomad.service", "systemd unit path.")
	fs.StringVar(&cfg.dataDir, "data-dir", "/var/lib/nomad", "Nomad data directory.")
	fs.StringVar(&cfg.datacenter, "datacenter", "iad1", "Nomad datacenter.")
	fs.StringVar(&cfg.address, "address", "http://127.0.0.1:4646", "Nomad HTTP address to verify.")
	fs.StringVar(&cfg.vaultCAFile, "vault-ca-file", "/etc/verself/openbao/ca.pem", "OpenBao CA file. Vault integration is enabled only when this file exists.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("must run as root")
	}
	if err := installRuntime(cfg); err != nil {
		return err
	}
	configChanged, err := writeConfig(cfg)
	if err != nil {
		return err
	}
	serviceChanged, err := writeService(cfg)
	if err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if configChanged || serviceChanged {
		if err := systemctl("restart", "nomad.service"); err != nil {
			return err
		}
	} else if err := systemctl("enable", "--now", "nomad.service"); err != nil {
		return err
	}
	return waitHealthy(cfg)
}

func (cfg config) validate() error {
	for name, value := range map[string]string{
		"repo-root":        cfg.repoRoot,
		"runtime-artifact": cfg.runtimeArtifact,
		"install-root":     cfg.installRoot,
		"config":           cfg.configPath,
		"service":          cfg.servicePath,
		"data-dir":         cfg.dataDir,
		"datacenter":       cfg.datacenter,
		"address":          cfg.address,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.runtimeArtifact) {
		return errors.New("--runtime-artifact must be repo-relative")
	}
	if !filepath.IsAbs(cfg.repoRoot) || !filepath.IsAbs(cfg.installRoot) || !filepath.IsAbs(cfg.configPath) || !filepath.IsAbs(cfg.servicePath) || !filepath.IsAbs(cfg.dataDir) {
		return errors.New("--repo-root, --install-root, --config, --service, and --data-dir must be absolute")
	}
	return nil
}

func installRuntime(cfg config) error {
	artifact := filepath.Join(cfg.repoRoot, filepath.FromSlash(cfg.runtimeArtifact))
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open runtime artifact %s: %w", artifact, err)
	}
	defer func() { _ = file.Close() }()
	tmp := cfg.installRoot + ".tmp." + fmt.Sprint(os.Getpid())
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("remove stale runtime temp dir: %w", err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("create runtime temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(file, tmp); err != nil {
		return fmt.Errorf("extract runtime artifact: %w", err)
	}
	return installTree(tmp, cfg.installRoot)
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	destClean, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destClean, header.Name)
		targetClean, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if targetClean != destClean && !strings.HasPrefix(targetClean, destClean+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe tar member %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetClean, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetClean), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(targetClean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar member %q type %d", header.Name, header.Typeflag)
		}
	}
}

func installTree(srcRoot string, destRoot string) error {
	return filepath.WalkDir(srcRoot, func(src string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		dest := filepath.Join(destRoot, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dest, info.Mode())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported runtime file type: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		next := dest + ".next." + fmt.Sprint(os.Getpid())
		if err := os.Rename(src, next); err != nil {
			return fmt.Errorf("stage %s: %w", rel, err)
		}
		if err := os.Chmod(next, info.Mode()); err != nil {
			_ = os.Remove(next)
			return fmt.Errorf("chmod %s: %w", rel, err)
		}
		if err := os.Rename(next, dest); err != nil {
			_ = os.Remove(next)
			return fmt.Errorf("install %s: %w", rel, err)
		}
		return nil
	})
}

func writeConfig(cfg config) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.configPath), 0o755); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		return false, fmt.Errorf("create data dir: %w", err)
	}
	vault := ""
	if regularFile(cfg.vaultCAFile) {
		vault = fmt.Sprintf(`
vault {
  enabled               = true
  address               = "https://127.0.0.1:8200"
  ca_file               = %q
  jwt_auth_backend_path = "jwt-nomad"

  default_identity {
    aud = ["vault.io"]
    ttl = "1h"
  }
}
`, cfg.vaultCAFile)
	}
	body := fmt.Sprintf(`data_dir  = %q
log_level = "INFO"
datacenter = %q
bind_addr = "127.0.0.1"

ports {
  http = 4646
}

advertise {
  http = "127.0.0.1:4646"
  rpc  = "127.0.0.1:4647"
  serf = "127.0.0.1:4648"
}

server {
  enabled          = true
  bootstrap_expect = 1
}

client {
  enabled = true
  node_pool = "default"
  min_dynamic_port = 20000
  max_dynamic_port = 32000

  servers = ["127.0.0.1:4647"]

  host_network "loopback" {
    cidr = "127.0.0.0/8"
  }
}

consul {
  auto_advertise   = false
  client_auto_join = false
  server_auto_join = false
}

plugin "raw_exec" {
  config {
    enabled = true
  }
}
%s
telemetry {
  collection_interval        = "10s"
  disable_hostname           = true
  prometheus_metrics         = true
  publish_allocation_metrics = true
  publish_node_metrics       = true
}

acl {
  enabled = false
}
`, cfg.dataDir, cfg.datacenter, vault)
	return writeFileIfChanged(cfg.configPath, []byte(body), 0o644)
}

func writeService(cfg config) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.servicePath), 0o755); err != nil {
		return false, fmt.Errorf("create service dir: %w", err)
	}
	nomad := filepath.Join(cfg.installRoot, "bin", "nomad")
	body := fmt.Sprintf(`[Unit]
Description=Verself Nomad
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s agent -config=%s
Restart=always
RestartSec=2s
KillSignal=SIGINT
LimitNOFILE=1048576
TasksMax=infinity

[Install]
WantedBy=multi-user.target
`, nomad, cfg.configPath)
	return writeFileIfChanged(cfg.servicePath, []byte(body), 0o644)
}

func writeFileIfChanged(path string, body []byte, mode os.FileMode) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(body) {
		if chmodErr := os.Chmod(path, mode); chmodErr != nil {
			return false, fmt.Errorf("chmod %s: %w", path, chmodErr)
		}
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return false, fmt.Errorf("write temp %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("install %s: %w", path, err)
	}
	return true, nil
}

func waitHealthy(cfg config) error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		cmd := exec.Command(filepath.Join(cfg.installRoot, "bin", "nomad"), "node", "status", "-self")
		cmd.Env = append(os.Environ(), "NOMAD_ADDR="+cfg.address)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		last = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Nomad did not become healthy: %w", last)
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

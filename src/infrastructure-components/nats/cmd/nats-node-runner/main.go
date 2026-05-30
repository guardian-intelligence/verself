package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	natsUser               = "nats"
	natsGroup              = "nats"
	natsBinary             = "local/bin/nats-server"
	spiffeHelperBinary     = "local/bin/spiffe-helper"
	dataDir                = "/var/lib/nats"
	jetstreamDir           = "/var/lib/nats/jetstream"
	spiffeDir              = "/var/lib/nats/spiffe"
	pidPath                = "/var/lib/nats/nats.pid"
	trustDomainPath        = "/etc/verself/spiffe-trust-domain"
	spireAgentSocketPath   = "/run/spire-agent/sockets/agent.sock"
	helperConfigPath       = "local/nats-spiffe-helper.conf"
	serverConfigPath       = "local/nats-server.conf"
	notificationSVIDSuffix = "/svc/notifications-service"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "nats-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected exactly one mode")
	}
	switch args[0] {
	case "prepare":
		return prepare()
	case "spiffe-helper":
		return runSpiffeHelper()
	case "server":
		return runServer()
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}

func prepare() error {
	uid, gid, err := natsIDs()
	if err != nil {
		return err
	}
	for _, dir := range []string{dataDir, jetstreamDir, spiffeDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o750); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
		}
	}
	return nil
}

func runSpiffeHelper() error {
	body := fmt.Sprintf(`agent_address = %q
cert_dir = %q
daemon_mode = true
pid_file_name = %q
renew_signal = "SIGHUP"
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
`, spireAgentSocketPath, spiffeDir, pidPath)
	if err := writeLocalConfig(helperConfigPath, []byte(body)); err != nil {
		return err
	}
	return syscall.Exec(spiffeHelperBinary, []string{spiffeHelperBinary, "-config", helperConfigPath}, os.Environ())
}

func runServer() error {
	clientPort, err := requiredEnv("NOMAD_PORT_client")
	if err != nil {
		return err
	}
	monitoringPort, err := requiredEnv("NOMAD_PORT_monitoring")
	if err != nil {
		return err
	}
	trustDomain, err := trustDomain()
	if err != nil {
		return err
	}
	notificationsSVID := "spiffe://" + trustDomain + notificationSVIDSuffix
	body := fmt.Sprintf(`server_name: verself-nats
pid_file: %q
host: 127.0.0.1
port: %s
http: %s

jetstream {
  store_dir: %q
  max_mem_store: 256Mb
  max_file_store: 4Gb
}

tls {
  cert_file: %q
  key_file: %q
  ca_file: %q
  verify: true
  verify_and_map: true
  timeout: 2
}

authorization {
  users = [
    {
      user: %q
      permissions: {
        publish: {
          allow: ["events.>", "$JS.API.>", "$JS.ACK.>"]
        }
        subscribe: {
          allow: ["_INBOX.>"]
        }
      }
    }
  ]
}
`, pidPath, clientPort, monitoringPort, jetstreamDir, filepath.Join(spiffeDir, "svid.pem"), filepath.Join(spiffeDir, "svid_key.pem"), filepath.Join(spiffeDir, "bundle.pem"), notificationsSVID)
	if err := writeLocalConfig(serverConfigPath, []byte(body)); err != nil {
		return err
	}
	return syscall.Exec(natsBinary, []string{natsBinary, "-c", serverConfigPath}, os.Environ())
}

func natsIDs() (int, int, error) {
	u, err := user.Lookup(natsUser)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", natsUser, err)
	}
	g, err := user.LookupGroup(natsGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", natsGroup, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %s: %w", natsUser, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", natsGroup, err)
	}
	return uid, gid, nil
}

func trustDomain() (string, error) {
	body, err := os.ReadFile(trustDomainPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", trustDomainPath, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", trustDomainPath)
	}
	return value, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func writeLocalConfig(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

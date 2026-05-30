package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	garageGroup = "garage"
	garageUser  = "garage"
)

type garageNode struct {
	Instance  int
	S3Port    int
	RPCPort   int
	AdminPort int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "garage-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	garageBin := flag.String("garage-bin", "", "Garage binary path.")
	instance := flag.Int("instance", -1, "Garage instance number.")
	nodesArg := flag.String("nodes", "0:3900:3901:3903,1:3910:3911:3913,2:3920:3921:3923", "Comma-separated instance:s3:rpc:admin node list.")
	flag.Parse()
	if *garageBin == "" {
		return errors.New("--garage-bin is required")
	}
	nodes, err := parseNodes(*nodesArg)
	if err != nil {
		return err
	}
	if !nodeExists(nodes, *instance) {
		return fmt.Errorf("instance %d is not present in --nodes", *instance)
	}
	uid, gid, err := ensureGarageUser()
	if err != nil {
		return err
	}
	if err := ensureGarageState(nodes, uid, gid); err != nil {
		return err
	}
	config := fmt.Sprintf("/etc/garage/garage-%d.toml", *instance)
	return execGarageServer(*garageBin, config)
}

func parseNodes(raw string) ([]garageNode, error) {
	parts := strings.Split(raw, ",")
	nodes := make([]garageNode, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		fields := strings.Split(strings.TrimSpace(part), ":")
		if len(fields) != 4 {
			return nil, fmt.Errorf("node %q must be instance:s3:rpc:admin", part)
		}
		values := [4]int{}
		for i, field := range fields {
			value, err := strconv.Atoi(field)
			if err != nil || value < 0 {
				return nil, fmt.Errorf("node %q contains invalid integer %q", part, field)
			}
			values[i] = value
		}
		if seen[values[0]] {
			return nil, fmt.Errorf("duplicate garage instance %d", values[0])
		}
		seen[values[0]] = true
		nodes = append(nodes, garageNode{Instance: values[0], S3Port: values[1], RPCPort: values[2], AdminPort: values[3]})
	}
	if len(nodes) == 0 {
		return nil, errors.New("at least one garage node is required")
	}
	return nodes, nil
}

func nodeExists(nodes []garageNode, instance int) bool {
	for _, node := range nodes {
		if node.Instance == instance {
			return true
		}
	}
	return false
}

func ensureGarageUser() (int, int, error) {
	if err := runIfMissing([]string{"getent", "group", garageGroup}, []string{"groupadd", "--system", garageGroup}); err != nil {
		return 0, 0, err
	}
	if err := runIfMissing(
		[]string{"id", "-u", garageUser},
		[]string{"useradd", "--system", "--gid", garageGroup, "--home-dir", "/var/lib/garage-0", "--shell", "/usr/sbin/nologin", "--no-create-home", garageUser},
	); err != nil {
		return 0, 0, err
	}
	u, err := user.Lookup(garageUser)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup %s user: %w", garageUser, err)
	}
	g, err := user.LookupGroup(garageGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup %s group: %w", garageGroup, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s uid %q: %w", garageUser, u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s gid %q: %w", garageGroup, g.Gid, err)
	}
	return uid, gid, nil
}

func runIfMissing(check, create []string) error {
	if err := exec.Command(check[0], check[1:]...).Run(); err == nil {
		return nil
	}
	cmd := exec.Command(create[0], create[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(create, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureGarageState(nodes []garageNode, uid, gid int) error {
	if err := ensureDir("/etc/garage", 0, gid, 0o750); err != nil {
		return err
	}
	for _, node := range nodes {
		for _, path := range []string{
			fmt.Sprintf("/var/lib/garage-%d", node.Instance),
			fmt.Sprintf("/var/lib/garage-%d/data", node.Instance),
			fmt.Sprintf("/var/lib/garage-%d/meta", node.Instance),
			fmt.Sprintf("/var/lib/garage-%d/snapshots", node.Instance),
		} {
			if err := ensureDir(path, uid, gid, 0o750); err != nil {
				return err
			}
		}
	}
	rpcSecret, err := ensureSecret("/etc/garage/rpc-secret", 32, hex.EncodeToString, func(value string) bool {
		return len(value) == 64 && allHex(value)
	}, gid)
	if err != nil {
		return err
	}
	adminToken, err := ensureSecret("/etc/garage/admin-token", 48, base64.RawURLEncoding.EncodeToString, nonEmpty, gid)
	if err != nil {
		return err
	}
	metricsToken, err := ensureSecret("/etc/garage/metrics-token", 48, base64.RawURLEncoding.EncodeToString, nonEmpty, gid)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := writeConfig(node, rpcSecret, adminToken, metricsToken, gid); err != nil {
			return err
		}
	}
	return nil
}

func ensureDir(path string, uid, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func ensureSecret(path string, size int, encode func([]byte) string, valid func(string) bool, gid int) (string, error) {
	if body, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(body))
		if valid(value) {
			return value, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	random := make([]byte, size)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate %s: %w", filepath.Base(path), err)
	}
	value := encode(random)
	if err := writeRootGroupFile(path, value+"\n", gid, 0o640); err != nil {
		return "", err
	}
	return value, nil
}

func allHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func nonEmpty(value string) bool {
	return value != ""
}

func writeConfig(node garageNode, rpcSecret, adminToken, metricsToken string, gid int) error {
	body := fmt.Sprintf(`replication_factor = 3
consistency_mode = "consistent"

metadata_dir = "/var/lib/garage-%[1]d/meta"
data_dir = "/var/lib/garage-%[1]d/data"
metadata_snapshots_dir = "/var/lib/garage-%[1]d/snapshots"
metadata_fsync = true
data_fsync = true
db_engine = "lmdb"
block_size = "1M"
block_ram_buffer_max = "64MiB"
lmdb_map_size = "10G"
compression_level = 1

rpc_secret = "%[2]s"
rpc_bind_addr = "127.0.0.1:%[3]d"
rpc_public_addr = "127.0.0.1:%[3]d"
allow_world_readable_secrets = false

[s3_api]
api_bind_addr = "127.0.0.1:%[4]d"
s3_region = "garage"

[admin]
api_bind_addr = "127.0.0.1:%[5]d"
metrics_token = "%[6]s"
metrics_require_token = true
admin_token = "%[7]s"
`, node.Instance, rpcSecret, node.RPCPort, node.S3Port, node.AdminPort, metricsToken, adminToken)
	return writeRootGroupFile(fmt.Sprintf("/etc/garage/garage-%d.toml", node.Instance), body, gid, 0o640)
}

func writeRootGroupFile(path, body string, gid int, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, 0, gid); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

func execGarageServer(garageBin, config string) error {
	argv := []string{"runuser", "-u", garageUser, "--preserve-environment", "--", garageBin, "-c", config, "server"}
	return syscall.Exec("/usr/sbin/runuser", argv, os.Environ())
}

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	zotUser              = "zot"
	zotGroup             = "zot"
	zotConfigDir         = "/etc/zot"
	zotConfigPath        = "/etc/zot/config.json"
	zotStorageDir        = "/var/lib/zot"
	zotHTPasswdPath      = "/etc/zot/htpasswd"
	zotPublisherUser     = "artifact-publisher"
	zotPublisherPassword = "/etc/zot/publisher-password"
)

type config struct {
	zotBin      string
	htpasswdBin string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "zot-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("zot-node-runner", flag.ContinueOnError)
	fs.StringVar(&cfg.zotBin, "zot-bin", "", "Path to zot.")
	fs.StringVar(&cfg.htpasswdBin, "zot-htpasswd-bin", "", "Path to zot-htpasswd.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.zotBin == "" {
		return errors.New("--zot-bin is required")
	}
	if cfg.htpasswdBin == "" {
		return errors.New("--zot-htpasswd-bin is required")
	}
	if err := ensureState(cfg); err != nil {
		return err
	}
	return syscall.Exec("/usr/sbin/runuser", []string{"runuser", "-u", zotUser, "--preserve-environment", "--", cfg.zotBin, "serve", zotConfigPath}, os.Environ())
}

func ensureState(cfg config) error {
	if err := runIfMissing([]string{"getent", "group", zotGroup}, []string{"groupadd", "--system", zotGroup}); err != nil {
		return err
	}
	if err := runIfMissing(
		[]string{"id", "-u", zotUser},
		[]string{"useradd", "--system", "--gid", zotGroup, "--home-dir", zotStorageDir, "--shell", "/usr/sbin/nologin", "--no-create-home", zotUser},
	); err != nil {
		return err
	}
	uid, gid, err := zotIDs()
	if err != nil {
		return err
	}
	if err := ensureDir(zotConfigDir, 0, gid, 0o750); err != nil {
		return err
	}
	if err := ensureDir(zotStorageDir, uid, gid, 0o750); err != nil {
		return err
	}
	if _, err := ensureSecret(zotPublisherPassword, 32, gid); err != nil {
		return err
	}
	if err := ensureHTPasswd(cfg.htpasswdBin, gid); err != nil {
		return err
	}
	if err := writeRootGroupFile(zotConfigPath, zotConfig(), gid, 0o640); err != nil {
		return err
	}
	out, err := exec.Command(cfg.zotBin, "verify", zotConfigPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zot verify: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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

func zotIDs() (int, int, error) {
	u, err := user.Lookup(zotUser)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup %s user: %w", zotUser, err)
	}
	g, err := user.LookupGroup(zotGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup %s group: %w", zotGroup, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s uid %q: %w", zotUser, u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s gid %q: %w", zotGroup, g.Gid, err)
	}
	return uid, gid, nil
}

func ensureDir(path string, uid, gid int, mode fs.FileMode) error {
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

func ensureSecret(path string, byteCount int, gid int) (string, error) {
	if body, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(body))
		if value == "" {
			return "", fmt.Errorf("%s is empty", path)
		}
		return value, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %s: %w", filepath.Base(path), err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	if err := writeRootGroupFile(path, value+"\n", gid, 0o640); err != nil {
		return "", err
	}
	return value, nil
}

func ensureHTPasswd(htpasswdBin string, gid int) error {
	if body, err := os.ReadFile(zotHTPasswdPath); err == nil {
		if strings.TrimSpace(string(body)) == "" {
			return fmt.Errorf("%s is empty", zotHTPasswdPath)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", zotHTPasswdPath, err)
	}
	out, err := exec.Command(htpasswdBin, "--username", zotPublisherUser, "--password-file", zotPublisherPassword).Output()
	if err != nil {
		return fmt.Errorf("generate zot htpasswd: %w", err)
	}
	return writeRootGroupFile(zotHTPasswdPath, string(bytes.TrimSpace(out))+"\n", gid, 0o640)
}

func zotConfig() string {
	cfg := map[string]any{
		"distSpecVersion": "1.1.1",
		"storage": map[string]any{
			"rootDirectory": zotStorageDir,
			"commit":        true,
			"dedupe":        true,
			"gc":            true,
			"gcDelay":       "1h",
			"gcInterval":    "24h",
		},
		"http": map[string]any{
			"address": "127.0.0.1",
			"port":    "5080",
			"realm":   "verself-artifacts",
			"auth": map[string]any{
				"htpasswd":  map[string]any{"path": zotHTPasswdPath},
				"failDelay": 1,
			},
			"accessControl": map[string]any{
				"repositories": map[string]any{
					"verself/**": map[string]any{
						"anonymousPolicy": []string{"read"},
						"policies": []map[string]any{
							{
								"users":   []string{zotPublisherUser},
								"actions": []string{"read", "create", "update", "delete"},
							},
						},
					},
					"**": map[string]any{
						"anonymousPolicy": []string{"read"},
					},
				},
			},
		},
		"log": map[string]any{
			"level": "info",
		},
		"extensions": map[string]any{
			"scrub": map[string]any{
				"enable":   true,
				"interval": "24h",
			},
		},
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(body) + "\n"
}

func writeRootGroupFile(path, content string, gid int, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}

package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

type dockerConfig struct {
	Auths map[string]dockerAuth `json:"auths"`
}

type dockerAuth struct {
	Auth string `json:"auth"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deployment-service-zot-auth: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("deployment-service-zot-auth", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var registry string
	var username string
	var passwordFile string
	var output string
	var owner string
	var group string
	fs.StringVar(&registry, "registry", "127.0.0.1:5080", "Zot registry host[:port].")
	fs.StringVar(&username, "username", "artifact-publisher", "Zot publisher username.")
	fs.StringVar(&passwordFile, "password-file", "/etc/zot/publisher-password", "File containing the Zot publisher password.")
	fs.StringVar(&output, "output", "/var/lib/verself/deployment-service/.docker/config.json", "Docker-compatible auth config output path.")
	fs.StringVar(&owner, "owner", "deployment_service", "Output file owner.")
	fs.StringVar(&group, "group", "deployment_service", "Output file group.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	registry = strings.TrimSpace(registry)
	username = strings.TrimSpace(username)
	passwordFile = strings.TrimSpace(passwordFile)
	output = strings.TrimSpace(output)
	owner = strings.TrimSpace(owner)
	group = strings.TrimSpace(group)
	if registry == "" || strings.Contains(registry, "://") || strings.Contains(registry, "/") {
		return fmt.Errorf("--registry must be host[:port] without scheme or path")
	}
	if username == "" || passwordFile == "" || output == "" || owner == "" || group == "" {
		return fmt.Errorf("--username, --password-file, --output, --owner, and --group are required")
	}
	passwordRaw, err := os.ReadFile(passwordFile)
	if err != nil {
		return fmt.Errorf("read Zot publisher password: %w", err)
	}
	password := strings.TrimSpace(string(passwordRaw))
	if password == "" {
		return fmt.Errorf("zot publisher password file is empty")
	}
	uid, gid, err := userIDs(owner, group)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(dockerConfig{
		Auths: map[string]dockerAuth{
			registry: {
				Auth: base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
			},
		},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Docker auth config: %w", err)
	}
	body = append(body, '\n')
	return writeOwnedFile(output, body, uid, gid)
}

func userIDs(owner, group string) (int, int, error) {
	u, err := user.Lookup(owner)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup owner %q: %w", owner, err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %q: %w", group, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse owner uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse group gid %q: %w", g.Gid, err)
	}
	return uid, gid, nil
}

func writeOwnedFile(output string, body []byte, uid, gid int) error {
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Docker auth config directory: %w", err)
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return fmt.Errorf("chown Docker auth config directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("chmod Docker auth config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config.json-*")
	if err != nil {
		return fmt.Errorf("create temporary Docker auth config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary Docker auth config: %w", err)
	}
	if err := tmp.Chown(uid, gid); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown temporary Docker auth config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary Docker auth config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary Docker auth config: %w", err)
	}
	if err := os.Rename(tmpPath, output); err != nil {
		return fmt.Errorf("replace Docker auth config: %w", err)
	}
	if err := os.Chown(output, uid, gid); err != nil {
		return fmt.Errorf("chown Docker auth config: %w", err)
	}
	return os.Chmod(output, 0o600)
}

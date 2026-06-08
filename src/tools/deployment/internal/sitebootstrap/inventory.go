package sitebootstrap

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type InventoryOptions struct {
	Site         string
	Host         string
	HostAlias    string
	User         string
	OutputPath   string
	RecoveryHost string
	RecoveryPort int
	ForceWrite   bool
}

func WriteInventory(opts InventoryOptions) error {
	if strings.TrimSpace(opts.Site) == "" {
		return errors.New("site is required")
	}
	host := strings.TrimSpace(opts.Host)
	if host == "" {
		return errors.New("host is required")
	}
	if net.ParseIP(host) == nil && strings.ContainsAny(host, "/:@") {
		return fmt.Errorf("host %q must be an IP address or DNS name", host)
	}
	alias := strings.TrimSpace(opts.HostAlias)
	if alias == "" {
		alias = "vs-" + opts.Site + "-w0"
	}
	user := strings.TrimSpace(opts.User)
	if user == "" {
		user = "ubuntu"
	}
	output := strings.TrimSpace(opts.OutputPath)
	if output == "" {
		output = filepath.Join("src", "sites", opts.Site, "inventory.ini")
	}
	recoveryHost := strings.TrimSpace(opts.RecoveryHost)
	if recoveryHost == "" {
		recoveryHost = host
	}
	recoveryPort := opts.RecoveryPort
	if recoveryPort == 0 {
		recoveryPort = 22
	}
	body := fmt.Sprintf(`[workers]
%[1]s ansible_host=%[2]s verself_recovery_ssh_host=%[3]s verself_recovery_ssh_port=%[4]d verself_recovery_ssh_user=%[5]s

[infra]
%[1]s ansible_host=%[2]s verself_recovery_ssh_host=%[3]s verself_recovery_ssh_port=%[4]d verself_recovery_ssh_user=%[5]s

[all:vars]
ansible_user=%[5]s
ansible_python_interpreter=/usr/bin/python3
ansible_ssh_extra_args=-o IdentitiesOnly=yes
`, alias, host, recoveryHost, recoveryPort, user)
	return writePublicFile(output, []byte(body), opts.ForceWrite)
}

func writePublicFile(path string, body []byte, force bool) error {
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink %s", path)
		}
		if !force {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

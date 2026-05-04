// Package inventory parses Ansible-style INI inventory files just
// far enough to resolve the SSH `[infra]` host and ansible_user that
// the controller uses for deploys. Avoids a dependency on a full
// ansible inventory parser; the resolved (host, user) is the only
// signal verself-deploy needs.
package inventory

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Host carries the SSH endpoint resolved from inventory. ansible_host
// overrides the bare alias when set; ansible_user falls back to the
// process's USER env var when no group-vars line specifies it.
type Host struct {
	Alias string
	Host  string
	User  string
}

// LoadInfra reads the per-site inventory hosts.ini at path and
// returns the first [infra] host. Missing inventory or empty group
// is a hard failure — silent fallthroughs are the bug class the bash
// version sometimes hit.
func LoadInfra(path string) (*Host, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open inventory: %w", err)
	}
	defer func() { _ = f.Close() }()

	var (
		section     string
		ansibleUser string
		first       *Host
	)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		// :vars sections carry ansible_user=... default.
		if strings.HasSuffix(section, ":vars") {
			if k, v, ok := splitKV(line); ok && k == "ansible_user" {
				ansibleUser = v
			}
			continue
		}
		if section != "infra" {
			continue
		}
		if first != nil {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		alias := fields[0]
		host := alias
		user := ""
		for _, f := range fields[1:] {
			if k, v, ok := splitKV(f); ok {
				switch k {
				case "ansible_host":
					host = v
				case "ansible_user":
					user = v
				}
			}
		}
		first = &Host{Alias: alias, Host: host, User: user}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read inventory: %w", err)
	}
	if first == nil {
		return nil, fmt.Errorf("inventory %s has no entries under [infra]", path)
	}
	if first.User == "" {
		first.User = ansibleUser
	}
	if first.User == "" {
		return nil, errors.New("inventory: no ansible_user set on the [infra] host or in [all:vars]")
	}
	return first, nil
}

func splitKV(s string) (key, value string, ok bool) {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}

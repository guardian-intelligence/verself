package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type InventoryTarget struct {
	Alias string
	Host  string
	User  string
	Port  int
}

func (t InventoryTarget) SSHPorts() []int {
	defaults := []int{2222, 22}
	if strings.Contains(t.User, "@") {
		defaults = []int{22}
	}
	ports := make([]int, 0, len(defaults)+1)
	if t.Port > 0 {
		ports = appendUniquePort(ports, t.Port)
	}
	for _, port := range defaults {
		ports = appendUniquePort(ports, port)
	}
	return ports
}

func LoadInfraTarget(path string) (InventoryTarget, error) {
	f, err := os.Open(path)
	if err != nil {
		return InventoryTarget{}, fmt.Errorf("open inventory %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var (
		section     string
		ansibleUser string
		first       *InventoryTarget
	)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(stripInventoryComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if strings.HasSuffix(section, ":vars") {
			for _, field := range fields {
				key, value, ok := splitInventoryKV(field)
				if ok && key == "ansible_user" {
					ansibleUser = value
				}
			}
			continue
		}
		if section != "infra" || first != nil || strings.Contains(fields[0], "=") {
			continue
		}
		target := InventoryTarget{Alias: fields[0], Host: fields[0]}
		for _, field := range fields[1:] {
			key, value, ok := splitInventoryKV(field)
			if !ok {
				continue
			}
			switch key {
			case "ansible_host":
				target.Host = value
			case "ansible_user":
				target.User = value
			case "ansible_port":
				port, err := parseInventoryPort(value)
				if err != nil {
					return InventoryTarget{}, fmt.Errorf("inventory %s has invalid ansible_port for %s: %w", path, target.Alias, err)
				}
				target.Port = port
			}
		}
		first = &target
	}
	if err := scanner.Err(); err != nil {
		return InventoryTarget{}, fmt.Errorf("read inventory %s: %w", path, err)
	}
	if first == nil {
		return InventoryTarget{}, fmt.Errorf("inventory %s has no [infra] host", path)
	}
	if first.User == "" {
		first.User = ansibleUser
	}
	if first.User == "" {
		return InventoryTarget{}, errors.New("inventory has no ansible_user on [infra] host or [all:vars]")
	}
	if err := validateSSHHost(first.Host); err != nil {
		return InventoryTarget{}, fmt.Errorf("inventory resolved invalid [infra] host %q: %w", first.Host, err)
	}
	return *first, nil
}

func parseInventoryPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("%q is not a TCP port", value)
	}
	return port, nil
}

func appendUniquePort(ports []int, port int) []int {
	if port <= 0 || port > 65535 {
		return ports
	}
	for _, existing := range ports {
		if existing == port {
			return ports
		}
	}
	return append(ports, port)
}

func stripInventoryComment(line string) string {
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		return line[:idx]
	}
	return line
}

func splitInventoryKV(field string) (string, string, bool) {
	key, value, ok := strings.Cut(field, "=")
	if !ok {
		return "", "", false
	}
	value = strings.Trim(strings.TrimSpace(value), "\"'`")
	return strings.TrimSpace(key), value, true
}

func validateSSHHost(host string) error {
	if host == "" {
		return errors.New("empty host")
	}
	if strings.ContainsAny(host, " \t\r\n`'\"") {
		return errors.New("contains shell quoting or whitespace characters")
	}
	return nil
}

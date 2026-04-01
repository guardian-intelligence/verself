// forgevm-init is the PID 1 init process for Firecracker CI VMs.
//
// It mounts a minimal Linux userspace, brings up loopback, waits for a host
// vsock control connection, applies runtime network state from the host, runs
// the requested CI workload, and reboots cleanly when told to shut down.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	prSetChildSubreaper = 36
	ipBin               = "/sbin/ip"
	defaultWorkDir      = "/workspace"
	postgresUID         = 70
	postgresGID         = 70
)

func main() {
	if err := run(); err != nil {
		fatal("run agent", err)
	}
}

func run() error {
	bootStart := time.Now()

	mountVirtualFilesystems()
	if err := configureLoopback(); err != nil {
		return fmt.Errorf("configure loopback: %w", err)
	}
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		fmt.Fprintf(os.Stderr, "[init] warning: prctl PR_SET_CHILD_SUBREAPER: %v\n", errno)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)

	listener, err := listenVsockListener()
	if err != nil {
		return fmt.Errorf("listen vsock: %w", err)
	}
	defer listener.Close()

	readyAt := time.Now()
	fmt.Fprintf(os.Stdout, "[init] vsock listener ready (%dms)\n", readyAt.Sub(bootStart).Milliseconds())

	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("accept vsock connection: %w", err)
	}
	defer conn.Close()

	return runAgent(conn, bootStart, readyAt, sigCh)
}

func mountVirtualFilesystems() {
	mustMount("proc", "/proc", "proc", 0, "")
	mustMount("sysfs", "/sys", "sysfs", 0, "")
	mustMount("devtmpfs", "/dev", "devtmpfs", syscall.MS_NOSUID, "")

	mustMkdir("/dev/pts", 0o755)
	mustMount("devpts", "/dev/pts", "devpts", 0, "newinstance,ptmxmode=0666")

	mustMkdir("/dev/shm", 0o1777)
	mustMount("tmpfs", "/dev/shm", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")
	mustMount("tmpfs", "/run", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")
	mustMount("tmpfs", "/tmp", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")
}

func configureLoopback() error {
	steps := [][]string{
		{ipBin, "addr", "add", "127.0.0.1/8", "dev", "lo"},
		{ipBin, "link", "set", "lo", "up"},
	}
	for _, args := range steps {
		if out, err := runCommandOutput(args[0], args[1:]...); err != nil {
			return fmt.Errorf("%s: %s", strings.Join(args, " "), strings.TrimSpace(out))
		}
	}
	return nil
}

func buildRuntimeEnv(overrides map[string]string) ([]string, error) {
	envMap := map[string]string{
		"HOME": "/home/runner",
		"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM": "xterm",
		"CI":   "true",
	}
	for key, value := range overrides {
		envMap[key] = value
	}

	registry, err := resolveRegistryURL(envMap)
	if err != nil {
		return nil, err
	}
	if registry != "" {
		if envMap["FORGE_METAL_NPM_REGISTRY"] == "" {
			envMap["FORGE_METAL_NPM_REGISTRY"] = registry
		}
		if envMap["NPM_CONFIG_REGISTRY"] == "" {
			envMap["NPM_CONFIG_REGISTRY"] = registry
		}
		if envMap["npm_config_registry"] == "" {
			envMap["npm_config_registry"] = registry
		}
		if envMap["BUN_CONFIG_REGISTRY"] == "" {
			envMap["BUN_CONFIG_REGISTRY"] = registry
		}
	}

	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env, nil
}

func resolveRegistryURL(env map[string]string) (string, error) {
	if value := strings.TrimSpace(env["FORGE_METAL_NPM_REGISTRY"]); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(env["NPM_CONFIG_REGISTRY"]); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(env["npm_config_registry"]); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(env["BUN_CONFIG_REGISTRY"]); value != "" {
		return value, nil
	}

	gateway, err := routeGateway()
	if err != nil {
		return "", err
	}
	if gateway == "" {
		return "", fmt.Errorf("unable to determine host gateway for registry access")
	}
	return "http://" + gateway + ":4873", nil
}

func routeGateway() (string, error) {
	out, err := runCommandOutput(ipBin, "route", "show", "default")
	if err != nil {
		return "", fmt.Errorf("ip route show default: %s", strings.TrimSpace(out))
	}
	fields := strings.Fields(out)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "via" {
			return strings.TrimSpace(fields[i+1]), nil
		}
	}
	return "", nil
}

func resolveCommand(name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	pathDirs := []string{
		"/usr/local/sbin",
		"/usr/local/bin",
		"/usr/sbin",
		"/usr/bin",
		"/sbin",
		"/bin",
	}
	for _, dir := range pathDirs {
		path := dir + "/" + name
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("command not found: %s", name)
}

func normalizeWorkDir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultWorkDir
	}
	return value
}

func envWithHome(env []string, home string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "HOME=") {
			out = append(out, "HOME="+home)
			replaced = true
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, "HOME="+home)
	}
	return out
}

func mustMount(source, target, fstype string, flags uintptr, data string) {
	mustMkdir(target, 0o755)
	if err := syscall.Mount(source, target, fstype, flags, data); err != nil {
		if err == syscall.EBUSY {
			return
		}
		fatal(fmt.Sprintf("mount %s on %s (%s)", source, target, fstype), err)
	}
}

func mustMkdir(path string, perm os.FileMode) {
	if err := os.MkdirAll(path, perm); err != nil {
		fatal(fmt.Sprintf("mkdir %s", path), err)
	}
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "[init] FATAL: %s: %v\n", msg, err)
	os.Exit(1)
}

// vm-bridge is the guest bridge for Firecracker workload VMs.
//
// As PID 1 it mounts a minimal Linux userspace, brings up loopback, waits for a
// host vsock control connection, applies runtime network state from the host,
// executes host-directed commands, and exposes a local CLI control socket.
// As a user-invoked CLI it forwards snapshot requests to the PID 1 bridge.
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	logPrefix            = "[vm-bridge]"
	prSetChildSubreaper  = 36
	ipBin                = "/sbin/ip"
	defaultWorkDir       = "/workspace"
	runnerUID            = 1000
	runnerGID            = 1000
	vmGuestTelemetryBin  = "/usr/local/bin/vm-guest-telemetry"
	vmGuestTelemetryPort = 10790
	bridgeFaultEnvVar    = "FORGE_METAL_VM_BRIDGE_FAULT"
)

type bridgeFaultMode string

const (
	bridgeFaultNone          bridgeFaultMode = ""
	bridgeFaultResultSeqZero bridgeFaultMode = "result_seq_zero"
)

func main() {
	if len(os.Args) > 1 {
		if err := runCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "vm-bridge: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runInit(); err != nil {
		fatal("run bridge", err)
	}
}

func runInit() error {
	bootStart := time.Now()
	bootTimings := vmproto.GuestBootTimings{
		KernelBootToInitStartMS:        kernelUptimeMS(),
		InitStartMS:                    0,
		MountVirtualFilesystemsStartMS: 0,
	}
	bridgeFault, err := parseBridgeFaultMode(os.Getenv(bridgeFaultEnvVar))
	if err != nil {
		return err
	}

	mountVirtualFilesystems()
	bootTimings.MountVirtualFilesystemsDoneMS = elapsedBootMS(bootStart)
	bootTimings.ConfigureLoopbackStartMS = elapsedBootMS(bootStart)
	if err := configureLoopback(); err != nil {
		return fmt.Errorf("configure loopback: %w", err)
	}
	bootTimings.ConfigureLoopbackDoneMS = elapsedBootMS(bootStart)
	bootTimings.SetSubreaperStartMS = elapsedBootMS(bootStart)
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		fmt.Fprintf(os.Stderr, "%s warning: prctl PR_SET_CHILD_SUBREAPER: %v\n", logPrefix, errno)
	}
	bootTimings.SetSubreaperDoneMS = elapsedBootMS(bootStart)
	// Telemetry start used to live here, before the vsock listen. Moved to
	// after listener.Accept() below so the fork+exec does not steal CPU
	// from the kernel_boot_to_hello_enqueue window. The StartTelemetry*
	// fields are kept in the boot timings struct (zero-duration at this
	// point) so host-side span ordering checks remain stable.
	bootTimings.StartTelemetryStartMS = elapsedBootMS(bootStart)
	bootTimings.StartTelemetryDoneMS = bootTimings.StartTelemetryStartMS

	bootTimings.SignalNotifyStartMS = elapsedBootMS(bootStart)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	bootTimings.SignalNotifyDoneMS = elapsedBootMS(bootStart)

	bootTimings.VSockListenStartMS = elapsedBootMS(bootStart)
	listener, err := listenVsockListener()
	if err != nil {
		return fmt.Errorf("listen vsock: %w", err)
	}
	defer listener.Close()

	readyAt := time.Now()
	bootTimings.VSockListenDoneMS = readyAt.Sub(bootStart).Milliseconds()
	bootTimings.KernelBootToVSockListenDoneMS = kernelUptimeMS()
	fmt.Fprintf(os.Stdout, "%s vsock listener ready (%dms)\n", logPrefix, readyAt.Sub(bootStart).Milliseconds())

	bootTimings.VSockAcceptStartMS = elapsedBootMS(bootStart)
	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("accept vsock connection: %w", err)
	}
	bootTimings.VSockAcceptDoneMS = elapsedBootMS(bootStart)
	bootTimings.KernelBootToVSockAcceptDoneMS = kernelUptimeMS()
	defer conn.Close()
	// Telemetry agent fork+exec happens after accept so the post-hello
	// latency covers it instead of the pre-hello critical path.
	maybeStartGuestTelemetry()

	return runAgent(conn, bootStart, readyAt, sigCh, bridgeFault, bootTimings)
}

func elapsedBootMS(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

func kernelUptimeMS() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return -1
	}
	return ts.Sec*1000 + ts.Nsec/int64(time.Millisecond)
}

func parseBridgeFaultMode(raw string) (bridgeFaultMode, error) {
	mode := bridgeFaultMode(strings.TrimSpace(raw))
	switch mode {
	case bridgeFaultNone, bridgeFaultResultSeqZero:
		return mode, nil
	default:
		return bridgeFaultNone, fmt.Errorf("unsupported vm-bridge fault mode: %q", raw)
	}
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

// configureLoopback brings up lo via raw netlink instead of forking
// /sbin/ip. Two fork+exec cycles at boot consume ~10ms of CPU; three
// netlink syscalls run in well under 1ms. AddrReplace is idempotent on
// re-runs (AddrAdd would EEXIST on a second call via NLM_F_EXCL). Requires
// CAP_NET_ADMIN, which PID 1 in a fresh guest netns has by default.
func configureLoopback() error {
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("lookup lo: %w", err)
	}
	addr := &netlink.Addr{IPNet: &net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.CIDRMask(8, 32)}}
	if err := netlink.AddrReplace(lo, addr); err != nil {
		return fmt.Errorf("addr replace 127.0.0.1/8 on lo: %w", err)
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return fmt.Errorf("link set lo up: %w", err)
	}
	return nil
}

func buildRuntimeEnv(overrides map[string]string, network vmproto.NetworkConfig, filesystemMountPaths []string) ([]string, error) {
	envMap := map[string]string{
		"HOME":                         "/home/runner",
		"PATH":                         "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM":                         "xterm",
		"FORGE_METAL_VM_BRIDGE_SOCKET": bridgeSocketPath,
	}
	if len(filesystemMountPaths) > 0 {
		envMap["FORGE_METAL_COMPOSED_ZVOL_MOUNTS"] = strings.Join(filesystemMountPaths, ":")
	}
	for key, value := range overrides {
		envMap[key] = value
	}

	if network.HostServiceIP != "" {
		envMap["FORGE_METAL_HOST_SERVICE_IP"] = network.HostServiceIP
		if network.HostServicePort > 0 {
			envMap["FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN"] = fmt.Sprintf("http://%s:%d", network.HostServiceIP, network.HostServicePort)
		}
	}

	registry, err := resolveRegistryURL(envMap, network)
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

func resolveRegistryURL(env map[string]string, network vmproto.NetworkConfig) (string, error) {
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

	if network.HostServiceIP != "" {
		return "http://" + network.HostServiceIP + ":4873", nil
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
	fmt.Fprintf(os.Stderr, "%s FATAL: %s: %v\n", logPrefix, msg, err)
	os.Exit(1)
}

func maybeStartGuestTelemetry() {
	if _, err := os.Stat(vmGuestTelemetryBin); err != nil {
		return
	}

	cmd := exec.Command(vmGuestTelemetryBin, "--port", strconv.Itoa(vmGuestTelemetryPort))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "%s warning: start vm-guest-telemetry: %v\n", logPrefix, err)
		return
	}
	fmt.Fprintf(os.Stdout, "%s vm-guest-telemetry started (pid=%d port=%d)\n", logPrefix, cmd.Process.Pid, vmGuestTelemetryPort)
	go func() {
		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "%s warning: vm-guest-telemetry exited: %v\n", logPrefix, err)
		}
	}()
}

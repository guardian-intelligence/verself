// forgevm-init is the PID 1 init process for Firecracker CI VMs.
//
// Statically linked, ~30 lines of real logic. Runs as /sbin/init inside
// the guest. Reads job config from /etc/ci/job.json (written to the zvol
// by the host orchestrator before boot), execs the command, and exits
// with the command's exit code.
//
// Build: CGO_ENABLED=0 go build -ldflags='-s -w' -o forgevm-init ./cmd/forgevm-init
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// PR_SET_CHILD_SUBREAPER makes this process inherit orphaned children.
// As PID 1 inside the VM, we already get this behavior, but setting it
// explicitly is harmless and documents the intent.
const prSetChildSubreaper = 36

const ipBin = "/sbin/ip"

// jobConfig is the schema for /etc/ci/job.json, written to the zvol
// by the host orchestrator before VM boot.
type jobConfig struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
	WorkDir string            `json:"work_dir"`
}

func main() {
	bootStart := time.Now()

	// --- Mount virtual filesystems ---
	// Each mount is required for a functioning Linux userspace.

	// /proc: process info, /proc/self/exe, /proc/sys. Required by
	// nearly everything including Node.js (reads /proc/cpuinfo).
	mustMount("proc", "/proc", "proc", 0, "")

	// /sys: kernel objects, device info. Required by udev-like tools
	// and Node.js (reads /sys/devices for hardware info).
	mustMount("sysfs", "/sys", "sysfs", 0, "")

	// /dev: device nodes. devtmpfs auto-populates /dev/null, /dev/zero,
	// /dev/random etc. Without this, most programs fail immediately.
	mustMount("devtmpfs", "/dev", "devtmpfs", syscall.MS_NOSUID, "")

	// /dev/pts: pseudo-terminal devices. Required if any subprocess
	// needs a PTY (git, npm, interactive tools).
	mustMkdir("/dev/pts", 0755)
	mustMount("devpts", "/dev/pts", "devpts", 0, "newinstance,ptmxmode=0666")

	// /dev/shm: POSIX shared memory. Node.js V8 uses shm for
	// SharedArrayBuffer and worker threads.
	mustMkdir("/dev/shm", 01777)
	mustMount("tmpfs", "/dev/shm", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")

	// /run: runtime state (PID files, sockets). Standard FHS location.
	mustMount("tmpfs", "/run", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")

	// /tmp: temporary files. Many build tools (npm, tsc) write here.
	mustMount("tmpfs", "/tmp", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")

	fmt.Fprintf(os.Stdout, "[init] mounts complete (%dms)\n", time.Since(bootStart).Milliseconds())

	// --- Configure network from kernel boot params ---
	// The host orchestrator passes ip=<client>::<gw>:<mask>::<device>:off
	// in the kernel command line. Since CONFIG_IP_PNP is not enabled in
	// the kernel, we parse and apply it here in userspace.
	configureNetwork()

	// --- Read job config ---
	data, err := os.ReadFile("/etc/ci/job.json")
	if err != nil {
		fatal("read /etc/ci/job.json", err)
	}

	var cfg jobConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fatal("parse job config", err)
	}

	if len(cfg.Command) == 0 {
		fatal("job config", fmt.Errorf("command is empty"))
	}

	fmt.Fprintf(os.Stdout, "[init] job: %s\n", strings.Join(cfg.Command, " "))

	// --- Set child subreaper ---
	// Ensures we inherit orphaned grandchildren for reaping.
	_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0)
	if errno != 0 {
		fmt.Fprintf(os.Stderr, "[init] warning: prctl PR_SET_CHILD_SUBREAPER: %v\n", errno)
	}

	// --- Build environment ---
	env := []string{
		"HOME=/home/runner",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=xterm",
		"CI=true",
	}
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}

	// --- Fork child process ---
	// We use raw fork+exec instead of os/exec to avoid any pipe
	// goroutines that could race with our Wait4 reaper loop.
	argv0, err := resolveCommand(cfg.Command[0])
	if err != nil {
		fatal("resolve command", err)
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "/home/runner"
	}

	pid, err := syscall.ForkExec(argv0, cfg.Command, &syscall.ProcAttr{
		Dir:   workDir,
		Env:   env,
		Files: []uintptr{0, 1, 2}, // inherit stdin/stdout/stderr (serial console)
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		fatal("fork+exec", err)
	}

	fmt.Fprintf(os.Stdout, "[init] child pid=%d started (%dms since boot)\n",
		pid, time.Since(bootStart).Milliseconds())

	// --- Forward SIGTERM to child ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		<-sigCh
		syscall.Kill(pid, syscall.SIGTERM)
	}()

	// --- Reap loop ---
	// As PID 1, we receive SIGCHLD for ALL orphaned processes.
	// Block on Wait4(-1) until our main child exits, reaping
	// any orphans along the way.
	exitCode := reapUntilChild(pid)

	// Drain remaining zombies (non-blocking).
	for {
		var ws syscall.WaitStatus
		p, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		if p <= 0 || err != nil {
			break
		}
	}

	fmt.Fprintf(os.Stdout, "[init] child exited with code %d (%dms total)\n",
		exitCode, time.Since(bootStart).Milliseconds())

	// Machine-parseable exit code marker for the host orchestrator.
	// The host parses serial output for this line since Firecracker's
	// own exit code doesn't reflect the guest command's exit code.
	fmt.Fprintf(os.Stdout, "FORGEVM_EXIT_CODE=%d\n", exitCode)

	// Trigger VM shutdown. POWER_OFF halts the CPU but Firecracker
	// doesn't exit (it sees a halt, not a reboot). RESTART triggers
	// the keyboard controller reset path (reboot=k in boot args),
	// which Firecracker detects and exits cleanly.
	syscall.Sync()
	syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

// reapUntilChild blocks on Wait4(-1) reaping all children until the
// specified PID exits. Returns the exit code.
func reapUntilChild(childPID int) int {
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, 0, nil)
		if err != nil {
			// ECHILD: no children remain. Shouldn't happen but be safe.
			return 1
		}
		if pid == childPID {
			if ws.Exited() {
				return ws.ExitStatus()
			}
			if ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			return 1
		}
		// Reaped an orphan; keep waiting for our main child.
	}
}

// resolveCommand finds the absolute path for a command name.
// Searches PATH in the same order as the environment variable.
func resolveCommand(name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	// Match PATH order: /usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
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

// configureNetwork parses the ip= kernel boot parameter from /proc/cmdline
// and configures the network interface. Non-fatal: offline CI jobs still work.
//
// Format: ip=<client>:<server>:<gw>:<netmask>:<hostname>:<device>:<autoconf>
// Example: ip=172.16.0.2::172.16.0.1:255.255.255.252::eth0:off
// LEARNING: The kernel boot param ip=172.16.0.2::172.16.0.1:255.255.255.252::eth0:off
// is ignored because CONFIG_IP_PNP is not enabled. The kernel logs it as "Unknown
// kernel command line parameters". We parse and apply it here in userspace.
//
// LEARNING: Loopback (127.0.0.1) is NOT configured by default in Firecracker VMs.
// PostgreSQL binds to localhost and fails with "Address not available" without this.
//
// LEARNING: Must use absolute path /sbin/ip — BusyBox provides it, but PATH is not
// set when forgevm-init runs as PID 1 (before the job's env is applied).
func configureNetwork() {
	// Bring up loopback (required for localhost/127.0.0.1 services like Postgres).
	loSteps := [][]string{
		{ipBin, "addr", "add", "127.0.0.1/8", "dev", "lo"},
		{ipBin, "link", "set", "lo", "up"},
	}
	for _, args := range loSteps {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.CombinedOutput() // best-effort, don't fail
	}

	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[init] warning: read /proc/cmdline: %v\n", err)
		return
	}

	var ipParam string
	for _, param := range strings.Fields(string(cmdline)) {
		if strings.HasPrefix(param, "ip=") {
			ipParam = strings.TrimPrefix(param, "ip=")
			break
		}
	}
	if ipParam == "" {
		return // No ip= parameter, skip network setup.
	}

	// Parse: client:server:gw:netmask:hostname:device:autoconf
	parts := strings.Split(ipParam, ":")
	if len(parts) < 6 {
		fmt.Fprintf(os.Stderr, "[init] warning: malformed ip= parameter: %s\n", ipParam)
		return
	}
	clientIP := parts[0]
	gateway := parts[2]
	netmask := parts[3]
	device := parts[5]
	if clientIP == "" || device == "" {
		return
	}

	// Convert netmask to prefix length.
	prefixLen := 32
	if netmask != "" {
		mask := net.IPMask(net.ParseIP(netmask).To4())
		if mask != nil {
			ones, _ := mask.Size()
			if ones > 0 {
				prefixLen = ones
			}
		}
	}

	cidr := fmt.Sprintf("%s/%d", clientIP, prefixLen)

	// Apply network configuration via /sbin/ip (BusyBox applet).
	steps := []struct {
		name string
		args []string
	}{
		{"addr", []string{ipBin, "addr", "add", cidr, "dev", device}},
		{"link", []string{ipBin, "link", "set", device, "up"}},
	}
	if gateway != "" {
		steps = append(steps, struct {
			name string
			args []string
		}{"route", []string{ipBin, "route", "add", "default", "via", gateway}})
	}

	for _, step := range steps {
		cmd := exec.Command(step.args[0], step.args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "[init] warning: network %s: %s: %v\n",
				step.name, strings.TrimSpace(string(out)), err)
			return
		}
	}

	fmt.Fprintf(os.Stdout, "[init] network: %s dev %s gw %s\n", cidr, device, gateway)
}

func mustMount(source, target, fstype string, flags uintptr, data string) {
	mustMkdir(target, 0755)
	if err := syscall.Mount(source, target, fstype, flags, data); err != nil {
		// EBUSY means the target is already mounted (e.g., kernel auto-mounts
		// devtmpfs when CONFIG_DEVTMPFS_MOUNT=y). Not an error.
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

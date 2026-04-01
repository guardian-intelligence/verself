// forgevm-init is the PID 1 init process for Firecracker CI VMs.
//
// It mounts the minimal Linux userspace, configures networking, starts
// requested guest services, fetches structured CI phases from Firecracker MMDS,
// emits the final exit code marker, and reboots the VM cleanly.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	prSetChildSubreaper = 36
	ipBin               = "/sbin/ip"
	defaultWorkDir      = "/workspace"
	defaultMMDSIPv4     = "169.254.169.254"
	mmdsTokenTTLSeconds = "60"
	jobConfigPath       = "/etc/ci/job.json"
	postgresUID         = 70
	postgresGID         = 70
)

type jobConfig struct {
	PrepareCommand []string          `json:"prepare_command"`
	PrepareWorkDir string            `json:"prepare_work_dir"`
	RunCommand     []string          `json:"run_command"`
	RunWorkDir     string            `json:"run_work_dir"`
	Services       []string          `json:"services"`
	Env            map[string]string `json:"env"`
}

type mmdsDocument struct {
	ForgeMetal mmdsForgeMetal `json:"forge_metal"`
}

type mmdsForgeMetal struct {
	SchemaVersion int       `json:"schema_version"`
	Job           jobConfig `json:"job"`
}

func main() {
	bootStart := time.Now()

	mountVirtualFilesystems()
	fmt.Fprintf(os.Stdout, "[init] mounts complete (%dms)\n", time.Since(bootStart).Milliseconds())

	configureNetwork()

	cfg, transport, err := readJobConfig()
	if err != nil {
		fatal("read job config", err)
	}
	if len(cfg.RunCommand) == 0 {
		fatal("job config", fmt.Errorf("run_command is empty"))
	}
	fmt.Fprintf(os.Stdout, "[init] job config transport: %s\n", transport)
	fmt.Fprintf(os.Stdout, "FORGEVM_JOB_CONFIG_TRANSPORT=%s\n", transport)

	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		fmt.Fprintf(os.Stderr, "[init] warning: prctl PR_SET_CHILD_SUBREAPER: %v\n", errno)
	}

	env, err := buildRuntimeEnv(cfg.Env)
	if err != nil {
		fatal("build runtime env", err)
	}

	var activeChildPID atomic.Int64
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		<-sigCh
		if pid := int(activeChildPID.Load()); pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}()

	if err := startServices(cfg.Services, env); err != nil {
		fatal("start services", err)
	}

	exitCode := 0
	if len(cfg.PrepareCommand) > 0 {
		exitCode = runPhase("prepare", cfg.PrepareCommand, normalizeWorkDir(cfg.PrepareWorkDir), env, &activeChildPID)
	}
	if exitCode == 0 {
		exitCode = runPhase("run", cfg.RunCommand, normalizeWorkDir(cfg.RunWorkDir), env, &activeChildPID)
	}

	drainZombies()

	fmt.Fprintf(os.Stdout, "[init] job complete with code %d (%dms total)\n", exitCode, time.Since(bootStart).Milliseconds())
	fmt.Fprintf(os.Stdout, "FORGEVM_EXIT_CODE=%d\n", exitCode)

	syscall.Sync()
	syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}

func mountVirtualFilesystems() {
	mustMount("proc", "/proc", "proc", 0, "")
	mustMount("sysfs", "/sys", "sysfs", 0, "")
	mustMount("devtmpfs", "/dev", "devtmpfs", syscall.MS_NOSUID, "")

	mustMkdir("/dev/pts", 0755)
	mustMount("devpts", "/dev/pts", "devpts", 0, "newinstance,ptmxmode=0666")

	mustMkdir("/dev/shm", 01777)
	mustMount("tmpfs", "/dev/shm", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")
	mustMount("tmpfs", "/run", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")
	mustMount("tmpfs", "/tmp", "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, "")
}

func readJobConfig() (*jobConfig, string, error) {
	cfg, err := readJobConfigFromMMDS()
	if err == nil {
		return cfg, "mmds", nil
	}

	fileCfg, fileErr := readJobConfigFromFile()
	if fileErr == nil {
		fmt.Fprintf(os.Stderr, "[init] warning: MMDS unavailable, using file fallback: %v\n", err)
		return fileCfg, "file-fallback", nil
	}

	return nil, "", fmt.Errorf("read job config from MMDS: %w (file fallback: %v)", err, fileErr)
}

func readJobConfigFromMMDS() (*jobConfig, error) {
	if err := configureMMDSRoute(defaultMMDSIPv4); err != nil {
		return nil, err
	}
	defer func() {
		if err := deleteMMDSRoute(defaultMMDSIPv4); err != nil {
			fmt.Fprintf(os.Stderr, "[init] warning: delete MMDS route: %v\n", err)
		}
	}()

	return fetchMMDSJobConfig("http://"+defaultMMDSIPv4, &http.Client{Timeout: 2 * time.Second})
}

func fetchMMDSJobConfig(baseURL string, client *http.Client) (*jobConfig, error) {
	token, err := requestMMDSToken(baseURL, client)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/forge_metal", nil)
	if err != nil {
		return nil, fmt.Errorf("create MMDS data request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-metadata-token", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request MMDS data: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read MMDS data response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request MMDS data: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return decodeMMDSJobConfig(data)
}

func requestMMDSToken(baseURL string, client *http.Client) (string, error) {
	req, err := http.NewRequest(http.MethodPut, strings.TrimRight(baseURL, "/")+"/latest/api/token", nil)
	if err != nil {
		return "", fmt.Errorf("create MMDS token request: %w", err)
	}
	req.Header.Set("X-metadata-token-ttl-seconds", mmdsTokenTTLSeconds)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request MMDS token: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read MMDS token response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("request MMDS token: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("request MMDS token: empty response")
	}
	return token, nil
}

func decodeMMDSJobConfig(data []byte) (*jobConfig, error) {
	var direct mmdsForgeMetal
	if err := json.Unmarshal(data, &direct); err == nil && direct.SchemaVersion != 0 {
		if direct.SchemaVersion != 1 {
			return nil, fmt.Errorf("unsupported MMDS schema version %d", direct.SchemaVersion)
		}
		return &direct.Job, nil
	}

	var doc mmdsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode MMDS document: %w", err)
	}
	if doc.ForgeMetal.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported MMDS schema version %d", doc.ForgeMetal.SchemaVersion)
	}
	return &doc.ForgeMetal.Job, nil
}

func readJobConfigFromFile() (*jobConfig, error) {
	data, err := os.ReadFile(jobConfigPath)
	if err != nil {
		return nil, err
	}

	var cfg jobConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
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

	gateway := strings.TrimSpace(bootGateway())
	if gateway == "" {
		var err error
		gateway, err = routeGateway()
		if err != nil {
			return "", err
		}
	}
	if gateway == "" {
		return "", fmt.Errorf("unable to determine host gateway for registry access")
	}
	return "http://" + gateway + ":4873", nil
}

func startServices(services []string, env []string) error {
	for _, service := range services {
		switch service {
		case "postgres":
			if err := startPostgres(env); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported service %q", service)
		}
	}
	return nil
}

func startPostgres(env []string) error {
	mustMkdir("/run/postgresql", 0755)
	if err := os.Chown("/run/postgresql", postgresUID, postgresGID); err != nil {
		return fmt.Errorf("chown /run/postgresql: %w", err)
	}

	pgCtl, err := resolveCommand("pg_ctl")
	if err != nil {
		return err
	}

	cmd := exec.Command(pgCtl, "start", "-D", "/var/lib/postgresql/data", "-l", "/tmp/pg.log", "-w")
	cmd.Dir = "/var/lib/postgresql"
	cmd.Env = envWithHome(env, "/var/lib/postgresql")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: postgresUID, Gid: postgresGID},
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start postgres: %w", err)
	}
	fmt.Fprintln(os.Stdout, "[init] service postgres ready")
	return nil
}

func runPhase(label string, argv []string, workDir string, env []string, activeChild *atomic.Int64) int {
	if len(argv) == 0 {
		return 0
	}
	argv0, err := resolveCommand(argv[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "[init] %s resolve command: %v\n", label, err)
		return 127
	}

	fmt.Fprintf(os.Stdout, "[init] %s: cwd=%s argv=%s\n", label, workDir, strings.Join(argv, " "))

	pid, err := syscall.ForkExec(argv0, argv, &syscall.ProcAttr{
		Dir:   workDir,
		Env:   env,
		Files: []uintptr{0, 1, 2},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[init] %s fork+exec: %v\n", label, err)
		return 127
	}

	activeChild.Store(int64(pid))
	exitCode := reapUntilChild(pid)
	activeChild.Store(0)

	fmt.Fprintf(os.Stdout, "[init] %s exited with code %d\n", label, exitCode)
	return exitCode
}

func reapUntilChild(childPID int) int {
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, 0, nil)
		if err != nil {
			return 1
		}
		if pid != childPID {
			continue
		}
		if ws.Exited() {
			return ws.ExitStatus()
		}
		if ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return 1
	}
}

func drainZombies() {
	for {
		var ws syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			return
		}
	}
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

// configureNetwork parses the ip= kernel boot parameter from /proc/cmdline
// and configures the network interface. Non-fatal: offline CI jobs still work.
func configureNetwork() {
	loSteps := [][]string{
		{ipBin, "addr", "add", "127.0.0.1/8", "dev", "lo"},
		{ipBin, "link", "set", "lo", "up"},
	}
	for _, args := range loSteps {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.CombinedOutput()
	}

	clientIP, gateway, netmask, device, err := bootNetworkFields()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[init] warning: parse boot network: %v\n", err)
		return
	}
	if clientIP == "" || device == "" {
		return
	}

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
			fmt.Fprintf(os.Stderr, "[init] warning: network %s: %s: %v\n", step.name, strings.TrimSpace(string(out)), err)
			return
		}
	}

	fmt.Fprintf(os.Stdout, "[init] network: %s dev %s gw %s\n", cidr, device, gateway)
}

func bootNetworkFields() (clientIP, gateway, netmask, device string, err error) {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "", "", "", "", err
	}
	for _, param := range strings.Fields(string(cmdline)) {
		if !strings.HasPrefix(param, "ip=") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(param, "ip="), ":")
		if len(parts) < 6 {
			return "", "", "", "", fmt.Errorf("malformed ip parameter: %s", param)
		}
		return parts[0], parts[2], parts[3], parts[5], nil
	}
	return "", "", "", "", nil
}

func bootGateway() string {
	_, gateway, _, _, err := bootNetworkFields()
	if err != nil {
		return ""
	}
	return gateway
}

func routeGateway() (string, error) {
	out, err := exec.Command(ipBin, "route", "show", "default").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ip route show default: %s: %w", strings.TrimSpace(string(out)), err)
	}
	fields := strings.Fields(string(out))
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "via" {
			return strings.TrimSpace(fields[i+1]), nil
		}
	}
	return "", nil
}

func configureMMDSRoute(ip string) error {
	cmd := exec.Command(ipBin, "route", "replace", ip+"/32", "dev", "eth0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip route replace %s/32 dev eth0: %s: %w", ip, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func deleteMMDSRoute(ip string) error {
	cmd := exec.Command(ipBin, "route", "del", ip+"/32", "dev", "eth0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip route del %s/32 dev eth0: %s: %w", ip, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func mustMount(source, target, fstype string, flags uintptr, data string) {
	mustMkdir(target, 0755)
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

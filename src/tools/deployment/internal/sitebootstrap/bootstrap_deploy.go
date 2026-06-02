package sitebootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/verself/deployment-service/deployengine"
)

const (
	defaultNomadRemoteAddr = "127.0.0.1:4646"

	bootstrapArtifactRemotePort = 18734
	openBaoRootKeyPath          = "/etc/verself/bootstrap/openbao-root.key"
)

type BootstrapDeployOptions struct {
	Site            string
	SHA             string
	RepoRoot        string
	InventoryPath   string
	SecretVarsPath  string
	RuntimeSeedPath string
	SSHTransport    string
	Timeout         time.Duration
}

type inventoryTarget struct {
	Host         string
	User         string
	Port         int
	RecoveryHost string
	RecoveryUser string
	RecoveryPort int
}

type childProcess struct {
	cmd  *exec.Cmd
	done chan error
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func RunBootstrapDeploy(ctx context.Context, opts BootstrapDeployOptions) error {
	opts = normalizeBootstrapDeployOptions(opts)
	if opts.Site == "" {
		return errors.New("site is required")
	}
	if opts.SHA == "" {
		return errors.New("sha is required")
	}
	if opts.RepoRoot == "" {
		return errors.New("repo root is required")
	}
	if opts.InventoryPath == "" {
		return errors.New("inventory path is required")
	}
	if err := checkLocalBootstrapMaterial(opts); err != nil {
		return err
	}
	target, err := loadBootstrapInventoryTarget(opts.InventoryPath, opts.SSHTransport)
	if err != nil {
		return err
	}
	artifactPort, err := freeLoopbackPort()
	if err != nil {
		return err
	}
	nomadPort, err := freeLoopbackPort()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if err := checkRemoteOpenBaoRootKey(ctx, target); err != nil {
		return err
	}

	artifactDir, err := os.MkdirTemp("", "verself-bootstrap-artifacts-*")
	if err != nil {
		return fmt.Errorf("create bootstrap artifact directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(artifactDir) }()
	artifactServer, err := startBootstrapArtifactServer(artifactDir, artifactPort)
	if err != nil {
		return err
	}
	defer func() { _ = artifactServer.Shutdown(context.Background()) }()
	if err := waitHTTP(ctx, "http://127.0.0.1:"+strconv.Itoa(artifactPort)+"/healthz", http.StatusNoContent, nil); err != nil {
		return fmt.Errorf("local bootstrap artifact server readiness: %w", err)
	}

	artifactTunnelCmd := startArtifactTunnel(ctx, target, artifactPort, bootstrapArtifactRemotePort)
	artifactTunnel, err := startChildProcess(artifactTunnelCmd)
	if err != nil {
		return fmt.Errorf("start bootstrap artifact reverse tunnel: %w", err)
	}
	defer stopChildProcess(cancel, artifactTunnel)

	sshCmd := startNomadTunnel(ctx, target, nomadPort)
	sshProc, err := startChildProcess(sshCmd)
	if err != nil {
		return fmt.Errorf("start Nomad recovery tunnel: %w", err)
	}
	defer stopChildProcess(cancel, sshProc)
	if err := waitHTTP(ctx, "http://127.0.0.1:"+strconv.Itoa(nomadPort)+"/v1/status/leader", http.StatusOK, sshProc.done); err != nil {
		return fmt.Errorf("nomad tunnel readiness: %w", err)
	}

	deployRunKey, err := randomPrefixedID("bootstrap")
	if err != nil {
		return err
	}
	_, err = deployengine.Run(ctx, deployengine.Options{
		Site:                     opts.Site,
		SHA:                      opts.SHA,
		DeployRunKey:             deployRunKey,
		RepoRoot:                 opts.RepoRoot,
		NomadAddr:                "http://127.0.0.1:" + strconv.Itoa(nomadPort),
		BootstrapArtifactRootURL: "http://127.0.0.1:" + strconv.Itoa(bootstrapArtifactRemotePort),
		ArtifactStager:           localBootstrapArtifactStager(artifactDir),
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Printf("bootstrap_deploy_id=%s site=%s sha=%s\n", deployRunKey, opts.Site, opts.SHA); err != nil {
		return fmt.Errorf("write bootstrap deployment response: %w", err)
	}
	return nil
}

func normalizeBootstrapDeployOptions(opts BootstrapDeployOptions) BootstrapDeployOptions {
	opts.Site = strings.TrimSpace(opts.Site)
	opts.SHA = strings.TrimSpace(opts.SHA)
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)
	opts.InventoryPath = strings.TrimSpace(opts.InventoryPath)
	opts.SecretVarsPath = strings.TrimSpace(opts.SecretVarsPath)
	opts.RuntimeSeedPath = strings.TrimSpace(opts.RuntimeSeedPath)
	opts.SSHTransport = strings.TrimSpace(opts.SSHTransport)
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Minute
	}
	if opts.SSHTransport == "" {
		opts.SSHTransport = "recovery"
	}
	if opts.RepoRoot != "" && opts.Site != "" {
		if opts.SecretVarsPath == "" {
			opts.SecretVarsPath = defaultLocalSecretVarsPath(opts.RepoRoot, opts.Site)
		} else {
			opts.SecretVarsPath = resolveLocalBootstrapPath(opts.RepoRoot, opts.SecretVarsPath)
		}
		if opts.RuntimeSeedPath == "" {
			opts.RuntimeSeedPath = defaultLocalRuntimeSeedPath(opts.RepoRoot, opts.Site)
		} else {
			opts.RuntimeSeedPath = resolveLocalBootstrapPath(opts.RepoRoot, opts.RuntimeSeedPath)
		}
	}
	return opts
}

func defaultLocalSecretVarsPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, ".verself", "site-bootstrap", site, "ansible-secrets.json")
}

func defaultLocalRuntimeSeedPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, ".verself", "site-bootstrap", site, "openbao-runtime-seed.json")
}

func resolveLocalBootstrapPath(repoRoot, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repoRoot, path)
}

func checkLocalBootstrapMaterial(opts BootstrapDeployOptions) error {
	if err := checkLocalSecretFile(opts.SecretVarsPath, "generated Ansible secret vars"); err != nil {
		return fmt.Errorf("%w; run aspect site materialize-seed --site=%s first", err, opts.Site)
	}
	if err := checkLocalRuntimeSeed(opts.RuntimeSeedPath, opts.Site, opts.RepoRoot); err != nil {
		return fmt.Errorf("%w; run aspect site materialize-seed --site=%s first", err, opts.Site)
	}
	return nil
}

func checkLocalSecretFile(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("bootstrap deploy requires local %s at %s", label, path)
		}
		return fmt.Errorf("inspect local %s at %s: %w", label, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("bootstrap deploy requires local %s at %s to be a regular file", label, path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("bootstrap deploy requires non-empty local %s at %s", label, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("bootstrap deploy requires local %s at %s to be readable only by the operator", label, path)
	}
	return nil
}

func checkLocalRuntimeSeed(path, site, repoRoot string) error {
	if err := checkLocalSecretFile(path, "OpenBao runtime seed"); err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read local OpenBao runtime seed at %s: %w", path, err)
	}
	var seed RuntimeSeedBundle
	if err := json.Unmarshal(body, &seed); err != nil {
		return fmt.Errorf("decode local OpenBao runtime seed at %s: %w", path, err)
	}
	if seed.Version != RuntimeSeedBundleVersion {
		return fmt.Errorf("local OpenBao runtime seed at %s has unsupported version %q", path, seed.Version)
	}
	if seed.Site != site {
		return fmt.Errorf("local OpenBao runtime seed at %s is for site %q, not %q", path, seed.Site, site)
	}
	if len(seed.Values) == 0 {
		return fmt.Errorf("local OpenBao runtime seed at %s has no values", path)
	}
	required, err := requiredRuntimeSiteSecretKeys(repoRoot, site)
	if err != nil {
		return err
	}
	var missing []MissingSeedValue
	for key, meta := range required {
		if strings.TrimSpace(seed.Values[key]) == "" {
			missing = append(missing, MissingSeedValue{Key: key, Source: meta.Source})
		}
	}
	if len(missing) > 0 {
		return MissingRuntimeSeedValuesError{Path: path, Missing: missing}
	}
	return nil
}

func startNomadTunnel(ctx context.Context, target inventoryTarget, localPort int) *exec.Cmd {
	remote := defaultNomadRemoteAddr
	forward := "127.0.0.1:" + strconv.Itoa(localPort) + ":" + remote
	addr := target.User + "@" + target.Host
	args := []string{
		"-N",
		"-L", forward,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
	}
	if target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	args = append(args, addr)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func startArtifactTunnel(ctx context.Context, target inventoryTarget, localPort, remotePort int) *exec.Cmd {
	forward := "127.0.0.1:" + strconv.Itoa(remotePort) + ":127.0.0.1:" + strconv.Itoa(localPort)
	addr := target.User + "@" + target.Host
	args := []string{
		"-N",
		"-R", forward,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
	}
	if target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	args = append(args, addr)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func startBootstrapArtifactServer(root string, port int) (*http.Server, error) {
	root = filepath.Clean(root)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", http.FileServer(http.Dir(root)))
	server := &http.Server{
		Addr:              "127.0.0.1:" + strconv.Itoa(port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen bootstrap artifact server: %w", err)
	}
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "site-bootstrap: bootstrap artifact server: "+err.Error())
		}
	}()
	return server, nil
}

func localBootstrapArtifactStager(root string) deployengine.ArtifactStager {
	return func(ctx context.Context, candidates []deployengine.ArtifactStagingCandidate) error {
		for _, candidate := range candidates {
			if err := stageBootstrapArtifact(ctx, root, candidate); err != nil {
				return err
			}
		}
		return nil
	}
}

func stageBootstrapArtifact(ctx context.Context, root string, candidate deployengine.ArtifactStagingCandidate) error {
	rel, err := cleanBootstrapArtifactKey(candidate.Key)
	if err != nil {
		return fmt.Errorf("%s: %w", candidate.Output, err)
	}
	out := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("create bootstrap artifact directory: %w", err)
	}
	if candidate.Body != nil {
		if err := verifyBootstrapArtifactBodyDigest(candidate.Output, candidate.SHA256, candidate.Body); err != nil {
			return err
		}
		if err := os.WriteFile(out, candidate.Body, 0o644); err != nil {
			return fmt.Errorf("write bootstrap artifact %s: %w", candidate.Output, err)
		}
		return nil
	}
	if strings.TrimSpace(candidate.LocalPath) == "" {
		return fmt.Errorf("bootstrap artifact %s has no local path", candidate.Output)
	}
	in, err := os.Open(candidate.LocalPath)
	if err != nil {
		return fmt.Errorf("open bootstrap artifact %s: %w", candidate.Output, err)
	}
	defer func() { _ = in.Close() }()
	outFile, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create bootstrap artifact %s: %w", candidate.Output, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(outFile, hash), contextReader{ctx: ctx, r: in})
	closeErr := outFile.Close()
	if copyErr != nil {
		return fmt.Errorf("copy bootstrap artifact %s: %w", candidate.Output, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close bootstrap artifact %s: %w", candidate.Output, closeErr)
	}
	if err := verifyBootstrapArtifactDigestHex(candidate.Output, candidate.SHA256, hex.EncodeToString(hash.Sum(nil))); err != nil {
		return err
	}
	return nil
}

func verifyBootstrapArtifactBodyDigest(output string, expected string, body []byte) error {
	sum := sha256.Sum256(body)
	return verifyBootstrapArtifactDigestHex(output, expected, hex.EncodeToString(sum[:]))
}

func verifyBootstrapArtifactDigestHex(output string, expected string, actual string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	if actual != expected {
		return fmt.Errorf("bootstrap artifact %s sha256=%s does not match descriptor sha256=%s", output, actual, expected)
	}
	return nil
}

func cleanBootstrapArtifactKey(key string) (string, error) {
	key = path.Clean(strings.TrimSpace(key))
	if key == "." || key == ".." || key == "/" || strings.HasPrefix(key, "../") || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("invalid bootstrap artifact key %q", key)
	}
	return key, nil
}

func checkRemoteOpenBaoRootKey(ctx context.Context, target inventoryTarget) error {
	cmd := sshCommand(ctx, target, openBaoRootKeyPreflightCommand())
	if output, err := cmd.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			detail = ": " + detail
		}
		return fmt.Errorf("bootstrap deploy requires the OpenBao site root key on the host at %s; rerun host convergence with the site root key first%s", openBaoRootKeyPath, detail)
	}
	return nil
}

func openBaoRootKeyPreflightCommand() string {
	path := shellQuote(openBaoRootKeyPath)
	return "sudo -n test -s " + path + " && test \"$(sudo -n stat -c '%a' " + path + ")\" = 600"
}

func sshCommand(ctx context.Context, target inventoryTarget, remoteCommand string) *exec.Cmd {
	addr := target.User + "@" + target.Host
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
	}
	if target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	args = append(args, addr, remoteCommand)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	return cmd
}

func startChildProcess(cmd *exec.Cmd) (*childProcess, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	return &childProcess{cmd: cmd, done: done}, nil
}

func stopChildProcess(cancel context.CancelFunc, proc *childProcess) {
	cancel()
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return
	}
	select {
	case <-proc.done:
	case <-time.After(3 * time.Second):
		_ = proc.cmd.Process.Kill()
		<-proc.done
	}
}

func waitHTTP(ctx context.Context, url string, want int, processDone <-chan error) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case err, ok := <-processDone:
			if ok && err != nil {
				return fmt.Errorf("child process exited before readiness: %w", err)
			}
			return fmt.Errorf("child process exited before readiness")
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w: %v", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func randomPrefixedID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate bootstrap id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func loadBootstrapInventoryTarget(path, transport string) (inventoryTarget, error) {
	target, err := readInventoryTarget(path)
	if err != nil {
		return inventoryTarget{}, err
	}
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "recovery", "wireguard", "wg":
		if target.RecoveryHost == "" {
			return inventoryTarget{}, fmt.Errorf("inventory %s has no verself_recovery_ssh_host for recovery bootstrap deploy", path)
		}
		user := target.RecoveryUser
		if user == "" {
			user = target.User
		}
		return inventoryTarget{Host: target.RecoveryHost, User: user, Port: target.RecoveryPort}, nil
	case "inventory", "pomerium":
		return inventoryTarget{Host: target.Host, User: target.User, Port: target.Port}, nil
	default:
		return inventoryTarget{}, fmt.Errorf("ssh transport must be recovery or inventory, got %q", transport)
	}
}

func readInventoryTarget(path string) (inventoryTarget, error) {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return inventoryTarget{}, fmt.Errorf("read inventory %s: %w", path, err)
	}
	section := ""
	ansibleUser := ""
	var target *inventoryTarget
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(stripInventoryComment(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		fields := strings.Fields(line)
		if strings.HasSuffix(section, ":vars") {
			for _, field := range fields {
				key, value, ok := splitInventoryKV(field)
				if ok && key == "ansible_user" {
					ansibleUser = value
				}
			}
			continue
		}
		if section != "infra" || target != nil || len(fields) == 0 || strings.Contains(fields[0], "=") {
			continue
		}
		current := inventoryTarget{Host: fields[0]}
		for _, field := range fields[1:] {
			key, value, ok := splitInventoryKV(field)
			if !ok {
				continue
			}
			switch key {
			case "ansible_host":
				current.Host = value
			case "ansible_user":
				current.User = value
			case "ansible_port":
				port, err := parseInventoryPort(value)
				if err != nil {
					return inventoryTarget{}, err
				}
				current.Port = port
			case "verself_recovery_ssh_host":
				current.RecoveryHost = value
			case "verself_recovery_ssh_user":
				current.RecoveryUser = value
			case "verself_recovery_ssh_port":
				port, err := parseInventoryPort(value)
				if err != nil {
					return inventoryTarget{}, err
				}
				current.RecoveryPort = port
			}
		}
		target = &current
	}
	if target == nil {
		return inventoryTarget{}, fmt.Errorf("inventory %s has no [infra] host", path)
	}
	if target.User == "" {
		target.User = ansibleUser
	}
	if target.User == "" {
		return inventoryTarget{}, fmt.Errorf("inventory %s has no SSH user", path)
	}
	return *target, nil
}

func parseInventoryPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid inventory port %q", value)
	}
	return port, nil
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
	return strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), "\"'`"), true
}

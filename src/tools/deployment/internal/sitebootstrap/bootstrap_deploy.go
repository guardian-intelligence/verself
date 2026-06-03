package sitebootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/verself/deployment-service/deployengine"
)

const (
	defaultNomadRemoteAddr = "127.0.0.1:4646"

	bootstrapR2CredentialSource = "files"
	openBaoRootKeyPath          = "/etc/verself/bootstrap/openbao-root.key"
)

type BootstrapDeployOptions struct {
	Site                 string
	SHA                  string
	RepoRoot             string
	InventoryPath        string
	SSHTransport         string
	R2ControlPlaneBinary string
	CloudflareBinary     string
	Timeout              time.Duration
}

type bootstrapPublisherCredential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	TokenID         string `json:"token_id"`
	ExpiresOn       string `json:"expires_on"`
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

func RunBootstrapDeploy(ctx context.Context, opts BootstrapDeployOptions) (err error) {
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
	if err := checkBootstrapArtifactPublishingInput(opts); err != nil {
		return err
	}
	target, err := loadBootstrapInventoryTarget(opts.InventoryPath, opts.SSHTransport)
	if err != nil {
		return err
	}
	r2Port, err := freeLoopbackPort()
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

	r2Token, err := randomPrefixedID("r2bootstrap")
	if err != nil {
		return err
	}
	publisher, err := mintBootstrapPublisher(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		revokeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if revokeErr := revokeBootstrapPublisher(revokeCtx, opts, publisher.TokenID); revokeErr != nil {
			err = errors.Join(err, revokeErr)
		}
	}()

	r2ListenAddr := "127.0.0.1:" + strconv.Itoa(r2Port)
	r2ControlPlaneURL := "http://" + r2ListenAddr
	r2Cmd, r2Cleanup, err := startBootstrapR2ControlPlane(ctx, opts, r2ListenAddr, r2Token, publisher)
	if err != nil {
		return err
	}
	defer r2Cleanup()
	r2Proc, err := startChildProcess(r2Cmd)
	if err != nil {
		return fmt.Errorf("start bootstrap R2 control plane: %w", err)
	}
	defer stopChildProcess(cancel, r2Proc)
	if err := waitHTTP(ctx, r2ControlPlaneURL+"/healthz", http.StatusNoContent, r2Proc.done); err != nil {
		return fmt.Errorf("bootstrap R2 control-plane readiness: %w", err)
	}

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
		Site:                opts.Site,
		SHA:                 opts.SHA,
		DeployRunKey:        deployRunKey,
		RepoRoot:            opts.RepoRoot,
		R2ControlPlaneAddr:  r2ControlPlaneURL,
		R2ControlPlaneToken: r2Token,
		NomadAddr:           "http://127.0.0.1:" + strconv.Itoa(nomadPort),
		Bootstrap:           true,
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
	opts.SSHTransport = strings.TrimSpace(opts.SSHTransport)
	opts.R2ControlPlaneBinary = strings.TrimSpace(opts.R2ControlPlaneBinary)
	opts.CloudflareBinary = strings.TrimSpace(opts.CloudflareBinary)
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Minute
	}
	if opts.SSHTransport == "" {
		opts.SSHTransport = "recovery"
	}
	if opts.RepoRoot != "" && opts.Site != "" {
		opts.R2ControlPlaneBinary = resolveLocalBootstrapPath(opts.RepoRoot, opts.R2ControlPlaneBinary)
		opts.CloudflareBinary = resolveLocalBootstrapPath(opts.RepoRoot, opts.CloudflareBinary)
	}
	return opts
}

func resolveLocalBootstrapPath(repoRoot, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repoRoot, path)
}

func checkBootstrapArtifactPublishingInput(opts BootstrapDeployOptions) error {
	if opts.CloudflareBinary == "" {
		return errors.New("bootstrap deploy requires --cloudflare-control-plane-binary")
	}
	if err := checkLocalExecutable(opts.CloudflareBinary, "Cloudflare control-plane binary"); err != nil {
		return err
	}
	if opts.R2ControlPlaneBinary == "" {
		return errors.New("bootstrap deploy requires --r2-control-plane-binary")
	}
	if err := checkLocalExecutable(opts.R2ControlPlaneBinary, "Cloudflare R2 control-plane binary"); err != nil {
		return err
	}
	return nil
}

func mintBootstrapPublisher(ctx context.Context, opts BootstrapDeployOptions) (bootstrapPublisherCredential, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return bootstrapPublisherCredential{}, fmt.Errorf("create bootstrap publisher pipe: %w", err)
	}
	defer func() { _ = reader.Close() }()
	cmd := exec.CommandContext(ctx, opts.CloudflareBinary,
		"--action=mint-bootstrap-publisher",
		"--site="+opts.Site,
		"--repo-root="+opts.RepoRoot,
	)
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		return bootstrapPublisherCredential{}, fmt.Errorf("start Cloudflare bootstrap publisher mint: %w", err)
	}
	_ = writer.Close()
	body, readErr := io.ReadAll(io.LimitReader(reader, 1<<20))
	waitErr := cmd.Wait()
	if waitErr != nil {
		return bootstrapPublisherCredential{}, fmt.Errorf("Cloudflare bootstrap publisher mint failed: %w", waitErr)
	}
	if readErr != nil {
		return bootstrapPublisherCredential{}, fmt.Errorf("read Cloudflare bootstrap publisher credential: %w", readErr)
	}
	var out bootstrapPublisherCredential
	if err := json.Unmarshal(body, &out); err != nil {
		return bootstrapPublisherCredential{}, fmt.Errorf("decode Cloudflare bootstrap publisher credential: %w", err)
	}
	if strings.TrimSpace(out.AccessKeyID) == "" || strings.TrimSpace(out.SecretAccessKey) == "" || strings.TrimSpace(out.TokenID) == "" {
		return bootstrapPublisherCredential{}, errors.New("Cloudflare bootstrap publisher credential is incomplete")
	}
	return out, nil
}

func revokeBootstrapPublisher(ctx context.Context, opts BootstrapDeployOptions, tokenID string) error {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return errors.New("Cloudflare bootstrap publisher token ID is required for revoke")
	}
	credentialDir, err := os.MkdirTemp("", "verself-bootstrap-r2-revoke-")
	if err != nil {
		return fmt.Errorf("create bootstrap publisher revoke credential directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(credentialDir)
	}()
	tokenIDFile, err := writeBootstrapCredentialFile(credentialDir, "r2-publisher-token-id", tokenID)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, opts.CloudflareBinary,
		"--action=revoke-bootstrap-publisher",
		"--site="+opts.Site,
		"--repo-root="+opts.RepoRoot,
		"--bootstrap-publisher-token-id-file="+tokenIDFile,
	)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("revoke Cloudflare bootstrap publisher token: %w", err)
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

func startBootstrapR2ControlPlane(ctx context.Context, opts BootstrapDeployOptions, listenAddr, token string, publisher bootstrapPublisherCredential) (*exec.Cmd, func(), error) {
	if err := checkBootstrapArtifactPublishingInput(opts); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(publisher.AccessKeyID) == "" || strings.TrimSpace(publisher.SecretAccessKey) == "" || strings.TrimSpace(publisher.TokenID) == "" {
		return nil, nil, errors.New("bootstrap publisher credential is incomplete")
	}
	credentialDir, err := os.MkdirTemp("", "verself-bootstrap-r2-")
	if err != nil {
		return nil, nil, fmt.Errorf("create bootstrap R2 credential directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(credentialDir)
	}
	authTokenFile, err := writeBootstrapCredentialFile(credentialDir, "r2-control-plane-token", token)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	accessKeyIDFile, err := writeBootstrapCredentialFile(credentialDir, "r2-publisher-token-id", publisher.AccessKeyID)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	secretAccessKeyFile, err := writeBootstrapCredentialFile(credentialDir, "r2-publisher-secret-access-key", publisher.SecretAccessKey)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	args := []string{
		"--action=serve",
		"--site=" + opts.Site,
		"--repo-root=" + opts.RepoRoot,
		"--listen=" + listenAddr,
		"--auth-token-file=" + authTokenFile,
		"--credential-source=" + bootstrapR2CredentialSource,
		"--parent-access-key-id-file=" + accessKeyIDFile,
		"--parent-secret-access-key-file=" + secretAccessKeyFile,
	}
	cmd := exec.CommandContext(ctx, opts.R2ControlPlaneBinary, args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, cleanup, nil
}

func writeBootstrapCredentialFile(dir, name, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("bootstrap credential %s is empty", name)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", fmt.Errorf("write bootstrap credential %s: %w", name, err)
	}
	return path, nil
}

func checkLocalExecutable(path, label string) error {
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
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("bootstrap deploy requires local %s at %s to be executable", label, path)
	}
	return nil
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

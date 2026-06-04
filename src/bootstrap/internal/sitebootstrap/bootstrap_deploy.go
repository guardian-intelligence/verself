package sitebootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/verself/deployment-service/deployengine"
)

const (
	defaultNomadRemoteAddr   = "127.0.0.1:4646"
	bootstrapArtifactRoot    = "/var/lib/verself/bootstrap-artifacts"
	bootstrapArtifactHTTP    = "http://127.0.0.1:7380"
	openBaoSiteRootTokenPath = "/run/verself/bootstrap/openbao-site-root.token"
	bootstrapRemoteTmpPrefix = "/tmp/verself-bootstrap-artifacts-"
)

type BootstrapDeployOptions struct {
	Site                     string
	SHA                      string
	RepoRoot                 string
	InventoryPath            string
	OpenBaoSiteRootTokenFile string
	BootstrapCredentialsFile string
	SSHTransport             string
	Timeout                  time.Duration
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

type remoteBootstrapArtifactPublisher struct {
	target inventoryTarget
	root   string
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
	if opts.BootstrapCredentialsFile == "" {
		return errors.New("bootstrap credentials file is required")
	}
	target, err := loadBootstrapInventoryTarget(opts.InventoryPath, opts.SSHTransport)
	if err != nil {
		return err
	}
	if err := validateLocalOpenBaoSiteRootTokenFile(opts.OpenBaoSiteRootTokenFile); err != nil {
		return err
	}
	nomadPort, err := freeLoopbackPort()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	if err := stageRemoteOpenBaoSiteRootToken(ctx, target, opts.OpenBaoSiteRootTokenFile); err != nil {
		return err
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
	labels, err := deployengine.QueryNomadComponentLabels(ctx, opts.RepoRoot)
	if err != nil {
		return err
	}
	descriptors, err := deployengine.BuildNomadComponentDescriptors(ctx, opts.RepoRoot, labels)
	if err != nil {
		return err
	}
	bootstrapDescriptors, remainingDescriptors, activeDescriptors, err := partitionBootstrapNomadDescriptors(opts.Site, descriptors, []string{"postgresql", "openbao"})
	if err != nil {
		return err
	}
	common := deployengine.Options{
		Site:              opts.Site,
		ArtifactNamespace: opts.SHA,
		DeployRunKey:      deployRunKey,
		RepoRoot:          opts.RepoRoot,
		ArtifactPublisher: remoteBootstrapArtifactPublisher{target: target, root: bootstrapArtifactRoot},
		NomadAddr:         "http://127.0.0.1:" + strconv.Itoa(nomadPort),
		TaskUserResolver:  remoteTaskUserResolver(target),
	}
	bootstrapOptions := common
	bootstrapOptions.NomadComponentDescriptors = bootstrapDescriptors
	_, err = deployengine.Submit(ctx, bootstrapOptions)
	if err != nil {
		return err
	}
	if err := waitRemotePostgresReady(ctx, target); err != nil {
		return err
	}
	if err := waitRemoteOpenBaoReady(ctx, target); err != nil {
		return err
	}
	runtimeSecrets, err := loadRuntimeSecretCatalog(opts.RepoRoot)
	if err != nil {
		return err
	}
	runtimeAccess, err := inspectNomadRuntimeAccess(ctx, common.NomadAddr, opts.RepoRoot, activeDescriptors, runtimeSecrets)
	if err != nil {
		return err
	}
	bootstrapCredentials, err := loadBootstrapCredentialInput(opts.Site, opts.BootstrapCredentialsFile, runtimeSecrets)
	if err != nil {
		return err
	}
	runtimeValues, err := reconcileOpenBaoRuntime(ctx, target, opts.Site, opts.OpenBaoSiteRootTokenFile, runtimeSecrets, runtimeAccess, bootstrapCredentials)
	if err != nil {
		return err
	}
	postgresCatalog, err := loadPostgresCatalog(opts.RepoRoot)
	if err != nil {
		return err
	}
	if err := reconcilePostgres(ctx, target, postgresCatalog, runtimeValues); err != nil {
		return err
	}
	if err := submitBootstrapDescriptorWaves(ctx, target, opts.OpenBaoSiteRootTokenFile, common, remainingDescriptors, activeDescriptors, runtimeSecrets, runtimeAccess); err != nil {
		return err
	}
	if _, err := fmt.Printf("bootstrap_deploy_id=%s site=%s sha=%s\n", deployRunKey, opts.Site, opts.SHA); err != nil {
		return fmt.Errorf("write bootstrap deployment response: %w", err)
	}
	return nil
}

func submitBootstrapDescriptorWaves(ctx context.Context, target inventoryTarget, siteRootTokenFile string, common deployengine.Options, remainingPaths []string, activeDescriptors []bootstrapNomadDescriptor, secrets runtimeSecretCatalog, access runtimeAccessCatalog) error {
	pending := activeDescriptorByPath(activeDescriptors, remainingPaths)
	available := bootstrapAvailableRuntimeSecrets(secrets)
	for len(pending) > 0 {
		wave := []bootstrapNomadDescriptor{}
		next := []bootstrapNomadDescriptor{}
		blocked := map[string][]string{}
		for _, descriptor := range pending {
			missing := missingProducedRuntimeSecrets(descriptor.JobID, access, secrets, available)
			if len(missing) == 0 {
				wave = append(wave, descriptor)
				continue
			}
			blocked[descriptor.JobID] = missing
			next = append(next, descriptor)
		}
		if len(wave) == 0 {
			return bootstrapSecretWaveError(blocked)
		}
		paths := make([]string, 0, len(wave))
		produced := map[string]struct{}{}
		for _, descriptor := range wave {
			paths = append(paths, descriptor.Path)
			for _, name := range producedRuntimeSecretsForJob(secrets, descriptor.JobID) {
				produced[name] = struct{}{}
			}
		}
		options := common
		options.NomadComponentDescriptors = paths
		if _, err := deployengine.Submit(ctx, options); err != nil {
			return err
		}
		names := sortedSetValues(produced)
		if err := waitForOpenBaoRuntimeSecrets(ctx, target, siteRootTokenFile, names); err != nil {
			return err
		}
		for _, name := range names {
			available[name] = struct{}{}
		}
		pending = next
	}
	return nil
}

func activeDescriptorByPath(active []bootstrapNomadDescriptor, paths []string) []bootstrapNomadDescriptor {
	byPath := map[string]bootstrapNomadDescriptor{}
	for _, descriptor := range active {
		byPath[descriptor.Path] = descriptor
	}
	out := make([]bootstrapNomadDescriptor, 0, len(paths))
	for _, path := range paths {
		if descriptor, ok := byPath[path]; ok {
			out = append(out, descriptor)
		}
	}
	return out
}

func bootstrapAvailableRuntimeSecrets(secrets runtimeSecretCatalog) map[string]struct{} {
	available := map[string]struct{}{}
	for _, declaration := range secrets.Declarations {
		if strings.TrimSpace(declaration.ProducedByJob) == "" {
			available[declaration.Name] = struct{}{}
		}
	}
	return available
}

func missingProducedRuntimeSecrets(jobID string, access runtimeAccessCatalog, secrets runtimeSecretCatalog, available map[string]struct{}) []string {
	missing := map[string]struct{}{}
	for name := range access.JobReads[jobID] {
		declaration, ok := secrets.ByName[name]
		if !ok || strings.TrimSpace(declaration.ProducedByJob) == "" {
			continue
		}
		if _, ok := available[name]; !ok {
			missing[name] = struct{}{}
		}
	}
	return sortedSetValues(missing)
}

func producedRuntimeSecretsForJob(secrets runtimeSecretCatalog, jobID string) []string {
	produced := map[string]struct{}{}
	for _, declaration := range secrets.Declarations {
		if strings.TrimSpace(declaration.ProducedByJob) == jobID {
			produced[declaration.Name] = struct{}{}
		}
	}
	return sortedSetValues(produced)
}

func bootstrapSecretWaveError(blocked map[string][]string) error {
	jobs := make([]string, 0, len(blocked))
	for job := range blocked {
		jobs = append(jobs, job)
	}
	sort.Strings(jobs)
	parts := make([]string, 0, len(jobs))
	for _, job := range jobs {
		parts = append(parts, job+" waits for "+strings.Join(blocked[job], ", "))
	}
	return fmt.Errorf("bootstrap produced-secret graph is blocked: %s", strings.Join(parts, "; "))
}

func startTCPTunnel(ctx context.Context, target inventoryTarget, localPort int, remoteAddr string) *exec.Cmd {
	forward := "127.0.0.1:" + strconv.Itoa(localPort) + ":" + remoteAddr
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

func (p remoteBootstrapArtifactPublisher) PublishDeploymentArtifacts(ctx context.Context, req deployengine.ArtifactPublishRequest) (deployengine.ArtifactPublishResult, error) {
	root := strings.TrimRight(strings.TrimSpace(p.root), "/")
	if root == "" {
		root = bootstrapArtifactRoot
	}
	if strings.TrimSpace(req.Site) == "" {
		return deployengine.ArtifactPublishResult{}, errors.New("site is required")
	}
	if strings.TrimSpace(req.ArtifactNamespace) == "" {
		return deployengine.ArtifactPublishResult{}, errors.New("artifact namespace is required")
	}
	if err := ensureRemoteBootstrapArtifactServer(ctx, p.target); err != nil {
		return deployengine.ArtifactPublishResult{}, err
	}
	sources := map[string]string{}
	for _, artifact := range req.Artifacts {
		output := strings.TrimSpace(artifact.Output)
		if output == "" {
			return deployengine.ArtifactPublishResult{}, errors.New("artifact output is required")
		}
		if err := verifyArtifactCandidate(artifact); err != nil {
			return deployengine.ArtifactPublishResult{}, err
		}
		localPath, cleanup, err := localArtifactUploadPath(artifact)
		if err != nil {
			return deployengine.ArtifactPublishResult{}, err
		}
		if cleanup != nil {
			defer cleanup()
		}
		remoteTmp, err := remoteTempArtifactPath(output)
		if err != nil {
			return deployengine.ArtifactPublishResult{}, err
		}
		if err := copyLocalFileToRemote(ctx, p.target, localPath, remoteTmp); err != nil {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("%s: %w", output, err)
		}
		remoteFile := remoteArtifactFile(root, req.Site, req.ArtifactNamespace, artifact)
		if err := installRemoteArtifact(ctx, p.target, remoteTmp, remoteFile); err != nil {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("%s: %w", output, err)
		}
		sources[output] = remoteArtifactGetterSource(req.Site, req.ArtifactNamespace, artifact)
	}
	return deployengine.ArtifactPublishResult{GetterSources: sources}, nil
}

func verifyArtifactCandidate(artifact deployengine.ArtifactPublishCandidate) error {
	digest, err := artifactCandidateSHA256(artifact)
	if err != nil {
		return fmt.Errorf("%s: %w", artifact.Output, err)
	}
	if digest != strings.TrimSpace(artifact.SHA256) {
		return fmt.Errorf("%s: sha256=%s does not match expected %s", artifact.Output, digest, artifact.SHA256)
	}
	return nil
}

func artifactCandidateSHA256(artifact deployengine.ArtifactPublishCandidate) (string, error) {
	if artifact.Body != nil {
		sum := sha256.Sum256(artifact.Body)
		return hex.EncodeToString(sum[:]), nil
	}
	if strings.TrimSpace(artifact.LocalPath) == "" {
		return "", errors.New("artifact has no local path")
	}
	file, err := os.Open(artifact.LocalPath)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func localArtifactUploadPath(artifact deployengine.ArtifactPublishCandidate) (string, func(), error) {
	if artifact.Body == nil {
		info, err := os.Stat(artifact.LocalPath)
		if err != nil {
			return "", nil, fmt.Errorf("stat artifact: %w", err)
		}
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("artifact %s is not a regular file", artifact.LocalPath)
		}
		return artifact.LocalPath, nil, nil
	}
	file, err := os.CreateTemp("", "verself-bootstrap-artifact-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary artifact: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := file.Write(artifact.Body); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary artifact: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("chmod temporary artifact: %w", err)
	}
	return path, cleanup, nil
}

func remoteTempArtifactPath(output string) (string, error) {
	id, err := randomPrefixedID("artifact")
	if err != nil {
		return "", err
	}
	return bootstrapRemoteTmpPrefix + id + "-" + safeRemoteArtifactName(output) + ".tar", nil
}

func remoteArtifactFile(root, site, sha string, artifact deployengine.ArtifactPublishCandidate) string {
	return path.Join(root, remoteArtifactRelativePath(site, sha, artifact))
}

func remoteArtifactGetterSource(site, sha string, artifact deployengine.ArtifactPublishCandidate) string {
	base, err := url.Parse(bootstrapArtifactHTTP)
	if err != nil {
		panic(err)
	}
	base.Path = path.Join("/", remoteArtifactRelativePath(site, sha, artifact))
	return base.String()
}

func remoteArtifactRelativePath(site, sha string, artifact deployengine.ArtifactPublishCandidate) string {
	name := strings.TrimSpace(artifact.SHA256)
	if name == "" {
		name = "unknown"
	}
	return path.Join(safeRemoteArtifactName(site), safeRemoteArtifactName(sha), name+"-"+safeRemoteArtifactName(artifact.Output)+".tar")
}

func safeRemoteArtifactName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "artifact"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "." || out == ".." {
		return "_"
	}
	return out
}

func copyLocalFileToRemote(ctx context.Context, target inventoryTarget, localPath, remotePath string) error {
	cmd := scpCommand(ctx, target, localPath, remotePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy artifact over SSH: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func installRemoteArtifact(ctx context.Context, target inventoryTarget, remoteTmp, remoteFile string) error {
	command := "tmp=" + shellQuote(remoteTmp) + "; dest=" + shellQuote(remoteFile) + "; trap 'rm -f \"$tmp\"' EXIT; sudo -n install -D -o root -g root -m 0644 \"$tmp\" \"$dest\"; sudo -n test -s \"$dest\""
	cmd := sshCommand(ctx, target, command)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("install remote artifact: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ensureRemoteBootstrapArtifactServer(ctx context.Context, target inventoryTarget) error {
	cmd := sshCommand(ctx, target, "sudo -n systemctl is-active --quiet verself-bootstrap-artifact-server.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bootstrap artifact server is not active: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func scpCommand(ctx context.Context, target inventoryTarget, localPath, remotePath string) *exec.Cmd {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
	}
	if target.Port != 0 {
		args = append(args, "-P", strconv.Itoa(target.Port))
	}
	args = append(args, localPath, scpTarget(target, remotePath))
	cmd := exec.CommandContext(ctx, "scp", args...)
	return cmd
}

func scpTarget(target inventoryTarget, remotePath string) string {
	host := target.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return target.User + "@" + host + ":" + remotePath
}

func remoteTaskUserResolver(target inventoryTarget) deployengine.TaskUserResolver {
	return func(ctx context.Context, name string) (deployengine.TaskUserIdentity, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return deployengine.TaskUserIdentity{}, errors.New("task user is required")
		}
		cmd := sshCommand(ctx, target, "getent passwd "+shellQuote(name))
		output, err := cmd.CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(output))
			if detail != "" {
				detail = ": " + detail
			}
			return deployengine.TaskUserIdentity{}, fmt.Errorf("lookup remote task user %q%s: %w", name, detail, err)
		}
		return parseRemotePasswdEntry(name, string(output))
	}
}

func parseRemotePasswdEntry(name, output string) (deployengine.TaskUserIdentity, error) {
	line := strings.TrimSpace(output)
	if line == "" {
		return deployengine.TaskUserIdentity{}, fmt.Errorf("remote task user %q was not found", name)
	}
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	fields := strings.Split(line, ":")
	if len(fields) < 4 {
		return deployengine.TaskUserIdentity{}, fmt.Errorf("remote passwd entry for task user %q is malformed", name)
	}
	uid, err := parseRemoteUserID(fields[2], "uid", name)
	if err != nil {
		return deployengine.TaskUserIdentity{}, err
	}
	gid, err := parseRemoteUserID(fields[3], "gid", name)
	if err != nil {
		return deployengine.TaskUserIdentity{}, err
	}
	return deployengine.TaskUserIdentity{UID: uid, GID: gid}, nil
}

func parseRemoteUserID(raw, field, name string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parse remote %s for task user %q: %w", field, name, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("remote %s for task user %q must be non-negative", field, name)
	}
	return value, nil
}

func normalizeBootstrapDeployOptions(opts BootstrapDeployOptions) BootstrapDeployOptions {
	opts.Site = strings.TrimSpace(opts.Site)
	opts.SHA = strings.TrimSpace(opts.SHA)
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)
	opts.InventoryPath = strings.TrimSpace(opts.InventoryPath)
	opts.OpenBaoSiteRootTokenFile = strings.TrimSpace(opts.OpenBaoSiteRootTokenFile)
	opts.SSHTransport = strings.TrimSpace(opts.SSHTransport)
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Minute
	}
	if opts.SSHTransport == "" {
		opts.SSHTransport = "recovery"
	}
	return opts
}

func startNomadTunnel(ctx context.Context, target inventoryTarget, localPort int) *exec.Cmd {
	return startTCPTunnel(ctx, target, localPort, defaultNomadRemoteAddr)
}

func validateLocalOpenBaoSiteRootTokenFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("openbao site root token file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect openbao site root token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("openbao site root token file %s must be a regular file", path)
	}
	if info.Size() == 0 {
		return fmt.Errorf("openbao site root token file %s is empty", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("openbao site root token file %s must be readable only by the operator", path)
	}
	return nil
}

func stageRemoteOpenBaoSiteRootToken(ctx context.Context, target inventoryTarget, localPath string) error {
	remoteTmp, err := remoteTempSecretPath("openbao-site-root")
	if err != nil {
		return err
	}
	if err := copyLocalFileToRemote(ctx, target, localPath, remoteTmp); err != nil {
		return fmt.Errorf("stage openbao site root token: %w", err)
	}
	if err := installRemoteOpenBaoSiteRootToken(ctx, target, remoteTmp, openBaoSiteRootTokenPath); err != nil {
		return err
	}
	return nil
}

func remoteTempSecretPath(name string) (string, error) {
	id, err := randomPrefixedID("secret")
	if err != nil {
		return "", err
	}
	return "/tmp/verself-bootstrap-" + id + "-" + safeRemoteArtifactName(name), nil
}

func installRemoteOpenBaoSiteRootToken(ctx context.Context, target inventoryTarget, remoteTmp, remoteFile string) error {
	command := openBaoSiteRootTokenInstallCommand(remoteTmp, remoteFile)
	cmd := sshCommand(ctx, target, command)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("install remote openbao site root token: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func openBaoSiteRootTokenInstallCommand(remoteTmp, remoteFile string) string {
	return "tmp=" + shellQuote(remoteTmp) + "; dest=" + shellQuote(remoteFile) + "; dir=$(dirname \"$dest\"); trap 'rm -f \"$tmp\"' EXIT; sudo -n install -d -o root -g root -m 0700 \"$dir\"; sudo -n install -o root -g root -m 0600 \"$tmp\" \"$dest\"; sudo -n test -s \"$dest\""
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

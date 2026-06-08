package sitebootstrap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/verself/deployment-service/deployengine"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	defaultNomadRemoteAddr   = "127.0.0.1:4646"
	bootstrapArtifactRoot    = "/var/lib/verself/bootstrap/artifacts"
	openBaoRootKeyPath       = "/etc/verself/bootstrap/openbao-root.key"
	bootstrapRemoteTmpPrefix = "/tmp/verself-bootstrap-artifacts-"
)

type BootstrapDeployOptions struct {
	Site          string
	SHA           string
	RepoRoot      string
	InventoryPath string
	SSHTransport  string
	Timeout       time.Duration
}

type remoteBootstrapArtifactPublisher struct {
	ssh  *opruntime.SSHClient
	root string
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
	target, err := loadBootstrapInventoryTarget(opts.InventoryPath, opts.SSHTransport)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	sshClient, err := opruntime.DialSSH(ctx, opruntime.SSHOptions{
		User:           target.User,
		Host:           target.Host,
		PortCandidates: target.SSHPorts(),
	})
	if err != nil {
		return fmt.Errorf("bootstrap SSH connect: %w", err)
	}
	defer func() { err = errors.Join(err, sshClient.Close()) }()
	if err := checkRemoteOpenBaoRootKey(ctx, sshClient); err != nil {
		return err
	}

	nomadForward, err := sshClient.Forward(ctx, "nomad", defaultNomadRemoteAddr)
	if err != nil {
		return fmt.Errorf("start Nomad recovery tunnel: %w", err)
	}
	defer func() { err = errors.Join(err, nomadForward.Close()) }()
	nomadAddr := "http://" + nomadForward.ListenAddr
	if err := waitHTTP(ctx, nomadAddr+"/v1/status/leader", http.StatusOK, nil); err != nil {
		return fmt.Errorf("nomad tunnel readiness: %w", err)
	}

	deployRunKey, err := randomPrefixedID("bootstrap")
	if err != nil {
		return err
	}
	_, err = deployengine.Run(ctx, deployengine.Options{
		Site:              opts.Site,
		SHA:               opts.SHA,
		DeployRunKey:      deployRunKey,
		RepoRoot:          opts.RepoRoot,
		ArtifactPublisher: remoteBootstrapArtifactPublisher{ssh: sshClient, root: bootstrapArtifactRoot},
		NomadAddr:         nomadAddr,
		TaskUserResolver:  remoteTaskUserResolver(sshClient),
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Printf("bootstrap_deploy_id=%s site=%s sha=%s\n", deployRunKey, opts.Site, opts.SHA); err != nil {
		return fmt.Errorf("write bootstrap deployment response: %w", err)
	}
	return nil
}

func (p remoteBootstrapArtifactPublisher) PublishDeploymentArtifacts(ctx context.Context, req deployengine.ArtifactPublishRequest) (deployengine.ArtifactPublishResult, error) {
	root := strings.TrimRight(strings.TrimSpace(p.root), "/")
	if root == "" {
		root = bootstrapArtifactRoot
	}
	if strings.TrimSpace(req.Site) == "" {
		return deployengine.ArtifactPublishResult{}, errors.New("site is required")
	}
	if strings.TrimSpace(req.SHA) == "" {
		return deployengine.ArtifactPublishResult{}, errors.New("sha is required")
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
		if err := copyLocalFileToRemote(ctx, p.ssh, localPath, remoteTmp); err != nil {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("%s: %w", output, err)
		}
		remoteFile := remoteArtifactFile(root, req.Site, req.SHA, artifact)
		if err := installRemoteArtifact(ctx, p.ssh, remoteTmp, remoteFile); err != nil {
			return deployengine.ArtifactPublishResult{}, fmt.Errorf("%s: %w", output, err)
		}
		sources[output] = (&url.URL{Scheme: "file", Path: remoteFile}).String()
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
	name := strings.TrimSpace(artifact.SHA256)
	if name == "" {
		name = "unknown"
	}
	return path.Join(root, safeRemoteArtifactName(site), safeRemoteArtifactName(sha), name+"-"+safeRemoteArtifactName(artifact.Output)+".tar")
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

func copyLocalFileToRemote(ctx context.Context, sshClient *opruntime.SSHClient, localPath, remotePath string) error {
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open artifact %s: %w", localPath, err)
	}
	defer func() { _ = local.Close() }()
	remoteWord, err := opruntime.ShellWord(remotePath)
	if err != nil {
		return err
	}
	if err := sshClient.Run(ctx, "sudo -n /usr/bin/tee "+remoteWord+" >/dev/null", local, io.Discard, os.Stderr); err != nil {
		return fmt.Errorf("copy artifact over SSH: %w", err)
	}
	return nil
}

func installRemoteArtifact(ctx context.Context, sshClient *opruntime.SSHClient, remoteTmp, remoteFile string) error {
	command := "tmp=" + shellQuote(remoteTmp) + "; dest=" + shellQuote(remoteFile) + "; trap 'rm -f \"$tmp\"' EXIT; sudo -n install -D -o root -g root -m 0644 \"$tmp\" \"$dest\"; sudo -n test -s \"$dest\""
	if _, err := sshClient.Exec(ctx, command); err != nil {
		return fmt.Errorf("install remote artifact: %w", err)
	}
	return nil
}

func remoteTaskUserResolver(sshClient *opruntime.SSHClient) deployengine.TaskUserResolver {
	return func(ctx context.Context, name string) (deployengine.TaskUserIdentity, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return deployengine.TaskUserIdentity{}, errors.New("task user is required")
		}
		output, err := sshClient.Exec(ctx, "getent passwd "+shellQuote(name))
		if err != nil {
			return deployengine.TaskUserIdentity{}, fmt.Errorf("lookup remote task user %q: %w", name, err)
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
	opts.SSHTransport = strings.TrimSpace(opts.SSHTransport)
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Minute
	}
	if opts.SSHTransport == "" {
		opts.SSHTransport = "recovery"
	}
	return opts
}

func checkRemoteOpenBaoRootKey(ctx context.Context, sshClient *opruntime.SSHClient) error {
	if _, err := sshClient.Exec(ctx, openBaoRootKeyPreflightCommand()); err != nil {
		return fmt.Errorf("bootstrap deploy requires the OpenBao site root key on the host at %s; rerun host convergence with the site root key first: %w", openBaoRootKeyPath, err)
	}
	return nil
}

func openBaoRootKeyPreflightCommand() string {
	path := shellQuote(openBaoRootKeyPath)
	return "sudo -n test -s " + path + " && test \"$(sudo -n stat -c '%a' " + path + ")\" = 600"
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
			if processDone != nil {
				if ok && err != nil {
					return fmt.Errorf("child process exited before readiness: %w", err)
				}
				return fmt.Errorf("child process exited before readiness")
			}
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w: %v", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func randomPrefixedID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate bootstrap id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func loadBootstrapInventoryTarget(path, transport string) (opruntime.InventoryTarget, error) {
	target, err := opruntime.LoadInfraTarget(path)
	if err != nil {
		return opruntime.InventoryTarget{}, err
	}
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "recovery", "wireguard", "wg":
		recovery, ok := target.Recovery()
		if !ok {
			return opruntime.InventoryTarget{}, fmt.Errorf("inventory %s has no verself_recovery_ssh_host for recovery bootstrap deploy", path)
		}
		if recovery.User == "" {
			recovery.User = target.User
		}
		return recovery, nil
	case "inventory", "pomerium":
		return target, nil
	default:
		return opruntime.InventoryTarget{}, fmt.Errorf("ssh transport must be recovery or inventory, got %q", transport)
	}
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultSecretsRuntimeRoot    = "/var/lib/secrets-service/runtime"
	defaultSecretsProjectedGraph = "/run/verself/recovery/secrets-service/document.json"
	secretsServiceUser           = "secrets_service"
	secretsServiceUID            = 975
	secretsServiceGID            = 968
	secretsSPIREWorkloadGroup    = "spire_workload"
	secretsServiceStateRoot      = "/var/lib/secrets-service"
	secretsServiceHome           = "/var/lib/secrets-service/home"
	secretsBoardedBinaryRelative = "bazel-bin/src/services/secrets-service/cmd/secrets-service/secrets-service_/secrets-service"
)

type recoverOptions struct {
	RepoRoot       string
	ResourceGraph  string
	ResourceName   string
	RuntimeRoot    string
	ProjectedGraph string
}

type localIdentity struct {
	uid int
	gid int
}

func runRecoveryCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "recover" {
		return false, nil
	}
	opts, err := parseRecoverOptions(args[1:])
	if err != nil {
		return true, err
	}
	return true, recoverSecretsService(ctx, opts)
}

func parseRecoverOptions(args []string) (recoverOptions, error) {
	opts := recoverOptions{
		ResourceGraph:  defaultGuardianResourceGraph,
		ResourceName:   defaultSecretsServiceResource,
		RuntimeRoot:    defaultSecretsRuntimeRoot,
		ProjectedGraph: defaultSecretsProjectedGraph,
	}
	fs := flag.NewFlagSet("secrets-service recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.RepoRoot, "repo-root", opts.RepoRoot, "boarded Guardian repo root")
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "SecretsService resource name")
	fs.StringVar(&opts.RuntimeRoot, "runtime-root", opts.RuntimeRoot, "secrets-service runtime install root")
	fs.StringVar(&opts.ProjectedGraph, "projected-graph", opts.ProjectedGraph, "runtime-readable Guardian resource graph projection")
	if err := fs.Parse(args); err != nil {
		return recoverOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return recoverOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	required := map[string]string{
		"repo-root":       opts.RepoRoot,
		"resource-graph":  opts.ResourceGraph,
		"resource-name":   opts.ResourceName,
		"runtime-root":    opts.RuntimeRoot,
		"projected-graph": opts.ProjectedGraph,
	}
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			return recoverOptions{}, fmt.Errorf("--%s is required", label)
		}
	}
	if !filepath.IsAbs(opts.RepoRoot) || !filepath.IsAbs(opts.ResourceGraph) || !filepath.IsAbs(opts.RuntimeRoot) || !filepath.IsAbs(opts.ProjectedGraph) {
		return recoverOptions{}, errors.New("--repo-root, --resource-graph, --runtime-root, and --projected-graph must be absolute")
	}
	return opts, nil
}

func recoverSecretsService(ctx context.Context, opts recoverOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("secrets-service recover must run as root")
	}
	service, err := ensureLocalIdentity(secretsServiceUser)
	if err != nil {
		return err
	}
	sourceBinary := filepath.Join(opts.RepoRoot, secretsBoardedBinaryRelative)
	if err := installRuntime(opts.RuntimeRoot, sourceBinary, service.gid); err != nil {
		return err
	}
	if err := projectResourceGraph(opts.ResourceGraph, opts.ProjectedGraph, service.gid); err != nil {
		return err
	}
	if _, err := loadSecretsRuntimeConfig(opts.ProjectedGraph, opts.ResourceName); err != nil {
		return err
	}
	return ensureStateDirs(service)
}

func ensureLocalIdentity(name string) (localIdentity, error) {
	if err := ensureLocalGroup(name, secretsServiceGID); err != nil {
		return localIdentity{}, err
	}
	if _, err := user.LookupGroup(secretsSPIREWorkloadGroup); err != nil {
		return localIdentity{}, fmt.Errorf("lookup %s group: %w", secretsSPIREWorkloadGroup, err)
	}
	if _, err := user.Lookup(name); err == nil {
		if err := reconcileLocalUser(name, secretsServiceUID, secretsServiceGID); err != nil {
			return localIdentity{}, err
		}
	} else if isUnknownUser(err) {
		if err := runRootCommand(
			"/usr/sbin/useradd",
			"--system",
			"--uid", strconv.Itoa(secretsServiceUID),
			"--gid", name,
			"--groups", secretsSPIREWorkloadGroup,
			"--home-dir", secretsServiceHome,
			"--shell", "/usr/sbin/nologin",
			"--no-create-home",
			name,
		); err != nil {
			return localIdentity{}, err
		}
	} else {
		return localIdentity{}, err
	}
	if err := ensureSupplementaryGroup(name, secretsSPIREWorkloadGroup); err != nil {
		return localIdentity{}, err
	}
	identity, err := lookupLocalIdentity(name)
	if err != nil {
		return localIdentity{}, err
	}
	if identity.uid != secretsServiceUID || identity.gid != secretsServiceGID {
		return localIdentity{}, fmt.Errorf("%s has uid:gid %d:%d, want %d:%d", name, identity.uid, identity.gid, secretsServiceUID, secretsServiceGID)
	}
	return identity, nil
}

func ensureLocalGroup(name string, gid int) error {
	group, err := user.LookupGroup(name)
	if err != nil {
		if !isUnknownGroup(err) {
			return fmt.Errorf("lookup %s group: %w", name, err)
		}
		if err := runRootCommand("/usr/sbin/groupadd", "--system", "--gid", strconv.Itoa(gid), name); err != nil {
			return err
		}
		group, err = user.LookupGroup(name)
		if err != nil {
			return fmt.Errorf("lookup created %s group: %w", name, err)
		}
	}
	observed, err := strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("parse %s gid: %w", name, err)
	}
	if observed == gid {
		return nil
	}
	if err := runRootCommand("/usr/sbin/groupmod", "--gid", strconv.Itoa(gid), name); err != nil {
		return err
	}
	group, err = user.LookupGroup(name)
	if err != nil {
		return fmt.Errorf("lookup repaired %s group: %w", name, err)
	}
	observed, err = strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("parse repaired %s gid: %w", name, err)
	}
	if observed != gid {
		return fmt.Errorf("%s group has gid %d, want %d", name, observed, gid)
	}
	return nil
}

func reconcileLocalUser(name string, uid int, gid int) error {
	identity, err := lookupLocalIdentity(name)
	if err != nil {
		return err
	}
	if identity.uid != uid {
		if err := runRootCommand("/usr/sbin/usermod", "--uid", strconv.Itoa(uid), name); err != nil {
			return err
		}
	}
	if identity.gid != gid {
		if err := runRootCommand("/usr/sbin/usermod", "--gid", name, name); err != nil {
			return err
		}
	}
	if err := runRootCommand("/usr/sbin/usermod", "--home", secretsServiceHome, "--shell", "/usr/sbin/nologin", name); err != nil {
		return err
	}
	return nil
}

func ensureSupplementaryGroup(name string, group string) error {
	if strings.TrimSpace(group) == "" {
		return nil
	}
	u, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("lookup %s user: %w", name, err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return fmt.Errorf("lookup %s group: %w", group, err)
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("lookup %s supplementary groups: %w", name, err)
	}
	for _, id := range groupIDs {
		if id == g.Gid {
			return nil
		}
	}
	return runRootCommand("/usr/sbin/usermod", "--append", "--groups", group, name)
}

func lookupLocalIdentity(name string) (localIdentity, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return localIdentity{}, fmt.Errorf("lookup %s user: %w", name, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return localIdentity{}, fmt.Errorf("parse %s uid: %w", name, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return localIdentity{}, fmt.Errorf("parse %s gid: %w", name, err)
	}
	return localIdentity{uid: uid, gid: gid}, nil
}

func isUnknownUser(err error) bool {
	var unknown user.UnknownUserError
	return errors.As(err, &unknown)
}

func isUnknownGroup(err error) bool {
	var unknown user.UnknownGroupError
	return errors.As(err, &unknown)
}

func runRootCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureStateDirs(service localIdentity) error {
	if err := ensureOwnedDir(secretsServiceStateRoot, 0, 0, 0o755); err != nil {
		return err
	}
	dirs := []string{
		secretsServiceHome,
		filepath.Join(secretsServiceHome, "tmp"),
	}
	for _, dir := range dirs {
		if err := ensureOwnedDir(dir, service.uid, service.gid, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func installRuntime(runtimeRoot string, sourceBinary string, serviceGID int) error {
	digest, err := fileSHA256(sourceBinary)
	if err != nil {
		return fmt.Errorf("digest secrets-service binary: %w", err)
	}
	releaseDir := filepath.Join(runtimeRoot, "releases", "sha256-"+digest)
	binDir := filepath.Join(releaseDir, "bin")
	targetBinary := filepath.Join(binDir, "secrets-service")
	if err := ensureOwnedDir(runtimeRoot, 0, 0, 0o755); err != nil {
		return err
	}
	if err := ensureOwnedDir(filepath.Join(runtimeRoot, "releases"), 0, 0, 0o755); err != nil {
		return err
	}
	if err := ensureOwnedDir(filepath.Join(runtimeRoot, "tmp"), 0, 0, 0o700); err != nil {
		return err
	}
	if err := ensureOwnedDir(releaseDir, 0, serviceGID, 0o750); err != nil {
		return err
	}
	if err := ensureOwnedDir(binDir, 0, serviceGID, 0o750); err != nil {
		return err
	}
	if ok, err := fileDigestMatches(targetBinary, digest); err != nil {
		return err
	} else if !ok {
		tmp, err := os.CreateTemp(filepath.Join(runtimeRoot, "tmp"), "secrets-service-*")
		if err != nil {
			return fmt.Errorf("create secrets-service runtime temp: %w", err)
		}
		tmpName := tmp.Name()
		cleanup := true
		defer func() {
			if cleanup {
				_ = os.Remove(tmpName)
			}
		}()
		source, err := os.Open(sourceBinary)
		if err != nil {
			_ = tmp.Close()
			return fmt.Errorf("open secrets-service source binary: %w", err)
		}
		if _, err := io.Copy(tmp, source); err != nil {
			_ = source.Close()
			_ = tmp.Close()
			return fmt.Errorf("copy secrets-service runtime binary: %w", err)
		}
		if err := source.Close(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("close secrets-service source binary: %w", err)
		}
		if err := tmp.Chmod(0o550); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chmod secrets-service runtime binary: %w", err)
		}
		if err := tmp.Chown(0, serviceGID); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chown secrets-service runtime binary: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close secrets-service runtime temp: %w", err)
		}
		if err := os.Rename(tmpName, targetBinary); err != nil {
			return fmt.Errorf("promote secrets-service runtime binary: %w", err)
		}
		cleanup = false
	}
	tmpLink := filepath.Join(runtimeRoot, fmt.Sprintf("current.tmp-%d", os.Getpid()))
	if err := os.Remove(tmpLink); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale secrets-service runtime link: %w", err)
	}
	if err := os.Symlink(releaseDir, tmpLink); err != nil {
		return fmt.Errorf("create secrets-service runtime link: %w", err)
	}
	if err := os.Rename(tmpLink, filepath.Join(runtimeRoot, "current")); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("promote secrets-service runtime link: %w", err)
	}
	return nil
}

func projectResourceGraph(sourcePath string, targetPath string, serviceGID int) error {
	if err := ensureOwnedDir("/run/verself", 0, 0, 0o711); err != nil {
		return err
	}
	if err := ensureOwnedDir("/run/verself/recovery", 0, 0, 0o711); err != nil {
		return err
	}
	targetDir := filepath.Dir(targetPath)
	if err := ensureOwnedDir(targetDir, 0, serviceGID, 0o750); err != nil {
		return err
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read secrets-service resource graph: %w", err)
	}
	tmp, err := os.CreateTemp(targetDir, ".document-*.json")
	if err != nil {
		return fmt.Errorf("create secrets-service resource graph temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write secrets-service resource graph temp: %w", err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod secrets-service resource graph: %w", err)
	}
	if err := tmp.Chown(0, serviceGID); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown secrets-service resource graph: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close secrets-service resource graph temp: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("promote secrets-service resource graph: %w", err)
	}
	cleanup = false
	return nil
}

func ensureOwnedDir(path string, uid int, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileDigestMatches(path string, digest string) (bool, error) {
	observed, err := fileSHA256(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("digest existing %s: %w", path, err)
	}
	return observed == digest, nil
}

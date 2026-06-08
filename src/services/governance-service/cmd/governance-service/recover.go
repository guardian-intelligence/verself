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
	"syscall"

	"github.com/verself/governance-service/migrations"
)

const (
	defaultGovernanceRuntimeRoot    = "/var/lib/governance-service/runtime"
	defaultGovernanceProjectedGraph = "/run/verself/recovery/governance-service/document.json"
	governanceServiceUser           = "governance_service"
	governanceServiceUID            = 984
	governanceServiceGID            = 977
	governanceSPIREWorkloadGroup    = "spire_workload"
	governanceServiceStateRoot      = "/var/lib/governance-service"
	governanceServiceHome           = "/var/lib/governance-service/home"
	governanceBoardedBinaryRelative = "bazel-bin/src/services/governance-service/cmd/governance-service/governance-service_/governance-service"
)

type recoverOptions struct {
	RepoRoot       string
	ResourceGraph  string
	ResourceName   string
	RuntimeRoot    string
	ProjectedGraph string
	Migrate        bool
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
	return true, recoverGovernanceService(ctx, opts)
}

func parseRecoverOptions(args []string) (recoverOptions, error) {
	opts := recoverOptions{
		ResourceGraph:  defaultGuardianResourceGraph,
		ResourceName:   defaultGovernanceServiceResource,
		RuntimeRoot:    defaultGovernanceRuntimeRoot,
		ProjectedGraph: defaultGovernanceProjectedGraph,
	}
	fs := flag.NewFlagSet("governance-service recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.RepoRoot, "repo-root", opts.RepoRoot, "boarded Guardian repo root")
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "GovernanceService resource name")
	fs.StringVar(&opts.RuntimeRoot, "runtime-root", opts.RuntimeRoot, "governance-service runtime install root")
	fs.StringVar(&opts.ProjectedGraph, "projected-graph", opts.ProjectedGraph, "runtime-readable Guardian resource graph projection")
	fs.BoolVar(&opts.Migrate, "migrate", opts.Migrate, "run governance-service database migrations after runtime installation")
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

func recoverGovernanceService(ctx context.Context, opts recoverOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("governance-service recover must run as root")
	}
	service, err := ensureLocalIdentity(governanceServiceUser)
	if err != nil {
		return err
	}
	sourceBinary := filepath.Join(opts.RepoRoot, governanceBoardedBinaryRelative)
	if err := installRuntime(opts.RuntimeRoot, sourceBinary, "governance-service", service.gid); err != nil {
		return err
	}
	if err := projectResourceGraph(opts.ResourceGraph, opts.ProjectedGraph, "governance-service", service.gid); err != nil {
		return err
	}
	runtimeCfg, err := loadGovernanceRuntimeConfig(opts.ProjectedGraph, opts.ResourceName)
	if err != nil {
		return err
	}
	if err := ensureStateDirs(service, runtimeCfg.ExportDir); err != nil {
		return err
	}
	if !opts.Migrate {
		return nil
	}
	if err := dropToIdentity(service); err != nil {
		return err
	}
	return migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
}

func ensureLocalIdentity(name string) (localIdentity, error) {
	if err := ensureLocalGroup(name, governanceServiceGID); err != nil {
		return localIdentity{}, err
	}
	if _, err := user.LookupGroup(governanceSPIREWorkloadGroup); err != nil {
		return localIdentity{}, fmt.Errorf("lookup %s group: %w", governanceSPIREWorkloadGroup, err)
	}
	if _, err := user.Lookup(name); err == nil {
		if err := reconcileLocalUser(name, governanceServiceUID, governanceServiceGID); err != nil {
			return localIdentity{}, err
		}
	} else if isUnknownUser(err) {
		if err := runRootCommand(
			"/usr/sbin/useradd",
			"--system",
			"--uid", strconv.Itoa(governanceServiceUID),
			"--gid", name,
			"--groups", governanceSPIREWorkloadGroup,
			"--home-dir", governanceServiceHome,
			"--shell", "/usr/sbin/nologin",
			"--no-create-home",
			name,
		); err != nil {
			return localIdentity{}, err
		}
	} else {
		return localIdentity{}, err
	}
	if err := ensureSupplementaryGroup(name, governanceSPIREWorkloadGroup); err != nil {
		return localIdentity{}, err
	}
	identity, err := lookupLocalIdentity(name)
	if err != nil {
		return localIdentity{}, err
	}
	if identity.uid != governanceServiceUID || identity.gid != governanceServiceGID {
		return localIdentity{}, fmt.Errorf("%s has uid:gid %d:%d, want %d:%d", name, identity.uid, identity.gid, governanceServiceUID, governanceServiceGID)
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
	if err := runRootCommand("/usr/sbin/usermod", "--home", governanceServiceHome, "--shell", "/usr/sbin/nologin", name); err != nil {
		return err
	}
	return nil
}

func ensureSupplementaryGroup(name string, group string) error {
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

func ensureStateDirs(service localIdentity, exportDir string) error {
	if err := ensureOwnedDir(governanceServiceStateRoot, 0, 0, 0o755); err != nil {
		return err
	}
	dirs := []string{
		governanceServiceHome,
		filepath.Join(governanceServiceHome, "tmp"),
		exportDir,
	}
	for _, dir := range dirs {
		if err := ensureOwnedDir(dir, service.uid, service.gid, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func installRuntime(runtimeRoot string, sourceBinary string, label string, serviceGID int) error {
	digest, err := fileSHA256(sourceBinary)
	if err != nil {
		return fmt.Errorf("digest %s binary: %w", label, err)
	}
	releaseDir := filepath.Join(runtimeRoot, "releases", "sha256-"+digest)
	binDir := filepath.Join(releaseDir, "bin")
	targetBinary := filepath.Join(binDir, label)
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
		tmp, err := os.CreateTemp(filepath.Join(runtimeRoot, "tmp"), label+"-*")
		if err != nil {
			return fmt.Errorf("create %s runtime temp: %w", label, err)
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
			return fmt.Errorf("open %s source binary: %w", label, err)
		}
		if _, err := io.Copy(tmp, source); err != nil {
			_ = source.Close()
			_ = tmp.Close()
			return fmt.Errorf("copy %s runtime binary: %w", label, err)
		}
		if err := source.Close(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("close %s source binary: %w", label, err)
		}
		if err := tmp.Chmod(0o550); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chmod %s runtime binary: %w", label, err)
		}
		if err := tmp.Chown(0, serviceGID); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chown %s runtime binary: %w", label, err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close %s runtime temp: %w", label, err)
		}
		if err := os.Rename(tmpName, targetBinary); err != nil {
			return fmt.Errorf("promote %s runtime binary: %w", label, err)
		}
		cleanup = false
	}
	tmpLink := filepath.Join(runtimeRoot, fmt.Sprintf("current.tmp-%d", os.Getpid()))
	if err := os.Remove(tmpLink); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale %s runtime link: %w", label, err)
	}
	if err := os.Symlink(releaseDir, tmpLink); err != nil {
		return fmt.Errorf("create %s runtime link: %w", label, err)
	}
	if err := os.Rename(tmpLink, filepath.Join(runtimeRoot, "current")); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("promote %s runtime link: %w", label, err)
	}
	return nil
}

func projectResourceGraph(sourcePath string, targetPath string, label string, serviceGID int) error {
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
		return fmt.Errorf("read %s resource graph: %w", label, err)
	}
	tmp, err := os.CreateTemp(targetDir, ".document-*.json")
	if err != nil {
		return fmt.Errorf("create %s resource graph temp: %w", label, err)
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
		return fmt.Errorf("write %s resource graph temp: %w", label, err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s resource graph: %w", label, err)
	}
	if err := tmp.Chown(0, serviceGID); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown %s resource graph: %w", label, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s resource graph temp: %w", label, err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("promote %s resource graph: %w", label, err)
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
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
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

func dropToIdentity(identity localIdentity) error {
	if err := syscall.Setgroups([]int{identity.gid}); err != nil {
		return fmt.Errorf("drop supplementary groups: %w", err)
	}
	if err := syscall.Setgid(identity.gid); err != nil {
		return fmt.Errorf("drop gid: %w", err)
	}
	if err := syscall.Setuid(identity.uid); err != nil {
		return fmt.Errorf("drop uid: %w", err)
	}
	return nil
}

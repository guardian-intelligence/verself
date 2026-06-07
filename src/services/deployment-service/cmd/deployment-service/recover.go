package main

import (
	"archive/tar"
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

	"github.com/verself/deployment-service/migrations"
)

const (
	defaultDeploymentRuntimeRoot    = "/var/lib/deployment-service/runtime"
	defaultDeploymentProjectedGraph = "/run/verself/recovery/deployment-service/document.json"
	deploymentServiceUser           = "deployment_service"
	deploymentSPIREWorkloadGroup    = "spire_workload"
	deploymentServiceStateRoot      = "/var/lib/deployment-service"
	deploymentServiceHome           = "/var/lib/deployment-service/home"
	deploymentBoardedBinaryRelative = "bazel-bin/src/services/deployment-service/cmd/deployment-service/deployment-service_/deployment-service"
	guardianBoardedBinaryRelative   = "bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian"
	deploymentRuntimeToolsRelative  = "bazel-bin/src/services/deployment-service/deployment-service-runtime-tools.tar"
)

type deploymentRecoverOptions struct {
	RepoRoot       string
	ResourceGraph  string
	ResourceName   string
	RuntimeRoot    string
	ProjectedGraph string
	Migrate        bool
}

type deploymentLocalIdentity struct {
	uid int
	gid int
}

type deploymentRuntimeBinary struct {
	name   string
	label  string
	source string
}

type deploymentRuntimeArchive struct {
	label  string
	source string
}

func runDeploymentRecoveryCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "recover" {
		return false, nil
	}
	opts, err := parseDeploymentRecoverOptions(args[1:])
	if err != nil {
		return true, err
	}
	return true, recoverDeploymentService(ctx, opts)
}

func parseDeploymentRecoverOptions(args []string) (deploymentRecoverOptions, error) {
	opts := deploymentRecoverOptions{
		ResourceGraph:  defaultGuardianResourceGraph,
		ResourceName:   defaultDeploymentServiceResource,
		RuntimeRoot:    defaultDeploymentRuntimeRoot,
		ProjectedGraph: defaultDeploymentProjectedGraph,
	}
	fs := flag.NewFlagSet("deployment-service recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.RepoRoot, "repo-root", opts.RepoRoot, "boarded Guardian repo root")
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "DeploymentService resource name")
	fs.StringVar(&opts.RuntimeRoot, "runtime-root", opts.RuntimeRoot, "DeploymentService runtime install root")
	fs.StringVar(&opts.ProjectedGraph, "projected-graph", opts.ProjectedGraph, "runtime-readable Guardian resource graph projection")
	fs.BoolVar(&opts.Migrate, "migrate", opts.Migrate, "run deployment-service database migrations after runtime installation")
	if err := fs.Parse(args); err != nil {
		return deploymentRecoverOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return deploymentRecoverOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
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
			return deploymentRecoverOptions{}, fmt.Errorf("--%s is required", label)
		}
	}
	if !filepath.IsAbs(opts.RepoRoot) || !filepath.IsAbs(opts.ResourceGraph) || !filepath.IsAbs(opts.RuntimeRoot) || !filepath.IsAbs(opts.ProjectedGraph) {
		return deploymentRecoverOptions{}, errors.New("--repo-root, --resource-graph, --runtime-root, and --projected-graph must be absolute")
	}
	return opts, nil
}

func recoverDeploymentService(ctx context.Context, opts deploymentRecoverOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("deployment-service recover must run as root")
	}
	service, err := ensureDeploymentLocalIdentity(deploymentServiceUser)
	if err != nil {
		return err
	}
	binaries := []deploymentRuntimeBinary{
		{
			name:   "deployment-service",
			label:  "deployment-service",
			source: filepath.Join(opts.RepoRoot, deploymentBoardedBinaryRelative),
		},
		{
			name:   "guardian",
			label:  "guardian",
			source: filepath.Join(opts.RepoRoot, guardianBoardedBinaryRelative),
		},
	}
	archives := []deploymentRuntimeArchive{
		{
			label:  "deployment-service runtime tools",
			source: filepath.Join(opts.RepoRoot, deploymentRuntimeToolsRelative),
		},
	}
	if err := installDeploymentRuntime(opts.RuntimeRoot, binaries, archives, service.gid); err != nil {
		return err
	}
	if err := projectDeploymentResourceGraph(opts.ResourceGraph, opts.ProjectedGraph, service.gid); err != nil {
		return err
	}
	runtimeCfg, err := loadDeploymentRuntimeConfig(opts.ProjectedGraph, opts.ResourceName)
	if err != nil {
		return err
	}
	if err := ensureDeploymentStateDirs(service, runtimeCfg.SourceRepoRoot); err != nil {
		return err
	}
	if !opts.Migrate {
		return nil
	}
	if err := dropToDeploymentIdentity(service); err != nil {
		return err
	}
	return migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
}

func ensureDeploymentLocalIdentity(name string) (deploymentLocalIdentity, error) {
	if _, err := lookupDeploymentLocalIdentity(name); err == nil {
		if err := ensureDeploymentSupplementaryGroup(name, deploymentSPIREWorkloadGroup); err != nil {
			return deploymentLocalIdentity{}, err
		}
		if err := runRootCommand("/usr/sbin/usermod", "--home", deploymentServiceHome, "--shell", "/usr/sbin/nologin", name); err != nil {
			return deploymentLocalIdentity{}, err
		}
		return lookupDeploymentLocalIdentity(name)
	} else if !isUnknownUser(err) {
		return deploymentLocalIdentity{}, err
	}
	if err := runRootCommand("/usr/sbin/groupadd", "--system", name); err != nil {
		return deploymentLocalIdentity{}, err
	}
	if _, err := user.LookupGroup(deploymentSPIREWorkloadGroup); err != nil {
		return deploymentLocalIdentity{}, fmt.Errorf("lookup %s group: %w", deploymentSPIREWorkloadGroup, err)
	}
	if err := runRootCommand("/usr/sbin/useradd", "--system", "--gid", name, "--groups", deploymentSPIREWorkloadGroup, "--home-dir", deploymentServiceHome, "--shell", "/usr/sbin/nologin", "--no-create-home", name); err != nil {
		return deploymentLocalIdentity{}, err
	}
	return lookupDeploymentLocalIdentity(name)
}

func ensureDeploymentSupplementaryGroup(name string, group string) error {
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

func lookupDeploymentLocalIdentity(name string) (deploymentLocalIdentity, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return deploymentLocalIdentity{}, fmt.Errorf("lookup %s user: %w", name, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return deploymentLocalIdentity{}, fmt.Errorf("parse %s uid: %w", name, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return deploymentLocalIdentity{}, fmt.Errorf("parse %s gid: %w", name, err)
	}
	return deploymentLocalIdentity{uid: uid, gid: gid}, nil
}

func isUnknownUser(err error) bool {
	var unknown user.UnknownUserError
	return errors.As(err, &unknown)
}

func runRootCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureDeploymentStateDirs(service deploymentLocalIdentity, repoRoot string) error {
	if err := ensureDeploymentOwnedDir(deploymentServiceStateRoot, 0, 0, 0o755); err != nil {
		return err
	}
	dirs := []string{
		deploymentServiceHome,
		filepath.Join(deploymentServiceHome, ".cache"),
		filepath.Join(deploymentServiceHome, ".cache", "guardian"),
		filepath.Join(deploymentServiceHome, "tmp"),
		repoRoot,
	}
	for _, dir := range dirs {
		if err := ensureDeploymentOwnedDir(dir, service.uid, service.gid, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func installDeploymentRuntime(runtimeRoot string, binaries []deploymentRuntimeBinary, archives []deploymentRuntimeArchive, serviceGID int) error {
	digest, err := deploymentRuntimeDigest(binaries, archives)
	if err != nil {
		return err
	}
	releaseDir := filepath.Join(runtimeRoot, "releases", "sha256-"+digest)
	binDir := filepath.Join(releaseDir, "bin")
	if err := ensureDeploymentOwnedDir(runtimeRoot, 0, 0, 0o755); err != nil {
		return err
	}
	if err := ensureDeploymentOwnedDir(filepath.Join(runtimeRoot, "releases"), 0, 0, 0o755); err != nil {
		return err
	}
	if err := ensureDeploymentOwnedDir(filepath.Join(runtimeRoot, "tmp"), 0, 0, 0o700); err != nil {
		return err
	}
	if err := ensureDeploymentOwnedDir(releaseDir, 0, serviceGID, 0o750); err != nil {
		return err
	}
	if err := ensureDeploymentOwnedDir(binDir, 0, serviceGID, 0o750); err != nil {
		return err
	}
	for _, binary := range binaries {
		if err := installDeploymentRuntimeBinary(filepath.Join(runtimeRoot, "tmp"), filepath.Join(binDir, binary.name), binary, serviceGID); err != nil {
			return err
		}
	}
	for _, archive := range archives {
		if err := extractDeploymentRuntimeArchive(releaseDir, archive, serviceGID); err != nil {
			return err
		}
	}
	tmpLink := filepath.Join(runtimeRoot, fmt.Sprintf("current.tmp-%d", os.Getpid()))
	if err := os.Remove(tmpLink); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale deployment-service runtime link: %w", err)
	}
	if err := os.Symlink(releaseDir, tmpLink); err != nil {
		return fmt.Errorf("create deployment-service runtime link: %w", err)
	}
	if err := os.Rename(tmpLink, filepath.Join(runtimeRoot, "current")); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("promote deployment-service runtime link: %w", err)
	}
	return nil
}

func deploymentRuntimeDigest(binaries []deploymentRuntimeBinary, archives []deploymentRuntimeArchive) (string, error) {
	if len(binaries) == 0 {
		return "", errors.New("deployment runtime requires at least one binary")
	}
	h := sha256.New()
	for _, binary := range binaries {
		if strings.TrimSpace(binary.name) == "" || strings.TrimSpace(binary.source) == "" {
			return "", errors.New("deployment runtime binary name and source are required")
		}
		digest, err := deploymentFileSHA256(binary.source)
		if err != nil {
			return "", fmt.Errorf("digest %s binary: %w", binary.label, err)
		}
		if _, err := io.WriteString(h, binary.name+"\x00"+digest+"\x00"); err != nil {
			return "", err
		}
	}
	for _, archive := range archives {
		if strings.TrimSpace(archive.label) == "" || strings.TrimSpace(archive.source) == "" {
			return "", errors.New("deployment runtime archive label and source are required")
		}
		digest, err := deploymentFileSHA256(archive.source)
		if err != nil {
			return "", fmt.Errorf("digest %s archive: %w", archive.label, err)
		}
		if _, err := io.WriteString(h, archive.label+"\x00"+digest+"\x00"); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func installDeploymentRuntimeBinary(tmpDir string, targetBinary string, binary deploymentRuntimeBinary, serviceGID int) error {
	digest, err := deploymentFileSHA256(binary.source)
	if err != nil {
		return fmt.Errorf("digest %s binary: %w", binary.label, err)
	}
	if ok, err := deploymentFileDigestMatches(targetBinary, digest); err != nil {
		return err
	} else if ok {
		return nil
	}
	tmp, err := os.CreateTemp(tmpDir, binary.name+"-*")
	if err != nil {
		return fmt.Errorf("create %s runtime temp: %w", binary.label, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	source, err := os.Open(binary.source)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("open %s source binary: %w", binary.label, err)
	}
	if _, err := io.Copy(tmp, source); err != nil {
		_ = source.Close()
		_ = tmp.Close()
		return fmt.Errorf("copy %s runtime binary: %w", binary.label, err)
	}
	if err := source.Close(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("close %s source binary: %w", binary.label, err)
	}
	if err := tmp.Chmod(0o550); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s runtime binary: %w", binary.label, err)
	}
	if err := tmp.Chown(0, serviceGID); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown %s runtime binary: %w", binary.label, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s runtime temp: %w", binary.label, err)
	}
	if err := os.Rename(tmpName, targetBinary); err != nil {
		return fmt.Errorf("promote %s runtime binary: %w", binary.label, err)
	}
	cleanup = false
	return nil
}

func extractDeploymentRuntimeArchive(releaseDir string, archive deploymentRuntimeArchive, serviceGID int) error {
	f, err := os.Open(archive.source)
	if err != nil {
		return fmt.Errorf("open %s archive: %w", archive.label, err)
	}

	tr := tar.NewReader(f)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if err := f.Close(); err != nil {
				return fmt.Errorf("close %s archive: %w", archive.label, err)
			}
			return nil
		}
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("read %s archive: %w", archive.label, err)
		}
		target, err := deploymentRuntimeArchiveTarget(releaseDir, header.Name)
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("%s archive path %q: %w", archive.label, header.Name, err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureDeploymentOwnedDir(target, 0, serviceGID, 0o750); err != nil {
				_ = f.Close()
				return err
			}
		case tar.TypeReg:
			if err := ensureDeploymentOwnedDir(filepath.Dir(target), 0, serviceGID, 0o750); err != nil {
				_ = f.Close()
				return err
			}
			mode := os.FileMode(0o440)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o550
			}
			if err := extractDeploymentRuntimeFile(tr, target, mode, serviceGID); err != nil {
				_ = f.Close()
				return fmt.Errorf("extract %s: %w", target, err)
			}
		case tar.TypeSymlink:
			if err := extractDeploymentRuntimeSymlink(header.Linkname, target, serviceGID); err != nil {
				_ = f.Close()
				return fmt.Errorf("extract %s symlink: %w", target, err)
			}
		default:
			_ = f.Close()
			return fmt.Errorf("%s archive contains unsupported entry %q type %d", archive.label, header.Name, header.Typeflag)
		}
	}
}

func deploymentRuntimeArchiveTarget(root string, name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." {
		return root, nil
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes runtime root")
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes runtime root")
	}
	return target, nil
}

func extractDeploymentRuntimeFile(source io.Reader, target string, mode os.FileMode, serviceGID int) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chown(0, serviceGID); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func extractDeploymentRuntimeSymlink(linkname string, target string, serviceGID int) error {
	if strings.TrimSpace(linkname) == "" {
		return errors.New("empty symlink target")
	}
	cleanLink := filepath.Clean(linkname)
	if filepath.IsAbs(cleanLink) || cleanLink == ".." || strings.HasPrefix(cleanLink, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("unsafe symlink target %q", linkname)
	}
	if err := ensureDeploymentOwnedDir(filepath.Dir(target), 0, serviceGID, 0o750); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Symlink(cleanLink, target); err != nil {
		return err
	}
	if err := os.Lchown(target, 0, serviceGID); err != nil {
		return err
	}
	return nil
}

func projectDeploymentResourceGraph(sourcePath string, targetPath string, serviceGID int) error {
	if err := ensureDeploymentOwnedDir("/run/verself", 0, 0, 0o711); err != nil {
		return err
	}
	if err := ensureDeploymentOwnedDir("/run/verself/recovery", 0, 0, 0o711); err != nil {
		return err
	}
	targetDir := filepath.Dir(targetPath)
	if err := ensureDeploymentOwnedDir(targetDir, 0, serviceGID, 0o750); err != nil {
		return err
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read deployment-service resource graph: %w", err)
	}
	tmp, err := os.CreateTemp(targetDir, ".document-*.json")
	if err != nil {
		return fmt.Errorf("create deployment-service resource graph temp: %w", err)
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
		return fmt.Errorf("write deployment-service resource graph temp: %w", err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod deployment-service resource graph: %w", err)
	}
	if err := tmp.Chown(0, serviceGID); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown deployment-service resource graph: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close deployment-service resource graph temp: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("promote deployment-service resource graph: %w", err)
	}
	cleanup = false
	return nil
}

func ensureDeploymentOwnedDir(path string, uid int, gid int, mode os.FileMode) error {
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

func deploymentFileSHA256(path string) (string, error) {
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

func deploymentFileDigestMatches(path string, digest string) (bool, error) {
	observed, err := deploymentFileSHA256(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("digest existing %s: %w", path, err)
	}
	return observed == digest, nil
}

func dropToDeploymentIdentity(identity deploymentLocalIdentity) error {
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

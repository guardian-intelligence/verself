package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	defaultGuardianRepoRoot             = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultObjectStorageRuntimeArtifact = "bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar"
	defaultObjectStorageRuntimeRoot     = "/var/lib/object-storage-service/runtime"
	defaultObjectStorageProjectedGraph  = "/run/verself/recovery/object-storage/document.json"
	objectStorageServiceUser            = "object_storage_service"
	objectStorageAdminUser              = "object_storage_admin"
	objectStorageServiceUID             = 960
	objectStorageAdminUID               = 961
	objectStorageServiceHome            = "/var/lib/object-storage-service"
	objectStorageRuntimeBinary          = "bin/object-storage-service"
	maxUint32AsInt64                    = int64(1<<32 - 1)
)

type recoverOptions struct {
	RepoRoot       string
	ResourceGraph  string
	ResourceName   string
	RuntimeRoot    string
	RuntimeTar     string
	ProjectedGraph string
	Migrate        bool
}

func runRecoveryCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "recover" {
		return false, nil
	}
	opts, err := parseRecoverOptions(args[1:])
	if err != nil {
		return true, err
	}
	return true, recoverObjectStorage(ctx, opts)
}

func parseRecoverOptions(args []string) (recoverOptions, error) {
	opts := recoverOptions{
		RepoRoot:       defaultGuardianRepoRoot,
		ResourceName:   defaultResourceName,
		RuntimeRoot:    defaultObjectStorageRuntimeRoot,
		RuntimeTar:     defaultObjectStorageRuntimeArtifact,
		ProjectedGraph: defaultObjectStorageProjectedGraph,
		Migrate:        true,
	}
	fs := flag.NewFlagSet("object-storage-service recover", flag.ContinueOnError)
	fs.StringVar(&opts.RepoRoot, "repo-root", opts.RepoRoot, "Boarded repo root.")
	fs.StringVar(&opts.ResourceGraph, "resource-graph", "", "Guardian resource graph JSON path.")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "ObjectStorageService resource name.")
	fs.StringVar(&opts.RuntimeRoot, "runtime-root", opts.RuntimeRoot, "Object storage runtime install root.")
	fs.StringVar(&opts.RuntimeTar, "runtime-tar", opts.RuntimeTar, "Repo-relative runtime tar path.")
	fs.StringVar(&opts.ProjectedGraph, "projected-graph", opts.ProjectedGraph, "Projected Guardian resource graph path.")
	fs.BoolVar(&opts.Migrate, "migrate", opts.Migrate, "Run database migrations after runtime install.")
	if err := fs.Parse(args); err != nil {
		return recoverOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return recoverOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)
	opts.ResourceGraph = strings.TrimSpace(opts.ResourceGraph)
	opts.ResourceName = strings.TrimSpace(opts.ResourceName)
	opts.RuntimeRoot = strings.TrimSpace(opts.RuntimeRoot)
	opts.RuntimeTar = strings.TrimSpace(opts.RuntimeTar)
	opts.ProjectedGraph = strings.TrimSpace(opts.ProjectedGraph)
	if opts.ResourceGraph == "" {
		opts.ResourceGraph = filepath.Join(opts.RepoRoot, "workspace", ".guardian", "fly", "document.json")
	}
	for label, value := range map[string]string{
		"repo-root":       opts.RepoRoot,
		"resource-graph":  opts.ResourceGraph,
		"resource-name":   opts.ResourceName,
		"runtime-root":    opts.RuntimeRoot,
		"runtime-tar":     opts.RuntimeTar,
		"projected-graph": opts.ProjectedGraph,
	} {
		if value == "" {
			return recoverOptions{}, fmt.Errorf("--%s is required", label)
		}
	}
	if !filepath.IsAbs(opts.RepoRoot) || !filepath.IsAbs(opts.ResourceGraph) || !filepath.IsAbs(opts.RuntimeRoot) || !filepath.IsAbs(opts.ProjectedGraph) {
		return recoverOptions{}, errors.New("--repo-root, --resource-graph, --runtime-root, and --projected-graph must be absolute")
	}
	if filepath.IsAbs(opts.RuntimeTar) {
		return recoverOptions{}, errors.New("--runtime-tar must be repo-relative")
	}
	return opts, nil
}

func recoverObjectStorage(ctx context.Context, opts recoverOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("object-storage-service recover must run as root")
	}
	if _, err := loadObjectStorageRuntimeConfig(opts.ResourceGraph, opts.ResourceName); err != nil {
		return err
	}
	if err := ensureObjectStorageUsers(); err != nil {
		return err
	}
	service, err := lookupUser(objectStorageServiceUser)
	if err != nil {
		return err
	}
	if err := ensureDir(objectStorageServiceHome, service.uid, service.gid, 0o750); err != nil {
		return err
	}
	if err := installObjectStorageRuntime(opts, service.gid); err != nil {
		return err
	}
	if err := projectObjectStorageGraph(opts, service.gid); err != nil {
		return err
	}
	if opts.Migrate {
		if err := runObjectStorageMigrations(ctx, opts); err != nil {
			return err
		}
	}
	return nil
}

func ensureObjectStorageUsers() error {
	if err := ensureGroup(objectStorageServiceUser); err != nil {
		return err
	}
	if err := ensureUser(objectStorageServiceUser, objectStorageServiceUID, objectStorageServiceUser); err != nil {
		return err
	}
	return ensureUser(objectStorageAdminUser, objectStorageAdminUID, objectStorageServiceUser)
}

func ensureGroup(name string) error {
	if _, err := lookupGroup(name); err == nil {
		return nil
	}
	cmd := exec.Command("/usr/sbin/groupadd", "--system", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create group %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureUser(name string, uid int, group string) error {
	if _, err := lookupUser(name); err == nil {
		return nil
	}
	cmd := exec.Command(
		"/usr/sbin/useradd",
		"--system",
		"--uid", strconv.Itoa(uid),
		"--gid", group,
		"--home-dir", objectStorageServiceHome,
		"--shell", "/usr/sbin/nologin",
		"--no-create-home",
		name,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create user %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

type passwdEntry struct {
	uid int
	gid int
}

func lookupUser(name string) (passwdEntry, error) {
	out, err := exec.Command("/usr/bin/getent", "passwd", name).Output()
	if err != nil {
		return passwdEntry{}, fmt.Errorf("lookup user %s: %w", name, err)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(fields) < 4 {
		return passwdEntry{}, fmt.Errorf("lookup user %s: malformed passwd entry", name)
	}
	uid, err := strconv.Atoi(fields[2])
	if err != nil {
		return passwdEntry{}, fmt.Errorf("lookup user %s uid: %w", name, err)
	}
	gid, err := strconv.Atoi(fields[3])
	if err != nil {
		return passwdEntry{}, fmt.Errorf("lookup user %s gid: %w", name, err)
	}
	return passwdEntry{uid: uid, gid: gid}, nil
}

func lookupGroup(name string) (int, error) {
	out, err := exec.Command("/usr/bin/getent", "group", name).Output()
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", name, err)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(fields) < 3 {
		return 0, fmt.Errorf("lookup group %s: malformed group entry", name)
	}
	gid, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0, fmt.Errorf("lookup group %s gid: %w", name, err)
	}
	return gid, nil
}

func ensureDir(path string, uid int, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown directory %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod directory %s: %w", path, err)
	}
	return nil
}

func installObjectStorageRuntime(opts recoverOptions, serviceGID int) error {
	artifact := filepath.Join(opts.RepoRoot, filepath.FromSlash(opts.RuntimeTar))
	artifactInfo, err := os.Stat(artifact)
	if err != nil {
		return fmt.Errorf("stat object-storage runtime artifact %s: %w", artifact, err)
	}
	if !artifactInfo.Mode().IsRegular() {
		return fmt.Errorf("object-storage runtime artifact is not a regular file: %s", artifact)
	}
	if err := ensureDir(opts.RuntimeRoot, 0, serviceGID, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(opts.RuntimeRoot, "install.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open object-storage runtime install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	lockFD, err := intFromFileDescriptor(lock.Fd(), "object-storage runtime install lock")
	if err != nil {
		return err
	}
	if err := syscall.Flock(lockFD, syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock object-storage runtime install: %w", err)
	}
	defer func() { _ = syscall.Flock(lockFD, syscall.LOCK_UN) }()
	releases := filepath.Join(opts.RuntimeRoot, "releases")
	tmpRoot := filepath.Join(opts.RuntimeRoot, "tmp")
	if err := ensureDir(releases, 0, serviceGID, 0o755); err != nil {
		return err
	}
	if err := ensureDir(tmpRoot, 0, serviceGID, 0o755); err != nil {
		return err
	}
	digest, err := sha256File(artifact)
	if err != nil {
		return err
	}
	release := filepath.Join(releases, "sha256-"+digest)
	if !regularFile(filepath.Join(release, objectStorageRuntimeBinary)) {
		tmp := filepath.Join(tmpRoot, "sha256-"+digest+"."+strconv.Itoa(os.Getpid()))
		if err := os.RemoveAll(tmp); err != nil {
			return fmt.Errorf("remove stale object-storage runtime temp dir: %w", err)
		}
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return fmt.Errorf("create object-storage runtime temp dir: %w", err)
		}
		if err := extractSafeTar(artifact, tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		if !regularFile(filepath.Join(tmp, objectStorageRuntimeBinary)) {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("object-storage runtime artifact missing %s", objectStorageRuntimeBinary)
		}
		if err := os.RemoveAll(release); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("remove stale object-storage runtime release: %w", err)
		}
		if err := os.Rename(tmp, release); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("promote object-storage runtime release: %w", err)
		}
	}
	nextLink := filepath.Join(opts.RuntimeRoot, "current.next")
	currentLink := filepath.Join(opts.RuntimeRoot, "current")
	_ = os.Remove(nextLink)
	if err := os.Symlink(release, nextLink); err != nil {
		return fmt.Errorf("stage object-storage runtime symlink: %w", err)
	}
	if info, err := os.Lstat(currentLink); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(currentLink); err != nil {
			_ = os.Remove(nextLink)
			return fmt.Errorf("remove old object-storage runtime current path: %w", err)
		}
	}
	if err := os.Rename(nextLink, currentLink); err != nil {
		_ = os.Remove(nextLink)
		return fmt.Errorf("promote object-storage runtime symlink: %w", err)
	}
	return nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func extractSafeTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open object-storage runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	destClean, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(file)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read object-storage runtime tar: %w", err)
		}
		target := filepath.Join(destClean, header.Name)
		targetClean, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if targetClean != destClean && !strings.HasPrefix(targetClean, destClean+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe object-storage runtime artifact member: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			mode, err := modeOrDefault(header.Mode, 0o755)
			if err != nil {
				return fmt.Errorf("runtime directory %s mode: %w", header.Name, err)
			}
			if err := os.MkdirAll(targetClean, mode); err != nil {
				return fmt.Errorf("extract runtime directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetClean), 0o755); err != nil {
				return fmt.Errorf("create runtime file parent %s: %w", header.Name, err)
			}
			mode, err := modeOrDefault(header.Mode, 0o644)
			if err != nil {
				return fmt.Errorf("runtime file %s mode: %w", header.Name, err)
			}
			out, err := os.OpenFile(targetClean, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create runtime file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write runtime file %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close runtime file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported object-storage runtime artifact member %s type %d", header.Name, header.Typeflag)
		}
	}
}

func modeOrDefault(mode int64, fallback os.FileMode) (os.FileMode, error) {
	if mode == 0 {
		return fallback, nil
	}
	if mode < 0 || mode > maxUint32AsInt64 {
		return 0, fmt.Errorf("mode %d exceeds os.FileMode range", mode)
	}
	return os.FileMode(mode).Perm(), nil // #nosec G115 -- mode is checked against the uint32-backed os.FileMode range above.
}

// os.File.Fd is uintptr, but flock expects an int descriptor.
func intFromFileDescriptor(fd uintptr, field string) (int, error) {
	if strconv.IntSize == 32 && fd > uintptr(1<<31-1) {
		return 0, fmt.Errorf("%s file descriptor exceeds int range: %d", field, fd)
	}
	return int(fd), nil // #nosec G115 -- fd is range-checked for 32-bit; on 64-bit uintptr and int have the same width.
}

func projectObjectStorageGraph(opts recoverOptions, serviceGID int) error {
	body, err := os.ReadFile(opts.ResourceGraph)
	if err != nil {
		return fmt.Errorf("read Guardian resource graph: %w", err)
	}
	if err := ensureDir("/run/verself", 0, 0, 0o755); err != nil {
		return err
	}
	if err := ensureDir("/run/verself/recovery", 0, 0, 0o711); err != nil {
		return err
	}
	parent := filepath.Dir(opts.ProjectedGraph)
	if err := ensureDir(parent, 0, serviceGID, 0o750); err != nil {
		return err
	}
	tmp := opts.ProjectedGraph + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, body, 0o640); err != nil {
		return fmt.Errorf("write projected Guardian graph: %w", err)
	}
	if err := os.Chown(tmp, 0, serviceGID); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chown projected Guardian graph: %w", err)
	}
	if err := os.Rename(tmp, opts.ProjectedGraph); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("promote projected Guardian graph: %w", err)
	}
	return nil
}

func runObjectStorageMigrations(ctx context.Context, opts recoverOptions) error {
	binary := filepath.Join(opts.RuntimeRoot, "current", objectStorageRuntimeBinary)
	cmd := exec.CommandContext(
		ctx,
		"/usr/sbin/runuser",
		"-u", objectStorageServiceUser,
		"--",
		binary,
		"migrate",
		"--resource-graph="+opts.ProjectedGraph,
		"--resource-name="+opts.ResourceName,
		"up",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("object-storage migrations: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

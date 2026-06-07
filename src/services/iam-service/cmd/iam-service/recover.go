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
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/verself/iam-service/migrations"
)

const (
	defaultIAMRuntimeRoot    = "/var/lib/iam-service/runtime"
	defaultIAMProjectedGraph = "/run/verself/recovery/iam-service/document.json"
	iamServiceUser           = "iam_service"
	iamBoardedBinaryRelative = "bazel-bin/src/services/iam-service/cmd/iam-service/iam-service_/iam-service"
)

type iamRecoverOptions struct {
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

func runIAMRecoveryCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "recover" {
		return false, nil
	}
	opts, err := parseIAMRecoverOptions(args[1:])
	if err != nil {
		return true, err
	}
	return true, recoverIAM(ctx, opts)
}

func parseIAMRecoverOptions(args []string) (iamRecoverOptions, error) {
	opts := iamRecoverOptions{
		ResourceGraph:  defaultGuardianResourceGraph,
		ResourceName:   defaultIAMResourceName,
		RuntimeRoot:    defaultIAMRuntimeRoot,
		ProjectedGraph: defaultIAMProjectedGraph,
	}
	fs := flag.NewFlagSet("iam-service recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.RepoRoot, "repo-root", opts.RepoRoot, "boarded Guardian repo root")
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ResourceName, "resource-name", opts.ResourceName, "IAMService resource name")
	fs.StringVar(&opts.RuntimeRoot, "runtime-root", opts.RuntimeRoot, "IAM runtime install root")
	fs.StringVar(&opts.ProjectedGraph, "projected-graph", opts.ProjectedGraph, "runtime-readable Guardian resource graph projection")
	fs.BoolVar(&opts.Migrate, "migrate", opts.Migrate, "run IAM database migrations after runtime installation")
	if err := fs.Parse(args); err != nil {
		return iamRecoverOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return iamRecoverOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
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
			return iamRecoverOptions{}, fmt.Errorf("--%s is required", label)
		}
	}
	if !filepath.IsAbs(opts.RepoRoot) || !filepath.IsAbs(opts.ResourceGraph) || !filepath.IsAbs(opts.RuntimeRoot) || !filepath.IsAbs(opts.ProjectedGraph) {
		return iamRecoverOptions{}, errors.New("--repo-root, --resource-graph, --runtime-root, and --projected-graph must be absolute")
	}
	return opts, nil
}

func recoverIAM(ctx context.Context, opts iamRecoverOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("iam-service recover must run as root")
	}
	service, err := lookupLocalIdentity(iamServiceUser)
	if err != nil {
		return err
	}
	sourceBinary := filepath.Join(opts.RepoRoot, iamBoardedBinaryRelative)
	if err := installIAMRuntime(opts.RuntimeRoot, sourceBinary, service.gid); err != nil {
		return err
	}
	if err := projectIAMResourceGraph(opts.ResourceGraph, opts.ProjectedGraph, service.gid); err != nil {
		return err
	}
	if !opts.Migrate {
		return nil
	}
	runtimeCfg, err := loadIAMRuntimeConfig(opts.ProjectedGraph, opts.ResourceName)
	if err != nil {
		return err
	}
	if err := dropToLocalIdentity(service); err != nil {
		return err
	}
	return migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
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

func installIAMRuntime(runtimeRoot string, sourceBinary string, serviceGID int) error {
	digest, err := fileSHA256(sourceBinary)
	if err != nil {
		return fmt.Errorf("digest iam-service binary: %w", err)
	}
	releaseDir := filepath.Join(runtimeRoot, "releases", "sha256-"+digest)
	binDir := filepath.Join(releaseDir, "bin")
	targetBinary := filepath.Join(binDir, "iam-service")
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
		tmp, err := os.CreateTemp(filepath.Join(runtimeRoot, "tmp"), "iam-service-*")
		if err != nil {
			return fmt.Errorf("create iam-service runtime temp: %w", err)
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
			return fmt.Errorf("open iam-service source binary: %w", err)
		}
		if _, err := io.Copy(tmp, source); err != nil {
			_ = source.Close()
			_ = tmp.Close()
			return fmt.Errorf("copy iam-service runtime binary: %w", err)
		}
		if err := source.Close(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("close iam-service source binary: %w", err)
		}
		if err := tmp.Chmod(0o550); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chmod iam-service runtime binary: %w", err)
		}
		if err := tmp.Chown(0, serviceGID); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chown iam-service runtime binary: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close iam-service runtime temp: %w", err)
		}
		if err := os.Rename(tmpName, targetBinary); err != nil {
			return fmt.Errorf("promote iam-service runtime binary: %w", err)
		}
		cleanup = false
	}
	tmpLink := filepath.Join(runtimeRoot, fmt.Sprintf("current.tmp-%d", os.Getpid()))
	if err := os.Remove(tmpLink); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale iam-service runtime link: %w", err)
	}
	if err := os.Symlink(releaseDir, tmpLink); err != nil {
		return fmt.Errorf("create iam-service runtime link: %w", err)
	}
	if err := os.Rename(tmpLink, filepath.Join(runtimeRoot, "current")); err != nil {
		_ = os.Remove(tmpLink)
		return fmt.Errorf("promote iam-service runtime link: %w", err)
	}
	return nil
}

func projectIAMResourceGraph(sourcePath string, targetPath string, serviceGID int) error {
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
		return fmt.Errorf("read IAM resource graph: %w", err)
	}
	tmp, err := os.CreateTemp(targetDir, ".document-*.json")
	if err != nil {
		return fmt.Errorf("create IAM resource graph temp: %w", err)
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
		return fmt.Errorf("write IAM resource graph temp: %w", err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod IAM resource graph: %w", err)
	}
	if err := tmp.Chown(0, serviceGID); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown IAM resource graph: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close IAM resource graph temp: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("promote IAM resource graph: %w", err)
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

func dropToLocalIdentity(identity localIdentity) error {
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

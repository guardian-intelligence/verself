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
	defaultDistributionRuntimeRoot    = "/var/lib/distribution-service/runtime"
	defaultDistributionProjectedGraph = "/run/verself/recovery/distribution-service/document.json"
	defaultDistributionProjectedImage = "/var/lib/distribution-service/runtime/image.tar"
	distributionServiceUser           = "distribution_service"
	distributionSPIREWorkloadGroup    = "spire_workload"
	distributionServiceStateRoot      = "/var/lib/distribution-service"
	distributionServiceHome           = "/var/lib/distribution-service/home"
)

type recoverOptions struct {
	ResourceGraph  string
	ProjectedGraph string
	ImageArchive   string
	ProjectedImage string
	RuntimeRoot    string
}

type localIdentity struct {
	uid int
	gid int
}

type rootCommand struct {
	name string
	args []string
}

func runRecoveryCLI(ctx context.Context, args []string) (bool, error) {
	_ = ctx
	if len(args) < 1 || args[0] != "recover" {
		return false, nil
	}
	opts, err := parseRecoverOptions(args[1:])
	if err != nil {
		return true, err
	}
	return true, recoverDistributionService(opts)
}

func parseRecoverOptions(args []string) (recoverOptions, error) {
	opts := recoverOptions{
		ResourceGraph:  defaultGuardianResourceGraph,
		ProjectedGraph: defaultDistributionProjectedGraph,
		ProjectedImage: defaultDistributionProjectedImage,
		RuntimeRoot:    defaultDistributionRuntimeRoot,
	}
	fs := flag.NewFlagSet(serviceName+" recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.ResourceGraph, "resource-graph", opts.ResourceGraph, "Guardian resource graph JSON path")
	fs.StringVar(&opts.ProjectedGraph, "projected-graph", opts.ProjectedGraph, "runtime-readable Guardian resource graph projection")
	fs.StringVar(&opts.ImageArchive, "image-archive", opts.ImageArchive, "boarded distribution-service OCI image archive")
	fs.StringVar(&opts.ProjectedImage, "projected-image", opts.ProjectedImage, "runtime-readable distribution-service OCI image archive")
	fs.StringVar(&opts.RuntimeRoot, "runtime-root", opts.RuntimeRoot, "distribution-service runtime projection root")
	if err := fs.Parse(args); err != nil {
		return recoverOptions{}, err
	}
	if len(fs.Args()) != 0 {
		return recoverOptions{}, fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}
	required := map[string]string{
		"resource-graph":  opts.ResourceGraph,
		"projected-graph": opts.ProjectedGraph,
		"image-archive":   opts.ImageArchive,
		"projected-image": opts.ProjectedImage,
		"runtime-root":    opts.RuntimeRoot,
	}
	for label, value := range required {
		if strings.TrimSpace(value) == "" {
			return recoverOptions{}, fmt.Errorf("--%s is required", label)
		}
	}
	if !filepath.IsAbs(opts.ResourceGraph) || !filepath.IsAbs(opts.ProjectedGraph) || !filepath.IsAbs(opts.ImageArchive) || !filepath.IsAbs(opts.ProjectedImage) || !filepath.IsAbs(opts.RuntimeRoot) {
		return recoverOptions{}, errors.New("--resource-graph, --projected-graph, --image-archive, --projected-image, and --runtime-root must be absolute")
	}
	return opts, nil
}

func recoverDistributionService(opts recoverOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("distribution-service recover must run as root")
	}
	service, err := ensureLocalIdentity(distributionServiceUser)
	if err != nil {
		return err
	}
	if err := ensureStateDirs(service); err != nil {
		return err
	}
	if err := ensureOwnedDir(opts.RuntimeRoot, 0, service.gid, 0o750); err != nil {
		return err
	}
	if err := projectFile(opts.ImageArchive, opts.ProjectedImage, 0, service.gid, 0o640, "distribution-service image archive"); err != nil {
		return err
	}
	if err := ensureOwnedDir("/run/verself", 0, 0, 0o711); err != nil {
		return err
	}
	if err := ensureOwnedDir("/run/verself/recovery", 0, 0, 0o711); err != nil {
		return err
	}
	return projectFile(opts.ResourceGraph, opts.ProjectedGraph, 0, service.gid, 0o640, "distribution-service resource graph")
}

func ensureLocalIdentity(name string) (localIdentity, error) {
	if err := ensureLocalGroup(name); err != nil {
		return localIdentity{}, err
	}
	if _, err := user.LookupGroup(distributionSPIREWorkloadGroup); err != nil {
		return localIdentity{}, fmt.Errorf("lookup %s group: %w", distributionSPIREWorkloadGroup, err)
	}
	if _, err := user.Lookup(name); err == nil {
		if err := reconcileLocalUser(name); err != nil {
			return localIdentity{}, err
		}
	} else if isUnknownUser(err) {
		if err := runRootCommandSpec(createLocalUserCommand(name, distributionSPIREWorkloadGroup)); err != nil {
			return localIdentity{}, err
		}
	} else {
		return localIdentity{}, err
	}
	if err := ensureSupplementaryGroup(name, distributionSPIREWorkloadGroup); err != nil {
		return localIdentity{}, err
	}
	identity, err := lookupLocalIdentity(name)
	if err != nil {
		return localIdentity{}, err
	}
	return identity, nil
}

func ensureLocalGroup(name string) error {
	_, err := user.LookupGroup(name)
	if err != nil {
		if !isUnknownGroup(err) {
			return fmt.Errorf("lookup %s group: %w", name, err)
		}
		if err := runRootCommand("/usr/sbin/groupadd", "--system", name); err != nil {
			return err
		}
		_, err = user.LookupGroup(name)
		if err != nil {
			return fmt.Errorf("lookup created %s group: %w", name, err)
		}
	}
	return nil
}

func reconcileLocalUser(name string) error {
	for _, command := range reconcileLocalUserCommands(name) {
		if err := runRootCommandSpec(command); err != nil {
			return err
		}
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
	return runRootCommandSpec(appendSupplementaryGroupCommand(name, group))
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

func runRootCommandSpec(command rootCommand) error {
	return runRootCommand(command.name, command.args...)
}

func createLocalUserCommand(name string, supplementaryGroup string) rootCommand {
	return rootCommand{
		name: "/usr/sbin/useradd",
		args: []string{
			"--system",
			"--gid", name,
			"--groups", supplementaryGroup,
			"--home-dir", distributionServiceHome,
			"--shell", "/usr/sbin/nologin",
			"--no-create-home",
			name,
		},
	}
}

func reconcileLocalUserCommands(name string) []rootCommand {
	return []rootCommand{
		{name: "/usr/sbin/usermod", args: []string{"--gid", name, name}},
		{name: "/usr/sbin/usermod", args: []string{"--home", distributionServiceHome, "--shell", "/usr/sbin/nologin", name}},
	}
}

func appendSupplementaryGroupCommand(name string, group string) rootCommand {
	return rootCommand{name: "/usr/sbin/usermod", args: []string{"--append", "--groups", group, name}}
}

func ensureStateDirs(service localIdentity) error {
	if err := ensureOwnedDir(distributionServiceStateRoot, 0, 0, 0o755); err != nil {
		return err
	}
	dirs := []string{
		distributionServiceHome,
		filepath.Join(distributionServiceHome, "tmp"),
	}
	for _, dir := range dirs {
		if err := ensureOwnedDir(dir, service.uid, service.gid, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func projectFile(sourcePath string, targetPath string, uid int, gid int, mode os.FileMode, label string) error {
	sourceDigest, err := fileSHA256(sourcePath)
	if err != nil {
		return fmt.Errorf("digest %s: %w", label, err)
	}
	if ok, err := fileDigestMatches(targetPath, sourceDigest); err != nil {
		return err
	} else if ok {
		return ensureFileOwnerMode(targetPath, uid, gid, mode)
	}
	targetDir := filepath.Dir(targetPath)
	if err := ensureOwnedDir(targetDir, 0, gid, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(targetDir, "."+filepath.Base(targetPath)+".*")
	if err != nil {
		return fmt.Errorf("create %s temp: %w", label, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	source, err := os.Open(sourcePath)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("open %s: %w", label, err)
	}
	if _, err := io.Copy(tmp, source); err != nil {
		_ = source.Close()
		_ = tmp.Close()
		return fmt.Errorf("copy %s: %w", label, err)
	}
	if err := source.Close(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("close %s source: %w", label, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", label, err)
	}
	if err := tmp.Chown(uid, gid); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown %s: %w", label, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s temp: %w", label, err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("promote %s: %w", label, err)
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

func ensureFileOwnerMode(path string, uid int, gid int, mode os.FileMode) error {
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

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	substrateSizeMiB = 2048
	aptAcquireConfig = `Acquire::http::Timeout "5";
Acquire::https::Timeout "5";
Acquire::ftp::Timeout "5";
Acquire::Retries "1";
Acquire::ForceIPv4 "true";
Acquire::Queue-Mode "access";
DPkg::Lock::Timeout "60";
`
)

var substratePackages = []string{
	"bash",
	"ca-certificates",
	"curl",
	"git",
	"iproute2",
	"jq",
	"libicu74",
	"libkrb5-3",
	"libssl3",
	"openssh-client",
	"python3",
	"sudo",
	"tar",
	"unzip",
	"xz-utils",
	"zlib1g",
	"zstd",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "build-substrate: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	if os.Geteuid() != 0 {
		return errors.New("must run as root")
	}
	if err := requireCommands("chroot", "mkfs.ext4", "mount", "tar", "umount"); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	inputs := os.Getenv("SUBSTRATE_INPUTS")
	if inputs == "" {
		inputs = filepath.Dir(exe)
	}
	outputDir := os.Getenv("SUBSTRATE_OUTPUT_DIR")
	if outputDir == "" {
		outputDir = filepath.Join(filepath.Dir(exe), "guest", "output")
	}

	versions, err := readVersions(filepath.Join(inputs, "versions.json"))
	if err != nil {
		return err
	}
	requiredInputs := map[string]string{
		"ubuntu_base":        filepath.Join(inputs, "ubuntu-base.tar.gz"),
		"vmlinux":            filepath.Join(inputs, "vmlinux"),
		"vmlinux.config":     filepath.Join(inputs, "vmlinux.config"),
		"vm-bridge":          filepath.Join(inputs, "vm-bridge"),
		"vm-guest-telemetry": filepath.Join(inputs, "vm-guest-telemetry"),
		"versions":           filepath.Join(inputs, "versions.json"),
	}
	for name, path := range requiredInputs {
		if err := requireFile(name, path); err != nil {
			return err
		}
	}
	if err := validateKernelConfig(requiredInputs["vmlinux.config"]); err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "verself-substrate-*")
	if err != nil {
		return fmt.Errorf("create workdir: %w", err)
	}
	rootfs := filepath.Join(workDir, "rootfs")
	mounts := mountStack{}
	defer func() {
		_ = mounts.unmountAll()
		_ = os.RemoveAll(workDir)
	}()

	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		return fmt.Errorf("create rootfs: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	fmt.Printf("-> extracting Ubuntu base rootfs (%s)\n", versions.UbuntuBase.Version)
	if err := command("tar", "-xzf", requiredInputs["ubuntu_base"], "--numeric-owner", "-C", rootfs).run(); err != nil {
		return err
	}

	fmt.Println("-> staging kernel artefacts")
	if err := copyFile(requiredInputs["vmlinux"], filepath.Join(outputDir, "vmlinux"), 0o644); err != nil {
		return err
	}
	if err := copyFile(requiredInputs["vmlinux.config"], filepath.Join(outputDir, "vmlinux.config"), 0o644); err != nil {
		return err
	}

	fmt.Println("-> preparing chroot")
	for _, dir := range []string{
		filepath.Join(rootfs, "usr/local/bin"),
		filepath.Join(rootfs, "usr/sbin"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.Remove(filepath.Join(rootfs, "etc/resolv.conf")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove resolv.conf: %w", err)
	}
	if err := copyFile("/etc/resolv.conf", filepath.Join(rootfs, "etc/resolv.conf"), 0o644); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(rootfs, "etc/hosts"), "127.0.0.1 localhost\n::1 localhost ip6-localhost ip6-loopback\n", 0o644); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(rootfs, "etc/nsswitch.conf"), nsswitchConf, 0o644); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(rootfs, "usr/sbin/policy-rc.d"), "#!/bin/sh\nexit 101\n", 0o755); err != nil {
		return err
	}
	if err := seedChrootCABundle(rootfs); err != nil {
		return err
	}
	if err := configureChrootApt(rootfs); err != nil {
		return err
	}
	if err := mounts.mountChroot(rootfs); err != nil {
		return err
	}

	fmt.Println("-> installing minimal Ubuntu userspace")
	if err := runChroot(rootfs, aptGet("update")...); err != nil {
		return err
	}
	installArgs := aptGet("install", "-y", "--no-install-recommends")
	installArgs = append(installArgs, substratePackages...)
	if err := runChroot(rootfs, installArgs...); err != nil {
		return err
	}

	fmt.Println("-> installing vm-bridge as PID 1")
	if err := os.Remove(filepath.Join(rootfs, "sbin/init")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove existing init: %w", err)
	}
	if err := copyFile(requiredInputs["vm-bridge"], filepath.Join(rootfs, "sbin/init"), 0o755); err != nil {
		return err
	}
	if err := copyFile(requiredInputs["vm-bridge"], filepath.Join(rootfs, "usr/local/bin/vm-bridge"), 0o755); err != nil {
		return err
	}

	fmt.Println("-> installing vm-guest-telemetry")
	if err := copyFile(requiredInputs["vm-guest-telemetry"], filepath.Join(rootfs, "usr/local/bin/vm-guest-telemetry"), 0o755); err != nil {
		return err
	}

	fmt.Println("-> creating substrate write targets")
	for _, dir := range []string{filepath.Join(rootfs, "workspace"), filepath.Join(rootfs, "home")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.Chmod(filepath.Join(rootfs, "workspace"), 0o1777); err != nil {
		return fmt.Errorf("chmod workspace: %w", err)
	}
	if err := writeFile(filepath.Join(rootfs, "etc/profile.d/verself-base.sh"), "export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n", 0o644); err != nil {
		return err
	}

	if err := runChroot(rootfs, "git", "config", "--system", "--add", "safe.directory", "*"); err != nil {
		return err
	}
	if err := runChroot(rootfs, aptGet("clean")...); err != nil {
		return err
	}
	if err := removeContents(filepath.Join(rootfs, "var/lib/apt/lists")); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(rootfs, "usr/sbin/policy-rc.d")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove policy-rc.d: %w", err)
	}

	fmt.Println("-> generating SBOM")
	dpkgInstalled, err := command("chroot", rootfs, "dpkg-query", "-W", "-f=${binary:Package}\t${Version}\n").output()
	if err != nil {
		return err
	}
	sbomPath := filepath.Join(outputDir, "sbom.txt")
	if err := writeFile(sbomPath, string(dpkgInstalled), 0o644); err != nil {
		return err
	}
	packageCount := countNonEmptyLines(dpkgInstalled)
	initDigest, initBytes, err := fileDigest(filepath.Join(rootfs, "sbin/init"))
	if err != nil {
		return err
	}
	telemetryDigest, telemetryBytes, err := fileDigest(filepath.Join(rootfs, "usr/local/bin/vm-guest-telemetry"))
	if err != nil {
		return err
	}
	customComponents := fmt.Sprintf("\n# custom_components\nfile path=/sbin/init component=vm-bridge sha256=%s bytes=%d\nfile path=/usr/local/bin/vm-bridge component=vm-bridge sha256=%s bytes=%d\nfile path=/usr/local/bin/vm-guest-telemetry component=vm-guest-telemetry sha256=%s bytes=%d\n",
		initDigest, initBytes, initDigest, initBytes, telemetryDigest, telemetryBytes)
	if err := appendFile(sbomPath, customComponents); err != nil {
		return err
	}

	if err := mounts.unmountAll(); err != nil {
		return err
	}

	fmt.Printf("-> building ext4 image (%d MiB)\n", substrateSizeMiB)
	substratePath := filepath.Join(outputDir, "substrate.ext4")
	if err := truncateFile(substratePath, int64(substrateSizeMiB)*1024*1024); err != nil {
		return err
	}
	if err := command("mkfs.ext4", "-F", "-L", "substrate", "-d", rootfs, substratePath).discardStdout().run(); err != nil {
		return err
	}

	rootfsDigest, rootfsBytes, err := fileDigest(substratePath)
	if err != nil {
		return err
	}
	rootfsUsedBytes, err := dirSize(rootfs)
	if err != nil {
		return err
	}
	kernelDigest, kernelBytes, err := fileDigest(filepath.Join(outputDir, "vmlinux"))
	if err != nil {
		return err
	}
	sbomDigest, sbomBytes, err := fileDigest(sbomPath)
	if err != nil {
		return err
	}
	manifest := substrateManifest{
		SchemaVersion:          3,
		BuiltAtUTC:             time.Now().UTC().Format(time.RFC3339),
		UbuntuBaseVersion:      versions.UbuntuBase.Version,
		GuestKernelVersion:     versions.GuestKernel.Version,
		SubstrateSizeMiB:       substrateSizeMiB,
		SubstrateApparentBytes: rootfsBytes,
		SubstrateUsedBytes:     rootfsUsedBytes,
		SubstrateSHA256:        rootfsDigest,
		KernelSHA256:           kernelDigest,
		KernelBytes:            kernelBytes,
		SBOMSHA256:             sbomDigest,
		SBOMBytes:              sbomBytes,
		PackageCount:           packageCount,
		InitSHA256:             initDigest,
		InitBytes:              initBytes,
		VMGuestTelemetrySHA256: telemetryDigest,
		VMGuestTelemetryBytes:  telemetryBytes,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeFile(filepath.Join(outputDir, "manifest.json"), string(manifestBytes)+"\n", 0o644); err != nil {
		return err
	}

	fmt.Printf("OK substrate built: %s\n", substratePath)
	fmt.Printf("OK SBOM written:    %s\n", sbomPath)
	fmt.Printf("OK manifest:        %s\n", filepath.Join(outputDir, "manifest.json"))
	return nil
}

type versionsFile struct {
	UbuntuBase struct {
		Version string `json:"version"`
	} `json:"ubuntu_base"`
	GuestKernel struct {
		Version string `json:"version"`
	} `json:"guest_kernel"`
}

type substrateManifest struct {
	SchemaVersion          uint8  `json:"schema_version"`
	BuiltAtUTC             string `json:"built_at_utc"`
	UbuntuBaseVersion      string `json:"ubuntu_base_version"`
	GuestKernelVersion     string `json:"guest_kernel_version"`
	SubstrateSizeMiB       int    `json:"substrate_size_mib"`
	SubstrateApparentBytes int64  `json:"substrate_apparent_bytes"`
	SubstrateUsedBytes     int64  `json:"substrate_used_bytes"`
	SubstrateSHA256        string `json:"substrate_sha256"`
	KernelSHA256           string `json:"kernel_sha256"`
	KernelBytes            int64  `json:"kernel_bytes"`
	SBOMSHA256             string `json:"sbom_sha256"`
	SBOMBytes              int64  `json:"sbom_bytes"`
	PackageCount           int    `json:"package_count"`
	InitSHA256             string `json:"init_sha256"`
	InitBytes              int64  `json:"init_bytes"`
	VMGuestTelemetrySHA256 string `json:"vm_guest_telemetry_sha256"`
	VMGuestTelemetryBytes  int64  `json:"vm_guest_telemetry_bytes"`
}

func readVersions(path string) (versionsFile, error) {
	var out versionsFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("read versions: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("parse versions: %w", err)
	}
	if out.UbuntuBase.Version == "" || out.GuestKernel.Version == "" {
		return out, errors.New("versions.json missing ubuntu_base.version or guest_kernel.version")
	}
	return out, nil
}

func requireCommands(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s not in PATH", name)
		}
	}
	return nil
}

func requireFile(name, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("missing input %s at %s: %w", name, path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("input %s at %s is a directory", name, path)
	}
	return nil
}

func validateKernelConfig(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read kernel config: %w", err)
	}
	config := string(raw)
	required := []string{
		"CONFIG_VIRTIO_VSOCKETS=y",
		"CONFIG_HW_RANDOM_VIRTIO=y",
		"# CONFIG_CRYPTO_USER_API_AEAD is not set",
	}
	for _, line := range required {
		if !strings.Contains(config, line+"\n") && !strings.HasSuffix(config, line) {
			return fmt.Errorf("guest kernel config missing %s", line)
		}
	}
	return nil
}

func configureChrootApt(rootfs string) error {
	if err := writeFile(filepath.Join(rootfs, "etc/apt/apt.conf.d/90verself-acquire-timeouts"), aptAcquireConfig, 0o644); err != nil {
		return err
	}
	replacer := strings.NewReplacer(
		"http://archive.ubuntu.com/ubuntu", "https://archive.ubuntu.com/ubuntu",
		"http://security.ubuntu.com/ubuntu", "https://security.ubuntu.com/ubuntu",
	)
	for _, rel := range []string{
		"etc/apt/sources.list",
		"etc/apt/sources.list.d/ubuntu.sources",
	} {
		path := filepath.Join(rootfs, rel)
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		updated := replacer.Replace(string(raw))
		if updated == string(raw) {
			continue
		}
		if err := writeFile(path, updated, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func seedChrootCABundle(rootfs string) error {
	src := "/etc/ssl/certs/ca-certificates.crt"
	if err := requireFile("host CA bundle", src); err != nil {
		return err
	}
	return copyFile(src, filepath.Join(rootfs, "etc/ssl/certs/ca-certificates.crt"), 0o644)
}

func aptGet(args ...string) []string {
	base := []string{
		"apt-get",
		"-o", "Acquire::http::Timeout=5",
		"-o", "Acquire::https::Timeout=5",
		"-o", "Acquire::ftp::Timeout=5",
		"-o", "Acquire::Retries=1",
		"-o", "Acquire::ForceIPv4=true",
		"-o", "Acquire::Queue-Mode=access",
		"-o", "DPkg::Lock::Timeout=60",
	}
	return append(base, args...)
}

type mountStack []string

func (m *mountStack) mountChroot(rootfs string) error {
	for _, dir := range []string{"proc", "sys", "dev", "run"} {
		if err := os.MkdirAll(filepath.Join(rootfs, dir), 0o755); err != nil {
			return fmt.Errorf("create chroot mount %s: %w", dir, err)
		}
	}
	proc := filepath.Join(rootfs, "proc")
	if err := command("mount", "-t", "proc", "proc", proc).run(); err != nil {
		return err
	}
	*m = append(*m, proc)
	for _, dir := range []string{"sys", "dev", "run"} {
		target := filepath.Join(rootfs, dir)
		if err := command("mount", "--rbind", "/"+dir, target).run(); err != nil {
			return err
		}
		*m = append(*m, target)
		if err := command("mount", "--make-rslave", target).run(); err != nil {
			return err
		}
	}
	return nil
}

func (m *mountStack) unmountAll() error {
	var joined error
	for i := len(*m) - 1; i >= 0; i-- {
		if err := command("umount", "-l", (*m)[i]).run(); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	*m = nil
	return joined
}

type commandSpec struct {
	name          string
	args          []string
	discardOutput bool
}

func command(name string, args ...string) commandSpec {
	return commandSpec{name: name, args: args}
}

func (c commandSpec) discardStdout() commandSpec {
	c.discardOutput = true
	return c
}

func (c commandSpec) run() error {
	cmd := exec.Command(c.name, c.args...)
	if c.discardOutput {
		cmd.Stdout = io.Discard
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", c.name, strings.Join(c.args, " "), err)
	}
	return nil
}

func (c commandSpec) output() ([]byte, error) {
	cmd := exec.Command(c.name, c.args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", c.name, strings.Join(c.args, " "), err)
	}
	return out, nil
}

func runChroot(rootfs string, args ...string) error {
	allArgs := append([]string{rootfs, "/usr/bin/env", "DEBIAN_FRONTEND=noninteractive"}, args...)
	return command("chroot", allArgs...).run()
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", dst, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", dst, err)
	}
	return nil
}

func writeFile(path, content string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open append %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	return nil
}

func removeContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func truncateFile(path string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(size); err != nil {
		return fmt.Errorf("truncate %s: %w", path, err)
	}
	return nil
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open digest %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk %s: %w", root, err)
	}
	return total, nil
}

func countNonEmptyLines(raw []byte) int {
	count := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

const nsswitchConf = `passwd:         files systemd
group:          files systemd
shadow:         files systemd
gshadow:        files systemd

hosts:          files dns
networks:       files

protocols:      db files
services:       db files
ethers:         db files
rpc:            db files
`

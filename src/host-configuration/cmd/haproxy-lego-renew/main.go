package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type stringList []string

const maxIntAsUintptr = ^uintptr(0) >> 1

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty value")
	}
	*s = append(*s, value)
	return nil
}

type config struct {
	legoBin              string
	haproxyBin           string
	haproxyConfigs       stringList
	haproxyLDLibraryPath string
	legoPath             string
	email                string
	dnsProvider          string
	dnsPropagationWait   string
	keyType              string
	server               string
	dnsResolvers         stringList
	dnsDisableANS        bool
	certName             string
	domains              stringList
	pemOut               string
	pemGroup             string
	splitCertOut         string
	splitKeyOut          string
	splitGroup           string
	renewDays            int
	reloadUnits          stringList
	restartUnits         stringList
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "haproxy-lego-renew: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("haproxy-lego-renew", flag.ContinueOnError)
	fs.StringVar(&cfg.legoBin, "lego-bin", "/opt/verself/profile/bin/lego", "Path to the lego binary.")
	fs.StringVar(&cfg.haproxyBin, "haproxy-bin", "/opt/verself/profile/bin/haproxy", "Path to the HAProxy binary.")
	fs.Var(&cfg.haproxyConfigs, "haproxy-config", "HAProxy config to validate; repeat in HAProxy load order.")
	fs.StringVar(&cfg.haproxyLDLibraryPath, "haproxy-ld-library-path", "/opt/aws-lc/lib/x86_64-linux-gnu", "LD_LIBRARY_PATH used when invoking HAProxy.")
	fs.StringVar(&cfg.legoPath, "lego-path", "/var/lib/lego", "lego state directory.")
	fs.StringVar(&cfg.email, "email", "", "ACME account email.")
	fs.StringVar(&cfg.dnsProvider, "dns", "cloudflare", "lego DNS provider code.")
	fs.StringVar(&cfg.dnsPropagationWait, "dns-propagation-wait", "0s", "Fixed lego DNS-01 propagation wait; set to 0s to use lego's propagation checker.")
	fs.Var(&cfg.dnsResolvers, "dns-resolver", "Recursive DNS resolver used by lego; repeatable host:port values.")
	fs.BoolVar(&cfg.dnsDisableANS, "dns-propagation-disable-ans", false, "Disable lego authoritative nameserver propagation checks.")
	fs.StringVar(&cfg.keyType, "key-type", "ec256", "lego private key type.")
	fs.StringVar(&cfg.server, "server", "", "Optional ACME directory URL.")
	fs.StringVar(&cfg.certName, "cert-name", "", "lego certificate filename stem; defaults to the first domain.")
	fs.Var(&cfg.domains, "domain", "Certificate domain; repeat for SANs.")
	fs.StringVar(&cfg.pemOut, "pem-out", "", "HAProxy PEM output path.")
	fs.StringVar(&cfg.pemGroup, "pem-group", "haproxy", "Group for the HAProxy PEM file.")
	fs.StringVar(&cfg.splitCertOut, "split-cert-out", "", "Optional split fullchain output path.")
	fs.StringVar(&cfg.splitKeyOut, "split-key-out", "", "Optional split private key output path.")
	fs.StringVar(&cfg.splitGroup, "split-group", "stalwart", "Group for split cert/key outputs.")
	fs.IntVar(&cfg.renewDays, "days", 45, "Renew when the certificate has this many days remaining.")
	fs.Var(&cfg.reloadUnits, "reload-unit", "systemd unit to reload after a valid certificate swap; repeatable.")
	fs.Var(&cfg.restartUnits, "restart-unit", "systemd unit to restart after a valid certificate swap; repeatable.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(cfg.domains) == 0 {
		return errors.New("at least one --domain is required")
	}
	if cfg.email == "" {
		return errors.New("--email is required")
	}
	if cfg.pemOut == "" {
		return errors.New("--pem-out is required")
	}
	if cfg.certName == "" {
		cfg.certName = cfg.domains[0]
	}
	if len(cfg.haproxyConfigs) == 0 {
		cfg.haproxyConfigs = append(cfg.haproxyConfigs, "/etc/haproxy/haproxy.cfg")
	}
	if err := requireCloudflareCredential(cfg.dnsProvider); err != nil {
		return err
	}
	// Deploy-time issuance and the renewal timer share lego state, so a process lock serializes ACME mutations.
	unlock, err := lockLegoState(cfg.legoPath)
	if err != nil {
		return err
	}
	defer unlock()

	certPath := filepath.Join(cfg.legoPath, "certificates", cfg.certName+".crt")
	keyPath := filepath.Join(cfg.legoPath, "certificates", cfg.certName+".key")
	if err := runLego(cfg, certPath, keyPath); err != nil {
		return err
	}

	keyPEM, certPEM, err := readAndValidateMaterial(keyPath, certPath, cfg.domains)
	if err != nil {
		return err
	}
	combined := append(append([]byte{}, keyPEM...), certPEM...)
	pemChanged, err := atomicWriteWithValidatedHAProxy(cfg, cfg.pemOut, combined, cfg.pemGroup)
	if err != nil {
		return err
	}
	splitChanged := false
	if cfg.splitCertOut != "" || cfg.splitKeyOut != "" {
		if cfg.splitCertOut == "" || cfg.splitKeyOut == "" {
			return errors.New("--split-cert-out and --split-key-out must be provided together")
		}
		changed, err := atomicWriteIfChanged(cfg.splitCertOut, certPEM, cfg.splitGroup)
		if err != nil {
			return err
		}
		splitChanged = splitChanged || changed
		changed, err = atomicWriteIfChanged(cfg.splitKeyOut, keyPEM, cfg.splitGroup)
		if err != nil {
			return err
		}
		splitChanged = splitChanged || changed
	}
	if pemChanged {
		for _, unit := range cfg.reloadUnits {
			if err := systemctlIfActive("reload", unit); err != nil {
				return err
			}
		}
	}
	if pemChanged || splitChanged {
		for _, unit := range cfg.restartUnits {
			if err := systemctlIfActive("restart", unit); err != nil {
				return err
			}
		}
	}
	fmt.Printf("haproxy-lego-renew: pem_changed=%t split_changed=%t\n", pemChanged, splitChanged)
	return nil
}

func requireCloudflareCredential(provider string) error {
	if provider != "cloudflare" {
		return nil
	}
	if os.Getenv("CF_DNS_API_TOKEN") != "" || os.Getenv("CF_DNS_API_TOKEN_FILE") != "" ||
		os.Getenv("CLOUDFLARE_DNS_API_TOKEN") != "" || os.Getenv("CLOUDFLARE_DNS_API_TOKEN_FILE") != "" {
		return nil
	}
	return errors.New("cloudflare DNS-01 requires CF_DNS_API_TOKEN or CF_DNS_API_TOKEN_FILE")
}

func lockLegoState(path string) (func(), error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	lockPath := filepath.Join(path, ".haproxy-lego-renew.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", lockPath, err)
	}
	fd, err := fileDescriptor(f, lockPath)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", lockPath, err)
	}
	return func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func fileDescriptor(f *os.File, path string) (int, error) {
	fd := f.Fd()
	if fd > maxIntAsUintptr {
		return 0, fmt.Errorf("file descriptor for %s exceeds int range: %d", path, fd)
	}
	return int(fd), nil // #nosec G115 -- fd is checked against the platform int range above.
}

func runLego(cfg config, certPath, keyPath string) error {
	command := "renew"
	certExists := false
	keyExists := false
	if _, err := os.Stat(certPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", certPath, err)
		}
		command = "run"
	} else {
		certExists = true
	}
	if _, err := os.Stat(keyPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", keyPath, err)
		}
		command = "run"
	} else {
		keyExists = true
	}
	if certExists && keyExists {
		if shouldSkipRenewal(cfg, certPath, keyPath) {
			return nil
		}
	}

	argv := []string{
		"--path", cfg.legoPath,
		"--email", cfg.email,
		"--accept-tos",
		"--dns", cfg.dnsProvider,
		"--key-type", cfg.keyType,
		"--pem",
	}
	for _, resolver := range cfg.dnsResolvers {
		argv = append(argv, "--dns.resolvers", resolver)
	}
	// Cloudflare can publish the TXT record while lego's propagation checker still waits; a fixed wait lets ACME verify the visible record.
	if cfg.dnsPropagationWait != "" && cfg.dnsPropagationWait != "0" && cfg.dnsPropagationWait != "0s" {
		argv = append(argv, "--dns.propagation-wait", cfg.dnsPropagationWait)
	} else if cfg.dnsDisableANS {
		argv = append(argv, "--dns.propagation-disable-ans")
	}
	if cfg.server != "" {
		argv = append(argv, "--server", cfg.server)
	}
	for _, domain := range cfg.domains {
		argv = append(argv, "--domains", domain)
	}
	argv = append(argv, command)
	if command == "renew" {
		// Lego 4.35 may sleep for the ACME ARI renewal window; convergence must be deterministic.
		argv = append(argv, "--days", strconv.Itoa(cfg.renewDays), "--force-cert-domains", "--no-random-sleep")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.legoBin, argv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lego %s %s: %w", cfg.certName, command, err)
	}
	return nil
}

func shouldSkipRenewal(cfg config, certPath, keyPath string) bool {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil || !containsPrivateKey(keyPEM) {
		return false
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	leaf, err := firstCertificate(certPEM)
	if err != nil {
		return false
	}
	for _, domain := range cfg.domains {
		if err := verifyCertificateDomain(leaf, domain); err != nil {
			return false
		}
	}
	return time.Until(leaf.NotAfter) > time.Duration(cfg.renewDays)*24*time.Hour
}

func readAndValidateMaterial(keyPath, certPath string, domains []string) ([]byte, []byte, error) {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", keyPath, err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", certPath, err)
	}
	if !containsPrivateKey(keyPEM) {
		return nil, nil, fmt.Errorf("%s does not contain a PEM private key", keyPath)
	}
	leaf, err := firstCertificate(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", certPath, err)
	}
	for _, domain := range domains {
		if err := verifyCertificateDomain(leaf, domain); err != nil {
			return nil, nil, fmt.Errorf("certificate %s does not cover %s: %w", certPath, domain, err)
		}
	}
	if !bytes.HasSuffix(keyPEM, []byte("\n")) {
		keyPEM = append(keyPEM, '\n')
	}
	if !bytes.HasSuffix(certPEM, []byte("\n")) {
		certPEM = append(certPEM, '\n')
	}
	return keyPEM, certPEM, nil
}

func verifyCertificateDomain(cert *x509.Certificate, domain string) error {
	if strings.HasPrefix(domain, "*.") {
		for _, dnsName := range cert.DNSNames {
			if strings.EqualFold(dnsName, domain) {
				return nil
			}
		}
		return fmt.Errorf("wildcard SAN %q is absent", domain)
	}
	return cert.VerifyHostname(domain)
}

func containsPrivateKey(b []byte) bool {
	for {
		block, rest := pem.Decode(b)
		if block == nil {
			return false
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			return true
		}
		b = rest
	}
}

func firstCertificate(b []byte) (*x509.Certificate, error) {
	for {
		block, rest := pem.Decode(b)
		if block == nil {
			return nil, errors.New("no CERTIFICATE PEM block found")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		b = rest
	}
}

func atomicWriteWithValidatedHAProxy(cfg config, path string, content []byte, group string) (bool, error) {
	oldContent, oldErr := os.ReadFile(path)
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return false, fmt.Errorf("read existing %s: %w", path, oldErr)
	}
	if oldErr == nil && bytes.Equal(oldContent, content) {
		return false, nil
	}
	if err := atomicWrite(path, content, group); err != nil {
		return false, err
	}
	if err := validateHAProxy(cfg); err != nil {
		if oldErr == nil {
			if restoreErr := atomicWrite(path, oldContent, group); restoreErr != nil {
				return false, fmt.Errorf("haproxy validation failed after writing %s: %w; rollback failed: %v", path, err, restoreErr)
			}
		} else if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return false, fmt.Errorf("haproxy validation failed after writing %s: %w; cleanup failed: %v", path, err, removeErr)
		}
		return false, fmt.Errorf("haproxy validation failed after writing %s; previous material restored: %w", path, err)
	}
	return true, nil
}

func atomicWriteIfChanged(path string, content []byte, group string) (bool, error) {
	oldContent, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read existing %s: %w", path, err)
	}
	if err == nil && bytes.Equal(oldContent, content) {
		return false, nil
	}
	if err := atomicWrite(path, content, group); err != nil {
		return false, err
	}
	return true, nil
}

func atomicWrite(path string, content []byte, group string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if group != "" {
		gid, err := groupID(group)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Chown(0, gid); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chown %s root:%s: %w", tmpName, group, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func groupID(group string) (int, error) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", group, err)
	}
	return gid, nil
}

func validateHAProxy(cfg config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := []string{"-c"}
	for _, config := range cfg.haproxyConfigs {
		argv = append(argv, "-f", config)
	}
	cmd := exec.CommandContext(ctx, cfg.haproxyBin, argv...)
	cmd.Env = withLDLibraryPath(os.Environ(), cfg.haproxyLDLibraryPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", cfg.haproxyBin, strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func withLDLibraryPath(env []string, path string) []string {
	if path == "" {
		return env
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") {
			env[i] = kv + ":" + path
			return env
		}
	}
	return append(env, "LD_LIBRARY_PATH="+path)
}

func systemctlIfActive(action, unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).Run(); err != nil {
		return nil
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", action, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w: %s", action, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

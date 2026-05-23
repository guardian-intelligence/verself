package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultSite      = "prod"
	defaultKeyRel    = ".config/sops/age/keys.txt"
	passwordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

var agePublicKeyPattern = regexp.MustCompile(`age1[ac-hj-np-z02-9]+`)

type config struct {
	repoRoot     string
	site         string
	ageKeygenBin string
	sopsBin      string
	ageKeyFile   string
}

type secretSeed struct {
	Key    string
	Value  string
	Secret bool
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "sops-init: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := commandVersion(ctx, cfg.ageKeygenBin, "-version"); err != nil {
		return err
	}
	if err := commandVersion(ctx, cfg.sopsBin, "--version"); err != nil {
		return err
	}
	if err := ensureAgeKey(ctx, cfg.ageKeygenBin, cfg.ageKeyFile); err != nil {
		return err
	}
	recipient, err := ageRecipient(cfg.ageKeyFile)
	if err != nil {
		return err
	}
	if err := writeSOPSConfig(cfg.repoRoot, recipient); err != nil {
		return err
	}
	secretPath := filepath.Join(cfg.repoRoot, "src", "host", "sites", cfg.site, "secrets", "host.sops.yml")
	if _, err := os.Stat(secretPath); err == nil {
		fmt.Printf("sops-init: %s already exists\n", repoRel(cfg.repoRoot, secretPath))
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", secretPath, err)
	}
	body, err := hostSecretsYAML()
	if err != nil {
		return err
	}
	if err := writeAndEncryptHostSecrets(ctx, cfg.sopsBin, secretPath, body); err != nil {
		return err
	}
	fmt.Printf("sops-init: wrote %s\n", repoRel(cfg.repoRoot, secretPath))
	return nil
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("sops-init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := config{site: defaultSite}
	fs.StringVar(&cfg.repoRoot, "repo-root", "", "Workspace root.")
	fs.StringVar(&cfg.site, "site", cfg.site, "Site name.")
	fs.StringVar(&cfg.ageKeygenBin, "age-keygen-bin", "age-keygen", "age-keygen executable.")
	fs.StringVar(&cfg.sopsBin, "sops-bin", "sops", "sops executable.")
	fs.StringVar(&cfg.ageKeyFile, "age-key-file", "", "Age key file. Defaults to SOPS_AGE_KEY_FILE or ~/.config/sops/age/keys.txt.")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if cfg.ageKeyFile == "" {
		cfg.ageKeyFile = os.Getenv("SOPS_AGE_KEY_FILE")
	}
	if cfg.ageKeyFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config{}, fmt.Errorf("resolve home directory: %w", err)
		}
		cfg.ageKeyFile = filepath.Join(home, defaultKeyRel)
	}
	return cfg, nil
}

func (cfg *config) validate() error {
	if cfg.repoRoot == "" {
		return errors.New("--repo-root is required")
	}
	root, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return fmt.Errorf("resolve --repo-root: %w", err)
	}
	cfg.repoRoot = root
	if cfg.site == "" {
		return errors.New("--site is required")
	}
	if cfg.ageKeygenBin == "" {
		return errors.New("--age-keygen-bin is required")
	}
	if cfg.sopsBin == "" {
		return errors.New("--sops-bin is required")
	}
	if cfg.ageKeyFile == "" {
		return errors.New("age key file is required")
	}
	return nil
}

func commandVersion(ctx context.Context, bin string, arg string) error {
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(versionCtx, bin, arg)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", bin, arg, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func ensureAgeKey(ctx context.Context, ageKeygenBin, ageKeyFile string) error {
	if _, err := os.Stat(ageKeyFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", ageKeyFile, err)
	}
	if err := os.MkdirAll(filepath.Dir(ageKeyFile), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(ageKeyFile), err)
	}
	keyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(keyCtx, ageKeygenBin, "-o", ageKeyFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s -o %s: %w: %s", ageKeygenBin, ageKeyFile, err, strings.TrimSpace(stderr.String()))
	}
	if err := os.Chmod(ageKeyFile, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", ageKeyFile, err)
	}
	return nil
}

func ageRecipient(ageKeyFile string) (string, error) {
	body, err := os.ReadFile(ageKeyFile)
	if err != nil {
		return "", fmt.Errorf("read age key file: %w", err)
	}
	recipient := agePublicKeyPattern.Find(body)
	if len(recipient) == 0 {
		return "", fmt.Errorf("%s does not contain an Age public recipient", ageKeyFile)
	}
	return string(recipient), nil
}

func writeSOPSConfig(repoRoot, recipient string) error {
	path := filepath.Join(repoRoot, ".sops.yaml")
	body := []byte(fmt.Sprintf(`---
creation_rules:
  - path_regex: \.sops\.yml$
    age: %s
`, recipient))
	return writeFileAtomic(path, body, 0o644)
}

func hostSecretsYAML() ([]byte, error) {
	generated := []string{
		"postgresql_admin_password",
		"postgresql_billing_password",
		"zitadel_db_password",
		"zitadel_masterkey",
		"zitadel_admin_password",
		"postgresql_sandbox_rental_password",
		"postgresql_iam_service_password",
	}
	values := map[string]string{}
	for _, key := range generated {
		value, err := randomToken(32)
		if err != nil {
			return nil, err
		}
		if key == "zitadel_admin_password" {
			value += "#@F1"
		}
		values[key] = value
	}
	seeds := []secretSeed{
		{Key: "cloudflare_api_token"},
		{Key: "latitudesh_auth_token"},
		{Key: "postgresql_admin_password", Value: values["postgresql_admin_password"], Secret: true},
		{Key: "postgresql_billing_password", Value: values["postgresql_billing_password"], Secret: true},
		{Key: "zitadel_db_password", Value: values["zitadel_db_password"], Secret: true},
		{Key: "zitadel_masterkey", Value: values["zitadel_masterkey"], Secret: true},
		{Key: "zitadel_admin_password", Value: values["zitadel_admin_password"], Secret: true},
		{Key: "zitadel_admin_pat"},
		{Key: "stripe_secret_key"},
		{Key: "stripe_webhook_secret"},
		{Key: "resend_api_key"},
		{Key: "github_integration_service_github_app_private_key"},
		{Key: "github_integration_service_github_app_webhook_secret"},
		{Key: "postgresql_sandbox_rental_password", Value: values["postgresql_sandbox_rental_password"], Secret: true},
		{Key: "postgresql_iam_service_password", Value: values["postgresql_iam_service_password"], Secret: true},
	}
	var out strings.Builder
	out.WriteString("---\n")
	for _, seed := range seeds {
		out.WriteString(seed.Key)
		out.WriteString(": ")
		if seed.Secret {
			out.WriteString(seed.Value)
		}
		out.WriteString("\n")
	}
	return []byte(out.String()), nil
}

func writeAndEncryptHostSecrets(ctx context.Context, sopsBin, path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := writeFileAtomic(path, body, 0o600); err != nil {
		return err
	}
	encryptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(encryptCtx, sopsBin, "encrypt", "-i", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("%s encrypt -i %s: %w: %s", sopsBin, path, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func randomToken(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("random token length must be positive")
	}
	max := big.NewInt(int64(len(passwordAlphabet)))
	var out strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("random token: %w", err)
		}
		out.WriteByte(passwordAlphabet[n.Int64()])
	}
	return out.String(), nil
}

func writeFileAtomic(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func repoRel(repoRoot, path string) string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

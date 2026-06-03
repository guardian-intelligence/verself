// Command cloudflare-r2-control-plane serves site-local deployment artifact
// upload sessions over scoped Cloudflare R2 publisher credentials.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
)

type config struct {
	action                    string
	repoRoot                  string
	site                      string
	accountID                 string
	bucket                    string
	keyPrefix                 string
	region                    string
	credentialSource          string
	parentAccessKeyIDFile     string
	parentSecretAccessKeyFile string
	parentSessionTokenFile    string
	listenAddr                string
	authTokenFile             string
	testPrefix                string
	inventoryPrefix           string
	inventoryDepth            int
	tempTTL                   time.Duration
	uploadSessionTTL          time.Duration
	timeout                   time.Duration
	verifyTempCredentials     bool
}

type report struct {
	Timestamp                    string                  `json:"timestamp"`
	Action                       string                  `json:"action"`
	Site                         string                  `json:"site"`
	AccountID                    string                  `json:"account_id"`
	Endpoint                     string                  `json:"endpoint"`
	Bucket                       string                  `json:"bucket"`
	ParentCredentialSource       string                  `json:"parent_credential_source"`
	ParentAccessKeyIDFingerprint string                  `json:"parent_access_key_id_fingerprint"`
	VerifiedWith                 string                  `json:"verified_with"`
	TempCredentialTTLSeconds     int64                   `json:"temp_credential_ttl_seconds,omitempty"`
	TempCredentialPrefix         string                  `json:"temp_credential_prefix,omitempty"`
	TestObjectKey                string                  `json:"test_object_key,omitempty"`
	TestObjectSHA256             string                  `json:"test_object_sha256,omitempty"`
	TestObjectHeadStatus         int                     `json:"test_object_head_status,omitempty"`
	TestObjectGetStatus          int                     `json:"test_object_get_status,omitempty"`
	PrefixIsolationProbeStatus   int                     `json:"prefix_isolation_probe_status,omitempty"`
	Inventory                    []inventoryPrefixReport `json:"inventory,omitempty"`
}

type inventoryPrefixReport struct {
	Prefix     string `json:"prefix"`
	Objects    int    `json:"objects"`
	TotalBytes int64  `json:"total_bytes"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cloudflare-r2-control-plane: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("cloudflare-r2-control-plane", flag.ContinueOnError)
	fs.StringVar(&cfg.action, "action", "verify", "Action: serve, inventory, or verify.")
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "Repository root for loading Cloudflare account config and src/host/sites/<site>/site.json.")
	fs.StringVar(&cfg.site, "site", "prod", "Deployment site.")
	fs.StringVar(&cfg.accountID, "account-id", "", "Cloudflare account ID. Defaults to src/integrations/cloudflare/account.json.")
	fs.StringVar(&cfg.bucket, "bucket", "", "R2 bucket name. Defaults to account.json r2.deployment_artifacts_bucket.")
	fs.StringVar(&cfg.keyPrefix, "key-prefix", "sha256", "R2 artifact key prefix.")
	fs.StringVar(&cfg.region, "region", "auto", "R2 S3 signing region.")
	fs.StringVar(&cfg.credentialSource, "credential-source", "files", "Scoped R2 credential source: files.")
	fs.StringVar(&cfg.parentAccessKeyIDFile, "parent-access-key-id-file", "", "File containing the R2 publisher token ID.")
	fs.StringVar(&cfg.parentSecretAccessKeyFile, "parent-secret-access-key-file", "", "File containing the R2 publisher S3 secret.")
	fs.StringVar(&cfg.parentSessionTokenFile, "parent-session-token-file", "", "Optional file containing the R2 publisher session token.")
	fs.StringVar(&cfg.listenAddr, "listen", "127.0.0.1:18732", "HTTP listen address for --action=serve.")
	fs.StringVar(&cfg.authTokenFile, "auth-token-file", "", "File containing the --action=serve bearer token.")
	fs.StringVar(&cfg.testPrefix, "test-prefix", "control-plane-verification/", "R2 object prefix used for live verification.")
	fs.StringVar(&cfg.inventoryPrefix, "inventory-prefix", "", "R2 object prefix for --action=inventory.")
	fs.IntVar(&cfg.inventoryDepth, "inventory-depth", 2, "Prefix depth for --action=inventory summaries.")
	fs.DurationVar(&cfg.tempTTL, "temp-ttl", 15*time.Minute, "TTL for Cloudflare temporary scoped R2 verification credentials.")
	fs.DurationVar(&cfg.uploadSessionTTL, "upload-session-ttl", 30*time.Minute, "TTL for deployment artifact upload sessions.")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Total timeout for Cloudflare R2 calls.")
	fs.BoolVar(&cfg.verifyTempCredentials, "verify-temp-credentials", true, "Mint scoped temporary credentials and use them for the object verification.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.applySiteDefaults(); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if cfg.action == "serve" {
		ctx, cancel = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	} else {
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
	}
	defer cancel()

	parent, err := r2control.LoadParentCredentials(ctx, cfg.parentCredentialConfig())
	if err != nil {
		return err
	}
	if cfg.action == "serve" {
		return serveUploadAPI(ctx, cfg, parent)
	}
	parentClient, err := r2Client(cfg, parent, "cloudflare-r2-control-plane-parent")
	if err != nil {
		return err
	}
	if cfg.action == "inventory" {
		objects, err := parentClient.ListObjectsV2(ctx, cfg.bucket, cfg.inventoryPrefix)
		if err != nil {
			return err
		}
		return writeReport(baseReport(cfg, parent, "list-objects-v2", summarizeInventory(objects, cfg.inventoryDepth)))
	}
	return verifyScopedR2(ctx, cfg, parent, parentClient)
}

func (cfg *config) applySiteDefaults() error {
	if cfg.site == "" {
		return nil
	}
	repoRoot, err := resolveRepoRoot(cfg.repoRoot)
	if err != nil {
		return err
	}
	cfg.repoRoot = repoRoot
	if cfg.accountID != "" && cfg.bucket != "" {
		return nil
	}
	siteCfg, err := loadSiteConfig(cfg.repoRoot, cfg.site)
	if err != nil {
		return err
	}
	if cfg.accountID == "" {
		cfg.accountID = siteCfg.AccountID
	}
	if cfg.bucket == "" {
		cfg.bucket = siteCfg.Bucket
	}
	if cfg.region == "" {
		cfg.region = siteCfg.Region
	}
	if cfg.keyPrefix == "" {
		cfg.keyPrefix = siteCfg.KeyPrefix
	}
	return nil
}

func resolveRepoRoot(raw string) (string, error) {
	if raw == "" {
		raw = "."
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}
	if workspace := strings.TrimSpace(os.Getenv("BUILD_WORKSPACE_DIRECTORY")); workspace != "" {
		return filepath.Join(workspace, raw), nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	return abs, nil
}

func (cfg config) validate() error {
	switch cfg.action {
	case "serve", "inventory", "verify":
	default:
		return fmt.Errorf("--action must be serve, inventory, or verify, got %q", cfg.action)
	}
	if !r2control.IsCloudflareAccountID(cfg.accountID) {
		return fmt.Errorf("--account-id must be a 32-character lowercase hex Cloudflare account ID")
	}
	if !r2control.IsR2BucketName(cfg.bucket) {
		return fmt.Errorf("--bucket must be a valid lowercase R2 bucket name")
	}
	if strings.TrimSpace(cfg.region) == "" {
		return fmt.Errorf("--region is required")
	}
	if strings.Trim(strings.TrimSpace(cfg.keyPrefix), "/") == "" {
		return fmt.Errorf("--key-prefix is required")
	}
	if cfg.tempTTL < time.Minute || cfg.tempTTL > 7*24*time.Hour {
		return fmt.Errorf("--temp-ttl must be between 1 minute and 7 days")
	}
	if cfg.uploadSessionTTL < time.Minute || cfg.uploadSessionTTL > 7*24*time.Hour {
		return fmt.Errorf("--upload-session-ttl must be between 1 minute and 7 days")
	}
	if cfg.inventoryDepth < 1 || cfg.inventoryDepth > 8 {
		return fmt.Errorf("--inventory-depth must be between 1 and 8")
	}
	if cfg.action == "serve" && strings.TrimSpace(cfg.authTokenFile) == "" {
		return fmt.Errorf("--auth-token-file is required for action=serve")
	}
	switch cfg.credentialSource {
	case r2control.ParentCredentialSourceFiles:
	default:
		return fmt.Errorf("--credential-source must be files")
	}
	return nil
}

func (cfg config) parentCredentialConfig() r2control.ParentCredentialConfig {
	return r2control.ParentCredentialConfig{
		Source:              cfg.credentialSource,
		AccountID:           cfg.accountID,
		AccessKeyIDFile:     cfg.parentAccessKeyIDFile,
		SecretAccessKeyFile: cfg.parentSecretAccessKeyFile,
		SessionTokenFile:    cfg.parentSessionTokenFile,
		Timeout:             cfg.timeout,
	}
}

func verifyScopedR2(ctx context.Context, cfg config, parent r2control.ParentCredentials, parentClient *r2control.R2Client) error {
	out := baseReport(cfg, parent, "", nil)
	if cfg.verifyTempCredentials {
		temp, err := temporaryCredentials(ctx, cfg, parent, r2control.TemporaryPermissionObjectReadWrite, []string{normalizedPrefix(cfg.testPrefix)}, nil, cfg.tempTTL)
		if err != nil {
			return err
		}
		tempClient, err := r2Client(cfg, temp, "cloudflare-r2-control-plane-verification")
		if err != nil {
			return err
		}
		if err := verifyObjectRoundTrip(ctx, tempClient, cfg, "temporary-credential", &out); err != nil {
			return err
		}
		out.TempCredentialTTLSeconds = int64(cfg.tempTTL / time.Second)
		out.TempCredentialPrefix = normalizedPrefix(cfg.testPrefix)
		deniedStatus, err := tempClient.HeadObject(ctx, cfg.bucket, prefixIsolationDeniedKey(cfg.site, cfg.testPrefix))
		if err != nil {
			return err
		}
		out.PrefixIsolationProbeStatus = deniedStatus
		if deniedStatus != http.StatusForbidden {
			return fmt.Errorf("temporary credential prefix isolation probe returned status %d, expected 403", deniedStatus)
		}
		return writeReport(out)
	}
	if err := verifyObjectRoundTrip(ctx, parentClient, cfg, "parent", &out); err != nil {
		return err
	}
	return writeReport(out)
}

func temporaryCredentials(_ context.Context, cfg config, parent r2control.ParentCredentials, permission string, prefixes, objects []string, ttl time.Duration) (r2control.ParentCredentials, error) {
	temp, err := r2control.CreateLocalTemporaryCredentials(r2control.Endpoint(cfg.accountID), cfg.accountID, parent.SecretAccessKey, r2control.TemporaryCredentialRequest{
		ParentAccessKeyID: parent.AccessKeyID,
		Bucket:            cfg.bucket,
		Permission:        permission,
		Prefixes:          prefixes,
		Objects:           objects,
		TTL:               ttl,
	})
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	return r2control.ParentCredentials{
		AccessKeyID:     temp.AccessKeyID,
		SecretAccessKey: temp.SecretAccessKey,
		SessionToken:    temp.SessionToken,
		Source:          "cloudflare-r2-temp-credential",
	}, nil
}

func r2Client(cfg config, creds r2control.ParentCredentials, source string) (*r2control.R2Client, error) {
	return r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
		Source:          source,
		Timeout:         cfg.timeout,
	})
}

func baseReport(cfg config, parent r2control.ParentCredentials, verifiedWith string, inventory []inventoryPrefixReport) report {
	return report{
		Timestamp:                    time.Now().UTC().Format(time.RFC3339),
		Action:                       cfg.action,
		Site:                         cfg.site,
		AccountID:                    cfg.accountID,
		Endpoint:                     r2control.Endpoint(cfg.accountID),
		Bucket:                       cfg.bucket,
		ParentCredentialSource:       parent.Source,
		ParentAccessKeyIDFingerprint: r2control.Fingerprint(parent.AccessKeyID),
		VerifiedWith:                 verifiedWith,
		Inventory:                    inventory,
	}
}

func summarizeInventory(objects []r2control.ObjectSummary, depth int) []inventoryPrefixReport {
	byPrefix := map[string]inventoryPrefixReport{}
	for _, object := range objects {
		prefix := inventoryPrefix(object.Key, depth)
		entry := byPrefix[prefix]
		entry.Prefix = prefix
		entry.Objects++
		entry.TotalBytes += object.Size
		byPrefix[prefix] = entry
	}
	out := make([]inventoryPrefixReport, 0, len(byPrefix))
	for _, entry := range byPrefix {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

func inventoryPrefix(key string, depth int) string {
	if depth < 1 {
		depth = 1
	}
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if len(parts) < depth {
		depth = len(parts)
	}
	return strings.Join(parts[:depth], "/") + "/"
}

func verifyObjectRoundTrip(ctx context.Context, client *r2control.R2Client, cfg config, verifiedWith string, out *report) error {
	key, body, err := verificationObject(cfg.site, cfg.testPrefix)
	if err != nil {
		return err
	}
	digest := r2control.SHA256Hex(body)
	if status, err := client.PutObject(ctx, cfg.bucket, key, bytes.NewReader(body), digest); err != nil {
		return err
	} else if status < 200 || status >= 300 {
		return fmt.Errorf("put verification object returned status %d", status)
	}
	headStatus, err := client.HeadObject(ctx, cfg.bucket, key)
	if err != nil {
		return err
	}
	if headStatus != http.StatusOK {
		return fmt.Errorf("head verification object returned status %d", headStatus)
	}
	getStatus, got, err := client.GetObject(ctx, cfg.bucket, key)
	if err != nil {
		return err
	}
	if getStatus != http.StatusOK {
		return fmt.Errorf("get verification object returned status %d", getStatus)
	}
	if !bytes.Equal(got, body) {
		return fmt.Errorf("verification object body mismatch")
	}
	out.VerifiedWith = verifiedWith
	out.TestObjectKey = key
	out.TestObjectSHA256 = digest
	out.TestObjectHeadStatus = headStatus
	out.TestObjectGetStatus = getStatus
	return nil
}

func verificationObject(site, prefix string) (string, []byte, error) {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", nil, fmt.Errorf("random verification nonce: %w", err)
	}
	key := normalizedPrefix(prefix) + site + "-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce[:]) + ".txt"
	body := []byte("verself cloudflare-r2-control-plane verification\nsite=" + site + "\nkey=" + key + "\n")
	return key, body, nil
}

func normalizedPrefix(prefix string) string {
	prefix = strings.TrimLeft(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

func prefixIsolationDeniedKey(site, allowedPrefix string) string {
	site = strings.TrimSpace(site)
	if site == "" {
		site = "unknown"
	}
	return "prefix-isolation-denied/" + site + "/" + strings.TrimPrefix(normalizedPrefix(allowedPrefix), "prefix-isolation-denied/")
}

func writeReport(out report) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

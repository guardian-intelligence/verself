// Command cloudflare-r2-control-plane owns controller-side R2 bucket
// provisioning and scoped artifact credential verification.
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
	"path/filepath"
	"strings"
	"time"
)

type config struct {
	action                   string
	repoRoot                 string
	site                     string
	accountID                string
	bucket                   string
	region                   string
	credentialSource         string
	credentialsFile          string
	parentAccessKeyIDEnv     string
	parentSecretAccessKeyEnv string
	parentAPITokenEnv        string
	parentSessionTokenEnv    string
	openBaoAddr              string
	openBaoPath              string
	openBaoTokenEnv          string
	openBaoTokenFile         string
	testPrefix               string
	tempTTL                  time.Duration
	timeout                  time.Duration
	verifyTempCredentials    bool
}

type report struct {
	Timestamp                    string `json:"timestamp"`
	Action                       string `json:"action"`
	Site                         string `json:"site"`
	AccountID                    string `json:"account_id"`
	Endpoint                     string `json:"endpoint"`
	Bucket                       string `json:"bucket"`
	ParentCredentialSource       string `json:"parent_credential_source"`
	ParentAccessKeyIDFingerprint string `json:"parent_access_key_id_fingerprint"`
	BucketExisted                bool   `json:"bucket_existed"`
	BucketCreated                bool   `json:"bucket_created"`
	VerifiedWith                 string `json:"verified_with"`
	TempCredentialTTLSeconds     int64  `json:"temp_credential_ttl_seconds,omitempty"`
	TempCredentialPrefix         string `json:"temp_credential_prefix,omitempty"`
	TestObjectKey                string `json:"test_object_key,omitempty"`
	TestObjectSHA256             string `json:"test_object_sha256,omitempty"`
	TestObjectHeadStatus         int    `json:"test_object_head_status,omitempty"`
	TestObjectGetStatus          int    `json:"test_object_get_status,omitempty"`
	PrefixIsolationProbeStatus   int    `json:"prefix_isolation_probe_status,omitempty"`
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
	fs.StringVar(&cfg.action, "action", "verify", "Action: verify or ensure-bucket.")
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "Repository root for loading src/host/sites/<site>/site.json.")
	fs.StringVar(&cfg.site, "site", "prod", "Deployment site.")
	fs.StringVar(&cfg.accountID, "account-id", "", "Cloudflare account ID. Defaults to site.json artifact_delivery.cloudflare_account_id.")
	fs.StringVar(&cfg.bucket, "bucket", "", "R2 bucket name. Defaults to site.json artifact_delivery.bucket.")
	fs.StringVar(&cfg.region, "region", "auto", "R2 S3 signing region.")
	fs.StringVar(&cfg.credentialSource, "credential-source", "auto", "Credential source: auto, env, env-file, or openbao.")
	fs.StringVar(&cfg.credentialsFile, "credentials-file", "", "Environment file containing the parent R2 credentials.")
	fs.StringVar(&cfg.parentAccessKeyIDEnv, "parent-access-key-id-env", "CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID", "Environment variable name for the parent R2 access key ID.")
	fs.StringVar(&cfg.parentSecretAccessKeyEnv, "parent-secret-access-key-env", "CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY", "Environment variable name for the parent R2 secret access key.")
	fs.StringVar(&cfg.parentAPITokenEnv, "parent-api-token-env", "CLOUDFLARE_R2_ADMIN_API_TOKEN", "Environment variable name for the parent R2 API token value; the S3 secret is derived from this value.")
	fs.StringVar(&cfg.parentSessionTokenEnv, "parent-session-token-env", "CLOUDFLARE_R2_ADMIN_SESSION_TOKEN", "Environment variable name for an optional parent R2 session token.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", "", "Controller OpenBao address. Defaults to BAO_ADDR or VAULT_ADDR.")
	fs.StringVar(&cfg.openBaoPath, "openbao-path", "kv-controller/data/integrations/cloudflare/r2-admin", "Controller OpenBao KV path for parent R2 credentials.")
	fs.StringVar(&cfg.openBaoTokenEnv, "openbao-token-env", "BAO_TOKEN", "Environment variable name for the OpenBao token.")
	fs.StringVar(&cfg.openBaoTokenFile, "openbao-token-file", "", "File containing the OpenBao token.")
	fs.StringVar(&cfg.testPrefix, "test-prefix", "control-plane-verification/", "R2 object prefix used for live verification.")
	fs.DurationVar(&cfg.tempTTL, "temp-ttl", 15*time.Minute, "TTL for locally minted scoped R2 verification credentials.")
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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	parent, err := loadParentCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	parentClient, err := newR2Client(r2ClientConfig{
		Endpoint:        r2Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     parent.AccessKeyID,
		SecretAccessKey: parent.SecretAccessKey,
		SessionToken:    parent.SessionToken,
		Source:          "cloudflare-r2-control-plane-parent",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}

	existed, created, err := parentClient.ensureBucket(ctx, cfg.bucket)
	if err != nil {
		return err
	}
	out := report{
		Timestamp:                    time.Now().UTC().Format(time.RFC3339),
		Action:                       cfg.action,
		Site:                         cfg.site,
		AccountID:                    cfg.accountID,
		Endpoint:                     r2Endpoint(cfg.accountID),
		Bucket:                       cfg.bucket,
		ParentCredentialSource:       parent.Source,
		ParentAccessKeyIDFingerprint: fingerprint(parent.AccessKeyID),
		BucketExisted:                existed,
		BucketCreated:                created,
	}
	if cfg.action == "ensure-bucket" {
		return writeReport(out)
	}
	if cfg.verifyTempCredentials {
		if parent.SessionToken != "" {
			return fmt.Errorf("cannot locally mint scoped R2 temporary credentials from a parent credential that is itself temporary")
		}
		temp, err := mintLocalTemporaryCredentials(localTemporaryCredentialInput{
			AccountID:             cfg.accountID,
			Endpoint:              r2Endpoint(cfg.accountID),
			ParentAccessKeyID:     parent.AccessKeyID,
			ParentSecretAccessKey: parent.SecretAccessKey,
			Bucket:                cfg.bucket,
			Scope:                 "object-read-write",
			Actions:               []string{"PutObject", "GetObject", "HeadObject"},
			PrefixPaths:           []string{normalizedPrefix(cfg.testPrefix)},
			TTL:                   cfg.tempTTL,
			Now:                   time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		tempClient, err := newR2Client(r2ClientConfig{
			Endpoint:        r2Endpoint(cfg.accountID),
			Region:          cfg.region,
			AccessKeyID:     temp.AccessKeyID,
			SecretAccessKey: temp.SecretAccessKey,
			SessionToken:    temp.SessionToken,
			Source:          "cloudflare-r2-control-plane-local-temp",
			Timeout:         cfg.timeout,
		})
		if err != nil {
			return err
		}
		if err := verifyObjectRoundTrip(ctx, tempClient, cfg, "local-temp", &out); err != nil {
			return err
		}
		out.TempCredentialTTLSeconds = int64(cfg.tempTTL / time.Second)
		out.TempCredentialPrefix = normalizedPrefix(cfg.testPrefix)
		deniedStatus, err := tempClient.headObject(ctx, cfg.bucket, prefixIsolationDeniedKey(cfg.site, cfg.testPrefix))
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

func (cfg *config) applySiteDefaults() error {
	if cfg.site == "" {
		return nil
	}
	repoRoot, err := resolveRepoRoot(cfg.repoRoot)
	if err != nil {
		return err
	}
	cfg.repoRoot = repoRoot
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
	case "verify", "ensure-bucket":
	default:
		return fmt.Errorf("--action must be verify or ensure-bucket, got %q", cfg.action)
	}
	if !isCloudflareAccountID(cfg.accountID) {
		return fmt.Errorf("--account-id must be a 32-character lowercase hex Cloudflare account ID")
	}
	if !isR2BucketName(cfg.bucket) {
		return fmt.Errorf("--bucket must be a valid lowercase R2 bucket name")
	}
	if strings.TrimSpace(cfg.region) == "" {
		return fmt.Errorf("--region is required")
	}
	if cfg.tempTTL < time.Minute || cfg.tempTTL > 7*24*time.Hour {
		return fmt.Errorf("--temp-ttl must be between 1 minute and 7 days")
	}
	return nil
}

func verifyObjectRoundTrip(ctx context.Context, client *r2Client, cfg config, verifiedWith string, out *report) error {
	key, body, err := verificationObject(cfg.site, cfg.testPrefix)
	if err != nil {
		return err
	}
	digest := sha256Hex(body)
	if status, err := client.putObject(ctx, cfg.bucket, key, bytes.NewReader(body), digest); err != nil {
		return err
	} else if status < 200 || status >= 300 {
		return fmt.Errorf("put verification object returned status %d", status)
	}
	headStatus, err := client.headObject(ctx, cfg.bucket, key)
	if err != nil {
		return err
	}
	if headStatus != http.StatusOK {
		return fmt.Errorf("head verification object returned status %d", headStatus)
	}
	getStatus, got, err := client.getObject(ctx, cfg.bucket, key)
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
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, fmt.Errorf("generate verification nonce: %w", err)
	}
	key := normalizedPrefix(prefix) + site + "/" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce) + ".txt"
	body := []byte("verself cloudflare-r2-control-plane verification\nsite=" + site + "\nkey=" + key + "\n")
	return key, body, nil
}

func normalizedPrefix(prefix string) string {
	prefix = strings.TrimLeft(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "control-plane-verification/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

func prefixIsolationDeniedKey(site, allowedPrefix string) string {
	allowedPrefix = normalizedPrefix(allowedPrefix)
	key := "control-plane-denied/" + site + "/probe.txt"
	if strings.HasPrefix(key, allowedPrefix) {
		key = "outside-" + key
	}
	return key
}

func writeReport(out report) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func r2Endpoint(accountID string) string {
	return "https://" + accountID + ".r2.cloudflarestorage.com"
}

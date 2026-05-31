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

	"github.com/verself/deployment-tools/r2control"
	"gopkg.in/yaml.v3"
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
	getterVarsFile           string
	seedBundleFile           string
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
	GetterCredentialPermission   string `json:"getter_credential_permission,omitempty"`
	GetterCredentialName         string `json:"getter_credential_name,omitempty"`
	GetterAccessKeyIDFingerprint string `json:"getter_access_key_id_fingerprint,omitempty"`
	GetterSecretKeyFingerprint   string `json:"getter_secret_key_fingerprint,omitempty"`
	GetterVarsFile               string `json:"getter_vars_file,omitempty"`
	SeedBundleFile               string `json:"seed_bundle_file,omitempty"`
	GetterObjectGetStatus        int    `json:"getter_object_get_status,omitempty"`
}

type seedBundle struct {
	Version string            `yaml:"version"`
	Site    string            `yaml:"site"`
	Values  map[string]string `yaml:"values"`
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
	fs.StringVar(&cfg.action, "action", "verify", "Action: verify, ensure-bucket, ensure-getter, rotate-getter, or rotate-object-storage-provider.")
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
	fs.StringVar(&cfg.getterVarsFile, "getter-vars-file", "", "JSON vars file to receive the durable Nomad artifact getter keypair.")
	fs.StringVar(&cfg.seedBundleFile, "seed-bundle-file", "", "Seed bundle file to receive generated provider values.")
	fs.StringVar(&cfg.testPrefix, "test-prefix", "control-plane-verification/", "R2 object prefix used for live verification.")
	fs.DurationVar(&cfg.tempTTL, "temp-ttl", 15*time.Minute, "TTL for Cloudflare temporary scoped R2 verification credentials.")
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

	parent, err := r2control.LoadParentCredentials(ctx, cfg.parentCredentialConfig())
	if err != nil {
		return err
	}
	parentClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
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

	existed, created, err := parentClient.EnsureBucket(ctx, cfg.bucket)
	if err != nil {
		return err
	}
	out := report{
		Timestamp:                    time.Now().UTC().Format(time.RFC3339),
		Action:                       cfg.action,
		Site:                         cfg.site,
		AccountID:                    cfg.accountID,
		Endpoint:                     r2control.Endpoint(cfg.accountID),
		Bucket:                       cfg.bucket,
		ParentCredentialSource:       parent.Source,
		ParentAccessKeyIDFingerprint: r2control.Fingerprint(parent.AccessKeyID),
		BucketExisted:                existed,
		BucketCreated:                created,
	}
	if cfg.action == "ensure-bucket" {
		return writeReport(out)
	}
	if cfg.action == "ensure-getter" || cfg.action == "rotate-getter" {
		return provisionGetterCredential(ctx, cfg, parent, parentClient, &out)
	}
	if cfg.action == "rotate-object-storage-provider" {
		return provisionObjectStorageProviderCredential(ctx, cfg, parent, &out)
	}
	if cfg.verifyTempCredentials {
		if parent.SessionToken != "" {
			return fmt.Errorf("cannot create scoped R2 temporary credentials from a parent credential that is itself temporary")
		}
		if strings.TrimSpace(parent.APIToken) == "" {
			return fmt.Errorf("temporary R2 credential verification requires the parent Cloudflare API token value")
		}
		apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
		if err != nil {
			return err
		}
		temp, err := apiClient.CreateTemporaryCredentials(ctx, cfg.accountID, r2control.TemporaryCredentialRequest{
			ParentAccessKeyID: parent.AccessKeyID,
			Bucket:            cfg.bucket,
			Permission:        r2control.TemporaryPermissionObjectReadWrite,
			Prefixes:          []string{normalizedPrefix(cfg.testPrefix)},
			TTL:               cfg.tempTTL,
		})
		if err != nil {
			return err
		}
		tempClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
			Endpoint:        r2control.Endpoint(cfg.accountID),
			Region:          cfg.region,
			AccessKeyID:     temp.AccessKeyID,
			SecretAccessKey: temp.SecretAccessKey,
			SessionToken:    temp.SessionToken,
			Source:          "cloudflare-r2-control-plane-temp",
			Timeout:         cfg.timeout,
		})
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
	case "verify", "ensure-bucket", "ensure-getter", "rotate-getter", "rotate-object-storage-provider":
	default:
		return fmt.Errorf("--action must be verify, ensure-bucket, ensure-getter, rotate-getter, or rotate-object-storage-provider, got %q", cfg.action)
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
	if cfg.tempTTL < time.Minute || cfg.tempTTL > 7*24*time.Hour {
		return fmt.Errorf("--temp-ttl must be between 1 minute and 7 days")
	}
	return nil
}

func (cfg config) parentCredentialConfig() r2control.ParentCredentialConfig {
	return r2control.ParentCredentialConfig{
		Source:             cfg.credentialSource,
		CredentialsFile:    cfg.credentialsFile,
		AccessKeyIDEnv:     cfg.parentAccessKeyIDEnv,
		SecretAccessKeyEnv: cfg.parentSecretAccessKeyEnv,
		APITokenEnv:        cfg.parentAPITokenEnv,
		SessionTokenEnv:    cfg.parentSessionTokenEnv,
		OpenBaoAddr:        cfg.openBaoAddr,
		OpenBaoPath:        cfg.openBaoPath,
		OpenBaoTokenEnv:    cfg.openBaoTokenEnv,
		OpenBaoTokenFile:   cfg.openBaoTokenFile,
		Timeout:            cfg.timeout,
	}
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

func provisionGetterCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, parentClient *r2control.R2Client, out *report) error {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("R2 getter provisioning requires the parent Cloudflare API token value")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	tokenName := "verself-" + cfg.site + "-nomad-artifact-getter-" + time.Now().UTC().Format("20060102T150405Z")
	getter, err := apiClient.CreateR2BucketToken(ctx, cfg.accountID, cfg.bucket, tokenName, r2control.PermissionR2BucketItemRead)
	if err != nil {
		return err
	}
	getterClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     getter.S3AccessKeyID,
		SecretAccessKey: getter.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-getter-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	key, body, err := verificationObject(cfg.site, cfg.testPrefix)
	if err != nil {
		return err
	}
	digest := r2control.SHA256Hex(body)
	if status, err := parentClient.PutObject(ctx, cfg.bucket, key, bytes.NewReader(body), digest); err != nil {
		return err
	} else if status < 200 || status >= 300 {
		return fmt.Errorf("put getter verification object returned status %d", status)
	}
	getStatus, got, err := getterClient.GetObject(ctx, cfg.bucket, key)
	if err != nil {
		return err
	}
	if getStatus != http.StatusOK {
		return fmt.Errorf("getter credential get verification object returned status %d", getStatus)
	}
	if !bytes.Equal(got, body) {
		return fmt.Errorf("getter credential verification object body mismatch")
	}
	varsFile := cfg.getterVarsFile
	seedFile := defaultSeedBundleFile(cfg)
	if err := mergeSeedBundle(seedFile, cfg.site, map[string]string{
		"nomad_artifact_getter_s3_access_key_id":     getter.S3AccessKeyID,
		"nomad_artifact_getter_s3_secret_access_key": getter.S3SecretKey,
	}); err != nil {
		return err
	}
	if strings.TrimSpace(varsFile) != "" {
		if err := writeGetterVars(varsFile, getter); err != nil {
			return err
		}
	}
	out.GetterCredentialPermission = getter.PermissionGroup
	out.GetterCredentialName = getter.Name
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(getter.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(getter.S3SecretKey)
	out.GetterVarsFile = varsFile
	out.SeedBundleFile = seedFile
	out.GetterObjectGetStatus = getStatus
	out.TestObjectKey = key
	out.TestObjectSHA256 = digest
	return writeReport(*out)
}

func provisionObjectStorageProviderCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, out *report) error {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("object-storage R2 provider provisioning requires the parent Cloudflare API token value")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	suffix := time.Now().UTC().Format("20060102T150405Z")
	adminName := "verself-" + cfg.site + "-object-storage-admin-" + suffix
	adminToken, err := apiClient.CreateR2AccountToken(ctx, cfg.accountID, adminName, r2control.PermissionR2StorageWrite)
	if err != nil {
		return err
	}
	proxyName := "verself-" + cfg.site + "-object-storage-proxy-" + suffix
	proxyToken, err := apiClient.CreateR2AllBucketsToken(ctx, cfg.accountID, proxyName, r2control.PermissionR2BucketItemWrite)
	if err != nil {
		return err
	}
	adminClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     adminToken.S3AccessKeyID,
		SecretAccessKey: adminToken.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-object-storage-admin-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if _, _, err := adminClient.EnsureBucket(ctx, cfg.bucket); err != nil {
		return err
	}
	proxyClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     proxyToken.S3AccessKeyID,
		SecretAccessKey: proxyToken.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-object-storage-proxy-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, proxyClient, cfg, "object-storage-proxy", out); err != nil {
		return err
	}
	varsFile := cfg.getterVarsFile
	seedFile := defaultSeedBundleFile(cfg)
	updates := objectStorageVars(adminToken, proxyToken)
	if err := mergeSeedBundle(seedFile, cfg.site, updates); err != nil {
		return err
	}
	if strings.TrimSpace(varsFile) != "" {
		if err := writeObjectStorageVars(varsFile, adminToken, proxyToken); err != nil {
			return err
		}
	}
	out.GetterCredentialPermission = proxyToken.PermissionGroup
	out.GetterCredentialName = proxyToken.Name
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(proxyToken.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(proxyToken.S3SecretKey)
	out.GetterVarsFile = varsFile
	out.SeedBundleFile = seedFile
	out.GetterObjectGetStatus = out.TestObjectGetStatus
	return writeReport(*out)
}

func writeGetterVars(path string, getter r2control.CreatedAPIToken) error {
	return mergeVars(path, map[string]string{
		"nomad_artifact_getter_s3_access_key_id":     getter.S3AccessKeyID,
		"nomad_artifact_getter_s3_secret_access_key": getter.S3SecretKey,
	})
}

func writeObjectStorageVars(path string, adminToken, proxyToken r2control.CreatedAPIToken) error {
	return mergeVars(path, objectStorageVars(adminToken, proxyToken))
}

func objectStorageVars(adminToken, proxyToken r2control.CreatedAPIToken) map[string]string {
	return map[string]string{
		"object_storage_service_r2_admin_access_key_id":     adminToken.S3AccessKeyID,
		"object_storage_service_r2_admin_secret_access_key": adminToken.S3SecretKey,
		"object_storage_service_r2_proxy_access_key_id":     proxyToken.S3AccessKeyID,
		"object_storage_service_r2_proxy_secret_access_key": proxyToken.S3SecretKey,
	}
}

func defaultSeedBundleFile(cfg config) string {
	if strings.TrimSpace(cfg.seedBundleFile) != "" {
		return cfg.seedBundleFile
	}
	return filepath.Join(cfg.repoRoot, ".verself", "site-bootstrap", cfg.site, "seed.yml")
}

func mergeSeedBundle(path, site string, updates map[string]string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("seed bundle file is required")
	}
	bundle := seedBundle{
		Version: "verself.site-bootstrap.seed.v1",
		Site:    site,
		Values:  map[string]string{},
	}
	body, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(body)) > 0 {
		if err := yaml.Unmarshal(body, &bundle); err != nil {
			return fmt.Errorf("decode seed bundle %s: %w", path, err)
		}
		if bundle.Values == nil {
			bundle.Values = map[string]string{}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read seed bundle %s: %w", path, err)
	}
	if strings.TrimSpace(bundle.Site) == "" {
		bundle.Site = site
	}
	if bundle.Site != site {
		return fmt.Errorf("seed bundle %s is for site %q, not %q", path, bundle.Site, site)
	}
	if strings.TrimSpace(bundle.Version) == "" {
		bundle.Version = "verself.site-bootstrap.seed.v1"
	}
	for key, value := range updates {
		bundle.Values[key] = value
	}
	body, err = yaml.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode seed bundle %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create seed bundle directory: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write seed bundle %s: %w", path, err)
	}
	return nil
}

func mergeVars(path string, updates map[string]string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("vars file is required")
	}
	values := map[string]string{}
	body, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &values); err != nil {
			return fmt.Errorf("decode vars file %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read vars file %s: %w", path, err)
	}
	for key, value := range updates {
		values[key] = value
	}
	body, err = json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode vars file %s: %w", path, err)
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create vars directory: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write vars file %s: %w", path, err)
	}
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

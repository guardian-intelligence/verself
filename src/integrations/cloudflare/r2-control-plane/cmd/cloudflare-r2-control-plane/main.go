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
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
	"gopkg.in/yaml.v3"
)

const recoveryBucket = "verself-recovery"

const (
	credentialSourceTokenAdmin        = "token-admin"
	childPersistenceControllerOpenBao = "controller-openbao"
	childPersistenceSiteSeed          = "site-seed"
)

type config struct {
	action                     string
	repoRoot                   string
	site                       string
	accountID                  string
	bucket                     string
	keyPrefix                  string
	region                     string
	credentialSource           string
	credentialsFile            string
	parentAccessKeyIDEnv       string
	parentSecretAccessKeyEnv   string
	parentAPITokenEnv          string
	parentSessionTokenEnv      string
	tokenAdminCredentialSource string
	tokenAdminCredentialsFile  string
	tokenAdminAPITokenEnv      string
	tokenAdminAPITokenFile     string
	tokenAdminOpenBaoPath      string
	tokenAdminAOpenBaoPath     string
	tokenAdminBOpenBaoPath     string
	tokenAdminSlot             string
	openBaoAddr                string
	openBaoPath                string
	openBaoCACertFile          string
	openBaoTokenEnv            string
	openBaoTokenFile           string
	getterVarsFile             string
	seedBundleFile             string
	capabilityOpenBaoPath      string
	childCredentialPersistence string
	listenAddr                 string
	authTokenEnv               string
	testPrefix                 string
	inventoryPrefix            string
	inventoryDepth             int
	tempTTL                    time.Duration
	uploadSessionTTL           time.Duration
	ephemeralPublisherTTL      time.Duration
	childTokenTTL              time.Duration
	tokenAdminTTL              time.Duration
	timeout                    time.Duration
	verifyTempCredentials      bool
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
	BucketExisted                bool                    `json:"bucket_existed"`
	BucketCreated                bool                    `json:"bucket_created"`
	VerifiedWith                 string                  `json:"verified_with"`
	TempCredentialTTLSeconds     int64                   `json:"temp_credential_ttl_seconds,omitempty"`
	TempCredentialPrefix         string                  `json:"temp_credential_prefix,omitempty"`
	TestObjectKey                string                  `json:"test_object_key,omitempty"`
	TestObjectSHA256             string                  `json:"test_object_sha256,omitempty"`
	TestObjectHeadStatus         int                     `json:"test_object_head_status,omitempty"`
	TestObjectGetStatus          int                     `json:"test_object_get_status,omitempty"`
	PrefixIsolationProbeStatus   int                     `json:"prefix_isolation_probe_status,omitempty"`
	GetterCredentialPermission   string                  `json:"getter_credential_permission,omitempty"`
	GetterCredentialName         string                  `json:"getter_credential_name,omitempty"`
	GetterCredentialExpiresOn    string                  `json:"getter_credential_expires_on,omitempty"`
	GetterAccessKeyIDFingerprint string                  `json:"getter_access_key_id_fingerprint,omitempty"`
	GetterSecretKeyFingerprint   string                  `json:"getter_secret_key_fingerprint,omitempty"`
	GetterVarsFile               string                  `json:"getter_vars_file,omitempty"`
	SeedBundleFile               string                  `json:"seed_bundle_file,omitempty"`
	SeedCredentialFingerprints   map[string]string       `json:"seed_credential_fingerprints,omitempty"`
	GetterObjectGetStatus        int                     `json:"getter_object_get_status,omitempty"`
	Inventory                    []inventoryPrefixReport `json:"inventory,omitempty"`
	TokenAdminAStatus            tokenAdminStatus        `json:"token_admin_a_status,omitempty"`
	TokenAdminBStatus            tokenAdminStatus        `json:"token_admin_b_status,omitempty"`
}

type inventoryPrefixReport struct {
	Prefix     string `json:"prefix"`
	Objects    int    `json:"objects"`
	TotalBytes int64  `json:"total_bytes"`
}

type tokenAdminStatus struct {
	TokenIDFingerprint string `json:"token_id_fingerprint,omitempty"`
	Status             string `json:"status,omitempty"`
	ExpiresOn          string `json:"expires_on,omitempty"`
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
	fs.StringVar(&cfg.action, "action", "verify", "Action: serve, inventory, verify, import-token-admin, rotate-token-admin-pair, ensure-bucket, provision-site-bootstrap, ensure-getter, rotate-getter, ensure-publisher, rotate-publisher, ensure-recovery, rotate-recovery, or rotate-object-storage-provider.")
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "Repository root for loading src/host/sites/<site>/site.json.")
	fs.StringVar(&cfg.site, "site", "prod", "Deployment site.")
	fs.StringVar(&cfg.accountID, "account-id", "", "Cloudflare account ID. Defaults to site.json artifact_delivery.cloudflare_account_id.")
	fs.StringVar(&cfg.bucket, "bucket", "", "R2 bucket name. Defaults to site.json artifact_delivery.bucket.")
	fs.StringVar(&cfg.keyPrefix, "key-prefix", "sha256", "R2 artifact key prefix.")
	fs.StringVar(&cfg.region, "region", "auto", "R2 S3 signing region.")
	fs.StringVar(&cfg.credentialSource, "credential-source", "openbao", "Credential source: openbao, env, env-file, auto, or token-admin for bootstrap-only serving.")
	fs.StringVar(&cfg.credentialsFile, "credentials-file", "", "Environment file containing R2 credentials.")
	fs.StringVar(&cfg.parentAccessKeyIDEnv, "parent-access-key-id-env", "CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID", "Environment variable name for the R2 access key ID.")
	fs.StringVar(&cfg.parentSecretAccessKeyEnv, "parent-secret-access-key-env", "CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY", "Environment variable name for the R2 secret access key.")
	fs.StringVar(&cfg.parentAPITokenEnv, "parent-api-token-env", "CLOUDFLARE_R2_ADMIN_API_TOKEN", "Environment variable name for the R2 API token value.")
	fs.StringVar(&cfg.parentSessionTokenEnv, "parent-session-token-env", "CLOUDFLARE_R2_ADMIN_SESSION_TOKEN", "Environment variable name for an optional R2 session token.")
	fs.StringVar(&cfg.tokenAdminCredentialSource, "token-admin-credential-source", "", "Optional token-admin credential source: openbao, env, env-file, or auto. Defaults to the active R2 credential.")
	fs.StringVar(&cfg.tokenAdminCredentialsFile, "token-admin-credentials-file", "", "Optional environment file containing the Cloudflare token-admin credential.")
	fs.StringVar(&cfg.tokenAdminAPITokenEnv, "token-admin-api-token-env", "CLOUDFLARE_TOKEN_ADMIN_API_TOKEN", "Environment variable name for a Cloudflare API token with Account API Tokens Write.")
	fs.StringVar(&cfg.tokenAdminAPITokenFile, "token-admin-api-token-file", "", "File containing a Cloudflare token-admin API token value.")
	fs.StringVar(&cfg.tokenAdminOpenBaoPath, "token-admin-openbao-path", "", "Controller OpenBao KV path for the Cloudflare token-admin credential.")
	fs.StringVar(&cfg.tokenAdminAOpenBaoPath, "token-admin-a-openbao-path", "kv-controller/data/integrations/cloudflare/token-admin/a", "Controller OpenBao KV path for Cloudflare token-admin A.")
	fs.StringVar(&cfg.tokenAdminBOpenBaoPath, "token-admin-b-openbao-path", "kv-controller/data/integrations/cloudflare/token-admin/b", "Controller OpenBao KV path for Cloudflare token-admin B.")
	fs.StringVar(&cfg.tokenAdminSlot, "token-admin-slot", "", "Token-admin slot for action=import-token-admin: a or b.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", "", "Controller OpenBao address. Defaults to BAO_ADDR or VAULT_ADDR.")
	fs.StringVar(&cfg.openBaoPath, "openbao-path", "kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher", "Controller OpenBao KV path for R2 credentials.")
	fs.StringVar(&cfg.openBaoCACertFile, "openbao-ca-cert", "", "Controller OpenBao CA certificate file. Defaults to BAO_CACERT or VAULT_CACERT.")
	fs.StringVar(&cfg.openBaoTokenEnv, "openbao-token-env", "BAO_TOKEN", "Environment variable name for the OpenBao token.")
	fs.StringVar(&cfg.openBaoTokenFile, "openbao-token-file", "", "File containing the OpenBao token.")
	fs.StringVar(&cfg.getterVarsFile, "getter-vars-file", "", "JSON vars file to receive the durable Nomad artifact getter keypair.")
	fs.StringVar(&cfg.seedBundleFile, "seed-bundle-file", "", "Seed bundle file to receive generated provider values.")
	fs.StringVar(&cfg.capabilityOpenBaoPath, "capability-openbao-path", "", "Controller OpenBao KV path for generated capability credentials.")
	fs.StringVar(&cfg.childCredentialPersistence, "child-credential-persistence", "", "Persistence for generated child credentials: controller-openbao or site-seed. Defaults by action.")
	fs.StringVar(&cfg.listenAddr, "listen", "127.0.0.1:18732", "HTTP listen address for --action=serve.")
	fs.StringVar(&cfg.authTokenEnv, "auth-token-env", "VERSELF_R2_CONTROL_PLANE_TOKEN", "Environment variable containing the --action=serve bearer token.")
	fs.StringVar(&cfg.testPrefix, "test-prefix", "control-plane-verification/", "R2 object prefix used for live verification.")
	fs.StringVar(&cfg.inventoryPrefix, "inventory-prefix", "", "R2 object prefix for --action=inventory.")
	fs.IntVar(&cfg.inventoryDepth, "inventory-depth", 2, "Prefix depth for --action=inventory summaries.")
	fs.DurationVar(&cfg.tempTTL, "temp-ttl", 15*time.Minute, "TTL for Cloudflare temporary scoped R2 verification credentials.")
	fs.DurationVar(&cfg.uploadSessionTTL, "upload-session-ttl", 30*time.Minute, "TTL for deployment artifact upload sessions.")
	fs.DurationVar(&cfg.ephemeralPublisherTTL, "ephemeral-publisher-ttl", time.Hour, "TTL for the bootstrap-only publisher token minted from token-admin credentials.")
	fs.DurationVar(&cfg.childTokenTTL, "child-token-ttl", 7*24*time.Hour, "TTL for generated Cloudflare R2 child API tokens.")
	fs.DurationVar(&cfg.tokenAdminTTL, "token-admin-ttl", 7*24*time.Hour, "TTL for Cloudflare token-admin expiration updates.")
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

	if cfg.action == "import-token-admin" {
		return importTokenAdminCredential(ctx, cfg)
	}
	if cfg.action == "rotate-token-admin-pair" {
		return rotateTokenAdminPair(ctx, cfg)
	}
	if isChildProvisioningAction(cfg.action) {
		return provisionChildCredential(ctx, cfg)
	}

	parent, cleanup, err := loadServeOrParentCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer func() {
			if err := cleanup(); err != nil {
				fmt.Fprintln(os.Stderr, "cloudflare-r2-control-plane: cleanup bootstrap publisher: "+err.Error())
			}
		}()
	}
	if cfg.action == "serve" {
		return serveUploadAPI(ctx, cfg, parent)
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
	if cfg.action == "inventory" {
		status, err := parentClient.HeadBucket(ctx, cfg.bucket)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("head R2 bucket %s returned status %d", cfg.bucket, status)
		}
		objects, err := parentClient.ListObjectsV2(ctx, cfg.bucket, cfg.inventoryPrefix)
		if err != nil {
			return err
		}
		return writeReport(report{
			Timestamp:                    time.Now().UTC().Format(time.RFC3339),
			Action:                       cfg.action,
			Site:                         cfg.site,
			AccountID:                    cfg.accountID,
			Endpoint:                     r2control.Endpoint(cfg.accountID),
			Bucket:                       cfg.bucket,
			ParentCredentialSource:       parent.Source,
			ParentAccessKeyIDFingerprint: r2control.Fingerprint(parent.AccessKeyID),
			BucketExisted:                true,
			VerifiedWith:                 "list-objects-v2",
			Inventory:                    summarizeInventory(objects, cfg.inventoryDepth),
		})
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
		return fmt.Errorf("%s must use token-admin provisioning path", cfg.action)
	}
	if cfg.action == "ensure-publisher" || cfg.action == "rotate-publisher" {
		return fmt.Errorf("%s must use token-admin provisioning path", cfg.action)
	}
	if cfg.action == "ensure-recovery" || cfg.action == "rotate-recovery" {
		return fmt.Errorf("%s must use token-admin provisioning path", cfg.action)
	}
	if cfg.action == "rotate-object-storage-provider" {
		tokenAdmin, err := loadTokenAdminCredentials(ctx, cfg, parent)
		if err != nil {
			return err
		}
		return provisionObjectStorageProviderCredential(ctx, cfg, tokenAdmin, &out)
	}
	if cfg.verifyTempCredentials {
		if parent.SessionToken != "" {
			return fmt.Errorf("cannot create locally signed R2 temporary credentials from a credential that is itself temporary")
		}
		temp, err := r2control.CreateLocalTemporaryCredentials(r2control.Endpoint(cfg.accountID), cfg.accountID, parent.SecretAccessKey, r2control.TemporaryCredentialRequest{
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
	explicitBucket := cfg.bucket != ""
	repoRoot, err := resolveRepoRoot(cfg.repoRoot)
	if err != nil {
		return err
	}
	cfg.repoRoot = repoRoot
	needsSiteDefaults := cfg.accountID == "" || cfg.bucket == ""
	if needsSiteDefaults {
		siteCfg, err := loadSiteConfig(cfg.repoRoot, cfg.site)
		if err != nil {
			return err
		}
		cfg.accountID = siteCfg.AccountID
		cfg.bucket = siteCfg.Bucket
		if cfg.region == "" {
			cfg.region = siteCfg.Region
		}
		if cfg.keyPrefix == "" {
			cfg.keyPrefix = siteCfg.KeyPrefix
		}
	}
	if !explicitBucket && isRecoveryAction(cfg.action) {
		cfg.bucket = recoveryBucket
	}
	return nil
}

func isRecoveryAction(action string) bool {
	return action == "ensure-recovery" || action == "rotate-recovery"
}

func isChildProvisioningAction(action string) bool {
	switch action {
	case "ensure-bucket", "provision-site-bootstrap", "ensure-getter", "rotate-getter", "ensure-publisher", "rotate-publisher", "ensure-recovery", "rotate-recovery", "rotate-object-storage-provider":
		return true
	default:
		return false
	}
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
	case "serve", "inventory", "verify", "import-token-admin", "rotate-token-admin-pair", "ensure-bucket", "provision-site-bootstrap", "ensure-getter", "rotate-getter", "ensure-publisher", "rotate-publisher", "ensure-recovery", "rotate-recovery", "rotate-object-storage-provider":
	default:
		return fmt.Errorf("--action must be serve, inventory, verify, import-token-admin, rotate-token-admin-pair, ensure-bucket, provision-site-bootstrap, ensure-getter, rotate-getter, ensure-publisher, rotate-publisher, ensure-recovery, rotate-recovery, or rotate-object-storage-provider, got %q", cfg.action)
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
	if cfg.ephemeralPublisherTTL < cfg.uploadSessionTTL || cfg.ephemeralPublisherTTL > 7*24*time.Hour {
		return fmt.Errorf("--ephemeral-publisher-ttl must be at least --upload-session-ttl and no more than 7 days")
	}
	if cfg.childTokenTTL <= 0 || cfg.childTokenTTL > 7*24*time.Hour {
		return fmt.Errorf("--child-token-ttl must be greater than zero and no more than 7 days")
	}
	if cfg.tokenAdminTTL <= 0 || cfg.tokenAdminTTL > 7*24*time.Hour {
		return fmt.Errorf("--token-admin-ttl must be greater than zero and no more than 7 days")
	}
	if cfg.inventoryDepth < 1 || cfg.inventoryDepth > 8 {
		return fmt.Errorf("--inventory-depth must be between 1 and 8")
	}
	if cfg.action == "serve" && strings.TrimSpace(os.Getenv(cfg.authTokenEnv)) == "" {
		return fmt.Errorf("--auth-token-env value is required for action=serve")
	}
	switch cfg.effectiveChildCredentialPersistence() {
	case childPersistenceControllerOpenBao, childPersistenceSiteSeed:
	default:
		return fmt.Errorf("--child-credential-persistence must be %s or %s", childPersistenceControllerOpenBao, childPersistenceSiteSeed)
	}
	if cfg.effectiveChildCredentialPersistence() == childPersistenceSiteSeed && isRecoveryAction(cfg.action) {
		return fmt.Errorf("--child-credential-persistence=%s is not valid for recovery credentials", childPersistenceSiteSeed)
	}
	if cfg.action == "provision-site-bootstrap" && cfg.effectiveChildCredentialPersistence() != childPersistenceSiteSeed {
		return fmt.Errorf("--action=provision-site-bootstrap writes the local site seed and requires --child-credential-persistence=%s", childPersistenceSiteSeed)
	}
	if cfg.action == "import-token-admin" {
		if strings.TrimSpace(cfg.tokenAdminAPITokenFile) == "" {
			return fmt.Errorf("--token-admin-api-token-file is required for action=import-token-admin")
		}
		if _, err := tokenAdminOpenBaoPath(cfg, cfg.tokenAdminSlot); err != nil {
			return err
		}
	}
	return nil
}

func (cfg config) effectiveChildCredentialPersistence() string {
	if value := strings.TrimSpace(cfg.childCredentialPersistence); value != "" {
		return value
	}
	switch cfg.action {
	case "provision-site-bootstrap", "ensure-publisher", "rotate-publisher", "ensure-getter", "rotate-getter", "rotate-object-storage-provider":
		return childPersistenceSiteSeed
	default:
		return childPersistenceControllerOpenBao
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
	keys := make([]string, 0, len(byPrefix))
	for key := range byPrefix {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]inventoryPrefixReport, 0, len(keys))
	for _, key := range keys {
		out = append(out, byPrefix[key])
	}
	return out
}

func inventoryPrefix(key string, depth int) string {
	parts := strings.Split(strings.TrimLeft(key, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if len(parts) < depth {
		depth = len(parts)
	}
	return strings.Join(parts[:depth], "/") + "/"
}

func (cfg config) parentCredentialConfig() r2control.ParentCredentialConfig {
	return r2control.ParentCredentialConfig{
		Source:             cfg.credentialSource,
		AccountID:          cfg.accountID,
		CredentialsFile:    cfg.credentialsFile,
		AccessKeyIDEnv:     cfg.parentAccessKeyIDEnv,
		SecretAccessKeyEnv: cfg.parentSecretAccessKeyEnv,
		APITokenEnv:        cfg.parentAPITokenEnv,
		SessionTokenEnv:    cfg.parentSessionTokenEnv,
		OpenBaoAddr:        cfg.openBaoAddr,
		OpenBaoPath:        cfg.openBaoPath,
		OpenBaoCACertFile:  cfg.openBaoCACertFile,
		OpenBaoTokenEnv:    cfg.openBaoTokenEnv,
		OpenBaoTokenFile:   cfg.openBaoTokenFile,
		Timeout:            cfg.timeout,
	}
}

func loadServeOrParentCredentials(ctx context.Context, cfg config) (r2control.ParentCredentials, func() error, error) {
	if cfg.action == "serve" && strings.TrimSpace(cfg.credentialSource) == credentialSourceTokenAdmin {
		publisher, cleanup, err := createEphemeralServePublisher(ctx, cfg)
		return publisher, cleanup, err
	}
	parent, err := r2control.LoadParentCredentials(ctx, cfg.parentCredentialConfig())
	return parent, nil, err
}

func loadTokenAdminCredentials(ctx context.Context, cfg config, parent r2control.ParentCredentials) (r2control.ParentCredentials, error) {
	if !tokenAdminConfigured(cfg) {
		return parent, nil
	}
	adminCfg := cfg.parentCredentialConfig()
	adminCfg.Source = cfg.tokenAdminCredentialSource
	switch {
	case adminCfg.Source != "":
	case cfg.tokenAdminAPITokenFile != "":
		return loadTokenAdminCredentialsFromFile(ctx, cfg)
	case cfg.tokenAdminCredentialsFile != "":
		adminCfg.Source = r2control.ParentCredentialSourceEnvFile
	case strings.TrimSpace(os.Getenv(cfg.tokenAdminAPITokenEnv)) != "":
		adminCfg.Source = r2control.ParentCredentialSourceEnv
	case cfg.tokenAdminOpenBaoPath != "":
		adminCfg.Source = r2control.ParentCredentialSourceOpenBao
	default:
		adminCfg.Source = cfg.credentialSource
	}
	adminCfg.CredentialsFile = cfg.tokenAdminCredentialsFile
	adminCfg.APITokenEnv = cfg.tokenAdminAPITokenEnv
	adminCfg.OpenBaoPath = cfg.tokenAdminOpenBaoPath
	admin, err := r2control.LoadParentCredentials(ctx, adminCfg)
	if err != nil {
		return r2control.ParentCredentials{}, fmt.Errorf("load Cloudflare token-admin credential: %w", err)
	}
	if strings.TrimSpace(admin.APIToken) == "" {
		return r2control.ParentCredentials{}, fmt.Errorf("cloudflare token-admin credential must include api_token")
	}
	return admin, nil
}

func loadTokenAdminCredentialsFromFile(ctx context.Context, cfg config) (r2control.ParentCredentials, error) {
	apiToken, err := readAPITokenFile(cfg.tokenAdminAPITokenFile, "token-admin API token")
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(apiToken, cfg.timeout)
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	verified, err := apiClient.VerifyAccountToken(ctx, cfg.accountID)
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	if verified.Status != "" && verified.Status != "active" {
		return r2control.ParentCredentials{}, fmt.Errorf("cloudflare token-admin status is %q", verified.Status)
	}
	return r2control.ParentCredentials{
		AccessKeyID:     verified.ID,
		SecretAccessKey: r2control.SHA256Hex([]byte(apiToken)),
		APIToken:        apiToken,
		Source:          "file:" + cfg.tokenAdminAPITokenFile,
	}, nil
}

func createEphemeralServePublisher(ctx context.Context, cfg config) (r2control.ParentCredentials, func() error, error) {
	cfg = cfg.withTokenAdminAutoDefaults()
	tokenAdmin, err := loadRequiredTokenAdminCredentials(ctx, cfg)
	if err != nil {
		return r2control.ParentCredentials{}, nil, err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(tokenAdmin.APIToken, cfg.timeout)
	if err != nil {
		return r2control.ParentCredentials{}, nil, err
	}
	_, cleanupAdmin, _, _, err := ensureBucketWithEphemeralAdmin(ctx, cfg, apiClient)
	if err != nil {
		return r2control.ParentCredentials{}, nil, err
	}
	tokenName := "verself-" + cfg.site + "-bootstrap-publisher-" + time.Now().UTC().Format("20060102T150405Z")
	publisher, err := apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, tokenName, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, time.Now().UTC().Add(cfg.ephemeralPublisherTTL))
	if err != nil {
		if cleanupErr := cleanupAdmin(); cleanupErr != nil {
			return r2control.ParentCredentials{}, nil, fmt.Errorf("%w; additionally failed to delete ephemeral R2 admin token: %v", err, cleanupErr)
		}
		return r2control.ParentCredentials{}, nil, err
	}
	cleanup := func() error {
		var errs []string
		if err := apiClient.DeleteAccountToken(context.Background(), cfg.accountID, publisher.ID); err != nil {
			errs = append(errs, "publisher: "+err.Error())
		}
		if err := cleanupAdmin(); err != nil {
			errs = append(errs, "admin: "+err.Error())
		}
		if len(errs) > 0 {
			return fmt.Errorf("%s", strings.Join(errs, "; "))
		}
		return nil
	}
	return r2control.ParentCredentials{
		AccessKeyID:     publisher.S3AccessKeyID,
		SecretAccessKey: publisher.S3SecretKey,
		APIToken:        publisher.Value,
		Source:          "token-admin:bootstrap-ephemeral-publisher",
	}, cleanup, nil
}

func (cfg config) withTokenAdminAutoDefaults() config {
	if strings.TrimSpace(cfg.tokenAdminCredentialsFile) == "" && strings.TrimSpace(cfg.credentialsFile) != "" {
		cfg.tokenAdminCredentialsFile = cfg.credentialsFile
	}
	if strings.TrimSpace(cfg.tokenAdminCredentialSource) != "" {
		return cfg
	}
	switch {
	case strings.TrimSpace(cfg.tokenAdminAPITokenFile) != "":
	case strings.TrimSpace(cfg.tokenAdminCredentialsFile) != "":
		cfg.tokenAdminCredentialSource = r2control.ParentCredentialSourceEnvFile
	case strings.TrimSpace(os.Getenv(cfg.tokenAdminAPITokenEnv)) != "":
		cfg.tokenAdminCredentialSource = r2control.ParentCredentialSourceEnv
	default:
		cfg.tokenAdminCredentialSource = r2control.ParentCredentialSourceOpenBao
		cfg.tokenAdminOpenBaoPath = firstNonEmpty(cfg.tokenAdminOpenBaoPath, cfg.tokenAdminAOpenBaoPath)
	}
	return cfg
}

func tokenAdminConfigured(cfg config) bool {
	if strings.TrimSpace(cfg.tokenAdminCredentialSource) != "" || strings.TrimSpace(cfg.tokenAdminCredentialsFile) != "" || strings.TrimSpace(cfg.tokenAdminAPITokenFile) != "" || strings.TrimSpace(cfg.tokenAdminOpenBaoPath) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv(cfg.tokenAdminAPITokenEnv)) != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func importTokenAdminCredential(ctx context.Context, cfg config) error {
	path, err := tokenAdminOpenBaoPath(cfg, cfg.tokenAdminSlot)
	if err != nil {
		return err
	}
	apiToken, err := readAPITokenFile(cfg.tokenAdminAPITokenFile, "token-admin API token")
	if err != nil {
		return err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(apiToken, cfg.timeout)
	if err != nil {
		return err
	}
	verified, err := apiClient.VerifyAccountToken(ctx, cfg.accountID)
	if err != nil {
		return err
	}
	if verified.Status != "" && verified.Status != "active" {
		return fmt.Errorf("cloudflare token-admin status is %q", verified.Status)
	}
	details, err := apiClient.GetAccountToken(ctx, cfg.accountID, verified.ID)
	if err != nil {
		return fmt.Errorf("verify token-admin management permission: %w", err)
	}
	expiresOn := time.Now().UTC().Add(cfg.tokenAdminTTL)
	updated, err := apiClient.UpdateAccountTokenExpiresOn(ctx, cfg.accountID, details, expiresOn)
	if err != nil {
		return fmt.Errorf("extend token-admin expiration: %w", err)
	}
	if err := writeTokenAdminCredential(ctx, cfg, path, verified.ID, apiToken, firstNonEmpty(updated.ExpiresOn, expiresOn.Format(time.RFC3339))); err != nil {
		return err
	}
	loaded, status, err := loadAndVerifyTokenAdmin(ctx, cfg, path)
	if err != nil {
		return err
	}
	out := baseReport(cfg, loaded.Source)
	out.VerifiedWith = "cloudflare-token-admin-import"
	if cfg.tokenAdminSlot == "a" {
		out.TokenAdminAStatus = status
	} else {
		out.TokenAdminBStatus = status
	}
	return writeReport(out)
}

func rotateTokenAdminPair(ctx context.Context, cfg config) error {
	aPath := cfg.tokenAdminAOpenBaoPath
	bPath := cfg.tokenAdminBOpenBaoPath
	a, _, err := loadAndVerifyTokenAdmin(ctx, cfg, aPath)
	if err != nil {
		return fmt.Errorf("verify token-admin a: %w", err)
	}
	b, _, err := loadAndVerifyTokenAdmin(ctx, cfg, bPath)
	if err != nil {
		return fmt.Errorf("verify token-admin b: %w", err)
	}
	b, bStatus, err := rotateTokenAdminTarget(ctx, cfg, a, b, bPath)
	if err != nil {
		return fmt.Errorf("rotate token-admin b with token-admin a: %w", err)
	}
	_, aStatus, err := rotateTokenAdminTarget(ctx, cfg, b, a, aPath)
	if err != nil {
		return fmt.Errorf("rotate token-admin a with token-admin b: %w", err)
	}
	out := baseReport(cfg, "openbao:token-admin-pair")
	out.VerifiedWith = "cloudflare-token-admin-pair"
	out.TokenAdminAStatus = aStatus
	out.TokenAdminBStatus = bStatus
	return writeReport(out)
}

func rotateTokenAdminTarget(ctx context.Context, cfg config, actor, target r2control.ParentCredentials, targetPath string) (r2control.ParentCredentials, tokenAdminStatus, error) {
	if strings.TrimSpace(actor.APIToken) == "" {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, fmt.Errorf("actor token-admin credential must include api_token")
	}
	if strings.TrimSpace(target.AccessKeyID) == "" {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, fmt.Errorf("target token-admin credential must include token_id")
	}
	actorClient, err := r2control.NewCloudflareAPIClient(actor.APIToken, cfg.timeout)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	details, err := actorClient.GetAccountToken(ctx, cfg.accountID, target.AccessKeyID)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	expiresOn := time.Now().UTC().Add(cfg.tokenAdminTTL)
	updated, err := actorClient.UpdateAccountTokenExpiresOn(ctx, cfg.accountID, details, expiresOn)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	newValue, err := actorClient.RollAccountTokenValue(ctx, cfg.accountID, target.AccessKeyID)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	if err := writeTokenAdminCredential(ctx, cfg, targetPath, target.AccessKeyID, newValue, firstNonEmpty(updated.ExpiresOn, expiresOn.Format(time.RFC3339))); err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	return loadAndVerifyTokenAdmin(ctx, cfg, targetPath)
}

func loadAndVerifyTokenAdmin(ctx context.Context, cfg config, path string) (r2control.ParentCredentials, tokenAdminStatus, error) {
	adminCfg := cfg.parentCredentialConfig()
	adminCfg.Source = r2control.ParentCredentialSourceOpenBao
	adminCfg.OpenBaoPath = path
	admin, err := r2control.LoadParentCredentials(ctx, adminCfg)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	if strings.TrimSpace(admin.APIToken) == "" {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, fmt.Errorf("token-admin credential at %s must include api_token", path)
	}
	apiClient, err := r2control.NewCloudflareAPIClient(admin.APIToken, cfg.timeout)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	verified, err := apiClient.VerifyAccountToken(ctx, cfg.accountID)
	if err != nil {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, err
	}
	if verified.Status != "" && verified.Status != "active" {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, fmt.Errorf("cloudflare token-admin status is %q", verified.Status)
	}
	if admin.AccessKeyID != "" && verified.ID != admin.AccessKeyID {
		return r2control.ParentCredentials{}, tokenAdminStatus{}, fmt.Errorf("cloudflare token-admin verified as %s but OpenBao stored %s", verified.ID, admin.AccessKeyID)
	}
	admin.AccessKeyID = verified.ID
	return admin, tokenAdminStatus{
		TokenIDFingerprint: r2control.Fingerprint(verified.ID),
		Status:             verified.Status,
		ExpiresOn:          verified.ExpiresOn,
	}, nil
}

func writeTokenAdminCredential(ctx context.Context, cfg config, path, tokenID, apiToken, expiresOn string) error {
	writeCfg := cfg.parentCredentialConfig()
	writeCfg.Source = r2control.ParentCredentialSourceOpenBao
	writeCfg.OpenBaoPath = path
	return r2control.WriteParentCredentialsToOpenBao(ctx, writeCfg, map[string]string{
		"api_token":  apiToken,
		"token_id":   tokenID,
		"expires_on": expiresOn,
	})
}

func provisionChildCredential(ctx context.Context, cfg config) error {
	tokenAdmin, err := loadRequiredTokenAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	if isRecoveryAction(cfg.action) {
		if err := preflightOpenBaoPersistence(cfg, "recovery credential persistence"); err != nil {
			return err
		}
	}
	if (cfg.action == "ensure-publisher" || cfg.action == "rotate-publisher") && cfg.effectiveChildCredentialPersistence() == childPersistenceControllerOpenBao {
		if err := preflightOpenBaoPersistence(cfg, "deployment publisher credential persistence"); err != nil {
			return err
		}
	}
	apiClient, err := r2control.NewCloudflareAPIClient(tokenAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	adminClient, cleanup, existed, created, err := ensureBucketWithEphemeralAdmin(ctx, cfg, apiClient)
	if err != nil {
		return err
	}
	out := baseReport(cfg, tokenAdmin.Source)
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(tokenAdmin.AccessKeyID)
	out.BucketExisted = existed
	out.BucketCreated = created
	switch cfg.action {
	case "ensure-bucket":
		err = nil
	case "provision-site-bootstrap":
		err = provisionSiteBootstrapCredentials(ctx, cfg, apiClient, adminClient, &out)
	case "ensure-getter", "rotate-getter":
		err = provisionGetterCredential(ctx, cfg, tokenAdmin, adminClient, &out)
	case "ensure-publisher", "rotate-publisher":
		err = provisionPublisherCredential(ctx, cfg, tokenAdmin, adminClient, &out)
	case "ensure-recovery", "rotate-recovery":
		err = provisionRecoveryCredential(ctx, cfg, tokenAdmin, &out)
	case "rotate-object-storage-provider":
		err = provisionObjectStorageProviderCredential(ctx, cfg, tokenAdmin, &out)
	default:
		err = fmt.Errorf("unsupported child credential action %q", cfg.action)
	}
	if err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return fmt.Errorf("%w; additionally failed to delete ephemeral R2 admin token: %v", err, cleanupErr)
		}
		return err
	}
	if cleanupErr := cleanup(); cleanupErr != nil {
		return fmt.Errorf("delete ephemeral R2 admin token: %w", cleanupErr)
	}
	return writeReport(out)
}

func loadRequiredTokenAdminCredentials(ctx context.Context, cfg config) (r2control.ParentCredentials, error) {
	if !tokenAdminConfigured(cfg) {
		cfg.tokenAdminCredentialSource = r2control.ParentCredentialSourceOpenBao
		cfg.tokenAdminOpenBaoPath = cfg.tokenAdminAOpenBaoPath
	}
	admin, err := loadTokenAdminCredentials(ctx, cfg, r2control.ParentCredentials{})
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	if strings.TrimSpace(admin.APIToken) == "" {
		return r2control.ParentCredentials{}, fmt.Errorf("cloudflare token-admin credential must include api_token")
	}
	return admin, nil
}

func preflightOpenBaoPersistence(cfg config, label string) error {
	if firstNonEmpty(cfg.openBaoAddr, os.Getenv("BAO_ADDR"), os.Getenv("VAULT_ADDR")) == "" {
		return fmt.Errorf("%s requires OpenBao address via --openbao-addr, BAO_ADDR, or VAULT_ADDR", label)
	}
	if _, err := r2control.LoadOpenBaoToken(cfg.parentCredentialConfig()); err != nil {
		return fmt.Errorf("%s preflight failed: %w", label, err)
	}
	return nil
}

func ensureBucketWithEphemeralAdmin(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient) (*r2control.R2Client, func() error, bool, bool, error) {
	name := "verself-r2-ephemeral-admin-" + time.Now().UTC().Format("20060102T150405Z")
	// Cleanup can fail after process death; keep temporary bucket-admin tokens short.
	admin, err := apiClient.CreateR2AccountToken(ctx, cfg.accountID, name, r2control.PermissionR2StorageWrite, time.Now().UTC().Add(cfg.ephemeralPublisherTTL))
	if err != nil {
		return nil, nil, false, false, err
	}
	cleanup := func() error {
		return apiClient.DeleteAccountToken(context.Background(), cfg.accountID, admin.ID)
	}
	adminClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     admin.S3AccessKeyID,
		SecretAccessKey: admin.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-ephemeral-admin",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return nil, nil, false, false, fmt.Errorf("%w; additionally failed to delete ephemeral R2 admin token: %v", err, cleanupErr)
		}
		return nil, nil, false, false, err
	}
	existed, created, err := adminClient.EnsureBucket(ctx, cfg.bucket)
	if err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return nil, nil, false, false, fmt.Errorf("%w; additionally failed to delete ephemeral R2 admin token: %v", err, cleanupErr)
		}
		return nil, nil, false, false, err
	}
	return adminClient, cleanup, existed, created, nil
}

func tokenAdminOpenBaoPath(cfg config, slot string) (string, error) {
	switch strings.TrimSpace(slot) {
	case "a":
		return cfg.tokenAdminAOpenBaoPath, nil
	case "b":
		return cfg.tokenAdminBOpenBaoPath, nil
	default:
		return "", fmt.Errorf("--token-admin-slot must be a or b")
	}
}

func baseReport(cfg config, source string) report {
	return report{
		Timestamp:              time.Now().UTC().Format(time.RFC3339),
		Action:                 cfg.action,
		Site:                   cfg.site,
		AccountID:              cfg.accountID,
		Endpoint:               r2control.Endpoint(cfg.accountID),
		Bucket:                 cfg.bucket,
		ParentCredentialSource: source,
	}
}

func readAPITokenFile(path, label string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s file is required", label)
	}
	var body []byte
	var err error
	if path == "-" {
		body, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	} else {
		body, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("%s is empty", label)
	}
	if strings.ContainsAny(token, "\r\n\t ") {
		return "", fmt.Errorf("%s must be a single token value", label)
	}
	return token, nil
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

func provisionSiteBootstrapCredentials(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient, adminClient *r2control.R2Client, out *report) (err error) {
	suffix := time.Now().UTC().Format("20060102T150405Z")
	expiresOn := time.Now().UTC().Add(cfg.childTokenTTL)
	created := []r2control.CreatedAPIToken{}
	persisted := false
	defer func() {
		if !persisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, created...)
		}
	}()

	publisher, err := apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, "verself-"+cfg.site+"-nomad-artifact-publisher-"+suffix, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, expiresOn)
	if err != nil {
		return err
	}
	created = append(created, publisher)
	publisherClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     publisher.S3AccessKeyID,
		SecretAccessKey: publisher.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-bootstrap-publisher-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, publisherClient, cfg, "bootstrap-publisher", out); err != nil {
		return err
	}

	getter, err := apiClient.CreateR2BucketToken(ctx, cfg.accountID, cfg.bucket, "verself-"+cfg.site+"-nomad-artifact-getter-"+suffix, r2control.PermissionR2BucketItemRead, expiresOn)
	if err != nil {
		return err
	}
	created = append(created, getter)
	getterClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     getter.S3AccessKeyID,
		SecretAccessKey: getter.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-bootstrap-getter-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	getterKey, getterBody, err := verificationObject(cfg.site, cfg.testPrefix)
	if err != nil {
		return err
	}
	getterDigest := r2control.SHA256Hex(getterBody)
	if status, err := adminClient.PutObject(ctx, cfg.bucket, getterKey, bytes.NewReader(getterBody), getterDigest); err != nil {
		return err
	} else if status < 200 || status >= 300 {
		return fmt.Errorf("put bootstrap getter verification object returned status %d", status)
	}
	getStatus, got, err := getterClient.GetObject(ctx, cfg.bucket, getterKey)
	if err != nil {
		return err
	}
	if getStatus != http.StatusOK {
		return fmt.Errorf("bootstrap getter credential get verification object returned status %d", getStatus)
	}
	if !bytes.Equal(got, getterBody) {
		return fmt.Errorf("bootstrap getter credential verification object body mismatch")
	}
	out.GetterObjectGetStatus = getStatus

	objectAdmin, err := apiClient.CreateR2AccountToken(ctx, cfg.accountID, "verself-"+cfg.site+"-object-storage-admin-"+suffix, r2control.PermissionR2StorageWrite, expiresOn)
	if err != nil {
		return err
	}
	created = append(created, objectAdmin)
	objectAdminClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     objectAdmin.S3AccessKeyID,
		SecretAccessKey: objectAdmin.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-bootstrap-object-storage-admin-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if _, _, err := objectAdminClient.EnsureBucket(ctx, cfg.bucket); err != nil {
		return err
	}
	objectProxy, err := apiClient.CreateR2AllBucketsToken(ctx, cfg.accountID, "verself-"+cfg.site+"-object-storage-proxy-"+suffix, r2control.PermissionR2BucketItemWrite, expiresOn)
	if err != nil {
		return err
	}
	created = append(created, objectProxy)
	objectProxyClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     objectProxy.S3AccessKeyID,
		SecretAccessKey: objectProxy.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-bootstrap-object-storage-proxy-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, objectProxyClient, cfg, "bootstrap-object-storage-proxy", out); err != nil {
		return err
	}

	updates := siteBootstrapSeedUpdates(publisher, getter, objectAdmin, objectProxy)
	seedFile := defaultSeedBundleFile(cfg)
	if err := mergeSeedBundle(seedFile, cfg.site, updates); err != nil {
		return err
	}
	persisted = true
	out.SeedBundleFile = seedFile
	out.GetterCredentialPermission = "bootstrap"
	out.GetterCredentialName = "verself-" + cfg.site + "-bootstrap-" + suffix
	out.GetterCredentialExpiresOn = firstNonEmpty(publisher.ExpiresOn, getter.ExpiresOn, objectAdmin.ExpiresOn, objectProxy.ExpiresOn)
	out.SeedCredentialFingerprints = fingerprintMap(updates)
	return nil
}

func provisionGetterCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, writerClient *r2control.R2Client, out *report) (err error) {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("r2 getter provisioning requires the parent cloudflare API token value")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	tokenName := "verself-" + cfg.site + "-nomad-artifact-getter-" + time.Now().UTC().Format("20060102T150405Z")
	getter, err := apiClient.CreateR2BucketToken(ctx, cfg.accountID, cfg.bucket, tokenName, r2control.PermissionR2BucketItemRead, time.Now().UTC().Add(cfg.childTokenTTL))
	if err != nil {
		return err
	}
	persisted := false
	defer func() {
		if !persisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, getter)
		}
	}()
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
	if status, err := writerClient.PutObject(ctx, cfg.bucket, key, bytes.NewReader(body), digest); err != nil {
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
	persisted = true
	if strings.TrimSpace(varsFile) != "" {
		if err := writeGetterVars(varsFile, getter); err != nil {
			return err
		}
	}
	out.GetterCredentialPermission = getter.PermissionGroup
	out.GetterCredentialName = getter.Name
	out.GetterCredentialExpiresOn = getter.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(getter.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(getter.S3SecretKey)
	out.GetterVarsFile = varsFile
	out.SeedBundleFile = seedFile
	out.GetterObjectGetStatus = getStatus
	out.TestObjectKey = key
	out.TestObjectSHA256 = digest
	return nil
}

func provisionPublisherCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, parentClient *r2control.R2Client, out *report) (err error) {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("r2 publisher provisioning requires the parent cloudflare API token value")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	tokenName := "verself-" + cfg.site + "-nomad-artifact-publisher-" + time.Now().UTC().Format("20060102T150405Z")
	publisher, err := apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, tokenName, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, time.Now().UTC().Add(cfg.childTokenTTL))
	if err != nil {
		return err
	}
	persisted := false
	defer func() {
		if !persisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, publisher)
		}
	}()
	publisherClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     publisher.S3AccessKeyID,
		SecretAccessKey: publisher.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-publisher-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if _, _, err := parentClient.EnsureBucket(ctx, cfg.bucket); err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, publisherClient, cfg, "publisher", out); err != nil {
		return err
	}
	seedFile := defaultSeedBundleFile(cfg)
	if err := mergeSeedBundle(seedFile, cfg.site, map[string]string{
		"cloudflare_r2_control_plane_publisher_token_id":  publisher.ID,
		"cloudflare_r2_control_plane_publisher_api_token": publisher.Value,
	}); err != nil {
		return err
	}
	if cfg.effectiveChildCredentialPersistence() == childPersistenceSiteSeed {
		persisted = true
		out.GetterCredentialPermission = publisher.PermissionGroup
		out.GetterCredentialName = publisher.Name
		out.GetterCredentialExpiresOn = publisher.ExpiresOn
		out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(publisher.S3AccessKeyID)
		out.GetterSecretKeyFingerprint = r2control.Fingerprint(publisher.S3SecretKey)
		out.SeedBundleFile = seedFile
		out.GetterObjectGetStatus = out.TestObjectGetStatus
		return nil
	}
	if err := writeCapabilityCredential(ctx, cfg, capabilityOpenBaoPath(cfg, "deployment-publisher"), "deployment-publisher", publisher); err != nil {
		return err
	}
	persisted = true
	out.GetterCredentialPermission = publisher.PermissionGroup
	out.GetterCredentialName = publisher.Name
	out.GetterCredentialExpiresOn = publisher.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(publisher.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(publisher.S3SecretKey)
	out.SeedBundleFile = seedFile
	out.GetterObjectGetStatus = out.TestObjectGetStatus
	return nil
}

func provisionObjectStorageProviderCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, out *report) (err error) {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("object-storage R2 provider provisioning requires the token-admin Cloudflare API token value")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	suffix := time.Now().UTC().Format("20060102T150405Z")
	adminName := "verself-" + cfg.site + "-object-storage-admin-" + suffix
	adminToken, err := apiClient.CreateR2AccountToken(ctx, cfg.accountID, adminName, r2control.PermissionR2StorageWrite, time.Now().UTC().Add(cfg.childTokenTTL))
	if err != nil {
		return err
	}
	persisted := false
	defer func() {
		if !persisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, adminToken)
		}
	}()
	proxyName := "verself-" + cfg.site + "-object-storage-proxy-" + suffix
	proxyToken, err := apiClient.CreateR2AllBucketsToken(ctx, cfg.accountID, proxyName, r2control.PermissionR2BucketItemWrite, time.Now().UTC().Add(cfg.childTokenTTL))
	if err != nil {
		return err
	}
	defer func() {
		if !persisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, proxyToken)
		}
	}()
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
	persisted = true
	if strings.TrimSpace(varsFile) != "" {
		if err := writeObjectStorageVars(varsFile, adminToken, proxyToken); err != nil {
			return err
		}
	}
	out.GetterCredentialPermission = proxyToken.PermissionGroup
	out.GetterCredentialName = proxyToken.Name
	out.GetterCredentialExpiresOn = proxyToken.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(proxyToken.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(proxyToken.S3SecretKey)
	out.GetterVarsFile = varsFile
	out.SeedBundleFile = seedFile
	out.GetterObjectGetStatus = out.TestObjectGetStatus
	return nil
}

func provisionRecoveryCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, out *report) (err error) {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("recovery R2 provisioning requires the token-admin Cloudflare API token value")
	}
	capabilityPath := capabilityOpenBaoPath(cfg, "recovery")
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	tokenName := "verself-recovery-" + time.Now().UTC().Format("20060102T150405Z")
	recovery, err := apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, tokenName, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, time.Now().UTC().Add(cfg.childTokenTTL))
	if err != nil {
		return err
	}
	persisted := false
	defer func() {
		if !persisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, recovery)
		}
	}()
	recoveryClient, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     recovery.S3AccessKeyID,
		SecretAccessKey: recovery.S3SecretKey,
		Source:          "cloudflare-r2-control-plane-recovery-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, recoveryClient, cfg, "recovery", out); err != nil {
		return err
	}
	if err := writeCapabilityCredential(ctx, cfg, capabilityPath, "recovery", recovery); err != nil {
		return err
	}
	persisted = true
	out.GetterCredentialPermission = recovery.PermissionGroup
	out.GetterCredentialName = recovery.Name
	out.GetterCredentialExpiresOn = recovery.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(recovery.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(recovery.S3SecretKey)
	out.GetterObjectGetStatus = out.TestObjectGetStatus
	return nil
}

func capabilityOpenBaoPath(cfg config, capability string) string {
	if strings.TrimSpace(cfg.capabilityOpenBaoPath) != "" {
		return strings.TrimSpace(cfg.capabilityOpenBaoPath)
	}
	return "kv-controller/data/integrations/cloudflare/r2/capabilities/" + capability
}

func writeCapabilityCredential(ctx context.Context, cfg config, path, capability string, token r2control.CreatedAPIToken) error {
	writeCfg := cfg.parentCredentialConfig()
	writeCfg.Source = r2control.ParentCredentialSourceOpenBao
	writeCfg.OpenBaoPath = path
	return r2control.WriteParentCredentialsToOpenBao(ctx, writeCfg, map[string]string{
		"access_key_id":     token.S3AccessKeyID,
		"secret_access_key": token.S3SecretKey,
		"api_token":         token.Value,
		"token_id":          token.ID,
		"expires_on":        token.ExpiresOn,
		"bucket":            token.Bucket,
		"capability":        capability,
		"permission":        token.PermissionGroup,
	})
}

func deleteCreatedTokensOnError(errp *error, apiClient *r2control.CloudflareAPIClient, cfg config, tokens ...r2control.CreatedAPIToken) {
	if errp == nil || *errp == nil {
		return
	}
	cleanupErrors := []string{}
	for _, token := range tokens {
		if strings.TrimSpace(token.ID) == "" {
			continue
		}
		if err := apiClient.DeleteAccountToken(context.Background(), cfg.accountID, token.ID); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
	}
	if len(cleanupErrors) > 0 {
		*errp = fmt.Errorf("%w; additionally failed to delete created Cloudflare tokens: %s", *errp, strings.Join(cleanupErrors, "; "))
	}
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

func siteBootstrapSeedUpdates(publisher, getter, objectAdmin, objectProxy r2control.CreatedAPIToken) map[string]string {
	updates := map[string]string{
		"cloudflare_r2_control_plane_publisher_token_id":  publisher.ID,
		"cloudflare_r2_control_plane_publisher_api_token": publisher.Value,
		"nomad_artifact_getter_s3_access_key_id":          getter.S3AccessKeyID,
		"nomad_artifact_getter_s3_secret_access_key":      getter.S3SecretKey,
	}
	for key, value := range objectStorageVars(objectAdmin, objectProxy) {
		updates[key] = value
	}
	return updates
}

func fingerprintMap(values map[string]string) map[string]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(values))
	for _, key := range keys {
		out[key] = r2control.Fingerprint(values[key])
	}
	return out
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
	if err := writeFileAtomic(path, body, 0o600); err != nil {
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
	if err := writeFileAtomic(path, body, 0o600); err != nil {
		return fmt.Errorf("write vars file %s: %w", path, err)
	}
	return nil
}

func writeFileAtomic(path string, body []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true
	return os.Rename(tmpPath, path)
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

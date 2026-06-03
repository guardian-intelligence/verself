// Command cloudflare-control-plane owns controller-side Cloudflare account
// authority, child credential provisioning, and provider evidence.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/internal/r2control"
	"gopkg.in/yaml.v3"
)

const (
	cloudflareControlPlaneSite = "prod"
	recoveryBucket             = "verself-recovery"
)

const (
	r2CredentialPropagationRetryInterval = 2 * time.Second
	r2CredentialPropagationTimeout       = 45 * time.Second
)

const (
	accountAdminAOpenBaoPathDefault = "kv-controller/data/integrations/cloudflare/account-admin/a"
	accountAdminBOpenBaoPathDefault = "kv-controller/data/integrations/cloudflare/account-admin/b"
	bootstrapPublisherOutputFD      = 3
	bootstrapPublisherTTL           = time.Hour
)

type config struct {
	action                        string
	repoRoot                      string
	site                          string
	accountID                     string
	bucket                        string
	keyPrefix                     string
	region                        string
	accountAdminAOpenBaoPath      string
	accountAdminBOpenBaoPath      string
	openBaoAddr                   string
	openBaoPath                   string
	openBaoCACertFile             string
	openBaoTokenEnv               string
	openBaoTokenFile              string
	runtimeOpenBaoAddr            string
	runtimeOpenBaoCACertFile      string
	runtimeOpenBaoTokenEnv        string
	runtimeOpenBaoTokenFile       string
	dnsInventory                  string
	dnsConcurrency                int
	dryRun                        bool
	certificateProjectionDir      string
	acmeDirectoryURL              string
	acmeContactEmail              string
	acmeDNSPropagationWait        time.Duration
	certificateRenewBefore        time.Duration
	bootstrapVarsFile             string
	testPrefix                    string
	inventoryPrefix               string
	inventoryDepth                int
	tempTTL                       time.Duration
	uploadSessionTTL              time.Duration
	childTokenTTL                 time.Duration
	accountAdminTTL               time.Duration
	timeout                       time.Duration
	verifyTempCredentials         bool
	bootstrapPublisherTokenIDFile string
}

type report struct {
	Timestamp                    string                  `json:"timestamp"`
	Action                       string                  `json:"action"`
	ControlPlaneSite             string                  `json:"cloudflare_control_plane_site"`
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
	DNSZones                     []dnsZoneReport         `json:"dns_zones,omitempty"`
	DNSRecordsSeen               int                     `json:"dns_records_seen,omitempty"`
	DNSRecordsDiffed             int                     `json:"dns_records_diffed,omitempty"`
	DNSRecordsApplied            int                     `json:"dns_records_applied,omitempty"`
	DNSDryRun                    bool                    `json:"dns_dry_run,omitempty"`
	DNSChanges                   []dnsChangeReport       `json:"dns_changes,omitempty"`
	TLSCertificates              []tlsCertificateReport  `json:"tls_certificates,omitempty"`
	GetterCredentialPermission   string                  `json:"getter_credential_permission,omitempty"`
	GetterCredentialName         string                  `json:"getter_credential_name,omitempty"`
	GetterCredentialExpiresOn    string                  `json:"getter_credential_expires_on,omitempty"`
	GetterAccessKeyIDFingerprint string                  `json:"getter_access_key_id_fingerprint,omitempty"`
	GetterSecretKeyFingerprint   string                  `json:"getter_secret_key_fingerprint,omitempty"`
	BootstrapVarsFile            string                  `json:"bootstrap_vars_file,omitempty"`
	BootstrapVarsFingerprints    map[string]string       `json:"bootstrap_vars_fingerprints,omitempty"`
	RuntimeSecretFingerprints    map[string]string       `json:"runtime_secret_fingerprints,omitempty"`
	GetterObjectGetStatus        int                     `json:"getter_object_get_status,omitempty"`
	Inventory                    []inventoryPrefixReport `json:"inventory,omitempty"`
	AccountAdminAStatus          accountAdminStatus      `json:"account_admin_a_status,omitempty"`
	AccountAdminBStatus          accountAdminStatus      `json:"account_admin_b_status,omitempty"`
}

type inventoryPrefixReport struct {
	Prefix     string `json:"prefix"`
	Objects    int    `json:"objects"`
	TotalBytes int64  `json:"total_bytes"`
}

type dnsZoneReport struct {
	Name              string `json:"name"`
	ZoneIDFingerprint string `json:"zone_id_fingerprint"`
}

type dnsChangeReport struct {
	Operation string `json:"operation"`
	Zone      string `json:"zone"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	TTL       int    `json:"ttl"`
	Proxied   bool   `json:"proxied"`
}

type accountAdminStatus struct {
	TokenIDFingerprint string `json:"token_id_fingerprint,omitempty"`
	Status             string `json:"status,omitempty"`
	ExpiresOn          string `json:"expires_on,omitempty"`
}

type bootstrapPublisherCredential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	TokenID         string `json:"token_id"`
	ExpiresOn       string `json:"expires_on"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cloudflare-control-plane: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("cloudflare-control-plane", flag.ContinueOnError)
	fs.StringVar(&cfg.action, "action", "verify-admin-pair", "Action: verify-admin-pair, rotate-admin-pair, verify-dns-authority, reconcile-dns, issue-site-certificates, provision-site, provision-site-bootstrap, mint-bootstrap-publisher, revoke-bootstrap-publisher, ensure-bucket, ensure-getter, rotate-getter, ensure-publisher, rotate-publisher, ensure-recovery, rotate-recovery, rotate-object-storage-provider, inventory, or verify.")
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "Repository root for loading Cloudflare account config and src/host/sites/<site>/site.json.")
	fs.StringVar(&cfg.site, "site", "prod", "Target deployment site. Cloudflare account authority is global and anchored to prod.")
	fs.StringVar(&cfg.accountID, "account-id", "", "Cloudflare account ID. Defaults to src/integrations/cloudflare/account.json.")
	fs.StringVar(&cfg.bucket, "bucket", "", "R2 bucket name. Defaults to account.json r2.deployment_artifacts_bucket.")
	fs.StringVar(&cfg.keyPrefix, "key-prefix", "sha256", "R2 artifact key prefix.")
	fs.StringVar(&cfg.region, "region", "auto", "R2 S3 signing region.")
	fs.StringVar(&cfg.accountAdminAOpenBaoPath, "account-admin-a-openbao-path", accountAdminAOpenBaoPathDefault, "Controller OpenBao KV path for Cloudflare account-admin slot A.")
	fs.StringVar(&cfg.accountAdminBOpenBaoPath, "account-admin-b-openbao-path", accountAdminBOpenBaoPathDefault, "Controller OpenBao KV path for Cloudflare account-admin slot B.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", "", "Controller OpenBao address. Defaults to BAO_ADDR or VAULT_ADDR.")
	fs.StringVar(&cfg.openBaoPath, "openbao-path", "kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher", "Controller OpenBao KV path for R2 credentials.")
	fs.StringVar(&cfg.openBaoCACertFile, "openbao-ca-cert", "", "Controller OpenBao CA certificate file. Defaults to BAO_CACERT or VAULT_CACERT.")
	fs.StringVar(&cfg.openBaoTokenEnv, "openbao-token-env", "BAO_TOKEN", "Environment variable name for the OpenBao token.")
	fs.StringVar(&cfg.openBaoTokenFile, "openbao-token-file", "", "File containing the OpenBao token.")
	fs.StringVar(&cfg.runtimeOpenBaoAddr, "runtime-openbao-addr", "", "Runtime OpenBao address for service-required secret projection. Defaults to --openbao-addr, BAO_ADDR, or VAULT_ADDR.")
	fs.StringVar(&cfg.runtimeOpenBaoCACertFile, "runtime-openbao-ca-cert", "", "Runtime OpenBao CA certificate file. Defaults to --openbao-ca-cert, BAO_CACERT, or VAULT_CACERT.")
	fs.StringVar(&cfg.runtimeOpenBaoTokenEnv, "runtime-openbao-token-env", "", "Environment variable name for the runtime OpenBao token. Defaults to --openbao-token-env.")
	fs.StringVar(&cfg.runtimeOpenBaoTokenFile, "runtime-openbao-token-file", "", "File containing the runtime OpenBao token. Defaults to --openbao-token-file.")
	fs.StringVar(&cfg.dnsInventory, "dns-inventory", "", "Path to the site inventory for DNS target IP fallback. Defaults to src/host/sites/<site>/inventory.ini.")
	fs.IntVar(&cfg.dnsConcurrency, "dns-concurrency", 8, "Maximum parallel Cloudflare DNS write requests for --action=reconcile-dns.")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Print and report the DNS diff without applying writes for --action=reconcile-dns.")
	fs.StringVar(&cfg.certificateProjectionDir, "certificate-projection-dir", "", "Local directory to receive HAProxy public certificate PEM projections. Defaults to .verself/site-bootstrap/<site>/tls/haproxy.")
	fs.StringVar(&cfg.acmeDirectoryURL, "acme-directory-url", letsEncryptProductionDirectoryURL, "ACME directory URL for public certificate issuance.")
	fs.StringVar(&cfg.acmeContactEmail, "acme-contact-email", "", "ACME account contact email for --action=issue-site-certificates.")
	fs.DurationVar(&cfg.acmeDNSPropagationWait, "acme-dns-propagation-wait", 2*time.Minute, "Maximum wait for ACME DNS-01 TXT propagation.")
	fs.DurationVar(&cfg.certificateRenewBefore, "certificate-renew-before", 30*24*time.Hour, "Renew projected certificates expiring before this duration.")
	fs.StringVar(&cfg.bootstrapVarsFile, "bootstrap-vars-file", "", "Bootstrap vars JSON file to receive the Nomad artifact getter credential.")
	fs.StringVar(&cfg.testPrefix, "test-prefix", "control-plane-verification/", "R2 object prefix used for live verification.")
	fs.StringVar(&cfg.inventoryPrefix, "inventory-prefix", "", "R2 object prefix for --action=inventory.")
	fs.IntVar(&cfg.inventoryDepth, "inventory-depth", 2, "Prefix depth for --action=inventory summaries.")
	fs.DurationVar(&cfg.tempTTL, "temp-ttl", 15*time.Minute, "TTL for Cloudflare temporary scoped R2 verification credentials.")
	fs.DurationVar(&cfg.uploadSessionTTL, "upload-session-ttl", 30*time.Minute, "TTL for deployment artifact upload sessions.")
	fs.DurationVar(&cfg.childTokenTTL, "child-token-ttl", 7*24*time.Hour, "TTL for generated Cloudflare child API tokens.")
	fs.DurationVar(&cfg.accountAdminTTL, "account-admin-ttl", 7*24*time.Hour, "TTL for Cloudflare account-admin expiration updates.")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Total timeout for Cloudflare R2 calls.")
	fs.BoolVar(&cfg.verifyTempCredentials, "verify-temp-credentials", true, "Mint scoped temporary credentials and use them for the object verification.")
	fs.StringVar(&cfg.bootstrapPublisherTokenIDFile, "bootstrap-publisher-token-id-file", "", "File containing the bootstrap publisher token ID for --action=revoke-bootstrap-publisher.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.applySiteDefaults(); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	timeout := cfg.timeout
	if (cfg.action == "issue-site-certificates" || cfg.action == "provision-site") && timeout < 5*time.Minute {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch cfg.action {
	case "verify-admin-pair":
		return verifyAccountAdminPair(ctx, cfg)
	case "rotate-admin-pair":
		return rotateAccountAdminPair(ctx, cfg)
	case "verify-dns-authority":
		return verifyDNSAuthority(ctx, cfg)
	case "reconcile-dns":
		return reconcileDNS(ctx, cfg)
	case "issue-site-certificates":
		return issueSiteCertificates(ctx, cfg)
	case "provision-site":
		return provisionSite(ctx, cfg)
	}
	if isChildProvisioningAction(cfg.action) {
		return provisionChildCredential(ctx, cfg)
	}
	if cfg.action == "mint-bootstrap-publisher" {
		return mintBootstrapPublisher(ctx, cfg)
	}
	if cfg.action == "revoke-bootstrap-publisher" {
		return revokeBootstrapPublisher(ctx, cfg)
	}

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
		Source:          "cloudflare-control-plane-scoped-r2",
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
			ControlPlaneSite:             cloudflareControlPlaneSite,
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
		ControlPlaneSite:             cloudflareControlPlaneSite,
		Site:                         cfg.site,
		AccountID:                    cfg.accountID,
		Endpoint:                     r2control.Endpoint(cfg.accountID),
		Bucket:                       cfg.bucket,
		ParentCredentialSource:       parent.Source,
		ParentAccessKeyIDFingerprint: r2control.Fingerprint(parent.AccessKeyID),
		BucketExisted:                existed,
		BucketCreated:                created,
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
	case "verify-admin-pair", "rotate-admin-pair", "verify-dns-authority", "reconcile-dns", "issue-site-certificates", "provision-site", "inventory", "verify", "ensure-bucket", "provision-site-bootstrap", "mint-bootstrap-publisher", "revoke-bootstrap-publisher", "ensure-getter", "rotate-getter", "ensure-publisher", "rotate-publisher", "ensure-recovery", "rotate-recovery", "rotate-object-storage-provider":
	default:
		return fmt.Errorf("--action must be verify-admin-pair, rotate-admin-pair, verify-dns-authority, reconcile-dns, issue-site-certificates, provision-site, inventory, verify, ensure-bucket, provision-site-bootstrap, mint-bootstrap-publisher, revoke-bootstrap-publisher, ensure-getter, rotate-getter, ensure-publisher, rotate-publisher, ensure-recovery, rotate-recovery, or rotate-object-storage-provider, got %q", cfg.action)
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
	if cfg.uploadSessionTTL > bootstrapPublisherTTL {
		return fmt.Errorf("--upload-session-ttl must be no more than the bootstrap publisher TTL")
	}
	if cfg.childTokenTTL <= 0 || cfg.childTokenTTL > 7*24*time.Hour {
		return fmt.Errorf("--child-token-ttl must be greater than zero and no more than 7 days")
	}
	if cfg.inventoryDepth < 1 || cfg.inventoryDepth > 8 {
		return fmt.Errorf("--inventory-depth must be between 1 and 8")
	}
	if cfg.dnsConcurrency < 1 || cfg.dnsConcurrency > 64 {
		return fmt.Errorf("--dns-concurrency must be between 1 and 64")
	}
	if cfg.accountAdminTTL <= 0 || cfg.accountAdminTTL > 7*24*time.Hour {
		return fmt.Errorf("--account-admin-ttl must be greater than zero and no more than 7 days")
	}
	if cfg.acmeDNSPropagationWait <= 0 || cfg.acmeDNSPropagationWait > 10*time.Minute {
		return fmt.Errorf("--acme-dns-propagation-wait must be greater than zero and no more than 10 minutes")
	}
	if cfg.certificateRenewBefore <= 0 || cfg.certificateRenewBefore > 90*24*time.Hour {
		return fmt.Errorf("--certificate-renew-before must be greater than zero and no more than 90 days")
	}
	if cfg.action == "issue-site-certificates" || cfg.action == "provision-site" {
		if strings.TrimSpace(cfg.acmeDirectoryURL) == "" {
			return fmt.Errorf("--acme-directory-url is required for certificate issuance")
		}
		if strings.TrimSpace(cfg.acmeContactEmail) == "" {
			return fmt.Errorf("--acme-contact-email is required for certificate issuance")
		}
	}
	return nil
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
		Source:            r2control.ParentCredentialSourceOpenBao,
		AccountID:         cfg.accountID,
		OpenBaoAddr:       cfg.openBaoAddr,
		OpenBaoPath:       cfg.openBaoPath,
		OpenBaoCACertFile: cfg.openBaoCACertFile,
		OpenBaoTokenEnv:   cfg.openBaoTokenEnv,
		OpenBaoTokenFile:  cfg.openBaoTokenFile,
		Timeout:           cfg.timeout,
	}
}

func (cfg config) runtimeOpenBaoCredentialConfig(path string) r2control.ParentCredentialConfig {
	tokenEnv := cfg.runtimeOpenBaoTokenEnv
	if strings.TrimSpace(tokenEnv) == "" {
		tokenEnv = cfg.openBaoTokenEnv
	}
	return r2control.ParentCredentialConfig{
		Source:            r2control.ParentCredentialSourceOpenBao,
		OpenBaoAddr:       firstNonEmpty(cfg.runtimeOpenBaoAddr, cfg.openBaoAddr),
		OpenBaoPath:       path,
		OpenBaoCACertFile: firstNonEmpty(cfg.runtimeOpenBaoCACertFile, cfg.openBaoCACertFile),
		OpenBaoTokenEnv:   tokenEnv,
		OpenBaoTokenFile:  firstNonEmpty(cfg.runtimeOpenBaoTokenFile, cfg.openBaoTokenFile),
		Timeout:           cfg.timeout,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type accountAdminPair struct {
	A       r2control.ParentCredentials
	B       r2control.ParentCredentials
	AStatus accountAdminStatus
	BStatus accountAdminStatus
}

func verifyAccountAdminPair(ctx context.Context, cfg config) error {
	pair, err := loadAndVerifyAccountAdminPair(ctx, cfg)
	if err != nil {
		return err
	}
	out := baseReport(cfg, "controller-openbao:cloudflare-account-admin-pair")
	out.VerifiedWith = "cloudflare-account-admin-pair"
	out.AccountAdminAStatus = pair.AStatus
	out.AccountAdminBStatus = pair.BStatus
	return writeReport(out)
}

func rotateAccountAdminPair(ctx context.Context, cfg config) error {
	pair, err := loadAndVerifyAccountAdminPair(ctx, cfg)
	if err != nil {
		return err
	}
	newB, bStatus, err := rotateAccountAdminTarget(ctx, cfg, pair.A, pair.B, accountAdminBOpenBaoPath(cfg))
	if err != nil {
		return fmt.Errorf("rotate account-admin b with account-admin a: %w", err)
	}
	_, aStatus, err := rotateAccountAdminTarget(ctx, cfg, newB, pair.A, accountAdminAOpenBaoPath(cfg))
	if err != nil {
		return fmt.Errorf("rotate account-admin a with account-admin b: %w", err)
	}
	out := baseReport(cfg, "controller-openbao:cloudflare-account-admin-pair")
	out.VerifiedWith = "cloudflare-account-admin-pair-rotation"
	out.AccountAdminAStatus = aStatus
	out.AccountAdminBStatus = bStatus
	return writeReport(out)
}

func rotateAccountAdminTarget(ctx context.Context, cfg config, actor, target r2control.ParentCredentials, targetPath string) (r2control.ParentCredentials, accountAdminStatus, error) {
	if strings.TrimSpace(actor.APIToken) == "" {
		return r2control.ParentCredentials{}, accountAdminStatus{}, fmt.Errorf("actor account-admin credential must include api_token")
	}
	if strings.TrimSpace(target.AccessKeyID) == "" {
		return r2control.ParentCredentials{}, accountAdminStatus{}, fmt.Errorf("target account-admin credential must include token_id")
	}
	actorClient, err := r2control.NewCloudflareAPIClient(actor.APIToken, cfg.timeout)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	details, err := actorClient.GetAccountToken(ctx, cfg.accountID, target.AccessKeyID)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	expiresOn := time.Now().UTC().Add(cfg.accountAdminTTL)
	updated, err := actorClient.UpdateAccountTokenExpiresOn(ctx, cfg.accountID, details, expiresOn)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	newValue, err := actorClient.RollAccountTokenValue(ctx, cfg.accountID, target.AccessKeyID)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	if err := writeAccountAdminCredential(ctx, cfg, targetPath, target.AccessKeyID, newValue, firstNonEmpty(updated.ExpiresOn, expiresOn.Format(time.RFC3339))); err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	return loadAndVerifyAccountAdmin(ctx, cfg, targetPath)
}

func loadAndVerifyAccountAdminPair(ctx context.Context, cfg config) (accountAdminPair, error) {
	a, aStatus, err := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminAOpenBaoPath(cfg))
	if err != nil {
		return accountAdminPair{}, fmt.Errorf("verify account-admin a: %w", err)
	}
	b, bStatus, err := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminBOpenBaoPath(cfg))
	if err != nil {
		return accountAdminPair{}, fmt.Errorf("verify account-admin b: %w", err)
	}
	if a.AccessKeyID == b.AccessKeyID {
		return accountAdminPair{}, fmt.Errorf("account-admin slots a and b resolve to the same Cloudflare token ID")
	}
	return accountAdminPair{A: a, B: b, AStatus: aStatus, BStatus: bStatus}, nil
}

func provisionSite(ctx context.Context, cfg config) error {
	dnsCfg := cfg
	dnsCfg.action = "reconcile-dns"
	if err := reconcileDNS(ctx, dnsCfg); err != nil {
		return fmt.Errorf("reconcile-dns: %w", err)
	}
	certCfg := cfg
	certCfg.action = "issue-site-certificates"
	if err := issueSiteCertificates(ctx, certCfg); err != nil {
		return fmt.Errorf("issue-site-certificates: %w", err)
	}
	bootstrapCfg := cfg
	bootstrapCfg.action = "provision-site-bootstrap"
	if err := provisionChildCredential(ctx, bootstrapCfg); err != nil {
		return fmt.Errorf("provision-site-bootstrap: %w", err)
	}
	out := baseReport(cfg, "controller-openbao:cloudflare-account-admin-pair")
	out.VerifiedWith = "cloudflare-site-provisioned"
	return writeReport(out)
}

func loadAndVerifyAccountAdmin(ctx context.Context, cfg config, path string) (r2control.ParentCredentials, accountAdminStatus, error) {
	adminCfg := cfg.parentCredentialConfig()
	adminCfg.Source = r2control.ParentCredentialSourceOpenBao
	adminCfg.OpenBaoPath = path
	admin, err := r2control.LoadParentCredentials(ctx, adminCfg)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	if strings.TrimSpace(admin.APIToken) == "" {
		return r2control.ParentCredentials{}, accountAdminStatus{}, fmt.Errorf("account-admin credential at %s must include api_token", path)
	}
	apiClient, err := r2control.NewCloudflareAPIClient(admin.APIToken, cfg.timeout)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	verified, err := apiClient.VerifyAccountToken(ctx, cfg.accountID)
	if err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
	}
	if verified.Status != "" && verified.Status != "active" {
		return r2control.ParentCredentials{}, accountAdminStatus{}, fmt.Errorf("cloudflare account-admin status is %q", verified.Status)
	}
	if admin.AccessKeyID != "" && verified.ID != admin.AccessKeyID {
		return r2control.ParentCredentials{}, accountAdminStatus{}, fmt.Errorf("cloudflare account-admin verified as %s but OpenBao stored %s", verified.ID, admin.AccessKeyID)
	}
	admin.AccessKeyID = verified.ID
	return admin, accountAdminStatus{
		TokenIDFingerprint: r2control.Fingerprint(verified.ID),
		Status:             verified.Status,
		ExpiresOn:          verified.ExpiresOn,
	}, nil
}

func writeAccountAdminCredential(ctx context.Context, cfg config, path, tokenID, apiToken, expiresOn string) error {
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
	accountAdmin, err := loadRequiredAccountAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	if isRecoveryAction(cfg.action) {
		if err := preflightOpenBaoPersistence(cfg.parentCredentialConfig(), "recovery credential persistence"); err != nil {
			return err
		}
	}
	if cfg.action == "ensure-publisher" || cfg.action == "rotate-publisher" {
		if err := preflightOpenBaoPersistence(cfg.parentCredentialConfig(), "deployment publisher capability persistence"); err != nil {
			return err
		}
		if err := preflightOpenBaoPersistence(cfg.runtimeOpenBaoCredentialConfig(runtimeSecretOpenBaoPath("cloudflare-r2-control-plane.publisher_token_id")), "deployment publisher runtime secret projection"); err != nil {
			return err
		}
	}
	if cfg.action == "rotate-object-storage-provider" {
		if err := preflightOpenBaoPersistence(cfg.runtimeOpenBaoCredentialConfig(runtimeSecretOpenBaoPath("object-storage-service.r2.admin_access_key_id")), "object-storage provider runtime secret projection"); err != nil {
			return err
		}
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	if cfg.action == "provision-site-bootstrap" {
		out := baseReport(cfg, accountAdmin.Source)
		out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
		if err := provisionSiteBootstrapCredentials(ctx, cfg, apiClient, &out); err != nil {
			return err
		}
		return writeReport(out)
	}
	existed, created, err := ensureR2BucketWithAccountAdmin(ctx, cfg, apiClient)
	if err != nil {
		return err
	}
	parentClient, err := accountAdminR2Client(cfg, accountAdmin, "cloudflare-r2-control-plane-account-admin-verification")
	if err != nil {
		return err
	}
	out := baseReport(cfg, accountAdmin.Source)
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
	out.BucketExisted = existed
	out.BucketCreated = created
	switch cfg.action {
	case "ensure-bucket":
		err = nil
	case "ensure-getter", "rotate-getter":
		err = provisionGetterCredential(ctx, cfg, accountAdmin, parentClient, &out)
	case "ensure-publisher", "rotate-publisher":
		err = provisionPublisherCredential(ctx, cfg, accountAdmin, parentClient, &out)
	case "ensure-recovery", "rotate-recovery":
		err = provisionRecoveryCredential(ctx, cfg, accountAdmin, &out)
	case "rotate-object-storage-provider":
		err = provisionObjectStorageProviderCredential(ctx, cfg, accountAdmin, &out)
	default:
		err = fmt.Errorf("unsupported child credential action %q", cfg.action)
	}
	if err != nil {
		return err
	}
	return writeReport(out)
}

func loadRequiredAccountAdminCredentials(ctx context.Context, cfg config) (r2control.ParentCredentials, error) {
	pair, err := loadAndVerifyAccountAdminPair(ctx, cfg)
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	if strings.TrimSpace(pair.A.APIToken) == "" {
		return r2control.ParentCredentials{}, fmt.Errorf("cloudflare account-admin credential must include api_token")
	}
	return pair.A, nil
}

func preflightOpenBaoPersistence(openBao r2control.ParentCredentialConfig, label string) error {
	if firstNonEmpty(openBao.OpenBaoAddr, os.Getenv("BAO_ADDR"), os.Getenv("VAULT_ADDR")) == "" {
		return fmt.Errorf("%s requires OpenBao address via flags, BAO_ADDR, or VAULT_ADDR", label)
	}
	if _, err := r2control.LoadOpenBaoToken(openBao); err != nil {
		return fmt.Errorf("%s preflight failed: %w", label, err)
	}
	return nil
}

func ensureR2BucketWithAccountAdmin(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient) (bool, bool, error) {
	var existed bool
	var created bool
	err := retryR2CredentialPropagation(ctx, "ensure R2 bucket with account-admin credential", func() error {
		var ensureErr error
		existed, created, ensureErr = apiClient.EnsureR2Bucket(ctx, cfg.accountID, cfg.bucket)
		return ensureErr
	})
	if err != nil {
		return false, false, err
	}
	return existed, created, nil
}

func accountAdminR2Client(cfg config, parent r2control.ParentCredentials, source string) (*r2control.R2Client, error) {
	return r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     parent.AccessKeyID,
		SecretAccessKey: parent.SecretAccessKey,
		SessionToken:    parent.SessionToken,
		Source:          source,
		Timeout:         cfg.timeout,
	})
}

func accountAdminAOpenBaoPath(cfg config) string {
	return firstNonEmpty(cfg.accountAdminAOpenBaoPath, accountAdminAOpenBaoPathDefault)
}

func accountAdminBOpenBaoPath(cfg config) string {
	return firstNonEmpty(cfg.accountAdminBOpenBaoPath, accountAdminBOpenBaoPathDefault)
}

func baseReport(cfg config, source string) report {
	return report{
		Timestamp:              time.Now().UTC().Format(time.RFC3339),
		Action:                 cfg.action,
		ControlPlaneSite:       cloudflareControlPlaneSite,
		Site:                   cfg.site,
		AccountID:              cfg.accountID,
		Endpoint:               r2control.Endpoint(cfg.accountID),
		Bucket:                 cfg.bucket,
		ParentCredentialSource: source,
	}
}

type r2VerificationStatusError struct {
	operation string
	status    int
}

func (e r2VerificationStatusError) Error() string {
	return fmt.Sprintf("%s returned status %d", e.operation, e.status)
}

func retryableR2CredentialPropagation(err error) bool {
	var apiErr r2control.APIStatusError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden
	}
	var responseErr r2control.StatusError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode == http.StatusUnauthorized || responseErr.StatusCode == http.StatusForbidden
	}
	var statusErr r2VerificationStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusUnauthorized || statusErr.status == http.StatusForbidden
	}
	return false
}

func retryR2CredentialPropagation(ctx context.Context, operation string, fn func() error) error {
	retryCtx, cancel := context.WithTimeout(ctx, r2CredentialPropagationTimeout)
	defer cancel()
	for {
		err := fn()
		if err == nil {
			return nil
		}
		if !retryableR2CredentialPropagation(err) {
			return err
		}
		timer := time.NewTimer(r2CredentialPropagationRetryInterval)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return fmt.Errorf("%s did not complete before timeout: %w", operation, err)
		case <-timer.C:
		}
	}
}

func verifyObjectRoundTrip(ctx context.Context, client *r2control.R2Client, cfg config, verifiedWith string, out *report) error {
	key, body, err := verificationObject(cfg.site, cfg.testPrefix)
	if err != nil {
		return err
	}
	digest := r2control.SHA256Hex(body)
	return retryR2CredentialPropagation(ctx, "verify R2 object credential propagation", func() error {
		return verifyObjectRoundTripOnce(ctx, client, cfg, verifiedWith, out, key, body, digest)
	})
}

func verifyObjectRoundTripOnce(ctx context.Context, client *r2control.R2Client, cfg config, verifiedWith string, out *report, key string, body []byte, digest string) error {
	if status, err := client.PutObject(ctx, cfg.bucket, key, bytes.NewReader(body), digest); err != nil {
		return err
	} else if status < 200 || status >= 300 {
		return r2VerificationStatusError{operation: "put verification object", status: status}
	}
	headStatus, err := client.HeadObject(ctx, cfg.bucket, key)
	if err != nil {
		return err
	}
	if headStatus != http.StatusOK {
		return r2VerificationStatusError{operation: "head verification object", status: headStatus}
	}
	getStatus, got, err := client.GetObject(ctx, cfg.bucket, key)
	if err != nil {
		return err
	}
	if getStatus != http.StatusOK {
		return r2VerificationStatusError{operation: "get verification object", status: getStatus}
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

func verifyGetterReadRoundTrip(ctx context.Context, writerClient *r2control.R2Client, getterClient *r2control.R2Client, cfg config, operation string, out *report) error {
	key, body, err := verificationObject(cfg.site, cfg.testPrefix)
	if err != nil {
		return err
	}
	digest := r2control.SHA256Hex(body)
	return retryR2CredentialPropagation(ctx, operation, func() error {
		if status, err := writerClient.PutObject(ctx, cfg.bucket, key, bytes.NewReader(body), digest); err != nil {
			return err
		} else if status < 200 || status >= 300 {
			return r2VerificationStatusError{operation: "put getter verification object", status: status}
		}
		getStatus, got, err := getterClient.GetObject(ctx, cfg.bucket, key)
		if err != nil {
			return err
		}
		if getStatus != http.StatusOK {
			return r2VerificationStatusError{operation: "getter credential get verification object", status: getStatus}
		}
		if !bytes.Equal(got, body) {
			return fmt.Errorf("getter credential verification object body mismatch")
		}
		if out != nil {
			out.GetterObjectGetStatus = getStatus
			out.TestObjectKey = key
			out.TestObjectSHA256 = digest
		}
		return nil
	})
}

func provisionSiteBootstrapCredentials(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient, out *report) (err error) {
	suffix := time.Now().UTC().Format("20060102T150405Z")
	expiresOn := time.Now().UTC().Add(cfg.childTokenTTL)
	var getter r2control.CreatedAPIToken
	getterPersisted := false
	defer func() {
		if !getterPersisted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, getter)
		}
	}()

	existed, bucketCreated, err := ensureR2BucketWithAccountAdmin(ctx, cfg, apiClient)
	if err != nil {
		return err
	}
	out.BucketExisted = existed
	out.BucketCreated = bucketCreated

	publisher, err := createBootstrapPublisherToken(ctx, cfg, apiClient, suffix)
	if err != nil {
		return err
	}
	publisherDeleted := false
	defer func() {
		if !publisherDeleted {
			deleteCreatedTokensOnError(&err, apiClient, cfg, publisher)
		}
	}()
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

	getter, err = apiClient.CreateR2BucketToken(ctx, cfg.accountID, cfg.bucket, "verself-"+cfg.site+"-nomad-artifact-getter-"+suffix, r2control.PermissionR2BucketItemRead, expiresOn)
	if err != nil {
		return err
	}
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
	if err := verifyGetterReadRoundTrip(ctx, publisherClient, getterClient, cfg, "verify bootstrap getter credential propagation", out); err != nil {
		return err
	}
	if err := apiClient.DeleteAccountToken(ctx, cfg.accountID, publisher.ID); err != nil {
		return fmt.Errorf("delete bootstrap publisher after getter verification: %w", err)
	}
	publisherDeleted = true

	updates := nomadArtifactGetterBootstrapVars(getter)
	varsFile := defaultBootstrapVarsFile(cfg)
	if err := mergeBootstrapVars(varsFile, updates); err != nil {
		return err
	}
	getterPersisted = true
	out.BootstrapVarsFile = varsFile
	out.GetterCredentialPermission = getter.PermissionGroup
	out.GetterCredentialName = getter.Name
	out.GetterCredentialExpiresOn = getter.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(getter.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(getter.S3SecretKey)
	out.BootstrapVarsFingerprints = fingerprintMap(updates)
	return nil
}

func createBootstrapPublisherToken(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient, suffix string) (r2control.CreatedAPIToken, error) {
	return apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, "verself-"+cfg.site+"-bootstrap-artifact-publisher-"+suffix, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, time.Now().UTC().Add(bootstrapPublisherTTL))
}

func mintBootstrapPublisher(ctx context.Context, cfg config) (err error) {
	accountAdmin, err := loadRequiredAccountAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	if _, _, err := ensureR2BucketWithAccountAdmin(ctx, cfg, apiClient); err != nil {
		return err
	}
	publisher, err := createBootstrapPublisherToken(ctx, cfg, apiClient, time.Now().UTC().Format("20060102T150405Z"))
	if err != nil {
		return err
	}
	delivered := false
	defer func() {
		if !delivered {
			deleteCreatedTokensOnError(&err, apiClient, cfg, publisher)
		}
	}()
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
	out := baseReport(cfg, accountAdmin.Source)
	if err := verifyObjectRoundTrip(ctx, publisherClient, cfg, "bootstrap-publisher", &out); err != nil {
		return err
	}
	if err := writeBootstrapPublisherCredential(publisher); err != nil {
		return err
	}
	delivered = true
	return nil
}

func revokeBootstrapPublisher(ctx context.Context, cfg config) error {
	if strings.TrimSpace(cfg.bootstrapPublisherTokenIDFile) == "" {
		return fmt.Errorf("--bootstrap-publisher-token-id-file is required")
	}
	tokenIDBody, err := os.ReadFile(cfg.bootstrapPublisherTokenIDFile)
	if err != nil {
		return fmt.Errorf("read bootstrap publisher token ID file: %w", err)
	}
	tokenID := strings.TrimSpace(string(tokenIDBody))
	if tokenID == "" {
		return fmt.Errorf("bootstrap publisher token ID file is empty")
	}
	accountAdmin, err := loadRequiredAccountAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	if err := apiClient.DeleteAccountToken(ctx, cfg.accountID, tokenID); err != nil {
		var statusErr r2control.APIStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return err
	}
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
	if err := verifyGetterReadRoundTrip(ctx, writerClient, getterClient, cfg, "verify getter credential propagation", out); err != nil {
		return err
	}
	updates := nomadArtifactGetterBootstrapVars(getter)
	varsFile := defaultBootstrapVarsFile(cfg)
	if err := mergeBootstrapVars(varsFile, updates); err != nil {
		return err
	}
	persisted = true
	out.GetterCredentialPermission = getter.PermissionGroup
	out.GetterCredentialName = getter.Name
	out.GetterCredentialExpiresOn = getter.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(getter.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(getter.S3SecretKey)
	out.BootstrapVarsFile = varsFile
	out.BootstrapVarsFingerprints = fingerprintMap(updates)
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
	if err := writeCapabilityCredential(ctx, cfg, capabilityOpenBaoPath("deployment-publisher"), "deployment-publisher", publisher); err != nil {
		return err
	}
	runtimeValues := publisherRuntimeSecretValues(publisher)
	if err := writeRuntimeSecrets(ctx, cfg, runtimeValues); err != nil {
		return err
	}
	persisted = true
	out.GetterCredentialPermission = publisher.PermissionGroup
	out.GetterCredentialName = publisher.Name
	out.GetterCredentialExpiresOn = publisher.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(publisher.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(publisher.S3SecretKey)
	out.RuntimeSecretFingerprints = fingerprintMap(runtimeValues)
	out.GetterObjectGetStatus = out.TestObjectGetStatus
	return nil
}

func provisionObjectStorageProviderCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, out *report) (err error) {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("object-storage R2 provider provisioning requires the account-admin Cloudflare API token value")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(parent.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	suffix := time.Now().UTC().Format("20060102T150405Z")
	adminName := "verself-" + cfg.site + "-object-storage-admin-" + suffix
	adminToken, err := apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, adminName, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, time.Now().UTC().Add(cfg.childTokenTTL))
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
	proxyToken, err := apiClient.CreateR2BucketTokenWithPermissions(ctx, cfg.accountID, cfg.bucket, proxyName, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}, time.Now().UTC().Add(cfg.childTokenTTL))
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
	if err := verifyObjectRoundTrip(ctx, adminClient, cfg, "object-storage-admin", out); err != nil {
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
	updates := objectStorageVars(adminToken, proxyToken)
	if err := writeRuntimeSecrets(ctx, cfg, updates); err != nil {
		return err
	}
	persisted = true
	out.GetterCredentialPermission = proxyToken.PermissionGroup
	out.GetterCredentialName = proxyToken.Name
	out.GetterCredentialExpiresOn = proxyToken.ExpiresOn
	out.GetterAccessKeyIDFingerprint = r2control.Fingerprint(proxyToken.S3AccessKeyID)
	out.GetterSecretKeyFingerprint = r2control.Fingerprint(proxyToken.S3SecretKey)
	out.RuntimeSecretFingerprints = fingerprintMap(updates)
	out.GetterObjectGetStatus = out.TestObjectGetStatus
	return nil
}

func provisionRecoveryCredential(ctx context.Context, cfg config, parent r2control.ParentCredentials, out *report) (err error) {
	if strings.TrimSpace(parent.APIToken) == "" {
		return fmt.Errorf("recovery R2 provisioning requires the account-admin Cloudflare API token value")
	}
	capabilityPath := capabilityOpenBaoPath("recovery")
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

func verifyDNSAuthority(ctx context.Context, cfg config) error {
	accountAdmin, err := loadRequiredAccountAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	zones, err := siteDNSZones(cfg)
	if err != nil {
		return err
	}
	zoneIDsByName, err := apiClient.ZonesByName(ctx, zones)
	if err != nil {
		return err
	}
	out := baseReport(cfg, accountAdmin.Source)
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
	out.VerifiedWith = "cloudflare-account-admin-dns-authority"
	for _, zone := range zones {
		out.DNSZones = append(out.DNSZones, dnsZoneReport{
			Name:              zone,
			ZoneIDFingerprint: r2control.Fingerprint(zoneIDsByName[zone]),
		})
	}
	return writeReport(out)
}

func reconcileDNS(ctx context.Context, cfg config) error {
	accountAdmin, err := loadRequiredAccountAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	desired, err := loadDNSDesiredState(cfg)
	if err != nil {
		return err
	}
	zoneIDsByName, err := apiClient.ZonesByName(ctx, desired.zoneNames())
	if err != nil {
		return fmt.Errorf("list cloudflare zones: %w", err)
	}
	plan, err := buildDNSPlan(ctx, apiClient, zoneIDsByName, desired)
	if err != nil {
		return err
	}
	jobs := dnsWriteJobs(plan)

	out := baseReport(cfg, accountAdmin.Source)
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
	out.VerifiedWith = "cloudflare-account-admin-dns-reconcile"
	out.DNSRecordsSeen = len(plan)
	out.DNSRecordsDiffed = len(jobs)
	out.DNSDryRun = cfg.dryRun
	for _, zone := range desired.zoneNames() {
		out.DNSZones = append(out.DNSZones, dnsZoneReport{
			Name:              zone,
			ZoneIDFingerprint: r2control.Fingerprint(zoneIDsByName[zone]),
		})
	}
	for _, job := range jobs {
		out.DNSChanges = append(out.DNSChanges, dnsChangeReport{
			Operation: job.operation,
			Zone:      job.entry.desired.zoneName,
			Name:      job.entry.desired.fqdn,
			Content:   job.entry.desired.targetIP,
			TTL:       job.entry.desired.ttl,
			Proxied:   job.entry.desired.proxied,
		})
	}
	if cfg.dryRun {
		return writeReport(out)
	}
	applied, err := applyDNSWrites(ctx, apiClient, cfg.dnsConcurrency, jobs)
	out.DNSRecordsApplied = applied
	if err != nil {
		_ = writeReport(out)
		return err
	}
	return writeReport(out)
}

func siteDNSZones(cfg config) ([]string, error) {
	path := filepath.Join(cfg.repoRoot, "src", "host", "sites", cfg.site, "vars.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var siteVars struct {
		VerselfDomain         string `yaml:"verself_domain"`
		CompanyDomain         string `yaml:"company_domain"`
		CloudflareProductZone string `yaml:"cloudflare_product_zone"`
		CloudflareCompanyZone string `yaml:"cloudflare_company_zone"`
		Records               []struct {
			Zone string `yaml:"zone"`
		} `yaml:"cloudflare_dns_records"`
	}
	if err := yaml.Unmarshal(body, &siteVars); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	zoneBySelector := map[string]string{
		"product": firstNonEmpty(siteVars.CloudflareProductZone, siteVars.VerselfDomain),
		"company": firstNonEmpty(siteVars.CloudflareCompanyZone, siteVars.CompanyDomain),
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, record := range siteVars.Records {
		selector := strings.TrimSpace(record.Zone)
		zone, ok := zoneBySelector[selector]
		if !ok {
			return nil, fmt.Errorf("%s: unknown cloudflare_dns_records[].zone %q", path, selector)
		}
		if zone == "" || strings.Contains(zone, "{{") {
			return nil, fmt.Errorf("%s: cloudflare DNS zone selector %q resolved to an invalid zone name", path, selector)
		}
		if _, ok := seen[zone]; ok {
			continue
		}
		seen[zone] = struct{}{}
		out = append(out, zone)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s declares no cloudflare_dns_records", path)
	}
	sort.Strings(out)
	return out, nil
}

type dnsDesiredRecord struct {
	zoneName string
	record   string
	fqdn     string
	targetIP string
	ttl      int
	proxied  bool
}

type dnsDesiredState struct {
	records []dnsDesiredRecord
}

func (d dnsDesiredState) zoneNames() []string {
	seen := map[string]struct{}{}
	for _, record := range d.records {
		seen[record.zoneName] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for zone := range seen {
		out = append(out, zone)
	}
	sort.Strings(out)
	return out
}

func (d dnsDesiredState) byZone(zone string) []dnsDesiredRecord {
	var out []dnsDesiredRecord
	for _, record := range d.records {
		if record.zoneName == zone {
			out = append(out, record)
		}
	}
	return out
}

func loadDNSDesiredState(cfg config) (dnsDesiredState, error) {
	path := filepath.Join(cfg.repoRoot, "src", "host", "sites", cfg.site, "vars.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		return dnsDesiredState{}, fmt.Errorf("read %s: %w", path, err)
	}
	var siteVars struct {
		VerselfDomain         string `yaml:"verself_domain"`
		CompanyDomain         string `yaml:"company_domain"`
		CloudflareProductZone string `yaml:"cloudflare_product_zone"`
		CloudflareCompanyZone string `yaml:"cloudflare_company_zone"`
		BareMetalPublicIPv4   string `yaml:"bare_metal_public_ipv4"`
		Records               []struct {
			Kind   string `yaml:"kind"`
			Record string `yaml:"record"`
			Zone   string `yaml:"zone"`
		} `yaml:"cloudflare_dns_records"`
	}
	if err := yaml.Unmarshal(body, &siteVars); err != nil {
		return dnsDesiredState{}, fmt.Errorf("decode %s: %w", path, err)
	}
	verself := strings.TrimSpace(siteVars.VerselfDomain)
	company := strings.TrimSpace(siteVars.CompanyDomain)
	publicIP := strings.TrimSpace(siteVars.BareMetalPublicIPv4)
	if publicIP == "" || publicIP == "0.0.0.0" {
		inventoryPath := cfg.dnsInventory
		if strings.TrimSpace(inventoryPath) == "" {
			inventoryPath = filepath.Join(cfg.repoRoot, "src", "host", "sites", cfg.site, "inventory.ini")
		}
		publicIP, err = inventoryInfraHost(inventoryPath)
		if err != nil {
			return dnsDesiredState{}, err
		}
	}
	if verself == "" || company == "" || publicIP == "" {
		return dnsDesiredState{}, fmt.Errorf("%s: missing verself_domain, company_domain, or site public IP", path)
	}
	productZone := firstNonEmpty(siteVars.CloudflareProductZone, verself)
	companyZone := firstNonEmpty(siteVars.CloudflareCompanyZone, company)
	seen := map[string]struct{}{}
	out := dnsDesiredState{}
	for _, record := range siteVars.Records {
		publicDomain := ""
		hostedZone := ""
		switch strings.TrimSpace(record.Zone) {
		case "product":
			publicDomain = verself
			hostedZone = productZone
		case "company":
			publicDomain = company
			hostedZone = companyZone
		default:
			return dnsDesiredState{}, fmt.Errorf("%s: unknown cloudflare_dns_records[].zone %q", path, record.Zone)
		}
		fqdn := publicFQDN(publicDomain, record.Record)
		relativeRecord, err := recordNameForHostedZone(fqdn, hostedZone)
		if err != nil {
			return dnsDesiredState{}, fmt.Errorf("%s: %w", path, err)
		}
		key := strings.TrimSpace(hostedZone) + "|" + fqdn
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.records = append(out.records, dnsDesiredRecord{
			zoneName: strings.Trim(strings.TrimSpace(hostedZone), "."),
			record:   relativeRecord,
			fqdn:     fqdn,
			targetIP: publicIP,
			ttl:      1,
			proxied:  false,
		})
	}
	if len(out.records) == 0 {
		return dnsDesiredState{}, fmt.Errorf("%s declares no cloudflare_dns_records", path)
	}
	return out, nil
}

func publicFQDN(publicDomain, record string) string {
	publicDomain = strings.Trim(strings.TrimSpace(publicDomain), ".")
	record = strings.Trim(strings.TrimSpace(record), ".")
	if record == "" || record == "@" {
		return publicDomain
	}
	return record + "." + publicDomain
}

func recordNameForHostedZone(fqdn, hostedZone string) (string, error) {
	fqdn = strings.Trim(strings.TrimSpace(fqdn), ".")
	hostedZone = strings.Trim(strings.TrimSpace(hostedZone), ".")
	if hostedZone == "" || strings.Contains(hostedZone, "{{") {
		return "", fmt.Errorf("invalid Cloudflare hosted zone %q", hostedZone)
	}
	if fqdn == hostedZone {
		return "@", nil
	}
	suffix := "." + hostedZone
	if !strings.HasSuffix(fqdn, suffix) {
		return "", fmt.Errorf("DNS name %s is not inside Cloudflare hosted zone %s", fqdn, hostedZone)
	}
	return strings.TrimSuffix(fqdn, suffix), nil
}

func inventoryInfraHost(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("inventory path is required when bare_metal_public_ipv4 is unset")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open inventory %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "infra" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		host := fields[0]
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if ok && key == "ansible_host" {
				host = value
				break
			}
		}
		if host == "" {
			return "", fmt.Errorf("inventory %s has an empty [infra] host", path)
		}
		return host, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read inventory %s: %w", path, err)
	}
	return "", fmt.Errorf("inventory %s has no [infra] host", path)
}

type dnsPlanEntry struct {
	zoneID  string
	desired dnsDesiredRecord
	actual  *r2control.DNSRecord
}

type dnsWriteJob struct {
	entry     dnsPlanEntry
	operation string
}

func buildDNSPlan(ctx context.Context, apiClient *r2control.CloudflareAPIClient, zones map[string]string, desired dnsDesiredState) ([]dnsPlanEntry, error) {
	var plan []dnsPlanEntry
	for _, zoneName := range desired.zoneNames() {
		zoneID := zones[zoneName]
		actual, err := apiClient.ListARecords(ctx, zoneID)
		if err != nil {
			return nil, fmt.Errorf("list A records for zone %s: %w", zoneName, err)
		}
		actualByName := map[string]r2control.DNSRecord{}
		for _, record := range actual {
			actualByName[record.Name] = record
		}
		for _, want := range desired.byZone(zoneName) {
			entry := dnsPlanEntry{zoneID: zoneID, desired: want}
			if current, ok := actualByName[want.fqdn]; ok {
				current := current
				entry.actual = &current
			}
			plan = append(plan, entry)
		}
	}
	return plan, nil
}

func dnsWriteJobs(plan []dnsPlanEntry) []dnsWriteJob {
	var jobs []dnsWriteJob
	for _, entry := range plan {
		if entry.actual == nil {
			jobs = append(jobs, dnsWriteJob{entry: entry, operation: "create"})
			continue
		}
		if entry.actual.Content == entry.desired.targetIP &&
			entry.actual.TTL == entry.desired.ttl &&
			entry.actual.Proxied == entry.desired.proxied {
			continue
		}
		jobs = append(jobs, dnsWriteJob{entry: entry, operation: "update"})
	}
	return jobs
}

func applyDNSWrites(ctx context.Context, apiClient *r2control.CloudflareAPIClient, concurrency int, jobs []dnsWriteJob) (int, error) {
	if len(jobs) == 0 {
		return 0, nil
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var applied int
	var applyErr error
	for _, job := range jobs {
		job := job
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var err error
			switch job.operation {
			case "create":
				_, err = apiClient.CreateARecord(ctx, job.entry.zoneID, job.entry.desired.fqdn, job.entry.desired.targetIP, job.entry.desired.ttl, job.entry.desired.proxied)
			case "update":
				_, err = apiClient.UpdateARecord(ctx, job.entry.zoneID, job.entry.actual.ID, job.entry.desired.fqdn, job.entry.desired.targetIP, job.entry.desired.ttl, job.entry.desired.proxied)
			default:
				err = fmt.Errorf("unknown DNS write operation %q", job.operation)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				applyErr = errors.Join(applyErr, fmt.Errorf("%s %s: %w", job.operation, job.entry.desired.fqdn, err))
				return
			}
			applied++
		}()
	}
	wg.Wait()
	return applied, applyErr
}

func capabilityOpenBaoPath(capability string) string {
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

func writeRuntimeSecrets(ctx context.Context, cfg config, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(values[key])
		if value == "" {
			return fmt.Errorf("runtime secret %s is empty", key)
		}
		writeCfg := cfg.runtimeOpenBaoCredentialConfig(runtimeSecretOpenBaoPath(key))
		if err := r2control.WriteParentCredentialsToOpenBao(ctx, writeCfg, map[string]string{"value": value}); err != nil {
			return fmt.Errorf("write runtime OpenBao secret %s: %w", key, err)
		}
	}
	return nil
}

func runtimeSecretOpenBaoPath(name string) string {
	return "kv-runtime/data/secret/org/" + url.PathEscape(name)
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

func writeBootstrapPublisherCredential(publisher r2control.CreatedAPIToken) error {
	credential := bootstrapPublisherCredential{
		AccessKeyID:     publisher.S3AccessKeyID,
		SecretAccessKey: publisher.S3SecretKey,
		TokenID:         publisher.ID,
		ExpiresOn:       publisher.ExpiresOn,
	}
	if strings.TrimSpace(credential.AccessKeyID) == "" || strings.TrimSpace(credential.SecretAccessKey) == "" || strings.TrimSpace(credential.TokenID) == "" {
		return fmt.Errorf("bootstrap publisher credential is incomplete")
	}
	f := os.NewFile(uintptr(bootstrapPublisherOutputFD), "bootstrap-publisher-credential")
	if f == nil {
		return fmt.Errorf("bootstrap publisher credential fd %d is not open", bootstrapPublisherOutputFD)
	}
	defer func() { _ = f.Close() }()
	if err := json.NewEncoder(f).Encode(credential); err != nil {
		return fmt.Errorf("write bootstrap publisher credential fd %d: %w", bootstrapPublisherOutputFD, err)
	}
	return nil
}

func objectStorageVars(adminToken, proxyToken r2control.CreatedAPIToken) map[string]string {
	return map[string]string{
		"object-storage-service.r2.admin_access_key_id":     adminToken.S3AccessKeyID,
		"object-storage-service.r2.admin_secret_access_key": adminToken.S3SecretKey,
		"object-storage-service.r2.proxy_access_key_id":     proxyToken.S3AccessKeyID,
		"object-storage-service.r2.proxy_secret_access_key": proxyToken.S3SecretKey,
	}
}

func publisherRuntimeSecretValues(publisher r2control.CreatedAPIToken) map[string]string {
	return map[string]string{
		"cloudflare-r2-control-plane.publisher_token_id":          publisher.S3AccessKeyID,
		"cloudflare-r2-control-plane.publisher_secret_access_key": publisher.S3SecretKey,
	}
}

func nomadArtifactGetterBootstrapVars(getter r2control.CreatedAPIToken) map[string]string {
	return map[string]string{
		"nomad_artifact_getter_s3_access_key_id":     getter.S3AccessKeyID,
		"nomad_artifact_getter_s3_secret_access_key": getter.S3SecretKey,
	}
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

func defaultBootstrapVarsFile(cfg config) string {
	if strings.TrimSpace(cfg.bootstrapVarsFile) != "" {
		return cfg.bootstrapVarsFile
	}
	return filepath.Join(cfg.repoRoot, ".verself", "site-bootstrap", cfg.site, "bootstrap-vars.json")
}

func mergeBootstrapVars(path string, updates map[string]string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("bootstrap vars file is required")
	}
	values := map[string]string{}
	body, err := os.ReadFile(path)
	if err == nil && len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &values); err != nil {
			return fmt.Errorf("decode bootstrap vars %s: %w", path, err)
		}
		if values == nil {
			values = map[string]string{}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read bootstrap vars %s: %w", path, err)
	}
	for key, value := range updates {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("bootstrap vars key is empty")
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("bootstrap vars %s is empty", key)
		}
		values[key] = value
	}
	body, err = json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bootstrap vars %s: %w", path, err)
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create bootstrap vars directory: %w", err)
	}
	if err := writeFileAtomic(path, body, 0o600); err != nil {
		return fmt.Errorf("write bootstrap vars %s: %w", path, err)
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

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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/r2control"
	recoveryv1alpha1 "github.com/verself/recovery-spec/types/go/v1alpha1"
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
	accountAdminOpenBaoPathDefault = "kv-controller/data/integrations/cloudflare/account-admin"
)

type config struct {
	action                   string
	repoRoot                 string
	site                     string
	accountID                string
	bucket                   string
	keyPrefix                string
	region                   string
	accountAdminOpenBaoPath  string
	accountAdminAPITokenFile string
	openBaoAddr              string
	openBaoPath              string
	openBaoCACertFile        string
	openBaoTokenFile         string
	runtimeOpenBaoAddr       string
	runtimeOpenBaoCACertFile string
	runtimeOpenBaoTokenFile  string
	dnsInventory             string
	dnsConcurrency           int
	dryRun                   bool
	provider                 string
	cloudflareAPITokenFile   string
	certificateOutputDir     string
	tlsProductDomain         string
	tlsCompanyDomain         string
	tlsProductZone           string
	tlsCompanyZone           string
	acmeDirectoryURL         string
	acmeContactEmail         string
	acmeDNSPropagationWait   time.Duration
	certificateRenewBefore   time.Duration
	testPrefix               string
	inventoryPrefix          string
	inventoryDepth           int
	tempTTL                  time.Duration
	childTokenTTL            time.Duration
	timeout                  time.Duration
	verifyTempCredentials    bool
	recoveryConfig           string
	recovery                 *recoveryv1alpha1.CloudflareRecovery
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
	ChildCredentialPermission    string                  `json:"child_credential_permission,omitempty"`
	ChildCredentialName          string                  `json:"child_credential_name,omitempty"`
	ChildCredentialExpiresOn     string                  `json:"child_credential_expires_on,omitempty"`
	ChildAccessKeyIDFingerprint  string                  `json:"child_access_key_id_fingerprint,omitempty"`
	ChildSecretKeyFingerprint    string                  `json:"child_secret_key_fingerprint,omitempty"`
	RuntimeSecretFingerprints    map[string]string       `json:"runtime_secret_fingerprints,omitempty"`
	VerificationObjectGetStatus  int                     `json:"verification_object_get_status,omitempty"`
	Inventory                    []inventoryPrefixReport `json:"inventory,omitempty"`
	AccountAdminStatus           accountAdminStatus      `json:"account_admin_status,omitempty"`
	RecoveryConditions           []string                `json:"recovery_conditions,omitempty"`
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

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cloudflare-control-plane: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("cloudflare-control-plane", flag.ContinueOnError)
	fs.StringVar(&cfg.action, "action", "verify-account-admin", "Action: recover, import-account-admin, verify-account-admin, verify-dns-authority, reconcile-dns, issue-site-certificates, provision-site, ensure-bucket, ensure-recovery, rotate-recovery, rotate-object-storage-provider, inventory, or verify.")
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "Repository root for loading Cloudflare account config and site vars.")
	fs.StringVar(&cfg.site, "site", "prod", "Target deployment site. Cloudflare account authority is global and anchored to prod.")
	fs.StringVar(&cfg.accountID, "account-id", "", "Cloudflare account ID. Defaults to src/integrations/cloudflare/account.json.")
	fs.StringVar(&cfg.bucket, "bucket", "", "R2 bucket name. Defaults to account.json r2.deployment_artifacts_bucket.")
	fs.StringVar(&cfg.keyPrefix, "key-prefix", "sha256", "R2 artifact key prefix.")
	fs.StringVar(&cfg.region, "region", "auto", "R2 S3 signing region.")
	fs.StringVar(&cfg.accountAdminOpenBaoPath, "account-admin-openbao-path", accountAdminOpenBaoPathDefault, "Controller OpenBao KV path for the Cloudflare account-admin API token.")
	fs.StringVar(&cfg.accountAdminAPITokenFile, "account-admin-api-token-file", "", "File containing the Cloudflare account-admin API token for --action=import-account-admin.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", "", "Controller OpenBao address.")
	fs.StringVar(&cfg.openBaoPath, "openbao-path", "kv-controller/data/integrations/cloudflare/r2/capabilities/object-storage-admin", "Controller OpenBao KV path for R2 credentials.")
	fs.StringVar(&cfg.openBaoCACertFile, "openbao-ca-cert", "", "Controller OpenBao CA certificate file. Defaults to BAO_CACERT or VAULT_CACERT.")
	fs.StringVar(&cfg.openBaoTokenFile, "openbao-token-file", "", "File containing the OpenBao token.")
	fs.StringVar(&cfg.runtimeOpenBaoAddr, "runtime-openbao-addr", "", "Runtime OpenBao address for service-required secret projection. Defaults to --openbao-addr.")
	fs.StringVar(&cfg.runtimeOpenBaoCACertFile, "runtime-openbao-ca-cert", "", "Runtime OpenBao CA certificate file. Defaults to --openbao-ca-cert, BAO_CACERT, or VAULT_CACERT.")
	fs.StringVar(&cfg.runtimeOpenBaoTokenFile, "runtime-openbao-token-file", "", "File containing the runtime OpenBao token. Defaults to --openbao-token-file.")
	fs.StringVar(&cfg.dnsInventory, "dns-inventory", "", "Path to the site inventory for DNS target IP fallback. Defaults to src/sites/<site>/inventory.ini.")
	fs.IntVar(&cfg.dnsConcurrency, "dns-concurrency", 8, "Maximum parallel Cloudflare DNS write requests for --action=reconcile-dns.")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Print and report the DNS diff without applying writes for --action=reconcile-dns.")
	fs.StringVar(&cfg.provider, "provider", "", "External provider for direct public-edge operations. Supported value: cloudflare.")
	fs.StringVar(&cfg.cloudflareAPITokenFile, "cloudflare-api-token-file", "", "File containing a Cloudflare API token for direct DNS operations.")
	fs.StringVar(&cfg.certificateOutputDir, "certificate-output-dir", "", "Directory to receive HAProxy public certificate PEM files.")
	fs.StringVar(&cfg.tlsProductDomain, "tls-product-domain", "", "Product domain for public TLS issuance.")
	fs.StringVar(&cfg.tlsCompanyDomain, "tls-company-domain", "", "Company domain for public TLS issuance.")
	fs.StringVar(&cfg.tlsProductZone, "tls-product-zone", "", "Cloudflare DNS zone for the product domain.")
	fs.StringVar(&cfg.tlsCompanyZone, "tls-company-zone", "", "Cloudflare DNS zone for the company domain.")
	fs.StringVar(&cfg.acmeDirectoryURL, "acme-directory-url", letsEncryptProductionDirectoryURL, "ACME directory URL for public certificate issuance.")
	fs.StringVar(&cfg.acmeContactEmail, "acme-contact-email", "", "ACME account contact email for --action=issue-site-certificates.")
	fs.DurationVar(&cfg.acmeDNSPropagationWait, "acme-dns-propagation-wait", 2*time.Minute, "Maximum wait for ACME DNS-01 TXT propagation.")
	fs.DurationVar(&cfg.certificateRenewBefore, "certificate-renew-before", 30*24*time.Hour, "Renew certificates expiring before this duration.")
	fs.StringVar(&cfg.testPrefix, "test-prefix", "control-plane-verification/", "R2 object prefix used for live verification.")
	fs.StringVar(&cfg.inventoryPrefix, "inventory-prefix", "", "R2 object prefix for --action=inventory.")
	fs.IntVar(&cfg.inventoryDepth, "inventory-depth", 2, "Prefix depth for --action=inventory summaries.")
	fs.DurationVar(&cfg.tempTTL, "temp-ttl", 15*time.Minute, "TTL for Cloudflare temporary scoped R2 verification credentials.")
	fs.DurationVar(&cfg.childTokenTTL, "child-token-ttl", 7*24*time.Hour, "TTL for generated Cloudflare child API tokens.")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Total timeout for Cloudflare R2 calls.")
	fs.BoolVar(&cfg.verifyTempCredentials, "verify-temp-credentials", true, "Mint scoped temporary credentials and use them for the object verification.")
	fs.StringVar(&cfg.recoveryConfig, "recovery-config", "", "Path to a recovery.verself.sh/v1alpha1 CloudflareRecovery document for --action=recover.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.applyRecoveryConfig(); err != nil {
		return err
	}
	if err := cfg.applySiteDefaults(); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	timeout := cfg.timeout
	if cfg.action == "issue-site-certificates" && timeout < 5*time.Minute {
		timeout = 5 * time.Minute
	}
	if cfg.action == "recover" && timeout < 10*time.Minute {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch cfg.action {
	case "recover":
		return recoverCloudflare(ctx, cfg)
	case "import-account-admin":
		return importAccountAdmin(ctx, cfg)
	case "verify-account-admin":
		return verifyAccountAdmin(ctx, cfg)
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
			Source:          "cloudflare-control-plane-temp",
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
	needsSiteDefaults := !certificateOnlyAction(cfg.action) && (cfg.accountID == "" || cfg.bucket == "")
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

func certificateOnlyAction(action string) bool {
	return action == "issue-site-certificates"
}

func isChildProvisioningAction(action string) bool {
	switch action {
	case "ensure-bucket", "ensure-recovery", "rotate-recovery", "rotate-object-storage-provider":
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
	case "recover", "import-account-admin", "verify-account-admin", "verify-dns-authority", "reconcile-dns", "issue-site-certificates", "provision-site", "inventory", "verify", "ensure-bucket", "ensure-recovery", "rotate-recovery", "rotate-object-storage-provider":
	default:
		return fmt.Errorf("--action must be recover, import-account-admin, verify-account-admin, verify-dns-authority, reconcile-dns, issue-site-certificates, provision-site, inventory, verify, ensure-bucket, ensure-recovery, rotate-recovery, or rotate-object-storage-provider, got %q", cfg.action)
	}
	if cfg.action == "recover" && cfg.recovery == nil {
		return fmt.Errorf("--recovery-config is required for recovery")
	}
	if !certificateOnlyAction(cfg.action) {
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
	}
	if cfg.tempTTL < time.Minute || cfg.tempTTL > 7*24*time.Hour {
		return fmt.Errorf("--temp-ttl must be between 1 minute and 7 days")
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
	if cfg.acmeDNSPropagationWait <= 0 || cfg.acmeDNSPropagationWait > 10*time.Minute {
		return fmt.Errorf("--acme-dns-propagation-wait must be greater than zero and no more than 10 minutes")
	}
	if cfg.certificateRenewBefore <= 0 || cfg.certificateRenewBefore > 90*24*time.Hour {
		return fmt.Errorf("--certificate-renew-before must be greater than zero and no more than 90 days")
	}
	if cfg.action == "issue-site-certificates" {
		if strings.TrimSpace(cfg.provider) != "cloudflare" {
			return fmt.Errorf("--provider=cloudflare is required for certificate issuance")
		}
		if strings.TrimSpace(cfg.cloudflareAPITokenFile) == "" {
			return fmt.Errorf("--cloudflare-api-token-file is required for certificate issuance")
		}
		if strings.TrimSpace(cfg.certificateOutputDir) == "" {
			return fmt.Errorf("--certificate-output-dir is required for certificate issuance")
		}
		if strings.TrimSpace(cfg.acmeDirectoryURL) == "" {
			return fmt.Errorf("--acme-directory-url is required for certificate issuance")
		}
		if strings.TrimSpace(cfg.acmeContactEmail) == "" {
			return fmt.Errorf("--acme-contact-email is required for certificate issuance")
		}
	}
	if cfg.action == "import-account-admin" {
		if strings.TrimSpace(cfg.openBaoAddr) == "" {
			return fmt.Errorf("--openbao-addr is required for account-admin import")
		}
		if strings.TrimSpace(cfg.openBaoTokenFile) == "" {
			return fmt.Errorf("--openbao-token-file is required for account-admin import")
		}
		if strings.TrimSpace(cfg.accountAdminAPITokenFile) == "" {
			return fmt.Errorf("--account-admin-api-token-file is required for account-admin import")
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
		OpenBaoTokenFile:  cfg.openBaoTokenFile,
		Timeout:           cfg.timeout,
	}
}

func (cfg config) runtimeOpenBaoCredentialConfig(path string) r2control.ParentCredentialConfig {
	return r2control.ParentCredentialConfig{
		Source:            r2control.ParentCredentialSourceOpenBao,
		OpenBaoAddr:       firstNonEmpty(cfg.runtimeOpenBaoAddr, cfg.openBaoAddr),
		OpenBaoPath:       path,
		OpenBaoCACertFile: firstNonEmpty(cfg.runtimeOpenBaoCACertFile, cfg.openBaoCACertFile),
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

func importAccountAdmin(ctx context.Context, cfg config) error {
	token, err := readRequiredSecretFile(cfg.accountAdminAPITokenFile, "account-admin API token")
	if err != nil {
		return err
	}
	defer clearBytes(token)

	imported, err := verifyAccountAdminToken(ctx, cfg, string(token))
	if err != nil {
		return fmt.Errorf("verify account-admin token: %w", err)
	}
	if err := writeAccountAdminCredential(ctx, cfg, accountAdminOpenBaoPath(cfg), imported.tokenID, string(token), imported.ExpiresOn); err != nil {
		return fmt.Errorf("write account-admin: %w", err)
	}
	_, verified, err := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminOpenBaoPath(cfg))
	if err != nil {
		return fmt.Errorf("verify imported account-admin: %w", err)
	}
	out := baseReport(cfg, "operator-token-file:cloudflare-account-admin")
	out.VerifiedWith = "cloudflare-account-admin-import"
	out.AccountAdminStatus = verified
	return writeReport(out)
}

func readRequiredSecretFile(path, label string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%s file is required", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect %s file: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s file %s must not be a symlink", label, path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s file %s must be a regular file", label, path)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("%s file %s is empty", label, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s file %s must be readable only by the operator", label, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s file: %w", label, err)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("%s file %s is empty", label, path)
	}
	return body, nil
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

type importedAccountAdminStatus struct {
	tokenID string
	accountAdminStatus
}

func verifyAccountAdminToken(ctx context.Context, cfg config, apiToken string) (importedAccountAdminStatus, error) {
	if strings.TrimSpace(apiToken) == "" {
		return importedAccountAdminStatus{}, fmt.Errorf("api token is empty")
	}
	apiClient, err := r2control.NewCloudflareAPIClient(apiToken, cfg.timeout)
	if err != nil {
		return importedAccountAdminStatus{}, err
	}
	verified, err := apiClient.VerifyAccountToken(ctx, cfg.accountID)
	if err != nil {
		return importedAccountAdminStatus{}, err
	}
	if verified.Status != "" && verified.Status != "active" {
		return importedAccountAdminStatus{}, fmt.Errorf("cloudflare account-admin status is %q", verified.Status)
	}
	if strings.TrimSpace(verified.ID) == "" {
		return importedAccountAdminStatus{}, fmt.Errorf("cloudflare account-admin token ID is empty")
	}
	if _, err := apiClient.GetAccountToken(ctx, cfg.accountID, verified.ID); err != nil {
		return importedAccountAdminStatus{}, fmt.Errorf("cloudflare account-admin cannot read account token metadata; requires Account API Tokens Read on account %s: %w", cfg.accountID, err)
	}
	if err := verifyAccountAdminR2Authority(ctx, cfg, apiClient); err != nil {
		return importedAccountAdminStatus{}, err
	}
	return importedAccountAdminStatus{
		tokenID: verified.ID,
		accountAdminStatus: accountAdminStatus{
			TokenIDFingerprint: r2control.Fingerprint(verified.ID),
			Status:             verified.Status,
			ExpiresOn:          verified.ExpiresOn,
		},
	}, nil
}

func verifyAccountAdmin(ctx context.Context, cfg config) error {
	_, status, err := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminOpenBaoPath(cfg))
	if err != nil {
		return err
	}
	out := baseReport(cfg, "controller-openbao:cloudflare-account-admin")
	out.VerifiedWith = "cloudflare-account-admin"
	out.AccountAdminStatus = status
	return writeReport(out)
}

func provisionSite(ctx context.Context, cfg config) error {
	dnsCfg := cfg
	dnsCfg.action = "reconcile-dns"
	if err := reconcileDNS(ctx, dnsCfg); err != nil {
		return fmt.Errorf("reconcile-dns: %w", err)
	}
	bucketCfg := cfg
	bucketCfg.action = "ensure-bucket"
	if err := provisionChildCredential(ctx, bucketCfg); err != nil {
		return fmt.Errorf("ensure-bucket: %w", err)
	}
	out := baseReport(cfg, "controller-openbao:cloudflare-account-admin")
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
	if _, err := apiClient.GetAccountToken(ctx, cfg.accountID, verified.ID); err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, fmt.Errorf("cloudflare account-admin cannot read account token metadata; requires Account API Tokens Read on account %s: %w", cfg.accountID, err)
	}
	if err := verifyAccountAdminR2Authority(ctx, cfg, apiClient); err != nil {
		return r2control.ParentCredentials{}, accountAdminStatus{}, err
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
	if cfg.action == "rotate-object-storage-provider" {
		adminSecret := cfg.objectStorageRuntimeSecretNames().AdminAccessKeyName()
		if err := preflightOpenBaoPersistence(cfg.runtimeOpenBaoCredentialConfig(runtimeSecretOpenBaoPath(adminSecret)), "object-storage provider runtime secret projection"); err != nil {
			return err
		}
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	existed, created, err := ensureR2BucketWithAccountAdmin(ctx, cfg, apiClient)
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
	accountAdmin, _, err := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminOpenBaoPath(cfg))
	if err != nil {
		return r2control.ParentCredentials{}, err
	}
	if strings.TrimSpace(accountAdmin.APIToken) == "" {
		return r2control.ParentCredentials{}, fmt.Errorf("cloudflare account-admin credential must include api_token")
	}
	return accountAdmin, nil
}

func preflightOpenBaoPersistence(openBao r2control.ParentCredentialConfig, label string) error {
	if strings.TrimSpace(openBao.OpenBaoAddr) == "" {
		return fmt.Errorf("%s requires OpenBao address", label)
	}
	if _, err := r2control.LoadOpenBaoToken(openBao); err != nil {
		return fmt.Errorf("%s preflight failed: %w", label, err)
	}
	return nil
}

func ensureR2BucketWithAccountAdmin(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient) (bool, bool, error) {
	existed, created, err := apiClient.EnsureR2Bucket(ctx, cfg.accountID, cfg.bucket)
	if err != nil {
		return false, false, accountAdminR2AuthorityError(cfg, err)
	}
	return existed, created, nil
}

func verifyAccountAdminR2Authority(ctx context.Context, cfg config, apiClient *r2control.CloudflareAPIClient) error {
	if err := apiClient.VerifyR2BucketTokenPermissionGroups(ctx, cfg.accountID, []string{
		r2control.PermissionR2BucketItemRead,
		r2control.PermissionR2BucketItemWrite,
	}); err != nil {
		return fmt.Errorf("cloudflare account-admin cannot inspect R2 bucket token permission groups; requires Account API Tokens Read on account %s: %w", cfg.accountID, err)
	}
	if strings.TrimSpace(cfg.bucket) == "" {
		return nil
	}
	if _, err := apiClient.GetR2Bucket(ctx, cfg.accountID, cfg.bucket); err != nil {
		var apiErr r2control.APIStatusError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return accountAdminR2AuthorityError(cfg, err)
	}
	return nil
}

func accountAdminR2AuthorityError(cfg config, err error) error {
	var apiErr r2control.APIStatusError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("cloudflare account-admin cannot access R2 bucket %s on account %s; requires Workers R2 Storage Read/Write on the account: %w", cfg.bucket, cfg.accountID, err)
	}
	return err
}

func accountAdminOpenBaoPath(cfg config) string {
	return firstNonEmpty(cfg.accountAdminOpenBaoPath, accountAdminOpenBaoPathDefault)
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
		Source:          "cloudflare-control-plane-object-storage-admin-verification",
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
		Source:          "cloudflare-control-plane-object-storage-proxy-verification",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, proxyClient, cfg, "object-storage-proxy", out); err != nil {
		return err
	}
	updates := cfg.objectStorageVars(adminToken, proxyToken)
	if err := writeRuntimeSecrets(ctx, cfg, updates); err != nil {
		return err
	}
	persisted = true
	out.ChildCredentialPermission = proxyToken.PermissionGroup
	out.ChildCredentialName = proxyToken.Name
	out.ChildCredentialExpiresOn = proxyToken.ExpiresOn
	out.ChildAccessKeyIDFingerprint = r2control.Fingerprint(proxyToken.S3AccessKeyID)
	out.ChildSecretKeyFingerprint = r2control.Fingerprint(proxyToken.S3SecretKey)
	out.RuntimeSecretFingerprints = fingerprintMap(updates)
	out.VerificationObjectGetStatus = out.TestObjectGetStatus
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
		Source:          "cloudflare-control-plane-recovery-verification",
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
	out.ChildCredentialPermission = recovery.PermissionGroup
	out.ChildCredentialName = recovery.Name
	out.ChildCredentialExpiresOn = recovery.ExpiresOn
	out.ChildAccessKeyIDFingerprint = r2control.Fingerprint(recovery.S3AccessKeyID)
	out.ChildSecretKeyFingerprint = r2control.Fingerprint(recovery.S3SecretKey)
	out.VerificationObjectGetStatus = out.TestObjectGetStatus
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
	out, err := reconcileDNSDesired(ctx, cfg, accountAdmin, apiClient, desired)
	if err != nil {
		_ = writeReport(out)
		return err
	}
	return writeReport(out)
}

func reconcileDNSDesired(ctx context.Context, cfg config, accountAdmin r2control.ParentCredentials, apiClient *r2control.CloudflareAPIClient, desired dnsDesiredState) (report, error) {
	zoneIDsByName, err := apiClient.ZonesByName(ctx, desired.zoneNames())
	if err != nil {
		return report{}, fmt.Errorf("list cloudflare zones: %w", err)
	}
	plan, err := buildDNSPlan(ctx, apiClient, zoneIDsByName, desired)
	if err != nil {
		return report{}, err
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
		return out, nil
	}
	applied, err := applyDNSWrites(ctx, apiClient, cfg.dnsConcurrency, jobs)
	out.DNSRecordsApplied = applied
	if err != nil {
		return out, err
	}
	return out, nil
}

func siteDNSZones(cfg config) ([]string, error) {
	path := filepath.Join(cfg.repoRoot, "src", "sites", cfg.site, "vars.yml")
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
	path := filepath.Join(cfg.repoRoot, "src", "sites", cfg.site, "vars.yml")
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
			inventoryPath = filepath.Join(cfg.repoRoot, "src", "sites", cfg.site, "inventory.ini")
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
			if ok && key == "verself_ssh_host" {
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

func (cfg config) objectStorageVars(adminToken, proxyToken r2control.CreatedAPIToken) map[string]string {
	names := cfg.objectStorageRuntimeSecretNames()
	return names.Map(
		adminToken.S3AccessKeyID,
		adminToken.S3SecretKey,
		proxyToken.S3AccessKeyID,
		proxyToken.S3SecretKey,
	)
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

func verificationObject(site, prefix string) (string, []byte, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, fmt.Errorf("generate verification nonce: %w", err)
	}
	key := normalizedPrefix(prefix) + site + "/" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(nonce) + ".txt"
	body := []byte("verself cloudflare-control-plane verification\nsite=" + site + "\nkey=" + key + "\n")
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

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/integrations/cloudflare/control-plane/r2control"
)

const rootTrustMaterialRequiredReason = "RootTrustMaterialRequired"

func (cfg *config) applyRecoveryConfig() error {
	if strings.TrimSpace(cfg.action) != "recover" {
		return nil
	}
	if strings.TrimSpace(cfg.recoveryConfig) == "" {
		return fmt.Errorf("--recovery-config is required for recovery")
	}
	doc, err := loadCloudflareControlPlane(cfg.recoveryConfig)
	if err != nil {
		return err
	}
	childTokenTTL, err := doc.childTokenTTL()
	if err != nil {
		return err
	}
	dnsWait, err := doc.acmeDNSPropagationWait()
	if err != nil {
		return err
	}
	renewBefore, err := doc.acmeRenewBefore()
	if err != nil {
		return err
	}
	cfg.recovery = &doc
	cfg.site = doc.Spec.Site
	cfg.accountID = doc.Spec.AccountID
	cfg.accountAdminOpenBaoPath = strings.Trim(strings.TrimSpace(doc.Spec.AccountAdminOpenBaoPath), "/")
	cfg.openBaoAddr = doc.Spec.OpenBao.Address
	cfg.openBaoTokenFile = expandRecoveryPath(doc.Spec.OpenBao.TokenFile)
	cfg.openBaoCACertFile = expandRecoveryPath(doc.Spec.OpenBao.CACertFile)
	cfg.runtimeOpenBaoAddr = cfg.openBaoAddr
	cfg.runtimeOpenBaoTokenFile = cfg.openBaoTokenFile
	cfg.runtimeOpenBaoCACertFile = cfg.openBaoCACertFile
	cfg.provider = "cloudflare"
	cfg.bucket = doc.Spec.ObjectStorage.Bucket
	cfg.childTokenTTL = childTokenTTL
	cfg.acmeDNSPropagationWait = dnsWait
	cfg.certificateRenewBefore = renewBefore
	cfg.acmeDirectoryURL = doc.Spec.TLS.ACME.DirectoryURL
	cfg.acmeContactEmail = doc.Spec.TLS.ACME.ContactEmail
	cfg.certificateOutputDir = expandRecoveryPath(doc.Spec.TLS.OutputDir)
	return nil
}

func recoverCloudflare(ctx context.Context, cfg config) error {
	if cfg.recovery == nil {
		return fmt.Errorf("recovery document is required")
	}
	recoveryCfg := recoveryCredentialConfig(cfg)
	out := baseReport(recoveryCfg, "cloudflare-recovery")
	out.Action = "recover"
	out.VerifiedWith = "cloudflare-recovery"
	recoveryCredentialErr := verifyRecoveryCredential(ctx, recoveryCfg, &out)

	accountAdmin, imported, accountAdminErr := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminOpenBaoPath(cfg))
	if accountAdminErr != nil && recoveryCredentialErr != nil {
		return cloudflareRecoveryAuthorityUnavailableError(
			accountAdminOpenBaoPath(cfg),
			accountAdminErr,
			capabilityOpenBaoPath("recovery"),
			recoveryCredentialErr,
		)
	}
	if accountAdminErr != nil {
		out.RecoveryConditions = append(out.RecoveryConditions, "AccountAdminUnavailable", "RecoveryCredentialsAvailable")
		sort.Strings(out.RecoveryConditions)
		return writeReport(out)
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	out.ParentCredentialSource = accountAdmin.Source
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
	out.AccountAdminStatus = imported
	out.RecoveryConditions = append(out.RecoveryConditions, "AccountTokenVerified")

	if recoveryCredentialErr != nil {
		if err := preflightOpenBaoPersistence(recoveryCfg.parentCredentialConfig(), "recovery capability credential persistence"); err != nil {
			return err
		}
		if err := provisionRecoveryCredential(ctx, recoveryCfg, accountAdmin, &out); err != nil {
			return err
		}
		out.RecoveryConditions = append(out.RecoveryConditions, "RecoveryCredentialsPersisted")
	} else {
		out.RecoveryConditions = append(out.RecoveryConditions, "RecoveryCredentialsAvailable")
	}

	desiredDNS, err := dnsDesiredStateFromRecovery(*cfg.recovery)
	if err != nil {
		return err
	}
	dnsReport, err := reconcileDNSDesired(ctx, cfg, accountAdmin, apiClient, desiredDNS)
	if err != nil {
		return err
	}
	mergeDNSReport(&out, dnsReport)
	if cfg.dryRun {
		out.RecoveryConditions = append(out.RecoveryConditions, "DNSDiffed", "DryRun")
		sort.Strings(out.RecoveryConditions)
		return writeReport(out)
	}
	out.RecoveryConditions = append(out.RecoveryConditions, "DNSConverged")

	certificates, zones, err := tlsCertificatesFromRecovery(*cfg.recovery)
	if err != nil {
		return err
	}
	tlsReport, err := issueCertificatesWithClient(ctx, cfg, apiClient, certificates, zones, accountAdmin.Source)
	if err != nil {
		return err
	}
	out.TLSCertificates = tlsReport.TLSCertificates
	out.RecoveryConditions = append(out.RecoveryConditions, "CertificatesReady")

	adminSecret := cfg.objectStorageRuntimeSecretNames().AdminAccessKeyName()
	if err := preflightOpenBaoPersistence(cfg.runtimeOpenBaoCredentialConfig(runtimeSecretOpenBaoPath(adminSecret)), "object-storage provider runtime secret projection"); err != nil {
		return err
	}
	if err := provisionObjectStorageProviderCredential(ctx, cfg, accountAdmin, &out); err != nil {
		return err
	}
	out.RecoveryConditions = append(out.RecoveryConditions, "ObjectStorageCredentialsPersisted")
	sort.Strings(out.RecoveryConditions)
	return writeReport(out)
}

func recoveryCredentialConfig(cfg config) config {
	recoveryCfg := cfg
	recoveryCfg.bucket = cfg.recovery.Spec.ObjectStorage.RecoveryBucket
	recoveryCfg.openBaoPath = capabilityOpenBaoPath("recovery")
	return recoveryCfg
}

func verifyRecoveryCredential(ctx context.Context, cfg config, out *report) error {
	credentialCfg := cfg.parentCredentialConfig()
	credentialCfg.OpenBaoPath = capabilityOpenBaoPath("recovery")
	recovery, err := r2control.LoadParentCredentials(ctx, credentialCfg)
	if err != nil {
		return err
	}
	client, err := r2control.NewR2Client(r2control.R2ClientConfig{
		Endpoint:        r2control.Endpoint(cfg.accountID),
		Region:          cfg.region,
		AccessKeyID:     recovery.AccessKeyID,
		SecretAccessKey: recovery.SecretAccessKey,
		SessionToken:    recovery.SessionToken,
		Source:          "cloudflare-control-plane-recovery-credential",
		Timeout:         cfg.timeout,
	})
	if err != nil {
		return err
	}
	if err := verifyObjectRoundTrip(ctx, client, cfg, "recovery", out); err != nil {
		return err
	}
	out.ChildAccessKeyIDFingerprint = r2control.Fingerprint(recovery.AccessKeyID)
	out.ChildSecretKeyFingerprint = r2control.Fingerprint(recovery.SecretAccessKey)
	out.VerificationObjectGetStatus = out.TestObjectGetStatus
	return nil
}

func cloudflareRecoveryAuthorityUnavailableError(accountAdminPath string, accountAdminErr error, recoveryCredentialPath string, recoveryCredentialErr error) error {
	return fmt.Errorf(
		"CloudflareRecoveryAuthorityAvailable=False: %s: no deployed Cloudflare account-admin authority exists at %s (%v), and no bucket-scoped R2 recovery credential exists at %s (%v). Provide root trust material only through the operator import path: cloudflare-control-plane --action=import-account-admin --operator-import-stdin. Use the encrypted OpenBao operator import token from init-material.json and keep account-admin material in an operator-local, gitignored source such as secret.env until it is imported into OpenBao.",
		rootTrustMaterialRequiredReason,
		accountAdminPath,
		accountAdminErr,
		recoveryCredentialPath,
		recoveryCredentialErr,
	)
}

func dnsDesiredStateFromRecovery(doc CloudflareControlPlane) (dnsDesiredState, error) {
	zones := map[string]CloudflareDNSZone{}
	for _, zone := range doc.Spec.DNS.Zones {
		zones[strings.TrimSpace(zone.Name)] = zone
	}
	out := dnsDesiredState{}
	for _, record := range doc.Spec.DNS.Records {
		zone, ok := zones[strings.TrimSpace(record.Zone)]
		if !ok {
			return dnsDesiredState{}, fmt.Errorf("recovery DNS record references unknown zone %q", record.Zone)
		}
		fqdn := publicFQDN(zone.Domain, record.Record)
		relative, err := recordNameForHostedZone(fqdn, zone.Zone)
		if err != nil {
			return dnsDesiredState{}, err
		}
		out.records = append(out.records, dnsDesiredRecord{
			zoneName: strings.Trim(strings.TrimSpace(zone.Zone), "."),
			record:   relative,
			fqdn:     fqdn,
			targetIP: strings.TrimSpace(doc.Spec.TargetIPv4),
			ttl:      record.TTL,
			proxied:  record.Proxied,
		})
	}
	return out, nil
}

func tlsCertificatesFromRecovery(doc CloudflareControlPlane) ([]siteTLSCertificate, []string, error) {
	certificates := make([]siteTLSCertificate, 0, len(doc.Spec.TLS.Certificates))
	for _, certificate := range doc.Spec.TLS.Certificates {
		certificates = append(certificates, siteTLSCertificate{
			Name:    strings.TrimSpace(certificate.Name),
			Domains: trimStringSlice(certificate.Domains),
		})
	}
	seen := map[string]struct{}{}
	for _, zone := range doc.Spec.DNS.Zones {
		seen[strings.Trim(strings.TrimSpace(zone.Zone), ".")] = struct{}{}
	}
	zones := make([]string, 0, len(seen))
	for zone := range seen {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	if len(zones) == 0 {
		return nil, nil, fmt.Errorf("recovery TLS needs at least one DNS zone")
	}
	return certificates, zones, nil
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (cfg config) objectStorageRuntimeSecretNames() ObjectStorageRuntimeKeys {
	if cfg.recovery != nil {
		return cfg.recovery.Spec.ObjectStorage.RuntimeSecrets
	}
	return ObjectStorageRuntimeKeys{
		AdminAccessKeyID:     "object-storage-service.r2.admin_access_key_id",
		AdminSecretAccessKey: "object-storage-service.r2.admin_secret_access_key",
		ProxyAccessKeyID:     "object-storage-service.r2.proxy_access_key_id",
		ProxySecretAccessKey: "object-storage-service.r2.proxy_secret_access_key",
	}
}

func mergeDNSReport(out *report, dns report) {
	out.DNSZones = dns.DNSZones
	out.DNSRecordsSeen = dns.DNSRecordsSeen
	out.DNSRecordsDiffed = dns.DNSRecordsDiffed
	out.DNSRecordsApplied = dns.DNSRecordsApplied
	out.DNSDryRun = dns.DNSDryRun
	out.DNSChanges = dns.DNSChanges
}

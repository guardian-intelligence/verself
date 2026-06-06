package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/integrations/cloudflare/control-plane/r2control"
)

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
	accountAdmin, imported, err := loadAndVerifyAccountAdmin(ctx, cfg, accountAdminOpenBaoPath(cfg))
	if err != nil {
		return fmt.Errorf("CloudflareAccountAuthorityAvailable=False: %w", err)
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	out := baseReport(cfg, accountAdmin.Source)
	out.Action = "recover"
	out.VerifiedWith = "cloudflare-recovery"
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
	out.AccountAdminStatus = imported
	out.RecoveryConditions = append(out.RecoveryConditions, "AccountTokenVerified")

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

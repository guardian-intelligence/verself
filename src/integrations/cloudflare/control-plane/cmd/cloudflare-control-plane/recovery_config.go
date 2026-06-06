package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	cloudflareControlPlaneAPIVersion = "cloudflare.guardianintelligence.org/v1alpha1"
	kindCloudflareControlPlane       = "CloudflareControlPlane"
)

var (
	dnsNameRE    = regexp.MustCompile(`^[A-Za-z0-9_*.-]+$`)
	objectNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	accountIDRE  = regexp.MustCompile(`^[a-f0-9]{32}$`)
	bucketRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	secretNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

type resourceMetadata struct {
	Name string `yaml:"name" json:"name"`
}

type CloudflareControlPlane struct {
	APIVersion string                     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                     `yaml:"kind" json:"kind"`
	Metadata   resourceMetadata           `yaml:"metadata" json:"metadata"`
	Spec       CloudflareControlPlaneSpec `yaml:"spec" json:"spec"`
}

type CloudflareControlPlaneSpec struct {
	Site                    string          `yaml:"site" json:"site"`
	AccountID               string          `yaml:"accountID" json:"accountID"`
	AccountAdminOpenBaoPath string          `yaml:"accountAdminOpenBaoPath" json:"accountAdminOpenBaoPath"`
	TargetIPv4              string          `yaml:"targetIPv4" json:"targetIPv4"`
	OpenBao                 OpenBao         `yaml:"openBao" json:"openBao"`
	DNS                     CloudflareDNS   `yaml:"dns" json:"dns"`
	TLS                     CloudflareTLS   `yaml:"tls" json:"tls"`
	ObjectStorage           R2ObjectStorage `yaml:"objectStorage" json:"objectStorage"`
}

type OpenBao struct {
	Address    string `yaml:"address" json:"address"`
	TokenFile  string `yaml:"tokenFile" json:"tokenFile"`
	CACertFile string `yaml:"caCertFile,omitempty" json:"caCertFile,omitempty"`
}

type CloudflareDNS struct {
	Zones   []CloudflareDNSZone   `yaml:"zones" json:"zones"`
	Records []CloudflareDNSRecord `yaml:"records" json:"records"`
}

type CloudflareDNSZone struct {
	Name   string `yaml:"name" json:"name"`
	Zone   string `yaml:"zone" json:"zone"`
	Domain string `yaml:"domain" json:"domain"`
}

type CloudflareDNSRecord struct {
	Zone    string `yaml:"zone" json:"zone"`
	Record  string `yaml:"record" json:"record"`
	TTL     int    `yaml:"ttl" json:"ttl"`
	Proxied bool   `yaml:"proxied" json:"proxied"`
}

type CloudflareTLS struct {
	OutputDir    string              `yaml:"outputDir" json:"outputDir"`
	ACME         ACME                `yaml:"acme" json:"acme"`
	Certificates []CloudflareTLSCert `yaml:"certificates" json:"certificates"`
}

type ACME struct {
	DirectoryURL       string `yaml:"directoryURL" json:"directoryURL"`
	ContactEmail       string `yaml:"contactEmail" json:"contactEmail"`
	DNSPropagationWait string `yaml:"dnsPropagationWait" json:"dnsPropagationWait"`
	RenewBefore        string `yaml:"renewBefore" json:"renewBefore"`
}

type CloudflareTLSCert struct {
	Name    string   `yaml:"name" json:"name"`
	Domains []string `yaml:"domains" json:"domains"`
}

type R2ObjectStorage struct {
	Bucket         string                   `yaml:"bucket" json:"bucket"`
	ChildTokenTTL  string                   `yaml:"childTokenTTL" json:"childTokenTTL"`
	RuntimeSecrets ObjectStorageRuntimeKeys `yaml:"runtimeSecrets" json:"runtimeSecrets"`
}

type ObjectStorageRuntimeKeys struct {
	AdminAccessKeyID     string `yaml:"adminAccessKeyID" json:"adminAccessKeyID"`
	AdminSecretAccessKey string `yaml:"adminSecretAccessKey" json:"adminSecretAccessKey"`
	ProxyAccessKeyID     string `yaml:"proxyAccessKeyID" json:"proxyAccessKeyID"`
	ProxySecretAccessKey string `yaml:"proxySecretAccessKey" json:"proxySecretAccessKey"`
}

func loadCloudflareControlPlane(path string) (CloudflareControlPlane, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return CloudflareControlPlane{}, fmt.Errorf("read CloudflareControlPlane %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	var doc CloudflareControlPlane
	if err := decoder.Decode(&doc); err != nil {
		return CloudflareControlPlane{}, fmt.Errorf("decode CloudflareControlPlane %s: %w", path, err)
	}
	if err := doc.validate(); err != nil {
		return CloudflareControlPlane{}, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func (d CloudflareControlPlane) validate() error {
	if strings.TrimSpace(d.APIVersion) != cloudflareControlPlaneAPIVersion {
		return fmt.Errorf("apiVersion must be %s", cloudflareControlPlaneAPIVersion)
	}
	if strings.TrimSpace(d.Kind) != kindCloudflareControlPlane {
		return fmt.Errorf("kind must be %s", kindCloudflareControlPlane)
	}
	if !objectNameRE.MatchString(strings.TrimSpace(d.Metadata.Name)) {
		return fmt.Errorf("metadata.name must be a DNS label")
	}
	if !objectNameRE.MatchString(strings.TrimSpace(d.Spec.Site)) {
		return fmt.Errorf("spec.site must be a DNS label")
	}
	if !accountIDRE.MatchString(strings.TrimSpace(d.Spec.AccountID)) {
		return fmt.Errorf("spec.accountID must be a 32-character lowercase hex Cloudflare account ID")
	}
	accountAdminPath := strings.Trim(strings.TrimSpace(d.Spec.AccountAdminOpenBaoPath), "/")
	if accountAdminPath == "" {
		return fmt.Errorf("spec.accountAdminOpenBaoPath is required")
	}
	if !strings.Contains(accountAdminPath, "/data/") {
		return fmt.Errorf("spec.accountAdminOpenBaoPath must be a KV v2 data path")
	}
	if ip := net.ParseIP(strings.TrimSpace(d.Spec.TargetIPv4)); ip == nil || ip.To4() == nil {
		return fmt.Errorf("spec.targetIPv4 must be an IPv4 address")
	}
	if err := d.Spec.OpenBao.validate(); err != nil {
		return fmt.Errorf("spec.openBao: %w", err)
	}
	if err := d.Spec.DNS.validate(); err != nil {
		return fmt.Errorf("spec.dns: %w", err)
	}
	if err := d.Spec.TLS.validate(); err != nil {
		return fmt.Errorf("spec.tls: %w", err)
	}
	if err := d.Spec.ObjectStorage.validate(); err != nil {
		return fmt.Errorf("spec.objectStorage: %w", err)
	}
	return nil
}

func (o OpenBao) validate() error {
	if _, err := url.ParseRequestURI(strings.TrimSpace(o.Address)); err != nil {
		return fmt.Errorf("address must be an absolute URL: %w", err)
	}
	if strings.TrimSpace(o.TokenFile) == "" {
		return fmt.Errorf("tokenFile is required")
	}
	return nil
}

func (d CloudflareDNS) validate() error {
	if len(d.Zones) == 0 {
		return fmt.Errorf("zones requires at least one zone")
	}
	zones := map[string]CloudflareDNSZone{}
	for i, zone := range d.Zones {
		name := strings.TrimSpace(zone.Name)
		if !objectNameRE.MatchString(name) {
			return fmt.Errorf("zones[%d].name must be a DNS label", i)
		}
		if _, exists := zones[name]; exists {
			return fmt.Errorf("zones[%d].name duplicates %q", i, name)
		}
		if !validDNSName(zone.Zone) {
			return fmt.Errorf("zones[%d].zone must be a DNS name", i)
		}
		if !validDNSName(zone.Domain) {
			return fmt.Errorf("zones[%d].domain must be a DNS name", i)
		}
		zones[name] = zone
	}
	if len(d.Records) == 0 {
		return fmt.Errorf("records requires at least one record")
	}
	seen := map[string]struct{}{}
	for i, record := range d.Records {
		zone := strings.TrimSpace(record.Zone)
		if _, ok := zones[zone]; !ok {
			return fmt.Errorf("records[%d].zone references unknown zone %q", i, zone)
		}
		name := strings.TrimSpace(record.Record)
		if name == "" {
			return fmt.Errorf("records[%d].record is required", i)
		}
		if record.TTL <= 0 {
			return fmt.Errorf("records[%d].ttl must be positive", i)
		}
		key := zone + "\x00" + name
		if _, exists := seen[key]; exists {
			return fmt.Errorf("records[%d] duplicates zone=%s record=%s", i, zone, name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (t CloudflareTLS) validate() error {
	if strings.TrimSpace(t.OutputDir) == "" {
		return fmt.Errorf("outputDir is required")
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(t.ACME.DirectoryURL)); err != nil {
		return fmt.Errorf("acme.directoryURL must be an absolute URL: %w", err)
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(t.ACME.ContactEmail)); err != nil {
		return fmt.Errorf("acme.contactEmail must be an email address: %w", err)
	}
	if _, err := parsePositiveDuration("acme.dnsPropagationWait", t.ACME.DNSPropagationWait); err != nil {
		return err
	}
	if _, err := parsePositiveDuration("acme.renewBefore", t.ACME.RenewBefore); err != nil {
		return err
	}
	if len(t.Certificates) == 0 {
		return fmt.Errorf("certificates requires at least one certificate")
	}
	for i, certificate := range t.Certificates {
		if strings.TrimSpace(certificate.Name) == "" {
			return fmt.Errorf("certificates[%d].name is required", i)
		}
		if len(certificate.Domains) == 0 {
			return fmt.Errorf("certificates[%d].domains requires at least one domain", i)
		}
		for j, domain := range certificate.Domains {
			if !validDNSName(strings.TrimPrefix(strings.TrimSpace(domain), "*.")) {
				return fmt.Errorf("certificates[%d].domains[%d] must be a DNS name", i, j)
			}
		}
	}
	return nil
}

func (s R2ObjectStorage) validate() error {
	if !bucketRE.MatchString(strings.TrimSpace(s.Bucket)) || strings.Contains(s.Bucket, "..") {
		return fmt.Errorf("bucket must be a valid lowercase R2 bucket name")
	}
	if _, err := parsePositiveDuration("childTokenTTL", s.ChildTokenTTL); err != nil {
		return err
	}
	names := []string{
		s.RuntimeSecrets.AdminAccessKeyID,
		s.RuntimeSecrets.AdminSecretAccessKey,
		s.RuntimeSecrets.ProxyAccessKeyID,
		s.RuntimeSecrets.ProxySecretAccessKey,
	}
	seen := map[string]struct{}{}
	for i, name := range names {
		name = strings.TrimSpace(name)
		if !secretNameRE.MatchString(name) {
			return fmt.Errorf("runtimeSecrets[%d] must be a runtime secret logical name", i)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("runtimeSecrets[%d] duplicates %q", i, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func parsePositiveDuration(field string, raw string) (time.Duration, error) {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration: %w", field, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", field)
	}
	return value, nil
}

func validDNSName(raw string) bool {
	name := strings.Trim(strings.TrimSpace(raw), ".")
	if name == "" || strings.Contains(name, "{{") || !dnsNameRE.MatchString(name) {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return false
		}
		if label == "*" {
			continue
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

func (d CloudflareControlPlane) childTokenTTL() (time.Duration, error) {
	return parsePositiveDuration("spec.objectStorage.childTokenTTL", d.Spec.ObjectStorage.ChildTokenTTL)
}

func (d CloudflareControlPlane) acmeDNSPropagationWait() (time.Duration, error) {
	return parsePositiveDuration("spec.tls.acme.dnsPropagationWait", d.Spec.TLS.ACME.DNSPropagationWait)
}

func (d CloudflareControlPlane) acmeRenewBefore() (time.Duration, error) {
	return parsePositiveDuration("spec.tls.acme.renewBefore", d.Spec.TLS.ACME.RenewBefore)
}

func (k ObjectStorageRuntimeKeys) Map(adminAccessKeyID string, adminSecretAccessKey string, proxyAccessKeyID string, proxySecretAccessKey string) map[string]string {
	return map[string]string{
		strings.TrimSpace(k.AdminAccessKeyID):     adminAccessKeyID,
		strings.TrimSpace(k.AdminSecretAccessKey): adminSecretAccessKey,
		strings.TrimSpace(k.ProxyAccessKeyID):     proxyAccessKeyID,
		strings.TrimSpace(k.ProxySecretAccessKey): proxySecretAccessKey,
	}
}

func (k ObjectStorageRuntimeKeys) AdminAccessKeyName() string {
	return strings.TrimSpace(k.AdminAccessKeyID)
}

func expandRecoveryPath(path string) string {
	return os.ExpandEnv(strings.TrimSpace(path))
}

func requiredRecoveryPath(path string, field string) (string, error) {
	path = expandRecoveryPath(path)
	if path == "" {
		return "", errors.New(field + " is required")
	}
	return path, nil
}

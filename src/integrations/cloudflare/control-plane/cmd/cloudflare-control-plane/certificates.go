package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/internal/r2control"
	"golang.org/x/crypto/acme"
	"gopkg.in/yaml.v3"
)

const letsEncryptProductionDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

type tlsCertificateReport struct {
	Name      string   `json:"name"`
	Domains   []string `json:"domains"`
	PEMFile   string   `json:"pem_file"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Reused    bool     `json:"reused"`
}

type siteTLSCertificate struct {
	Name    string
	Domains []string
}

type acmeChallengeRecord struct {
	ZoneID    string
	RecordID  string
	DNSName   string
	TXTValue  string
	AuthzURL  string
	Challenge *acme.Challenge
}

func issueSiteCertificates(ctx context.Context, cfg config) error {
	accountAdmin, err := loadRequiredAccountAdminCredentials(ctx, cfg)
	if err != nil {
		return err
	}
	apiClient, err := r2control.NewCloudflareAPIClient(accountAdmin.APIToken, cfg.timeout)
	if err != nil {
		return err
	}
	certificates, zones, err := loadSiteTLSCertificates(cfg)
	if err != nil {
		return err
	}
	zoneIDsByName, err := apiClient.ZonesByName(ctx, zones)
	if err != nil {
		return fmt.Errorf("list cloudflare zones for ACME DNS-01: %w", err)
	}
	projectionDir := certificateProjectionDir(cfg)
	accountKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ACME account key: %w", err)
	}
	acmeClient := &acme.Client{
		Key:          accountKey,
		DirectoryURL: strings.TrimSpace(cfg.acmeDirectoryURL),
	}
	contact, err := acmeContact(cfg.acmeContactEmail)
	if err != nil {
		return err
	}
	if _, err := acmeClient.Register(ctx, &acme.Account{Contact: []string{contact}}, acme.AcceptTOS); err != nil {
		return fmt.Errorf("register ACME account: %w", err)
	}

	out := baseReport(cfg, accountAdmin.Source)
	out.ParentAccessKeyIDFingerprint = r2control.Fingerprint(accountAdmin.AccessKeyID)
	out.VerifiedWith = "cloudflare-account-admin-acme-dns01"
	for _, certificate := range certificates {
		pemFile := filepath.Join(projectionDir, certificate.Name+".pem")
		expiresAt, reusable, err := projectedCertificateReusable(pemFile, certificate.Domains, cfg.certificateRenewBefore)
		if err != nil {
			return err
		}
		if !reusable {
			pemBody, expires, err := issueACMECertificate(ctx, acmeClient, apiClient, zoneIDsByName, certificate.Domains, cfg.acmeDNSPropagationWait)
			if err != nil {
				return fmt.Errorf("issue certificate %s: %w", certificate.Name, err)
			}
			if err := writeProjectedCertificate(pemFile, pemBody); err != nil {
				return err
			}
			expiresAt = expires.Format(time.RFC3339)
		}
		out.TLSCertificates = append(out.TLSCertificates, tlsCertificateReport{
			Name:      certificate.Name,
			Domains:   certificate.Domains,
			PEMFile:   pemFile,
			ExpiresAt: expiresAt,
			Reused:    reusable,
		})
	}
	return writeReport(out)
}

func loadSiteTLSCertificates(cfg config) ([]siteTLSCertificate, []string, error) {
	path := filepath.Join(cfg.repoRoot, "src", "host", "sites", cfg.site, "vars.yml")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var siteVars struct {
		VerselfDomain         string `yaml:"verself_domain"`
		CompanyDomain         string `yaml:"company_domain"`
		CloudflareProductZone string `yaml:"cloudflare_product_zone"`
		CloudflareCompanyZone string `yaml:"cloudflare_company_zone"`
	}
	if err := yaml.Unmarshal(body, &siteVars); err != nil {
		return nil, nil, fmt.Errorf("decode %s: %w", path, err)
	}
	verselfDomain := cleanDNSName(siteVars.VerselfDomain)
	companyDomain := cleanDNSName(siteVars.CompanyDomain)
	productZone := cleanDNSName(firstNonEmpty(siteVars.CloudflareProductZone, siteVars.VerselfDomain))
	companyZone := cleanDNSName(firstNonEmpty(siteVars.CloudflareCompanyZone, siteVars.CompanyDomain))
	if verselfDomain == "" || productZone == "" {
		return nil, nil, fmt.Errorf("%s: verself_domain and cloudflare_product_zone are required for TLS certificate issuance", path)
	}
	if companyDomain == "" || companyZone == "" {
		return nil, nil, fmt.Errorf("%s: company_domain and cloudflare_company_zone are required for TLS certificate issuance", path)
	}
	certificates := []siteTLSCertificate{
		{
			Name: verselfDomain,
			Domains: []string{
				verselfDomain,
				"*." + verselfDomain,
				"*.api." + verselfDomain,
			},
		},
		{
			Name:    companyDomain,
			Domains: []string{companyDomain},
		},
	}
	zones := dedupeStrings([]string{productZone, companyZone})
	return certificates, zones, nil
}

func issueACMECertificate(ctx context.Context, acmeClient *acme.Client, apiClient *r2control.CloudflareAPIClient, zones map[string]string, domains []string, propagationWait time.Duration) ([]byte, time.Time, error) {
	order, err := acmeClient.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("create ACME order: %w", err)
	}
	records := []acmeChallengeRecord{}
	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		for _, record := range records {
			if err := apiClient.DeleteDNSRecord(context.Background(), record.ZoneID, record.RecordID); err != nil {
				fmt.Fprintf(os.Stderr, "cloudflare-control-plane: cleanup ACME TXT %s: %v\n", record.DNSName, err)
			}
		}
	}()
	for _, authzURL := range order.AuthzURLs {
		authz, err := acmeClient.GetAuthorization(ctx, authzURL)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("get ACME authorization: %w", err)
		}
		if authz.Status == acme.StatusValid {
			continue
		}
		if authz.Status != acme.StatusPending {
			return nil, time.Time{}, fmt.Errorf("ACME authorization %s status is %s", authz.Identifier.Value, authz.Status)
		}
		challenge := findDNS01Challenge(authz.Challenges)
		if challenge == nil {
			return nil, time.Time{}, fmt.Errorf("ACME authorization %s has no dns-01 challenge", authz.Identifier.Value)
		}
		txtValue, err := acmeClient.DNS01ChallengeRecord(challenge.Token)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("build ACME dns-01 value: %w", err)
		}
		dnsName := acmeChallengeName(authz.Identifier.Value)
		zoneID, err := zoneIDForDNSName(dnsName, zones)
		if err != nil {
			return nil, time.Time{}, err
		}
		created, err := apiClient.CreateTXTRecord(ctx, zoneID, dnsName, txtValue, 60)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("create ACME TXT %s: %w", dnsName, err)
		}
		records = append(records, acmeChallengeRecord{
			ZoneID:    zoneID,
			RecordID:  created.ID,
			DNSName:   dnsName,
			TXTValue:  txtValue,
			AuthzURL:  authzURL,
			Challenge: challenge,
		})
	}
	for _, record := range records {
		if err := waitForTXT(ctx, record.DNSName, record.TXTValue, propagationWait); err != nil {
			return nil, time.Time{}, err
		}
	}
	for _, record := range records {
		if _, err := acmeClient.Accept(ctx, record.Challenge); err != nil {
			return nil, time.Time{}, fmt.Errorf("accept ACME challenge %s: %w", record.DNSName, err)
		}
	}
	for _, record := range records {
		if _, err := acmeClient.WaitAuthorization(ctx, record.AuthzURL); err != nil {
			return nil, time.Time{}, fmt.Errorf("wait ACME authorization %s: %w", record.DNSName, err)
		}
	}
	order, err = acmeClient.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("wait ACME order: %w", err)
	}
	if order.Status != acme.StatusReady && order.Status != acme.StatusValid {
		return nil, time.Time{}, fmt.Errorf("ACME order status is %s", order.Status)
	}
	csrDER, keyPEM, err := certificateRequest(domains)
	if err != nil {
		return nil, time.Time{}, err
	}
	var certDER [][]byte
	if order.Status == acme.StatusValid {
		certDER, err = acmeClient.FetchCert(ctx, order.CertURL, true)
	} else {
		certDER, _, err = acmeClient.CreateOrderCert(ctx, order.FinalizeURL, csrDER, true)
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("finalize ACME order: %w", err)
	}
	pemBody, expiresAt, err := certificatePEM(keyPEM, certDER, domains)
	if err != nil {
		return nil, time.Time{}, err
	}
	return pemBody, expiresAt, nil
}

func certificateRequest(domains []string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate certificate key: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal certificate key: %w", err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: firstDNSName(domains)},
		DNSNames: domains,
	}, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate request: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return csr, keyPEM, nil
}

func certificatePEM(keyPEM []byte, certDER [][]byte, domains []string) ([]byte, time.Time, error) {
	if len(certDER) == 0 {
		return nil, time.Time{}, errors.New("ACME returned no certificate chain")
	}
	leaf, err := x509.ParseCertificate(certDER[0])
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("parse ACME leaf certificate: %w", err)
	}
	for _, domain := range domains {
		if !containsString(leaf.DNSNames, domain) {
			return nil, time.Time{}, fmt.Errorf("ACME certificate missing DNS SAN %s", domain)
		}
	}
	out := append([]byte{}, keyPEM...)
	for _, der := range certDER {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	if _, err := tls.X509KeyPair(out, out); err != nil {
		return nil, time.Time{}, fmt.Errorf("validate projected certificate keypair: %w", err)
	}
	return out, leaf.NotAfter.UTC(), nil
}

func projectedCertificateReusable(path string, domains []string, renewBefore time.Duration) (string, bool, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read projected certificate %s: %w", path, err)
	}
	if _, err := tls.X509KeyPair(body, body); err != nil {
		return "", false, nil
	}
	leaf, err := firstCertificate(body)
	if err != nil {
		return "", false, nil
	}
	if time.Until(leaf.NotAfter) <= renewBefore {
		return leaf.NotAfter.UTC().Format(time.RFC3339), false, nil
	}
	for _, domain := range domains {
		if !containsString(leaf.DNSNames, domain) {
			return leaf.NotAfter.UTC().Format(time.RFC3339), false, nil
		}
	}
	return leaf.NotAfter.UTC().Format(time.RFC3339), true, nil
}

func firstCertificate(body []byte) (*x509.Certificate, error) {
	rest := body
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, errors.New("certificate PEM contains no certificate")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		rest = remaining
	}
}

func writeProjectedCertificate(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create certificate projection directory: %w", err)
	}
	tmp := path + ".tmp-" + randomSuffix()
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write certificate projection: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish certificate projection: %w", err)
	}
	return nil
}

func randomSuffix() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return fmt.Sprintf("%016x", new(big.Int).SetBytes(raw[:]))
}

func certificateProjectionDir(cfg config) string {
	if strings.TrimSpace(cfg.certificateProjectionDir) != "" {
		if filepath.IsAbs(cfg.certificateProjectionDir) {
			return filepath.Clean(cfg.certificateProjectionDir)
		}
		return filepath.Join(cfg.repoRoot, cfg.certificateProjectionDir)
	}
	return filepath.Join(cfg.repoRoot, ".verself", "site-bootstrap", cfg.site, "tls", "haproxy")
}

func acmeContact(raw string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("--acme-contact-email must be a valid email address: %w", err)
	}
	return "mailto:" + address.Address, nil
}

func findDNS01Challenge(challenges []*acme.Challenge) *acme.Challenge {
	for _, challenge := range challenges {
		if challenge != nil && challenge.Type == "dns-01" {
			return challenge
		}
	}
	return nil
}

func acmeChallengeName(identifier string) string {
	identifier = cleanDNSName(strings.TrimPrefix(strings.TrimSpace(identifier), "*."))
	return "_acme-challenge." + identifier
}

func zoneIDForDNSName(name string, zones map[string]string) (string, error) {
	name = cleanDNSName(name)
	matches := make([]string, 0, len(zones))
	for zone := range zones {
		zone = cleanDNSName(zone)
		if name == zone || strings.HasSuffix(name, "."+zone) {
			matches = append(matches, zone)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no Cloudflare zone covers ACME challenge name %s", name)
	}
	sort.Slice(matches, func(i, j int) bool { return len(matches[i]) > len(matches[j]) })
	return zones[matches[0]], nil
}

func waitForTXT(ctx context.Context, name, value string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolver := cloudflareResolver()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		txts, err := resolver.LookupTXT(waitCtx, name)
		if err == nil && containsString(txts, value) {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("ACME TXT %s did not propagate before deadline: %w", name, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func cloudflareResolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, network, "1.1.1.1:53")
		},
	}
}

func cleanDNSName(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ".")
}

func firstDNSName(domains []string) string {
	if len(domains) == 0 {
		return ""
	}
	return domains[0]
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

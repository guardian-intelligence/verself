package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCloudflareRecoveryRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudflare.yml")
	if err := os.WriteFile(path, []byte(validCloudflareRecoveryYAML()+"unexpected: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCloudflareRecovery(path)
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestCloudflareRecoveryValidatesMinimalGammaShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudflare.yml")
	if err := os.WriteFile(path, []byte(validCloudflareRecoveryYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadCloudflareRecovery(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Spec.Site != "gamma" {
		t.Fatalf("site = %q", doc.Spec.Site)
	}
	if doc.Spec.ObjectStorage.RuntimeSecrets.AdminAccessKeyName() != "object-storage-service.r2.admin_access_key_id" {
		t.Fatalf("runtime secrets = %#v", doc.Spec.ObjectStorage.RuntimeSecrets)
	}
}

func TestCloudflareRecoveryRejectsDuplicateRuntimeSecretNames(t *testing.T) {
	doc := validCloudflareRecovery()
	doc.Spec.ObjectStorage.RuntimeSecrets.ProxyAccessKeyID = doc.Spec.ObjectStorage.RuntimeSecrets.AdminAccessKeyID
	err := doc.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("error = %v", err)
	}
}

func validCloudflareRecovery() CloudflareRecovery {
	return CloudflareRecovery{
		APIVersion: APIVersion,
		Kind:       KindCloudflareRecovery,
		Metadata:   Metadata{Name: "gamma-cloudflare"},
		Spec: CloudflareRecoverySpec{
			Site:                  "gamma",
			AccountID:             "0123456789abcdef0123456789abcdef",
			AccountAdminTokenFile: "/run/verself/recovery/credentials/cloudflare-account-admin-api-token",
			TargetIPv4:            "203.0.113.10",
			OpenBao: OpenBao{
				Address:   "https://127.0.0.1:8200",
				TokenFile: "${NOMAD_SECRETS_DIR}/vault_token",
			},
			DNS: CloudflareDNS{
				Zones: []CloudflareDNSZone{
					{Name: "product", Zone: "verself.sh", Domain: "gamma.verself.sh"},
				},
				Records: []CloudflareDNSRecord{
					{Zone: "product", Record: "@", TTL: 1},
				},
			},
			TLS: CloudflareTLS{
				OutputDir: "/etc/haproxy/certs",
				ACME: ACME{
					DirectoryURL:       "https://acme-v02.api.letsencrypt.org/directory",
					ContactEmail:       "agents@guardianintelligence.org",
					DNSPropagationWait: "2m",
					RenewBefore:        "720h",
				},
				Certificates: []CloudflareTLSCert{
					{Name: "gamma.verself.sh", Domains: []string{"gamma.verself.sh", "*.gamma.verself.sh"}},
				},
			},
			ObjectStorage: R2ObjectStorage{
				Bucket:        "verself-deployment-artifacts",
				ChildTokenTTL: "168h",
				RuntimeSecrets: ObjectStorageRuntimeKeys{
					AdminAccessKeyID:     "object-storage-service.r2.admin_access_key_id",
					AdminSecretAccessKey: "object-storage-service.r2.admin_secret_access_key",
					ProxyAccessKeyID:     "object-storage-service.r2.proxy_access_key_id",
					ProxySecretAccessKey: "object-storage-service.r2.proxy_secret_access_key",
				},
			},
		},
	}
}

func validCloudflareRecoveryYAML() string {
	return `apiVersion: recovery.verself.sh/v1alpha1
kind: CloudflareRecovery
metadata:
  name: gamma-cloudflare
spec:
  site: gamma
  accountID: 0123456789abcdef0123456789abcdef
  accountAdminTokenFile: /run/verself/recovery/credentials/cloudflare-account-admin-api-token
  targetIPv4: 203.0.113.10
  openBao:
    address: https://127.0.0.1:8200
    tokenFile: ${NOMAD_SECRETS_DIR}/vault_token
  dns:
    zones:
      - name: product
        zone: verself.sh
        domain: gamma.verself.sh
    records:
      - zone: product
        record: "@"
        ttl: 1
        proxied: false
  tls:
    outputDir: /etc/haproxy/certs
    acme:
      directoryURL: https://acme-v02.api.letsencrypt.org/directory
      contactEmail: agents@guardianintelligence.org
      dnsPropagationWait: 2m
      renewBefore: 720h
    certificates:
      - name: gamma.verself.sh
        domains:
          - gamma.verself.sh
          - "*.gamma.verself.sh"
  objectStorage:
    bucket: verself-deployment-artifacts
    childTokenTTL: 168h
    runtimeSecrets:
      adminAccessKeyID: object-storage-service.r2.admin_access_key_id
      adminSecretAccessKey: object-storage-service.r2.admin_secret_access_key
      proxyAccessKeyID: object-storage-service.r2.proxy_access_key_id
      proxySecretAccessKey: object-storage-service.r2.proxy_secret_access_key
`
}

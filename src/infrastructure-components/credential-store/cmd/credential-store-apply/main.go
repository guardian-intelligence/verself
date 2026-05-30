package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	clickhouseCAPath               = "/etc/clickhouse-client/server-ca.pem"
	openbaoAddr                    = "https://127.0.0.1:8200"
	openbaoCACertPath              = "/etc/openbao/tls/cert.pem"
	openbaoRootTokenPath           = "/etc/credstore/openbao/root-token"
	siteConfigMount                = "site-config"
	trustDomainPath                = "/etc/verself/spiffe-trust-domain"
	openbaoSpiffeJWTMount          = "spiffe-jwt"
	openbaoWorkloadAudience        = "openbao"
	openbaoWorkloadTokenTTL        = "15m"
	openbaoWorkloadTokenMaxTTL     = "1h"
	runtimeSecretKVMount           = "kv-runtime"
	runtimeSecretNamespace         = "runtime"
	runtimeSecretReadPolicy        = "secrets-runtime-secret-read"
	runtimeSecretWritePolicy       = "secrets-runtime-secret-write"
	runtimeSecretReadRole          = "secrets-runtime-read"
	runtimeSecretWriteRole         = "secrets-runtime-write"
	runtimeSecretConsumerPath      = "/svc/secrets-service"
	runtimeSecretSeedTimestampPath = "/etc/credstore/openbao/runtime-secrets-runtime-created-at"
	garageAdminTokenPath           = "/etc/garage/admin-token"
	governanceAPIActivityHMACPath  = "/etc/credstore/governance-service/api-activity-hmac-key"
	objectStorageCredentialKEKPath = "/etc/credstore/object-storage-service/credential-kek"
	objectStorageS3TLSCertPath     = "/etc/credstore/object-storage-service/s3-tls-cert"
	objectStorageS3TLSKeyPath      = "/etc/credstore/object-storage-service/s3-tls-key"
	objectStorageProxyKeyIDPath    = "/etc/credstore/object-storage-service/garage-proxy-access-key-id"
	objectStorageProxySecretPath   = "/etc/credstore/object-storage-service/garage-proxy-secret-access-key"
	spiceDBPresharedKeyPath        = "/etc/credstore/spicedb/grpc-preshared-key"
	sandboxRunnerBootstrapKeyPath  = "/etc/credstore/sandbox-rental/runner-bootstrap-secret"
)

type fileItem struct {
	Path  string
	Group string
}

type audienceItem struct {
	fileItem
	HostPrefix string
}

type siteSecretItem struct {
	fileItem
	Secret string
}

type runtimeSecretSeed struct {
	Name       string
	SiteSecret string
	File       string
}

type garageKeyResponse struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}

var clickhouseCAFiles = []fileItem{
	{Path: "/etc/credstore/analytics-service/clickhouse-ca-cert", Group: "analytics_service"},
	{Path: "/etc/credstore/billing/clickhouse-ca-cert", Group: "billing"},
	{Path: "/etc/credstore/distribution-service/clickhouse-ca-cert", Group: "distribution_service"},
	{Path: "/etc/credstore/github-integration-service/clickhouse-ca-cert", Group: "github_integration_service"},
	{Path: "/etc/credstore/governance-service/clickhouse-ca-cert", Group: "governance_service"},
	{Path: "/etc/credstore/iam-service/clickhouse-ca-cert", Group: "iam_service"},
	{Path: "/etc/credstore/notifications-service/clickhouse-ca-cert", Group: "notifications_service"},
	{Path: "/etc/credstore/object-storage-service/clickhouse-ca-cert", Group: "object_storage_service"},
	{Path: "/etc/credstore/sandbox-rental/clickhouse-ca-cert", Group: "sandbox_rental"},
	{Path: "/etc/otelcol/clickhouse-server-ca.pem", Group: "otelcol"},
}

var openbaoCAFiles = []fileItem{
	{Path: "/etc/credstore/secrets-service/openbao-ca-cert", Group: "secrets_service"},
}

var authAudiences = []audienceItem{
	{fileItem: fileItem{Path: "/etc/credstore/analytics-service/auth-audience", Group: "analytics_service"}, HostPrefix: "analytics.api"},
	{fileItem: fileItem{Path: "/etc/credstore/billing/auth-audience", Group: "billing"}, HostPrefix: "billing.api"},
	{fileItem: fileItem{Path: "/etc/credstore/distribution-service/auth-audience", Group: "distribution_service"}, HostPrefix: "distribution.api"},
	{fileItem: fileItem{Path: "/etc/credstore/email-service/auth-audience", Group: "email_service"}, HostPrefix: "email.api"},
	{fileItem: fileItem{Path: "/etc/credstore/github-integration-service/auth-audience", Group: "github_integration_service"}, HostPrefix: "github.api"},
	{fileItem: fileItem{Path: "/etc/credstore/governance-service/auth-audience", Group: "governance_service"}, HostPrefix: "governance.api"},
	{fileItem: fileItem{Path: "/etc/credstore/iam-service/auth-audience", Group: "iam_service"}, HostPrefix: "iam.api"},
	{fileItem: fileItem{Path: "/etc/credstore/notifications-service/auth-audience", Group: "notifications_service"}, HostPrefix: "notifications.api"},
	{fileItem: fileItem{Path: "/etc/credstore/object-storage-service/auth-audience", Group: "object_storage_service"}, HostPrefix: "object-storage.api"},
	{fileItem: fileItem{Path: "/etc/credstore/profile-service/auth-audience", Group: "profile_service"}, HostPrefix: "profile.api"},
	{fileItem: fileItem{Path: "/etc/credstore/projects-service/auth-audience", Group: "projects_service"}, HostPrefix: "projects.api"},
	{fileItem: fileItem{Path: "/etc/credstore/sandbox-rental/auth-audience", Group: "sandbox_rental"}, HostPrefix: "sandbox.api"},
	{fileItem: fileItem{Path: "/etc/credstore/secrets-service/auth-audience", Group: "secrets_service"}, HostPrefix: "secrets.api"},
	{fileItem: fileItem{Path: "/etc/credstore/source-code-hosting-service/auth-audience", Group: "source_code_hosting_service"}, HostPrefix: "source.api"},
}

var siteSecretFiles = []siteSecretItem{
	{fileItem: fileItem{Path: "/etc/credstore/electric/pg-password", Group: "root"}, Secret: "electric_pg_password"},
	{fileItem: fileItem{Path: "/etc/credstore/electric/api-secret", Group: "root"}, Secret: "electric_api_secret"},
	{fileItem: fileItem{Path: "/etc/credstore/electric-notifications/pg-password", Group: "root"}, Secret: "electric_notifications_pg_password"},
	{fileItem: fileItem{Path: "/etc/credstore/electric-notifications/api-secret", Group: "root"}, Secret: "electric_notifications_api_secret"},
	{fileItem: fileItem{Path: "/etc/credstore/electric-iam/pg-password", Group: "root"}, Secret: "electric_iam_pg_password"},
	{fileItem: fileItem{Path: "/etc/credstore/verself-web/electric-api-secret", Group: "root"}, Secret: "verself_web_electric_api_secret"},
	{fileItem: fileItem{Path: "/etc/credstore/stalwart/admin-password", Group: "stalwart"}, Secret: "stalwart_admin_password"},
}

var runtimeSecretSeeds = []runtimeSecretSeed{
	{Name: "billing-service.stripe.secret_key", SiteSecret: "stripe_secret_key"},
	{Name: "billing-service.stripe.webhook_secret", SiteSecret: "stripe_webhook_secret"},
	{Name: "email-service.resend.api_key", SiteSecret: "resend_api_key"},
	{Name: "email-service.stalwart.admin_password", SiteSecret: "stalwart_admin_password"},
	{Name: "github-integration-service.github.private_key", SiteSecret: "github_integration_service_github_app_private_key"},
	{Name: "github-integration-service.github.webhook_secret", SiteSecret: "github_integration_service_github_app_webhook_secret"},
	{Name: "github-integration-service.github.oauth_client_secret", SiteSecret: "github_integration_service_github_app_oauth_client_secret"},
	{Name: "iam-service.email_identity.hmac_key", SiteSecret: "iam_service_email_identity_hmac_key"},
	{Name: "iam-service.spicedb.grpc_preshared_key", File: spiceDBPresharedKeyPath},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "credential-store-apply: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var profile string
	fs := flag.NewFlagSet("credential-store-apply", flag.ContinueOnError)
	fs.StringVar(&profile, "profile", "host", "Credential profile to converge: host or object-storage.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch profile {
	case "host":
		return runHostCredentialProfile()
	case "object-storage":
		return runObjectStorageCredentialProfile()
	default:
		return fmt.Errorf("unsupported credential profile %q", profile)
	}
}

func runHostCredentialProfile() error {
	trustDomain, err := trustDomain()
	if err != nil {
		return err
	}
	domain := siteDomain(trustDomain)
	ca, err := os.ReadFile(clickhouseCAPath)
	if err != nil {
		return fmt.Errorf("read ClickHouse CA: %w", err)
	}
	for _, item := range clickhouseCAFiles {
		if err := writeCredstoreFile(item, ca); err != nil {
			return err
		}
	}
	openbaoCA, err := os.ReadFile(openbaoCACertPath)
	if err != nil {
		return fmt.Errorf("read OpenBao CA: %w", err)
	}
	for _, item := range openbaoCAFiles {
		if err := writeCredstoreFile(item, openbaoCA); err != nil {
			return err
		}
	}
	for _, item := range authAudiences {
		body := []byte("https://" + item.HostPrefix + "." + domain + "\n")
		if err := writeCredstoreFile(item.fileItem, body); err != nil {
			return err
		}
	}
	secrets, err := openbaoClient()
	if err != nil {
		return err
	}
	if _, err := ensureGeneratedSecretFile(fileItem{Path: spiceDBPresharedKeyPath, Group: "spicedb"}, 32); err != nil {
		return err
	}
	if _, err := ensureGeneratedSecretFile(fileItem{Path: sandboxRunnerBootstrapKeyPath, Group: "sandbox_rental"}, 32); err != nil {
		return err
	}
	if _, err := ensureGeneratedSecretFile(fileItem{Path: governanceAPIActivityHMACPath, Group: "governance_service"}, 32); err != nil {
		return err
	}
	for _, item := range siteSecretFiles {
		value, err := secrets.siteSecret(item.Secret)
		if err != nil {
			return err
		}
		// Credential files are exact bytes; not every consumer trims whitespace.
		if err := writeCredstoreFile(item.fileItem, []byte(value)); err != nil {
			return err
		}
	}
	if err := secrets.convergeRuntimeSecrets(trustDomain); err != nil {
		return err
	}
	return nil
}

func runObjectStorageCredentialProfile() error {
	trustDomain, err := trustDomain()
	if err != nil {
		return err
	}
	secrets, err := openbaoClient()
	if err != nil {
		return err
	}
	if err := secrets.ensureRuntimeSecretStore(trustDomain); err != nil {
		return err
	}
	if _, err := ensureGeneratedHexSecretFile(fileItem{Path: objectStorageCredentialKEKPath, Group: "object_storage_service"}, 32); err != nil {
		return err
	}
	if err := ensureObjectStorageS3TLS(); err != nil {
		return err
	}
	if err := ensureObjectStorageGarageProxyCredentials(); err != nil {
		return err
	}
	adminToken, err := readNonEmptyFile(garageAdminTokenPath)
	if err != nil {
		return err
	}
	createdAt, err := runtimeSecretCreatedAt()
	if err != nil {
		return err
	}
	return secrets.writeRuntimeSecret("object-storage-service.garage.admin_token", adminToken, createdAt)
}

type openbao struct {
	client *http.Client
	token  string
}

func openbaoClient() (openbao, error) {
	tokenBody, err := os.ReadFile(openbaoRootTokenPath)
	if err != nil {
		return openbao{}, fmt.Errorf("read OpenBao root token: %w", err)
	}
	certBody, err := os.ReadFile(openbaoCACertPath)
	if err != nil {
		return openbao{}, fmt.Errorf("read OpenBao CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(certBody); !ok {
		return openbao{}, fmt.Errorf("parse OpenBao CA cert %s", openbaoCACertPath)
	}
	return openbao{
		token: strings.TrimSpace(string(tokenBody)),
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			},
		},
	}, nil
}

func (b openbao) siteSecret(name string) (string, error) {
	body, err := b.request(http.MethodGet, siteConfigMount+"/data/config/"+url.PathEscape(name), nil, http.StatusOK)
	if err != nil {
		return "", fmt.Errorf("read OpenBao site secret %s: %w", name, err)
	}
	var payload struct {
		Data struct {
			Data struct {
				Value string `json:"value"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode OpenBao site secret %s: %w", name, err)
	}
	value := strings.TrimSpace(payload.Data.Data.Value)
	if value == "" {
		return "", fmt.Errorf("OpenBao site secret %s is empty", name)
	}
	return value, nil
}

func (b openbao) convergeRuntimeSecrets(trustDomain string) error {
	if err := b.ensureRuntimeSecretStore(trustDomain); err != nil {
		return err
	}
	createdAt, err := runtimeSecretCreatedAt()
	if err != nil {
		return err
	}
	for _, seed := range runtimeSecretSeeds {
		value, err := b.runtimeSecretSeedValue(seed)
		if err != nil {
			return err
		}
		if err := b.writeRuntimeSecret(seed.Name, value, createdAt); err != nil {
			return err
		}
	}
	return nil
}

func (b openbao) ensureRuntimeSecretStore(trustDomain string) error {
	consumerSubject := "spiffe://" + trustDomain + runtimeSecretConsumerPath
	if err := b.ensureKVMount(runtimeSecretKVMount); err != nil {
		return err
	}
	readPolicy := fmt.Sprintf(
		"path %q { capabilities = [\"read\"] }\npath %q { capabilities = [\"read\", \"list\"] }\n",
		runtimeSecretKVMount+"/data/secret/org/*",
		runtimeSecretKVMount+"/metadata/secret/org/*",
	)
	writePolicy := fmt.Sprintf(
		"path %q { capabilities = [\"create\", \"update\", \"read\", \"delete\"] }\npath %q { capabilities = [\"read\", \"list\", \"delete\"] }\n",
		runtimeSecretKVMount+"/data/secret/org/*",
		runtimeSecretKVMount+"/metadata/secret/org/*",
	)
	if err := b.writePolicy(runtimeSecretReadPolicy, readPolicy); err != nil {
		return err
	}
	if err := b.writePolicy(runtimeSecretWritePolicy, writePolicy); err != nil {
		return err
	}
	if err := b.writeJWTRole(runtimeSecretReadRole, consumerSubject, []string{runtimeSecretReadPolicy}); err != nil {
		return err
	}
	if err := b.writeJWTRole(runtimeSecretWriteRole, consumerSubject, []string{runtimeSecretReadPolicy, runtimeSecretWritePolicy}); err != nil {
		return err
	}
	return nil
}

func (b openbao) runtimeSecretSeedValue(seed runtimeSecretSeed) (string, error) {
	switch {
	case seed.SiteSecret != "":
		return b.siteSecret(seed.SiteSecret)
	case seed.File != "":
		body, err := os.ReadFile(seed.File)
		if err != nil {
			return "", fmt.Errorf("read runtime secret seed file %s: %w", seed.File, err)
		}
		value := strings.TrimSpace(string(body))
		if value == "" {
			return "", fmt.Errorf("runtime secret seed file %s is empty", seed.File)
		}
		return value, nil
	default:
		return "", fmt.Errorf("runtime secret seed %s has no source", seed.Name)
	}
}

func (b openbao) ensureKVMount(mount string) error {
	body, err := b.request(http.MethodGet, "sys/mounts", nil, http.StatusOK)
	if err != nil {
		return fmt.Errorf("list OpenBao mounts: %w", err)
	}
	mounts, err := decodeMountNames(body)
	if err != nil {
		return err
	}
	if _, exists := mounts[mount+"/"]; exists {
		return nil
	}
	payload := map[string]any{
		"type":        "kv",
		"description": "Verself runtime secrets",
		"options": map[string]string{
			"version": "2",
		},
	}
	if _, err := b.request(http.MethodPost, "sys/mounts/"+url.PathEscape(mount), payload, http.StatusNoContent); err != nil {
		return fmt.Errorf("mount OpenBao runtime KV %s: %w", mount, err)
	}
	return nil
}

func decodeMountNames(body []byte) (map[string]struct{}, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("decode OpenBao mount list: %w", err)
	}
	if raw, ok := top["data"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 {
			top = nested
		}
	}
	out := make(map[string]struct{}, len(top))
	for name := range top {
		out[name] = struct{}{}
	}
	return out, nil
}

func (b openbao) writePolicy(name, policy string) error {
	payload := map[string]string{"policy": policy}
	if _, err := b.request(http.MethodPost, "sys/policies/acl/"+url.PathEscape(name), payload, http.StatusOK, http.StatusNoContent); err != nil {
		return fmt.Errorf("write OpenBao policy %s: %w", name, err)
	}
	return nil
}

func (b openbao) writeJWTRole(name, subject string, policies []string) error {
	payload := map[string]any{
		"role_type":       "jwt",
		"bound_audiences": []string{openbaoWorkloadAudience},
		"bound_subject":   subject,
		"user_claim":      "sub",
		"token_policies":  policies,
		"token_ttl":       openbaoWorkloadTokenTTL,
		"token_max_ttl":   openbaoWorkloadTokenMaxTTL,
	}
	if _, err := b.request(http.MethodPost, "auth/"+openbaoSpiffeJWTMount+"/role/"+url.PathEscape(name), payload, http.StatusNoContent); err != nil {
		return fmt.Errorf("write OpenBao SPIFFE JWT role %s: %w", name, err)
	}
	return nil
}

func (b openbao) writeRuntimeSecret(name, value, createdAt string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	path := runtimeSecretKVMount + "/data/secret/org/" + url.PathEscape(name)
	payload := map[string]any{
		"data": map[string]string{
			"org_id":      runtimeSecretNamespace,
			"kind":        "secret",
			"name":        name,
			"scope_level": "org",
			"source_id":   "",
			"env_id":      "",
			"branch":      "",
			"value":       value,
			"created_at":  createdAt,
			"updated_at":  now,
		},
	}
	if _, err := b.request(http.MethodPost, path, payload, http.StatusOK, http.StatusNoContent); err != nil {
		return fmt.Errorf("write OpenBao runtime secret %s: %w", name, err)
	}
	return nil
}

func (b openbao) request(method, path string, body any, expected ...int) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal OpenBao request %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, openbaoAddr+"/v1/"+strings.TrimLeft(path, "/"), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", b.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenBao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao response %s %s: %w", method, path, err)
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("OpenBao %s %s status %d", method, path, resp.StatusCode)
}

func trustDomain() (string, error) {
	body, err := os.ReadFile(trustDomainPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", trustDomainPath, err)
	}
	trustDomain := strings.TrimSpace(string(body))
	if trustDomain == "" {
		return "", fmt.Errorf("%s is empty", trustDomainPath)
	}
	return trustDomain, nil
}

func siteDomain(trustDomain string) string {
	return strings.TrimPrefix(strings.TrimSpace(trustDomain), "spiffe.")
}

func runtimeSecretCreatedAt() (string, error) {
	body, err := os.ReadFile(runtimeSecretSeedTimestampPath)
	if err == nil {
		value := strings.TrimSpace(string(body))
		if value == "" {
			return "", fmt.Errorf("%s is empty", runtimeSecretSeedTimestampPath)
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", runtimeSecretSeedTimestampPath, err)
	}
	value := time.Now().UTC().Format(time.RFC3339)
	if err := writeCredstoreFile(fileItem{Path: runtimeSecretSeedTimestampPath, Group: "openbao"}, []byte(value+"\n")); err != nil {
		return "", err
	}
	return value, nil
}

func ensureGeneratedSecretFile(item fileItem, byteCount int) (string, error) {
	body, err := os.ReadFile(item.Path)
	if err == nil {
		value := strings.TrimSpace(string(body))
		if value == "" {
			return "", fmt.Errorf("%s is empty", item.Path)
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", item.Path, err)
	}
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %s: %w", item.Path, err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	if err := writeCredstoreFile(item, []byte(value)); err != nil {
		return "", err
	}
	return value, nil
}

func ensureGeneratedHexSecretFile(item fileItem, byteCount int) (string, error) {
	body, err := os.ReadFile(item.Path)
	if err == nil {
		value := strings.TrimSpace(string(body))
		if len(value) != byteCount*2 {
			return "", fmt.Errorf("%s must be %d hex characters", item.Path, byteCount*2)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return "", fmt.Errorf("decode %s: %w", item.Path, err)
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", item.Path, err)
	}
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %s: %w", item.Path, err)
	}
	value := hex.EncodeToString(raw)
	if err := writeCredstoreFile(item, []byte(value+"\n")); err != nil {
		return "", err
	}
	return value, nil
}

func ensureObjectStorageS3TLS() error {
	if _, err := tls.LoadX509KeyPair(objectStorageS3TLSCertPath, objectStorageS3TLSKeyPath); err == nil {
		return nil
	} else if bothExist(objectStorageS3TLSCertPath, objectStorageS3TLSKeyPath) {
		return fmt.Errorf("load object-storage S3 TLS keypair: %w", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate object-storage S3 TLS key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("generate object-storage S3 TLS serial: %w", err)
	}
	now := time.Now().UTC()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkixName("Verself object-storage S3"),
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create object-storage S3 TLS cert: %w", err)
	}
	var certPEM bytes.Buffer
	if err := pem.Encode(&certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}
	var keyPEM bytes.Buffer
	if err := pem.Encode(&keyPEM, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return err
	}
	item := fileItem{Group: "object_storage_service"}
	item.Path = objectStorageS3TLSCertPath
	if err := writeCredstoreFile(item, certPEM.Bytes()); err != nil {
		return err
	}
	item.Path = objectStorageS3TLSKeyPath
	return writeCredstoreFile(item, keyPEM.Bytes())
}

func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: commonName}
}

func bothExist(paths ...string) bool {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func ensureObjectStorageGarageProxyCredentials() error {
	if existingCredsPresent(objectStorageProxyKeyIDPath, objectStorageProxySecretPath) {
		return nil
	}
	adminToken, err := readNonEmptyFile(garageAdminTokenPath)
	if err != nil {
		return err
	}
	response, err := createGarageProxyKey(adminToken)
	if err != nil {
		return err
	}
	if strings.TrimSpace(response.AccessKeyID) == "" || strings.TrimSpace(response.SecretAccessKey) == "" {
		return fmt.Errorf("Garage CreateKey response did not include accessKeyId and secretAccessKey")
	}
	item := fileItem{Group: "object_storage_service"}
	item.Path = objectStorageProxyKeyIDPath
	if err := writeCredstoreFile(item, []byte(strings.TrimSpace(response.AccessKeyID)+"\n")); err != nil {
		return err
	}
	item.Path = objectStorageProxySecretPath
	return writeCredstoreFile(item, []byte(strings.TrimSpace(response.SecretAccessKey)+"\n"))
}

func existingCredsPresent(paths ...string) bool {
	for _, path := range paths {
		value, err := readNonEmptyFile(path)
		if err != nil || value == "" {
			return false
		}
	}
	return true
}

func createGarageProxyKey(adminToken string) (garageKeyResponse, error) {
	payload := map[string]any{
		"name":         "object-storage-service-proxy",
		"neverExpires": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return garageKeyResponse{}, err
	}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:3903/v2/CreateKey", bytes.NewReader(raw))
	if err != nil {
		return garageKeyResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(adminToken))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return garageKeyResponse{}, fmt.Errorf("Garage CreateKey: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return garageKeyResponse{}, fmt.Errorf("read Garage CreateKey response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return garageKeyResponse{}, fmt.Errorf("Garage CreateKey status %d", resp.StatusCode)
	}
	var out garageKeyResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return garageKeyResponse{}, fmt.Errorf("decode Garage CreateKey response: %w", err)
	}
	return out, nil
}

func readNonEmptyFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func writeCredstoreFile(item fileItem, body []byte) error {
	gid, err := lookupGroupID(item.Group)
	if err != nil {
		return err
	}
	dir := filepath.Dir(item.Path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.Chown(dir, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0750); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	tmp := item.Path + ".tmp"
	if err := os.WriteFile(tmp, body, 0640); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0640); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, item.Path); err != nil {
		return fmt.Errorf("install %s: %w", item.Path, err)
	}
	return nil
}

func lookupGroupID(name string) (int, error) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", name, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	return gid, nil
}

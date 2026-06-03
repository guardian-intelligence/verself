package r2control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const ParentCredentialSourceOpenBao = "openbao"

type ParentCredentialConfig struct {
	Source            string
	AccountID         string
	OpenBaoAddr       string
	OpenBaoPath       string
	OpenBaoCACertFile string
	OpenBaoTokenFile  string
	Timeout           time.Duration
}

type ParentCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	APIToken        string
	SessionToken    string
	Source          string
}

func (cfg ParentCredentialConfig) WithDefaults() ParentCredentialConfig {
	if cfg.Source == "" {
		cfg.Source = ParentCredentialSourceOpenBao
	}
	if cfg.OpenBaoPath == "" {
		cfg.OpenBaoPath = "kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return cfg
}

func LoadParentCredentials(ctx context.Context, cfg ParentCredentialConfig) (ParentCredentials, error) {
	cfg = cfg.WithDefaults()
	switch strings.TrimSpace(cfg.Source) {
	case ParentCredentialSourceOpenBao:
		creds, err := loadParentCredentialsFromOpenBao(ctx, cfg)
		return resolveParentCredentials(ctx, cfg, creds, err)
	default:
		return ParentCredentials{}, fmt.Errorf("unsupported R2 credential source %q; use %q", cfg.Source, ParentCredentialSourceOpenBao)
	}
}

func loadParentCredentialsFromOpenBao(ctx context.Context, cfg ParentCredentialConfig) (ParentCredentials, error) {
	addr := strings.TrimRight(strings.TrimSpace(cfg.OpenBaoAddr), "/")
	if addr == "" {
		return ParentCredentials{}, errors.New("openbao address is required")
	}
	token, err := LoadOpenBaoToken(cfg)
	if err != nil {
		return ParentCredentials{}, err
	}
	path := strings.Trim(strings.TrimSpace(cfg.OpenBaoPath), "/")
	if path == "" {
		return ParentCredentials{}, errors.New("openbao path is required for r2 credentials")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/v1/"+path, http.NoBody)
	if err != nil {
		return ParentCredentials{}, err
	}
	req.Header.Set("X-Vault-Token", token)
	client, err := openBaoHTTPClient(cfg)
	if err != nil {
		return ParentCredentials{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ParentCredentials{}, fmt.Errorf("read OpenBao R2 credentials: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ParentCredentials{}, fmt.Errorf("read OpenBao response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ParentCredentials{}, fmt.Errorf("openbao read %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	values, err := OpenBaoSecretData(body)
	if err != nil {
		return ParentCredentials{}, err
	}
	return parentCredentialsFromValues(values, "openbao:"+path)
}

func WriteParentCredentialsToOpenBao(ctx context.Context, cfg ParentCredentialConfig, values map[string]string) error {
	cfg = cfg.WithDefaults()
	addr := strings.TrimRight(strings.TrimSpace(cfg.OpenBaoAddr), "/")
	if addr == "" {
		return errors.New("openbao address is required")
	}
	token, err := LoadOpenBaoToken(cfg)
	if err != nil {
		return err
	}
	path := strings.Trim(strings.TrimSpace(cfg.OpenBaoPath), "/")
	if path == "" {
		return errors.New("openbao path is required for r2 credentials")
	}
	if !strings.Contains(path, "/data/") {
		return fmt.Errorf("openbao path %q must be a KV v2 data path", path)
	}
	body, err := json.Marshal(map[string]map[string]string{"data": values})
	if err != nil {
		return fmt.Errorf("encode OpenBao R2 credentials: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addr+"/v1/"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", token)
	client, err := openBaoHTTPClient(cfg)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("write OpenBao R2 credentials: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read OpenBao response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("openbao write %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func openBaoHTTPClient(cfg ParentCredentialConfig) (*http.Client, error) {
	client := &http.Client{Timeout: cfg.Timeout}
	caFile := firstNonEmpty(cfg.OpenBaoCACertFile, os.Getenv("BAO_CACERT"), os.Getenv("VAULT_CACERT"))
	if caFile == "" {
		return client, nil
	}
	cert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		return nil, fmt.Errorf("openbao CA cert file contains no PEM certificates")
	}
	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
	}
	return client, nil
}

func resolveParentCredentials(ctx context.Context, cfg ParentCredentialConfig, creds ParentCredentials, err error) (ParentCredentials, error) {
	if err != nil {
		return ParentCredentials{}, err
	}
	if strings.TrimSpace(creds.AccessKeyID) != "" {
		return creds, nil
	}
	if strings.TrimSpace(creds.APIToken) == "" {
		return ParentCredentials{}, errors.New("r2 parent access key id is required")
	}
	accountID := strings.ToLower(strings.TrimSpace(cfg.AccountID))
	if !IsCloudflareAccountID(accountID) {
		return ParentCredentials{}, errors.New("cloudflare account ID is required to derive an r2 access key ID from an account API token")
	}
	client, err := NewCloudflareAPIClient(creds.APIToken, cfg.Timeout)
	if err != nil {
		return ParentCredentials{}, err
	}
	verified, err := client.VerifyAccountToken(ctx, accountID)
	if err != nil {
		return ParentCredentials{}, err
	}
	if verified.Status != "" && verified.Status != "active" {
		return ParentCredentials{}, fmt.Errorf("cloudflare API token status is %q", verified.Status)
	}
	creds.AccessKeyID = verified.ID
	creds.Source += ":verified-token-id"
	return creds, nil
}

func LoadOpenBaoToken(cfg ParentCredentialConfig) (string, error) {
	if cfg.OpenBaoTokenFile != "" {
		body, err := os.ReadFile(cfg.OpenBaoTokenFile)
		if err != nil {
			return "", fmt.Errorf("read OpenBao token file: %w", err)
		}
		token := strings.TrimSpace(string(body))
		if token == "" {
			return "", errors.New("openbao token file is empty")
		}
		return token, nil
	}
	return "", errors.New("openbao token file is required")
}

func OpenBaoSecretData(body []byte) (map[string]string, error) {
	var raw struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode OpenBao response: %w", err)
	}
	var maybeKV2 struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(raw.Data, &maybeKV2); err == nil && len(maybeKV2.Data) > 0 {
		return maybeKV2.Data, nil
	}
	values := map[string]string{}
	if err := json.Unmarshal(raw.Data, &values); err != nil {
		return nil, fmt.Errorf("decode OpenBao secret data: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("openbao secret has no data")
	}
	return values, nil
}

func parentCredentialsFromValues(values map[string]string, source string) (ParentCredentials, error) {
	access := firstNonEmpty(
		values["access_key_id"],
		values["parent_access_key_id"],
		values["api_token_id"],
		values["token_id"],
	)
	secret := firstNonEmpty(
		values["secret_access_key"],
		values["parent_secret_access_key"],
		values["s3_secret_access_key"],
	)
	apiToken := firstNonEmpty(values["api_token"], values["token"], values["value"])
	if secret == "" && apiToken != "" {
		secret = SHA256Hex([]byte(apiToken))
	}
	if strings.TrimSpace(secret) == "" {
		return ParentCredentials{}, errors.New("r2 parent secret access key or API token value is required")
	}
	return ParentCredentials{
		AccessKeyID:     strings.TrimSpace(access),
		SecretAccessKey: strings.TrimSpace(secret),
		APIToken:        strings.TrimSpace(apiToken),
		SessionToken:    strings.TrimSpace(values["session_token"]),
		Source:          source,
	}, nil
}

func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

func SHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

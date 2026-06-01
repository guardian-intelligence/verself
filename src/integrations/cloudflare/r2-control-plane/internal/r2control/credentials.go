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

const (
	ParentCredentialSourceAuto    = "auto"
	ParentCredentialSourceEnv     = "env"
	ParentCredentialSourceEnvFile = "env-file"
	ParentCredentialSourceOpenBao = "openbao"
)

type ParentCredentialConfig struct {
	Source             string
	AccountID          string
	CredentialsFile    string
	AccessKeyIDEnv     string
	SecretAccessKeyEnv string
	APITokenEnv        string
	SessionTokenEnv    string
	OpenBaoAddr        string
	OpenBaoPath        string
	OpenBaoCACertFile  string
	OpenBaoTokenEnv    string
	OpenBaoTokenFile   string
	Timeout            time.Duration
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
	if cfg.AccessKeyIDEnv == "" {
		cfg.AccessKeyIDEnv = "CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID"
	}
	if cfg.SecretAccessKeyEnv == "" {
		cfg.SecretAccessKeyEnv = "CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY"
	}
	if cfg.APITokenEnv == "" {
		cfg.APITokenEnv = "CLOUDFLARE_R2_ADMIN_API_TOKEN"
	}
	if cfg.SessionTokenEnv == "" {
		cfg.SessionTokenEnv = "CLOUDFLARE_R2_ADMIN_SESSION_TOKEN"
	}
	if cfg.OpenBaoPath == "" {
		cfg.OpenBaoPath = "kv-controller/data/integrations/cloudflare/r2-admin"
	}
	if cfg.OpenBaoTokenEnv == "" {
		cfg.OpenBaoTokenEnv = "BAO_TOKEN"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return cfg
}

func LoadParentCredentials(ctx context.Context, cfg ParentCredentialConfig) (ParentCredentials, error) {
	cfg = cfg.WithDefaults()
	switch strings.TrimSpace(cfg.Source) {
	case ParentCredentialSourceEnv:
		creds, err := loadParentCredentialsFromEnv(cfg)
		return resolveParentCredentials(ctx, cfg, creds, err)
	case ParentCredentialSourceEnvFile:
		creds, err := loadParentCredentialsFromEnvFile(cfg)
		return resolveParentCredentials(ctx, cfg, creds, err)
	case ParentCredentialSourceOpenBao:
		creds, err := loadParentCredentialsFromOpenBao(ctx, cfg)
		return resolveParentCredentials(ctx, cfg, creds, err)
	case ParentCredentialSourceAuto, "":
		// Explicit auto is for one-time bootstrap diagnostics; steady state reads controller OpenBao.
		if envHasParentCredentials(cfg) {
			creds, err := loadParentCredentialsFromEnv(cfg)
			return resolveParentCredentials(ctx, cfg, creds, err)
		}
		if cfg.CredentialsFile != "" {
			creds, err := loadParentCredentialsFromEnvFile(cfg)
			return resolveParentCredentials(ctx, cfg, creds, err)
		}
		if cfg.OpenBaoAddr != "" || cfg.OpenBaoTokenFile != "" || os.Getenv(cfg.OpenBaoTokenEnv) != "" || os.Getenv("BAO_TOKEN") != "" || os.Getenv("VAULT_TOKEN") != "" {
			creds, err := loadParentCredentialsFromOpenBao(ctx, cfg)
			return resolveParentCredentials(ctx, cfg, creds, err)
		}
		return ParentCredentials{}, fmt.Errorf("no R2 parent credentials found; set %s plus %s or %s, pass credentials file, or use OpenBao",
			cfg.AccessKeyIDEnv, cfg.SecretAccessKeyEnv, cfg.APITokenEnv)
	default:
		return ParentCredentials{}, fmt.Errorf("unsupported R2 parent credential source %q", cfg.Source)
	}
}

func loadParentCredentialsFromEnv(cfg ParentCredentialConfig) (ParentCredentials, error) {
	values := map[string]string{
		"access_key_id":     strings.TrimSpace(os.Getenv(cfg.AccessKeyIDEnv)),
		"secret_access_key": strings.TrimSpace(os.Getenv(cfg.SecretAccessKeyEnv)),
		"api_token":         strings.TrimSpace(os.Getenv(cfg.APITokenEnv)),
		"session_token":     strings.TrimSpace(os.Getenv(cfg.SessionTokenEnv)),
	}
	return parentCredentialsFromValues(values, "env:"+cfg.AccessKeyIDEnv)
}

func envHasParentCredentials(cfg ParentCredentialConfig) bool {
	access := strings.TrimSpace(os.Getenv(cfg.AccessKeyIDEnv))
	secret := strings.TrimSpace(os.Getenv(cfg.SecretAccessKeyEnv))
	apiToken := strings.TrimSpace(os.Getenv(cfg.APITokenEnv))
	return apiToken != "" || (access != "" && secret != "")
}

func loadParentCredentialsFromEnvFile(cfg ParentCredentialConfig) (ParentCredentials, error) {
	if cfg.CredentialsFile == "" {
		return ParentCredentials{}, errors.New("credentials file is required for env-file R2 parent credentials")
	}
	body, err := os.ReadFile(cfg.CredentialsFile)
	if err != nil {
		return ParentCredentials{}, fmt.Errorf("read credentials file: %w", err)
	}
	values, err := ParseEnvFile(body)
	if err != nil {
		return ParentCredentials{}, err
	}
	mapped := map[string]string{
		"access_key_id":     values[cfg.AccessKeyIDEnv],
		"secret_access_key": values[cfg.SecretAccessKeyEnv],
		"api_token":         values[cfg.APITokenEnv],
		"session_token":     values[cfg.SessionTokenEnv],
	}
	return parentCredentialsFromValues(mapped, "env-file:"+cfg.CredentialsFile)
}

func loadParentCredentialsFromOpenBao(ctx context.Context, cfg ParentCredentialConfig) (ParentCredentials, error) {
	addr := strings.TrimRight(strings.TrimSpace(firstNonEmpty(cfg.OpenBaoAddr, os.Getenv("BAO_ADDR"), os.Getenv("VAULT_ADDR"))), "/")
	if addr == "" {
		return ParentCredentials{}, errors.New("OpenBao address is required via config, BAO_ADDR, or VAULT_ADDR")
	}
	token, err := LoadOpenBaoToken(cfg)
	if err != nil {
		return ParentCredentials{}, err
	}
	path := strings.Trim(strings.TrimSpace(cfg.OpenBaoPath), "/")
	if path == "" {
		return ParentCredentials{}, errors.New("OpenBao path is required for R2 credentials")
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
		return ParentCredentials{}, fmt.Errorf("OpenBao read %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	values, err := OpenBaoSecretData(body)
	if err != nil {
		return ParentCredentials{}, err
	}
	return parentCredentialsFromValues(values, "openbao:"+path)
}

func WriteParentCredentialsToOpenBao(ctx context.Context, cfg ParentCredentialConfig, values map[string]string) error {
	cfg = cfg.WithDefaults()
	addr := strings.TrimRight(strings.TrimSpace(firstNonEmpty(cfg.OpenBaoAddr, os.Getenv("BAO_ADDR"), os.Getenv("VAULT_ADDR"))), "/")
	if addr == "" {
		return errors.New("OpenBao address is required via config, BAO_ADDR, or VAULT_ADDR")
	}
	token, err := LoadOpenBaoToken(cfg)
	if err != nil {
		return err
	}
	path := strings.Trim(strings.TrimSpace(cfg.OpenBaoPath), "/")
	if path == "" {
		return errors.New("OpenBao path is required for R2 credentials")
	}
	if !strings.Contains(path, "/data/") {
		return fmt.Errorf("OpenBao path %q must be a KV v2 data path", path)
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
		return fmt.Errorf("OpenBao write %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
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
		return nil, fmt.Errorf("OpenBao CA cert file contains no PEM certificates")
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
		return ParentCredentials{}, errors.New("R2 parent access key id is required")
	}
	accountID := strings.ToLower(strings.TrimSpace(cfg.AccountID))
	if !IsCloudflareAccountID(accountID) {
		return ParentCredentials{}, errors.New("Cloudflare account ID is required to derive an R2 access key ID from an Account API token")
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
		return ParentCredentials{}, fmt.Errorf("Cloudflare API token status is %q", verified.Status)
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
			return "", errors.New("OpenBao token file is empty")
		}
		return token, nil
	}
	for _, envName := range []string{cfg.OpenBaoTokenEnv, "BAO_TOKEN", "VAULT_TOKEN"} {
		if envName == "" {
			continue
		}
		if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
			return token, nil
		}
	}
	return "", errors.New("OpenBao token is required via token file, BAO_TOKEN, or VAULT_TOKEN")
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
		return nil, errors.New("OpenBao secret has no data")
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
		return ParentCredentials{}, errors.New("R2 parent secret access key or API token value is required")
	}
	return ParentCredentials{
		AccessKeyID:     strings.TrimSpace(access),
		SecretAccessKey: strings.TrimSpace(secret),
		APIToken:        strings.TrimSpace(apiToken),
		SessionToken:    strings.TrimSpace(values["session_token"]),
		Source:          source,
	}, nil
}

func ParseEnvFile(body []byte) (map[string]string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid environment line %q", line)
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return values, nil
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

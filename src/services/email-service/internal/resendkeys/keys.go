package resendkeys

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
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	DefaultAdminSecret    = "email-service.resend.full_access_api_key"
	DefaultMetadataSecret = "email-service.resend.key_metadata"
)

var DefaultChildSecrets = []string{
	"email-service.resend.api_key",
	"zitadel.smtp.password",
}

type Config struct {
	Site           string
	AdminSecret    string
	ChildSecrets   []string
	MetadataSecret string
	KeyNamePrefix  string
	Rotate         bool
	Now            func() time.Time
}

type Result struct {
	KeyID          string
	KeyName        string
	TokenSHA256    string
	Created        bool
	RevokedKeyID   string
	UpdatedSecrets []string
}

type SecretStore interface {
	Read(ctx context.Context, name string) (string, bool, error)
	Write(ctx context.Context, name string, value string) error
}

type ResendAPI interface {
	CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (CreateAPIKeyResponse, error)
	DeleteAPIKey(ctx context.Context, id string) error
}

type CreateAPIKeyRequest struct {
	Name       string
	Permission string
}

type CreateAPIKeyResponse struct {
	ID    string
	Token string
}

type Metadata struct {
	Version      string   `json:"version"`
	Site         string   `json:"site"`
	KeyID        string   `json:"key_id"`
	KeyName      string   `json:"key_name"`
	Permission   string   `json:"permission"`
	TokenSHA256  string   `json:"token_sha256"`
	ChildSecrets []string `json:"child_secrets"`
	CreatedAt    string   `json:"created_at"`

	PreviousKeyID          string `json:"previous_key_id"`
	PendingRevocationKeyID string `json:"pending_revocation_key_id"`
	RevokedKeyID           string `json:"revoked_key_id"`
}

func Apply(ctx context.Context, cfg Config, store SecretStore, api ResendAPI) (Result, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return Result{}, err
	}
	if store == nil {
		return Result{}, errors.New("secret store is required")
	}
	if api == nil {
		return Result{}, errors.New("resend api client is required")
	}
	prior, err := readMetadata(ctx, store, cfg.MetadataSecret)
	if err != nil {
		return Result{}, err
	}
	if !cfg.Rotate {
		present, err := allChildSecretsPresent(ctx, store, cfg.ChildSecrets)
		if err != nil {
			return Result{}, err
		}
		if present {
			revoked, err := revokePendingKey(ctx, store, api, cfg.MetadataSecret, prior)
			if err != nil {
				return Result{}, err
			}
			return Result{RevokedKeyID: revoked, UpdatedSecrets: []string{}}, nil
		}
	}
	keyName := cfg.keyName()
	created, err := api.CreateAPIKey(ctx, CreateAPIKeyRequest{
		Name:       keyName,
		Permission: "sending_access",
	})
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(created.ID) == "" || strings.TrimSpace(created.Token) == "" {
		return Result{}, errors.New("resend create api key response omitted id or token")
	}
	for _, secret := range cfg.ChildSecrets {
		if err := store.Write(ctx, secret, created.Token); err != nil {
			return Result{}, fmt.Errorf("write Resend child key to OpenBao runtime secret %s: %w", secret, err)
		}
	}
	tokenSHA := fingerprint(created.Token)
	previousKeyID := ""
	pendingRevocationKeyID := ""
	if prior.KeyID != "" && prior.KeyID != strings.TrimSpace(created.ID) {
		previousKeyID = prior.KeyID
		pendingRevocationKeyID = prior.KeyID
	}
	metadata := Metadata{
		Version:                "verself.email.resend.key.v1",
		Site:                   cfg.Site,
		KeyID:                  strings.TrimSpace(created.ID),
		KeyName:                keyName,
		Permission:             "sending_access",
		TokenSHA256:            tokenSHA,
		ChildSecrets:           append([]string(nil), cfg.ChildSecrets...),
		CreatedAt:              cfg.now().UTC().Format(time.RFC3339),
		PreviousKeyID:          previousKeyID,
		PendingRevocationKeyID: pendingRevocationKeyID,
	}
	sort.Strings(metadata.ChildSecrets)
	if err := writeMetadata(ctx, store, cfg.MetadataSecret, metadata); err != nil {
		return Result{}, fmt.Errorf("write Resend key metadata: %w", err)
	}
	revoked, err := revokePendingKey(ctx, store, api, cfg.MetadataSecret, metadata)
	if err != nil {
		return Result{}, err
	}
	return Result{
		KeyID:          metadata.KeyID,
		KeyName:        metadata.KeyName,
		TokenSHA256:    tokenSHA,
		Created:        true,
		RevokedKeyID:   revoked,
		UpdatedSecrets: append([]string(nil), cfg.ChildSecrets...),
	}, nil
}

func readMetadata(ctx context.Context, store SecretStore, name string) (Metadata, error) {
	raw, found, err := store.Read(ctx, name)
	if err != nil {
		return Metadata{}, fmt.Errorf("read Resend key metadata: %w", err)
	}
	if !found || strings.TrimSpace(raw) == "" {
		return Metadata{}, nil
	}
	var metadata Metadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return Metadata{}, fmt.Errorf("decode Resend key metadata: %w", err)
	}
	return metadata, nil
}

func writeMetadata(ctx context.Context, store SecretStore, name string, metadata Metadata) error {
	body, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode Resend key metadata: %w", err)
	}
	return store.Write(ctx, name, string(body))
}

func revokePendingKey(ctx context.Context, store SecretStore, api ResendAPI, metadataSecret string, metadata Metadata) (string, error) {
	pending := strings.TrimSpace(metadata.PendingRevocationKeyID)
	if pending == "" {
		return "", nil
	}
	if err := api.DeleteAPIKey(ctx, pending); err != nil {
		return "", fmt.Errorf("delete prior Resend API key %s: %w", pending, err)
	}
	metadata.PendingRevocationKeyID = ""
	metadata.RevokedKeyID = pending
	if err := writeMetadata(ctx, store, metadataSecret, metadata); err != nil {
		return "", fmt.Errorf("write Resend key metadata after revocation: %w", err)
	}
	return pending, nil
}

func allChildSecretsPresent(ctx context.Context, store SecretStore, names []string) (bool, error) {
	for _, name := range names {
		value, found, err := store.Read(ctx, name)
		if err != nil {
			return false, fmt.Errorf("read Resend child key %s: %w", name, err)
		}
		if !found || strings.TrimSpace(value) == "" {
			return false, nil
		}
	}
	return true, nil
}

func (cfg Config) withDefaults() Config {
	cfg.Site = strings.TrimSpace(cfg.Site)
	cfg.AdminSecret = firstNonEmpty(cfg.AdminSecret, DefaultAdminSecret)
	cfg.MetadataSecret = firstNonEmpty(cfg.MetadataSecret, DefaultMetadataSecret)
	if len(cfg.ChildSecrets) == 0 {
		cfg.ChildSecrets = append([]string(nil), DefaultChildSecrets...)
	}
	cfg.KeyNamePrefix = strings.Trim(strings.TrimSpace(cfg.KeyNamePrefix), "-")
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return cfg
}

func (cfg Config) validate() error {
	if cfg.Site == "" {
		return errors.New("site is required")
	}
	if cfg.AdminSecret == "" {
		return errors.New("admin secret name is required")
	}
	if cfg.MetadataSecret == "" {
		return errors.New("metadata secret name is required")
	}
	if len(cfg.ChildSecrets) == 0 {
		return errors.New("at least one child secret is required")
	}
	seen := map[string]bool{}
	for _, secret := range cfg.ChildSecrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			return errors.New("child secret names must not be empty")
		}
		if seen[secret] {
			return fmt.Errorf("child secret %s is duplicated", secret)
		}
		seen[secret] = true
	}
	return nil
}

func (cfg Config) keyName() string {
	prefix := cfg.KeyNamePrefix
	if prefix == "" {
		prefix = "verself-" + cfg.Site + "-email-service-sending"
	}
	return prefix + "-" + cfg.now().UTC().Format("20060102")
}

func (cfg Config) now() time.Time {
	return cfg.Now()
}

type HTTPResendAPI struct {
	baseURL    string
	adminToken string
	httpClient *http.Client
	userAgent  string
}

func NewHTTPResendAPI(baseURL, adminToken string, client *http.Client) (*HTTPResendAPI, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(firstNonEmpty(baseURL, "https://api.resend.com")), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("resend API base URL must be https: %q", baseURL)
	}
	adminToken = strings.TrimSpace(adminToken)
	if adminToken == "" {
		return nil, errors.New("resend full-access API key is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPResendAPI{
		baseURL:    baseURL,
		adminToken: adminToken,
		httpClient: client,
		userAgent:  "verself-email-service",
	}, nil
}

func (c *HTTPResendAPI) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (CreateAPIKeyResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return CreateAPIKeyResponse{}, errors.New("resend API key name is required")
	}
	permission := strings.TrimSpace(req.Permission)
	if permission == "" {
		permission = "sending_access"
	}
	body := map[string]string{
		"name":       name,
		"permission": permission,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return CreateAPIKeyResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api-keys", bytes.NewReader(raw))
	if err != nil {
		return CreateAPIKeyResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.adminToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CreateAPIKeyResponse{}, fmt.Errorf("create Resend API key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CreateAPIKeyResponse{}, fmt.Errorf("create Resend API key status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var decoded CreateAPIKeyResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return CreateAPIKeyResponse{}, fmt.Errorf("decode Resend API key response: %w", err)
	}
	decoded.ID = strings.TrimSpace(decoded.ID)
	decoded.Token = strings.TrimSpace(decoded.Token)
	return decoded, nil
}

func (c *HTTPResendAPI) DeleteAPIKey(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("resend API key id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api-keys/"+url.PathEscape(id), http.NoBody)
	if err != nil {
		return fmt.Errorf("build Resend delete api key request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete Resend API key: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("delete Resend API key status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
}

type OpenBaoStore struct {
	addr       string
	token      string
	httpClient *http.Client
}

func NewOpenBaoStore(addr, caCert, token string) (*OpenBaoStore, error) {
	addr = strings.TrimRight(strings.TrimSpace(addr), "/")
	if addr == "" {
		return nil, errors.New("openbao address is required")
	}
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("openbao address must be https: %q", addr)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("openbao token is required")
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}
	if strings.TrimSpace(caCert) != "" {
		pem, err := os.ReadFile(caCert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA certificate: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse OpenBao CA certificate %s", caCert)
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &OpenBaoStore{
		addr:       addr,
		token:      token,
		httpClient: &http.Client{Transport: transport, Timeout: 10 * time.Second},
	}, nil
}

func (s *OpenBaoStore) Read(ctx context.Context, name string) (string, bool, error) {
	var decoded struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	status, err := s.do(ctx, http.MethodGet, runtimeSecretPath(name), nil, &decoded, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	value, _ := decoded.Data.Data["value"].(string)
	return strings.TrimSpace(value), true, nil
}

func (s *OpenBaoStore) Write(ctx context.Context, name string, value string) error {
	return s.write(ctx, runtimeSecretPath(name), map[string]any{
		"data": map[string]string{"value": value},
	})
}

func (s *OpenBaoStore) write(ctx context.Context, path string, payload any) error {
	_, err := s.do(ctx, http.MethodPost, path, payload, nil, http.StatusOK, http.StatusNoContent)
	return err
}

func (s *OpenBaoStore) do(ctx context.Context, method string, path string, payload any, into any, expected ...int) (int, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("encode OpenBao request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.addr+"/v1/"+strings.Trim(path, "/"), body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Vault-Token", s.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	for _, status := range expected {
		if resp.StatusCode == status {
			if into != nil && len(raw) > 0 {
				if err := json.Unmarshal(raw, into); err != nil {
					return resp.StatusCode, fmt.Errorf("decode openbao %s %s response: %w", method, path, err)
				}
			}
			return resp.StatusCode, nil
		}
	}
	return resp.StatusCode, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func runtimeSecretPath(name string) string {
	return "kv-runtime/data/secret/org/" + url.PathEscape(strings.TrimSpace(name))
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

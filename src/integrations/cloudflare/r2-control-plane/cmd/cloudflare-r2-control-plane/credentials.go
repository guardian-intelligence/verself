package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type parentCredentialSource string

const (
	parentCredentialSourceAuto    parentCredentialSource = "auto"
	parentCredentialSourceEnv     parentCredentialSource = "env"
	parentCredentialSourceEnvFile parentCredentialSource = "env-file"
	parentCredentialSourceOpenBao parentCredentialSource = "openbao"
)

type parentCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Source          string
}

func loadParentCredentials(ctx context.Context, cfg config) (parentCredentials, error) {
	source := parentCredentialSource(strings.TrimSpace(cfg.credentialSource))
	if source == "" {
		source = parentCredentialSourceAuto
	}
	switch source {
	case parentCredentialSourceEnv:
		return loadParentCredentialsFromEnv(cfg)
	case parentCredentialSourceEnvFile:
		return loadParentCredentialsFromEnvFile(cfg)
	case parentCredentialSourceOpenBao:
		return loadParentCredentialsFromOpenBao(ctx, cfg)
	case parentCredentialSourceAuto:
		if envHasParentCredentials(cfg) {
			return loadParentCredentialsFromEnv(cfg)
		}
		if cfg.credentialsFile != "" {
			return loadParentCredentialsFromEnvFile(cfg)
		}
		if cfg.openBaoAddr != "" || cfg.openBaoTokenFile != "" || os.Getenv(cfg.openBaoTokenEnv) != "" {
			return loadParentCredentialsFromOpenBao(ctx, cfg)
		}
		return parentCredentials{}, fmt.Errorf("no R2 parent credentials found; set %s plus %s or %s, pass --credentials-file, or pass --credential-source=openbao",
			cfg.parentAccessKeyIDEnv, cfg.parentSecretAccessKeyEnv, cfg.parentAPITokenEnv)
	default:
		return parentCredentials{}, fmt.Errorf("unsupported --credential-source %q", source)
	}
}

func loadParentCredentialsFromEnv(cfg config) (parentCredentials, error) {
	values := map[string]string{
		"access_key_id":     strings.TrimSpace(os.Getenv(cfg.parentAccessKeyIDEnv)),
		"secret_access_key": strings.TrimSpace(os.Getenv(cfg.parentSecretAccessKeyEnv)),
		"api_token":         strings.TrimSpace(os.Getenv(cfg.parentAPITokenEnv)),
		"session_token":     strings.TrimSpace(os.Getenv(cfg.parentSessionTokenEnv)),
	}
	creds, err := parentCredentialsFromValues(values, "env:"+cfg.parentAccessKeyIDEnv)
	if err != nil {
		return parentCredentials{}, err
	}
	return creds, nil
}

func envHasParentCredentials(cfg config) bool {
	access := strings.TrimSpace(os.Getenv(cfg.parentAccessKeyIDEnv))
	secret := strings.TrimSpace(os.Getenv(cfg.parentSecretAccessKeyEnv))
	apiToken := strings.TrimSpace(os.Getenv(cfg.parentAPITokenEnv))
	return access != "" && (secret != "" || apiToken != "")
}

func loadParentCredentialsFromEnvFile(cfg config) (parentCredentials, error) {
	if cfg.credentialsFile == "" {
		return parentCredentials{}, errors.New("--credentials-file is required for --credential-source=env-file")
	}
	body, err := os.ReadFile(cfg.credentialsFile)
	if err != nil {
		return parentCredentials{}, fmt.Errorf("read credentials file: %w", err)
	}
	values, err := parseEnvFile(body)
	if err != nil {
		return parentCredentials{}, err
	}
	mapped := map[string]string{
		"access_key_id":     values[cfg.parentAccessKeyIDEnv],
		"secret_access_key": values[cfg.parentSecretAccessKeyEnv],
		"api_token":         values[cfg.parentAPITokenEnv],
		"session_token":     values[cfg.parentSessionTokenEnv],
	}
	creds, err := parentCredentialsFromValues(mapped, "env-file:"+cfg.credentialsFile)
	if err != nil {
		return parentCredentials{}, err
	}
	return creds, nil
}

func loadParentCredentialsFromOpenBao(ctx context.Context, cfg config) (parentCredentials, error) {
	addr := strings.TrimRight(strings.TrimSpace(firstNonEmpty(cfg.openBaoAddr, os.Getenv("BAO_ADDR"), os.Getenv("VAULT_ADDR"))), "/")
	if addr == "" {
		return parentCredentials{}, errors.New("OpenBao address is required via --openbao-addr, BAO_ADDR, or VAULT_ADDR")
	}
	token, err := loadOpenBaoToken(cfg)
	if err != nil {
		return parentCredentials{}, err
	}
	path := strings.Trim(strings.TrimSpace(cfg.openBaoPath), "/")
	if path == "" {
		return parentCredentials{}, errors.New("--openbao-path is required for OpenBao credentials")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/v1/"+path, http.NoBody)
	if err != nil {
		return parentCredentials{}, err
	}
	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: cfg.timeout}
	resp, err := client.Do(req)
	if err != nil {
		return parentCredentials{}, fmt.Errorf("read OpenBao R2 credentials: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return parentCredentials{}, fmt.Errorf("read OpenBao response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parentCredentials{}, fmt.Errorf("OpenBao read %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	values, err := openBaoSecretData(body)
	if err != nil {
		return parentCredentials{}, err
	}
	creds, err := parentCredentialsFromValues(values, "openbao:"+path)
	if err != nil {
		return parentCredentials{}, err
	}
	return creds, nil
}

func loadOpenBaoToken(cfg config) (string, error) {
	if cfg.openBaoTokenFile != "" {
		body, err := os.ReadFile(cfg.openBaoTokenFile)
		if err != nil {
			return "", fmt.Errorf("read OpenBao token file: %w", err)
		}
		token := strings.TrimSpace(string(body))
		if token == "" {
			return "", errors.New("OpenBao token file is empty")
		}
		return token, nil
	}
	for _, envName := range []string{cfg.openBaoTokenEnv, "BAO_TOKEN", "VAULT_TOKEN"} {
		if envName == "" {
			continue
		}
		if token := strings.TrimSpace(os.Getenv(envName)); token != "" {
			return token, nil
		}
	}
	return "", errors.New("OpenBao token is required via --openbao-token-file, BAO_TOKEN, or VAULT_TOKEN")
}

func openBaoSecretData(body []byte) (map[string]string, error) {
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

func parentCredentialsFromValues(values map[string]string, source string) (parentCredentials, error) {
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
		secret = sha256Hex([]byte(apiToken))
	}
	if strings.TrimSpace(access) == "" {
		return parentCredentials{}, errors.New("R2 parent access key id is required")
	}
	if strings.TrimSpace(secret) == "" {
		return parentCredentials{}, errors.New("R2 parent secret access key or API token value is required")
	}
	return parentCredentials{
		AccessKeyID:     strings.TrimSpace(access),
		SecretAccessKey: strings.TrimSpace(secret),
		SessionToken:    strings.TrimSpace(values["session_token"]),
		Source:          source,
	}, nil
}

func parseEnvFile(body []byte) (map[string]string, error) {
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

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

func sha256Hex(body []byte) string {
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

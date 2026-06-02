package r2control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	ParentCredentialSourceAuto    = "auto"
	ParentCredentialSourceEnv     = "env"
	ParentCredentialSourceEnvFile = "env-file"
)

type ParentCredentialConfig struct {
	Source             string
	AccountID          string
	CredentialsFile    string
	AccessKeyIDEnv     string
	SecretAccessKeyEnv string
	APITokenEnv        string
	SessionTokenEnv    string
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
		cfg.Source = ParentCredentialSourceEnv
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
	case ParentCredentialSourceAuto, "":
		if envHasParentCredentials(cfg) {
			creds, err := loadParentCredentialsFromEnv(cfg)
			return resolveParentCredentials(ctx, cfg, creds, err)
		}
		if cfg.CredentialsFile != "" {
			creds, err := loadParentCredentialsFromEnvFile(cfg)
			return resolveParentCredentials(ctx, cfg, creds, err)
		}
		return ParentCredentials{}, fmt.Errorf("no R2 credentials found; set %s plus %s or %s, or pass credentials file",
			cfg.AccessKeyIDEnv, cfg.SecretAccessKeyEnv, cfg.APITokenEnv)
	default:
		return ParentCredentials{}, fmt.Errorf("unsupported R2 credential source %q", cfg.Source)
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
		return ParentCredentials{}, errors.New("credentials file is required for env-file R2 credentials")
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

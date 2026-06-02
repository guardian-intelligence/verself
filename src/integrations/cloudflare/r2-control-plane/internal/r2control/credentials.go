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
	SessionTokenEnv    string
	Timeout            time.Duration
}

type ParentCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
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
		return ParentCredentials{}, fmt.Errorf("no R2 credentials found; set %s plus %s, or pass credentials file",
			cfg.AccessKeyIDEnv, cfg.SecretAccessKeyEnv)
	default:
		return ParentCredentials{}, fmt.Errorf("unsupported R2 credential source %q", cfg.Source)
	}
}

func loadParentCredentialsFromEnv(cfg ParentCredentialConfig) (ParentCredentials, error) {
	values := map[string]string{
		"access_key_id":     strings.TrimSpace(os.Getenv(cfg.AccessKeyIDEnv)),
		"secret_access_key": strings.TrimSpace(os.Getenv(cfg.SecretAccessKeyEnv)),
		"session_token":     strings.TrimSpace(os.Getenv(cfg.SessionTokenEnv)),
	}
	return parentCredentialsFromValues(values, "env:"+cfg.AccessKeyIDEnv)
}

func envHasParentCredentials(cfg ParentCredentialConfig) bool {
	access := strings.TrimSpace(os.Getenv(cfg.AccessKeyIDEnv))
	secret := strings.TrimSpace(os.Getenv(cfg.SecretAccessKeyEnv))
	return access != "" && secret != ""
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
		"session_token":     values[cfg.SessionTokenEnv],
	}
	return parentCredentialsFromValues(mapped, "env-file:"+cfg.CredentialsFile)
}

func resolveParentCredentials(_ context.Context, _ ParentCredentialConfig, creds ParentCredentials, err error) (ParentCredentials, error) {
	if err != nil {
		return ParentCredentials{}, err
	}
	if strings.TrimSpace(creds.AccessKeyID) == "" {
		return ParentCredentials{}, errors.New("r2 parent access key id is required")
	}
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
	if strings.TrimSpace(secret) == "" {
		return ParentCredentials{}, errors.New("r2 parent secret access key is required")
	}
	return ParentCredentials{
		AccessKeyID:     strings.TrimSpace(access),
		SecretAccessKey: strings.TrimSpace(secret),
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

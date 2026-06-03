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

const ParentCredentialSourceFiles = "files"

type ParentCredentialConfig struct {
	Source              string
	AccountID           string
	AccessKeyIDFile     string
	SecretAccessKeyFile string
	SessionTokenFile    string
	Timeout             time.Duration
}

type ParentCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Source          string
}

func (cfg ParentCredentialConfig) WithDefaults() ParentCredentialConfig {
	if cfg.Source == "" {
		cfg.Source = ParentCredentialSourceFiles
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return cfg
}

func LoadParentCredentials(ctx context.Context, cfg ParentCredentialConfig) (ParentCredentials, error) {
	cfg = cfg.WithDefaults()
	switch strings.TrimSpace(cfg.Source) {
	case ParentCredentialSourceFiles:
		creds, err := loadParentCredentialsFromFiles(cfg)
		return resolveParentCredentials(ctx, cfg, creds, err)
	default:
		return ParentCredentials{}, fmt.Errorf("unsupported R2 credential source %q; use %q", cfg.Source, ParentCredentialSourceFiles)
	}
}

func loadParentCredentialsFromFiles(cfg ParentCredentialConfig) (ParentCredentials, error) {
	access, err := readCredentialFile(cfg.AccessKeyIDFile, "r2 parent access key id")
	if err != nil {
		return ParentCredentials{}, err
	}
	secret, err := readCredentialFile(cfg.SecretAccessKeyFile, "r2 parent secret access key")
	if err != nil {
		return ParentCredentials{}, err
	}
	session := ""
	if strings.TrimSpace(cfg.SessionTokenFile) != "" {
		session, err = readCredentialFile(cfg.SessionTokenFile, "r2 parent session token")
		if err != nil {
			return ParentCredentials{}, err
		}
	}
	return parentCredentialsFromValues(map[string]string{
		"access_key_id":     access,
		"secret_access_key": secret,
		"session_token":     session,
	}, "files:"+cfg.AccessKeyIDFile)
}

func readCredentialFile(path, label string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s file is required", label)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", label, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s file is empty", label)
	}
	return value, nil
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

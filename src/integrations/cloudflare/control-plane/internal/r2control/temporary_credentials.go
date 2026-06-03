package r2control

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	TemporaryPermissionAdminReadWrite  = "admin-read-write"
	TemporaryPermissionAdminReadOnly   = "admin-read-only"
	TemporaryPermissionObjectReadWrite = "object-read-write"
	TemporaryPermissionObjectReadOnly  = "object-read-only"
)

type TemporaryCredentialRequest struct {
	ParentAccessKeyID string
	Bucket            string
	Permission        string
	TTL               time.Duration
	Prefixes          []string
	Objects           []string
}

type TemporaryCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type localTemporaryCredentialPayload struct {
	Bucket string                         `json:"bucket"`
	Scope  string                         `json:"scope"`
	Paths  *localTemporaryCredentialPaths `json:"paths,omitempty"`
	Sub    string                         `json:"sub"`
	Iss    string                         `json:"iss"`
	Aud    string                         `json:"aud"`
	Iat    int64                          `json:"iat"`
	Exp    int64                          `json:"exp"`
}

type localTemporaryCredentialPaths struct {
	PrefixPaths []string `json:"prefixPaths"`
	ObjectPaths []string `json:"objectPaths"`
}

func CreateLocalTemporaryCredentials(endpoint, accountID, parentSecretAccessKey string, req TemporaryCredentialRequest) (TemporaryCredentials, error) {
	return createLocalTemporaryCredentialsAt(time.Now().UTC(), endpoint, accountID, parentSecretAccessKey, req)
}

func createLocalTemporaryCredentialsAt(now time.Time, endpoint, accountID, parentSecretAccessKey string, req TemporaryCredentialRequest) (TemporaryCredentials, error) {
	req, err := req.validate()
	if err != nil {
		return TemporaryCredentials{}, err
	}
	accountID = strings.ToLower(strings.TrimSpace(accountID))
	if !IsCloudflareAccountID(accountID) {
		return TemporaryCredentials{}, fmt.Errorf("account ID must be a 32-character lowercase hex Cloudflare account ID")
	}
	parentSecretAccessKey = strings.TrimSpace(parentSecretAccessKey)
	if parentSecretAccessKey == "" {
		return TemporaryCredentials{}, fmt.Errorf("parent secret access key is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return TemporaryCredentials{}, fmt.Errorf("parse r2 endpoint: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return TemporaryCredentials{}, fmt.Errorf("r2 endpoint must be an https URL")
	}
	payload := localTemporaryCredentialPayload{
		Bucket: req.Bucket,
		Scope:  req.Permission,
		Sub:    accountID,
		Iss:    req.ParentAccessKeyID,
		Aud:    parsed.Host,
		Iat:    now.Unix(),
		Exp:    now.Add(req.TTL).Unix(),
	}
	if len(req.Prefixes) > 0 || len(req.Objects) > 0 {
		payload.Paths = &localTemporaryCredentialPaths{
			PrefixPaths: req.Prefixes,
			ObjectPaths: req.Objects,
		}
	}
	jwt, err := signHS256JWT(payload, []byte(parentSecretAccessKey))
	if err != nil {
		return TemporaryCredentials{}, err
	}
	return TemporaryCredentials{
		AccessKeyID:     req.ParentAccessKeyID,
		SecretAccessKey: SHA256Hex([]byte(jwt)),
		SessionToken:    base64.StdEncoding.EncodeToString([]byte("jwt/" + jwt)),
	}, nil
}

func signHS256JWT(payload localTemporaryCredentialPayload, secret []byte) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	signature := mac.Sum(nil)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (req TemporaryCredentialRequest) validate() (TemporaryCredentialRequest, error) {
	req.ParentAccessKeyID = strings.TrimSpace(req.ParentAccessKeyID)
	req.Bucket = strings.TrimSpace(req.Bucket)
	req.Permission = strings.TrimSpace(req.Permission)
	if req.ParentAccessKeyID == "" {
		return TemporaryCredentialRequest{}, fmt.Errorf("parent access key ID is required")
	}
	if !IsR2BucketName(req.Bucket) {
		return TemporaryCredentialRequest{}, fmt.Errorf("bucket must be a valid lowercase R2 bucket name")
	}
	switch req.Permission {
	case TemporaryPermissionAdminReadWrite, TemporaryPermissionAdminReadOnly, TemporaryPermissionObjectReadWrite, TemporaryPermissionObjectReadOnly:
	default:
		return TemporaryCredentialRequest{}, fmt.Errorf("unsupported temporary r2 permission %q", req.Permission)
	}
	if req.TTL == 0 {
		req.TTL = 15 * time.Minute
	}
	if req.TTL < time.Minute || req.TTL > 7*24*time.Hour {
		return TemporaryCredentialRequest{}, fmt.Errorf("r2 temporary credential TTL must be between 1 minute and 7 days")
	}
	req.Prefixes = cleanPaths(req.Prefixes)
	req.Objects = cleanPaths(req.Objects)
	return req, nil
}

func cleanPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimLeft(strings.TrimSpace(path), "/")
		if path != "" {
			cleaned = append(cleaned, path)
		}
	}
	return cleaned
}

package main

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

type localTemporaryCredentialInput struct {
	AccountID             string
	Endpoint              string
	ParentAccessKeyID     string
	ParentSecretAccessKey string
	Bucket                string
	Scope                 string
	Actions               []string
	PrefixPaths           []string
	ObjectPaths           []string
	TTL                   time.Duration
	Now                   time.Time
}

type temporaryCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

func mintLocalTemporaryCredentials(input localTemporaryCredentialInput) (temporaryCredentials, error) {
	if strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.Endpoint) == "" || strings.TrimSpace(input.ParentAccessKeyID) == "" || strings.TrimSpace(input.ParentSecretAccessKey) == "" || strings.TrimSpace(input.Bucket) == "" {
		return temporaryCredentials{}, fmt.Errorf("account ID, endpoint, parent access key, parent secret, and bucket are required for local R2 temporary credentials")
	}
	parsedEndpoint, err := url.Parse(input.Endpoint)
	if err != nil {
		return temporaryCredentials{}, fmt.Errorf("parse R2 endpoint: %w", err)
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := input.TTL
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	if ttl < time.Minute || ttl > 7*24*time.Hour {
		return temporaryCredentials{}, fmt.Errorf("R2 temporary credential TTL must be between 1 minute and 7 days")
	}
	claims := struct {
		Bucket  string   `json:"bucket"`
		Scope   string   `json:"scope"`
		Actions []string `json:"actions,omitempty"`
		Paths   struct {
			PrefixPaths []string `json:"prefixPaths"`
			ObjectPaths []string `json:"objectPaths"`
		} `json:"paths,omitempty"`
		Subject    string `json:"sub"`
		Issuer     string `json:"iss"`
		Audience   string `json:"aud"`
		IssuedAt   int64  `json:"iat"`
		Expiration int64  `json:"exp"`
	}{
		Bucket:     input.Bucket,
		Scope:      firstNonEmpty(input.Scope, "object-read-only"),
		Actions:    input.Actions,
		Subject:    strings.TrimSpace(input.AccountID),
		Issuer:     strings.TrimSpace(input.ParentAccessKeyID),
		Audience:   parsedEndpoint.Host,
		IssuedAt:   now.Unix(),
		Expiration: now.Add(ttl).Unix(),
	}
	claims.Paths.PrefixPaths = input.PrefixPaths
	claims.Paths.ObjectPaths = input.ObjectPaths

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return temporaryCredentials{}, err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return temporaryCredentials{}, err
	}
	signed := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(input.ParentSecretAccessKey)))
	_, _ = mac.Write([]byte(signed))
	jwt := signed + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	secret := sha256Hex([]byte(jwt))
	session := base64.StdEncoding.EncodeToString([]byte("jwt/" + jwt))
	return temporaryCredentials{
		AccessKeyID:     strings.TrimSpace(input.ParentAccessKeyID),
		SecretAccessKey: secret,
		SessionToken:    session,
	}, nil
}

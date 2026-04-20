package delegation

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	InjectionGrantVersion = "fmig1"
	injectionGrantParts   = 3
	injectionGrantSep     = ":"
)

var (
	ErrInvalidGrant = errors.New("invalid injection grant")
	ErrExpiredGrant = errors.New("expired injection grant")
)

type InjectionGrant struct {
	Version          string `json:"v"`
	GrantID          string `json:"gid"`
	IssuerSPIFFEID   string `json:"iss"`
	AudienceSPIFFEID string `json:"aud"`
	OrgID            string `json:"org"`
	ActorID          string `json:"act"`
	ExecutionID      string `json:"exe"`
	AttemptID        string `json:"att"`
	EnvName          string `json:"env"`
	Kind             string `json:"kind"`
	SecretName       string `json:"secret"`
	ScopeLevel       string `json:"scope"`
	SourceID         string `json:"src"`
	EnvID            string `json:"env_id"`
	Branch           string `json:"branch"`
	ExpiresAtUnix    int64  `json:"exp"`
}

func SignInjectionGrant(privateKey ed25519.PrivateKey, grant InjectionGrant) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("%w: ed25519 private key is required", ErrInvalidGrant)
	}
	payload, err := marshalInjectionGrant(grant)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, payload)
	return strings.Join([]string{
		InjectionGrantVersion,
		base64.RawURLEncoding.EncodeToString(payload),
		base64.RawURLEncoding.EncodeToString(signature),
	}, injectionGrantSep), nil
}

func VerifyInjectionGrant(publicKey ed25519.PublicKey, token string, now time.Time) (InjectionGrant, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return InjectionGrant{}, fmt.Errorf("%w: ed25519 public key is required", ErrInvalidGrant)
	}
	parts := strings.Split(strings.TrimSpace(token), injectionGrantSep)
	if len(parts) != injectionGrantParts || parts[0] != InjectionGrantVersion {
		return InjectionGrant{}, ErrInvalidGrant
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return InjectionGrant{}, ErrInvalidGrant
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return InjectionGrant{}, ErrInvalidGrant
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return InjectionGrant{}, ErrInvalidGrant
	}
	var grant InjectionGrant
	if err := json.Unmarshal(payload, &grant); err != nil {
		return InjectionGrant{}, ErrInvalidGrant
	}
	if _, err := marshalInjectionGrant(grant); err != nil {
		return InjectionGrant{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	if grant.ExpiresAtUnix <= 0 || now.Unix() > grant.ExpiresAtUnix {
		return InjectionGrant{}, ErrExpiredGrant
	}
	return grant, nil
}

func ParseEd25519PrivateKeyPEM(raw []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%w: private key PEM is missing", ErrInvalidGrant)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse private key PEM", ErrInvalidGrant)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: private key must be ed25519", ErrInvalidGrant)
	}
	return privateKey, nil
}

func ParseEd25519PublicKeyPEM(raw []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%w: public key PEM is missing", ErrInvalidGrant)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse public key PEM", ErrInvalidGrant)
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: public key must be ed25519", ErrInvalidGrant)
	}
	return publicKey, nil
}

func marshalInjectionGrant(grant InjectionGrant) ([]byte, error) {
	grant.Version = strings.TrimSpace(grant.Version)
	if grant.Version != InjectionGrantVersion {
		return nil, fmt.Errorf("%w: unsupported grant version", ErrInvalidGrant)
	}
	required := []string{
		grant.GrantID,
		grant.IssuerSPIFFEID,
		grant.AudienceSPIFFEID,
		grant.OrgID,
		grant.ActorID,
		grant.ExecutionID,
		grant.AttemptID,
		grant.EnvName,
		grant.Kind,
		grant.SecretName,
		grant.ScopeLevel,
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: grant has empty required field", ErrInvalidGrant)
		}
	}
	if grant.ExpiresAtUnix <= 0 {
		return nil, fmt.Errorf("%w: grant expiration is required", ErrInvalidGrant)
	}
	payload, err := json.Marshal(grant)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal grant", ErrInvalidGrant)
	}
	return payload, nil
}

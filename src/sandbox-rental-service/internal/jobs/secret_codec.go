package jobs

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const (
	secretCiphertextPrefix = "v1:"
	webhookSecretPrefix    = "whsec_"
)

type SecretCodec struct {
	aead cipher.AEAD
}

func NewSecretCodec(rawKey string) (*SecretCodec, error) {
	key, err := decodeSecretKey(rawKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create secret gcm: %w", err)
	}
	return &SecretCodec{aead: aead}, nil
}

func (c *SecretCodec) Encrypt(plaintext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("secret codec is not configured")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return secretCiphertextPrefix + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (c *SecretCodec) Decrypt(ciphertext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", fmt.Errorf("secret codec is not configured")
	}
	encoded := strings.TrimSpace(ciphertext)
	if !strings.HasPrefix(encoded, secretCiphertextPrefix) {
		return "", fmt.Errorf("unsupported secret ciphertext version")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, secretCiphertextPrefix))
	if err != nil {
		return "", fmt.Errorf("decode secret ciphertext: %w", err)
	}
	if len(raw) < c.aead.NonceSize() {
		return "", fmt.Errorf("secret ciphertext is too short")
	}
	nonce := raw[:c.aead.NonceSize()]
	body := raw[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret ciphertext: %w", err)
	}
	return string(plaintext), nil
}

func GenerateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return webhookSecretPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func SecretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func decodeSecretKey(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "base64:")
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	return nil, fmt.Errorf("secret key must decode to 32 bytes")
}

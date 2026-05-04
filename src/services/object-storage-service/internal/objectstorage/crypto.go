package objectstorage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type SecretBox struct {
	key [32]byte
}

func NewSecretBox(rawKey []byte) (*SecretBox, error) {
	if len(rawKey) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(rawKey))
	}
	box := &SecretBox{}
	copy(box.key[:], rawKey)
	return box, nil
}

func (b *SecretBox) Encrypt(plaintext string) ([]byte, []byte, error) {
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

func (b *SecretBox) Decrypt(ciphertext, nonce []byte) (string, error) {
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(nonce) != aead.NonceSize() {
		return "", fmt.Errorf("secret nonce must be %d bytes, got %d", aead.NonceSize(), len(nonce))
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func SecretFingerprint(secret string) string {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:8])
}

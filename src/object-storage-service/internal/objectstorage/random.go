package objectstorage

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func NewAccessKeyID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate access key id: %w", err)
	}
	return "VS" + stringsUpper(hex.EncodeToString(raw)), nil
}

func NewSecretAccessKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret access key: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(raw), nil
}

func stringsUpper(v string) string {
	out := make([]byte, len(v))
	copy(out, v)
	for i, b := range out {
		if b >= 'a' && b <= 'f' {
			out[i] = b - ('a' - 'A')
		}
	}
	return string(out)
}

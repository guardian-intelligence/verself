package runtime

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func ReadSecretVarsValue(path, key string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("secret vars path is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("secret vars key is required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret vars %s: %w", path, err)
	}
	var values map[string]string
	if err := yaml.Unmarshal(body, &values); err != nil {
		return "", fmt.Errorf("decode secret vars %s: %w", path, err)
	}
	value := strings.TrimSpace(values[key])
	if value == "" {
		return "", fmt.Errorf("secret vars %s has no non-empty %s", path, key)
	}
	return value, nil
}

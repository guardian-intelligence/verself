package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func DecryptSOPSValue(ctx context.Context, path, key string) (string, error) {
	if path == "" {
		return "", errors.New("sops decrypt: path is required")
	}
	if key == "" {
		return "", errors.New("sops decrypt: key is required")
	}
	extract := fmt.Sprintf("[\"%s\"]", key)
	cmd := exec.CommandContext(ctx, "sops", "-d", "--extract", extract, path)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("sops decrypt %s in %s: %w (%s)", key, path, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("sops decrypt %s in %s: %w", key, path, err)
	}
	value := strings.TrimRight(string(out), "\r\n")
	if value == "" {
		return "", fmt.Errorf("%s yielded an empty value for %s", path, key)
	}
	return value, nil
}

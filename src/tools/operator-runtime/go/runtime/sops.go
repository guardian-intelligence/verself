package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

func DecryptSOPSValue(ctx context.Context, path, key string) (string, error) {
	if path == "" {
		return "", errors.New("sops decrypt: path is required")
	}
	if key == "" {
		return "", errors.New("sops decrypt: key is required")
	}
	extract := fmt.Sprintf("[\"%s\"]", key)
	cmd := exec.CommandContext(ctx, sopsBinary(), "-d", "--extract", extract, path)
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

func sopsBinary() string {
	if value := strings.TrimSpace(os.Getenv("VERSELF_SOPS_BIN")); value != "" {
		return value
	}
	if path := bazelRunfile("src/tools/dev/binaries/sops"); path != "" {
		return path
	}
	return "sops"
}

func bazelRunfile(rel string) string {
	for _, candidate := range []string{"verself/" + rel, "_main/" + rel, rel} {
		path, err := runfiles.Rlocation(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

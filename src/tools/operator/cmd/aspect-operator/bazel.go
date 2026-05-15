package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func buildBazelBinary(ctx context.Context, repoRoot, target string) (string, error) {
	if repoRoot == "" {
		return "", errors.New("bazel build: repo root is required")
	}
	cmd := exec.CommandContext(ctx, "bazelisk", "build", target)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("bazelisk build %s: %w\n%s", target, err, string(out))
	}
	cmd = exec.CommandContext(ctx, "bazelisk", "cquery", "--output=files", target)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("bazelisk cquery --output=files %s: %w\n%s", target, err, stderr.String())
	}
	files := strings.Fields(string(out))
	if len(files) != 1 {
		return "", fmt.Errorf("bazel cquery %s: expected one output file, got %d", target, len(files))
	}
	return filepath.Join(repoRoot, files[0]), nil
}

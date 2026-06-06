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
	cmd := guardianBazelCommand(ctx, repoRoot, "build", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("guardian run bazel -- build %s: %w\n%s", target, err, string(out))
	}
	cmd = guardianBazelCommand(ctx, repoRoot, "cquery", "--output=files", target)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("guardian run bazel -- cquery --output=files %s: %w\n%s", target, err, stderr.String())
	}
	files := strings.Fields(string(out))
	if len(files) != 1 {
		return "", fmt.Errorf("bazel cquery %s: expected one output file, got %d", target, len(files))
	}
	return filepath.Join(repoRoot, files[0]), nil
}

func guardianBazelCommand(ctx context.Context, repoRoot string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "guardian", append([]string{"run", "bazel", "--"}, args...)...)
	cmd.Dir = repoRoot
	return cmd
}

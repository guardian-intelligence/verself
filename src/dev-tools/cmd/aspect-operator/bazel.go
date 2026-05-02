package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
)

func buildBazelBinary(ctx context.Context, repoRoot, target, binRel string) (string, error) {
	if repoRoot == "" {
		return "", errors.New("bazel build: repo root is required")
	}
	cmd := exec.CommandContext(ctx, "bazelisk", "build", target)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("bazelisk build %s: %w\n%s", target, err, string(out))
	}
	return filepath.Join(repoRoot, binRel), nil
}

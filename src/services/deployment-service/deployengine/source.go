package deployengine

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const sourceSyncTimeout = 2 * time.Minute

func prepareSource(ctx context.Context, exec execution) error {
	ctx, span := exec.Tracer.Start(ctx, "verself_deploy.source.prepare",
		trace.WithAttributes(
			attribute.String("verself.site", exec.Site),
			attribute.String("verself.deploy_sha", exec.SHA),
		),
	)
	defer span.End()
	if err := ensureGitWorktree(ctx, exec.RepoRoot); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureCleanWorktree(ctx, exec.RepoRoot); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	current, err := gitOutput(ctx, exec.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if strings.EqualFold(current, exec.SHA) {
		span.SetAttributes(attribute.String("verself.source.checkout", "already_at_sha"))
		span.SetStatus(codes.Ok, "")
		return nil
	}
	syncCtx, cancel := context.WithTimeout(ctx, sourceSyncTimeout)
	defer cancel()
	if err := gitRun(syncCtx, exec.RepoRoot, "fetch", "--no-tags", "--depth=1", "origin", exec.SHA); err != nil {
		// Some Git servers reject raw-SHA shallow fetches; fall back to the
		// repository's configured refs and still require the exact requested SHA.
		if fallbackErr := gitRun(syncCtx, exec.RepoRoot, "fetch", "--no-tags", "origin"); fallbackErr != nil {
			err = fmt.Errorf("%w; fallback fetch failed: %v", err, fallbackErr)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	if err := gitRun(syncCtx, exec.RepoRoot, "checkout", "--detach", exec.SHA); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	current, err = gitOutput(ctx, exec.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if !strings.EqualFold(current, exec.SHA) {
		err := fmt.Errorf("source checkout resolved %s, expected %s", current, exec.SHA)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.String("verself.source.checkout", "fetched"))
	span.SetStatus(codes.Ok, "")
	return nil
}

func ensureGitWorktree(ctx context.Context, repoRoot string) error {
	out, err := gitOutput(ctx, repoRoot, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return err
	}
	if out != "true" {
		return fmt.Errorf("%s is not a Git worktree", repoRoot)
	}
	return nil
}

func ensureCleanWorktree(ctx context.Context, repoRoot string) error {
	out, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("deployment-service source worktree is dirty; refusing to build from uncommitted state")
	}
	return nil
}

func gitRun(ctx context.Context, repoRoot string, args ...string) error {
	_, err := gitCommand(ctx, repoRoot, args...)
	return err
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	out, err := gitCommand(ctx, repoRoot, args...)
	return strings.TrimSpace(out), err
}

func gitCommand(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

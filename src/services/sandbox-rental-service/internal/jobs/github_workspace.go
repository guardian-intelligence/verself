package jobs

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	githubRunnerRuntimeDir          = "/tmp/verself-actions-runner"
	githubRunnerDurableWorkDir      = "/workspace/.verself/actions-runner/_work"
	githubWorkspaceComponentKind    = "github_workspace"
	githubWorkspaceKey              = "repo-workspace"
	githubWorkspaceDefaultSizeBytes = 100 * 1024 * 1024 * 1024
)

func (s *Service) prepareGitHubWorkspace(ctx context.Context, item executionWorkItem) error {
	if item.WorkloadKind != WorkloadKindRunner || item.ExternalProvider != RunnerProviderGitHub {
		return nil
	}
	if strings.TrimSpace(item.LeaseID) == "" {
		return fmt.Errorf("%w: github workspace requires an active lease", ErrCheckpointUnavailable)
	}
	ctx, span := tracer.Start(ctx, "github.workspace.prepare", trace.WithAttributes(
		attribute.String("execution.id", item.ExecutionID.String()),
		attribute.String("attempt.id", item.AttemptID.String()),
	))
	defer span.End()

	identity, err := s.runnerExecutionIdentity(ctx, item.ExecutionID, item.AttemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: runner execution identity missing", ErrCheckpointUnauthorized)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if identity.Provider != RunnerProviderGitHub {
		return nil
	}
	mountPath, err := githubWorkspacePath(identity.RepositoryFullName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	runnerWorkspacePath, err := githubRunnerWorkspacePath(identity.RepositoryFullName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.String("github.repository", identity.RepositoryFullName),
		attribute.String("github.workspace.path", mountPath),
		attribute.String("github.runner_workspace.path", runnerWorkspacePath),
		attribute.String("github.workspace.scope_ref", checkpointScopeRef(identity)),
	)
	mount, err := s.mountCheckpointWithPlan(ctx, identity, checkpointMountPlan{
		Key:             githubWorkspaceKey,
		MountPath:       mountPath,
		SizeBytes:       githubWorkspaceDefaultSizeBytes,
		ProductKind:     checkpointProductKind,
		ComponentKind:   githubWorkspaceComponentKind,
		RetentionPolicy: checkpointRetentionPolicy,
		ScopeKind:       checkpointScopeKind,
		ScopeRef:        checkpointScopeRef(identity),
		TrustClass:      checkpointTrustClass,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Bool("github.workspace.cache_hit", mount.CacheHit))
	if err := s.markCheckpointMounted(ctx, identity, mount.MountID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := s.requestCheckpointSave(ctx, identity, mount.MountID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func githubWorkspacePath(repositoryFullName string) (string, error) {
	if _, err := githubRunnerWorkspacePath(repositoryFullName); err != nil {
		return "", err
	}
	return githubRunnerDurableWorkDir, nil
}

func githubRunnerWorkspacePath(repositoryFullName string) (string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	if !ok || owner == "" || repo == "" {
		return "", fmt.Errorf("%w: github repository must be owner/name", ErrCheckpointInvalid)
	}
	if strings.ContainsAny(repo, "\x00/\r\n\t ") || strings.Contains(repo, "..") {
		return "", fmt.Errorf("%w: invalid github repository name", ErrCheckpointInvalid)
	}
	return path.Join(githubRunnerRuntimeDir, githubRunnerWorkFolder, repo, repo), nil
}

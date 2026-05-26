package githubintegration

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/verself/github-integration-service/internal/store"
)

const (
	providerSurfaceCreateFailureCheckRun = "create_failure_check_run"
	providerSurfaceStaleAfter            = 2 * time.Minute
)

type providerSurfaceCommand struct {
	SurfaceID              uuid.UUID
	CommandKey             string
	CommandKind            string
	OrgID                  string
	InstallationBindingID  uuid.UUID
	RepositoryBindingID    uuid.UUID
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	RepositoryFullName     string
	ProviderRunID          int64
	ProviderRunAttempt     int64
	ProviderJobID          int64
	RunnerID               int64
	RunnerName             string
	RunnerClass            string
	PrimaryProblemCode     string
	PrimaryProblemDetail   string
	AttemptCount           int32
}

func (s *Service) ProcessProviderSurfaceCommands(ctx context.Context) error {
	rows, err := s.queries.LockReadyProviderSurfaceCommands(ctx, store.LockReadyProviderSurfaceCommandsParams{
		LockedAt:   pgTime(time.Now().UTC()),
		LimitCount: s.cfg.WorkerBatchSize,
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, row := range rows {
		if err := s.processProviderSurfaceCommand(ctx, providerSurfaceCommandFromReady(row)); err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}
	return firstErr
}

func (s *Service) ProcessStaleProviderSurfaceCommands(ctx context.Context) error {
	rows, err := s.queries.ListStaleProviderSurfaceCommands(ctx, store.ListStaleProviderSurfaceCommandsParams{
		StaleBefore: pgTime(time.Now().UTC().Add(-providerSurfaceStaleAfter)),
		LimitCount:  s.cfg.WorkerBatchSize,
	})
	if err != nil {
		return err
	}
	var firstErr error
	for _, row := range rows {
		cmd := providerSurfaceCommandFromStale(row)
		problems := runnerProblemSet{}
		problems.add(githubRunnerProblemFromCatalog(problemGithubRunnerProviderSurfaceWorkerLost))
		terminal := cmd.AttemptCount >= s.cfg.MaxProviderSurfaceTries
		if terminal {
			problems.add(githubRunnerProblemFromCatalog(problemGithubRunnerProviderSurfaceAttemptsExhausted))
		}
		if err := s.recordProviderSurfaceCommandFailure(ctx, cmd, problems, terminal); err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}
	return firstErr
}

func (s *Service) processProviderSurfaceCommand(ctx context.Context, cmd providerSurfaceCommand) error {
	started := time.Now().UTC()
	switch cmd.CommandKind {
	case providerSurfaceCreateFailureCheckRun:
		err := s.executeCreateFailureCheckRunSurface(ctx, cmd)
		if err == nil {
			if markErr := s.queries.MarkProviderSurfaceCommandSucceeded(ctx, store.MarkProviderSurfaceCommandSucceededParams{
				SurfaceID:   pgUUID(cmd.SurfaceID),
				CompletedAt: pgTime(time.Now().UTC()),
			}); markErr != nil {
				return markErr
			}
			s.writeEvent(ctx, githubEventFromMetadata(providerSurfaceMetadata(cmd), "github.provider_surface.succeeded", "succeeded", cmd.PrimaryProblemReason(), started, time.Now().UTC()))
			return nil
		}
		problems := runnerProblemSet{}
		problems.add(githubRunnerProblemFromError(problemGithubRunnerProviderSurfaceFailed, err))
		terminal := cmd.AttemptCount >= s.cfg.MaxProviderSurfaceTries
		if terminal {
			problems.add(githubRunnerProblemFromCatalog(problemGithubRunnerProviderSurfaceAttemptsExhausted))
		}
		if recordErr := s.recordProviderSurfaceCommandFailure(ctx, cmd, problems, terminal); recordErr != nil {
			return recordErr
		}
		s.writeEvent(ctx, githubEventFromMetadata(providerSurfaceMetadata(cmd), "github.provider_surface.failed", providerSurfaceFailureResult(terminal), err.Error(), started, time.Now().UTC()))
		return err
	default:
		problems := runnerProblemSet{}
		problems.add(githubRunnerProblemFromCatalog(
			problemGithubRunnerProviderSurfaceCommandUnsupported,
			withProblemDiagnostic("unsupported provider surface command kind: "+cmd.CommandKind),
		))
		if err := s.recordProviderSurfaceCommandFailure(ctx, cmd, problems, true); err != nil {
			return err
		}
		return fmt.Errorf("%w: unsupported provider surface command kind %q", ErrConfiguration, cmd.CommandKind)
	}
}

func (s *Service) executeCreateFailureCheckRunSurface(ctx context.Context, cmd providerSurfaceCommand) error {
	if cmd.ProviderInstallationID == 0 || cmd.ProviderRunID == 0 || strings.TrimSpace(cmd.RepositoryFullName) == "" {
		return fmt.Errorf("provider surface command missing workflow run target")
	}
	surfaceCtx, cancel := context.WithTimeout(ctx, runnerProviderSurfaceTimeout)
	defer cancel()
	run, err := s.fetchWorkflowRun(surfaceCtx, cmd.ProviderInstallationID, cmd.RepositoryFullName, cmd.ProviderRunID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.HeadSHA) == "" {
		return fmt.Errorf("provider surface workflow run missing head_sha")
	}
	return s.createFailureCheckRun(surfaceCtx, cmd.ProviderInstallationID, cmd.RepositoryFullName, run.HeadSHA, cmd.CommandKey, providerSurfaceCheckSummary(cmd), providerSurfaceCheckText(cmd))
}

func (s *Service) recordProviderSurfaceCommandFailure(ctx context.Context, cmd providerSurfaceCommand, problems runnerProblemSet, terminal bool) error {
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	qtx := store.New(tx)
	if err := appendProviderSurfaceProblems(ctx, qtx, cmd.SurfaceID, problems); err != nil {
		return err
	}
	if err := appendProviderDemandProblems(ctx, qtx, cmd.ProviderJobID, problems); err != nil {
		return err
	}
	if err := appendRunnerInstanceProblems(ctx, qtx, cmd.RunnerName, problems); err != nil {
		return err
	}
	now := time.Now().UTC()
	if terminal {
		if err := qtx.MarkProviderSurfaceCommandFailed(ctx, store.MarkProviderSurfaceCommandFailedParams{
			SurfaceID: pgUUID(cmd.SurfaceID),
			FailedAt:  pgTime(now),
		}); err != nil {
			return err
		}
	} else {
		if err := qtx.MarkProviderSurfaceCommandRetryable(ctx, store.MarkProviderSurfaceCommandRetryableParams{
			SurfaceID:     pgUUID(cmd.SurfaceID),
			NextAttemptAt: pgTime(now.Add(retryDelay(cmd.AttemptCount))),
			UpdatedAt:     pgTime(now),
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func enqueueProviderSurfaceCommandTx(ctx context.Context, q *store.Queries, ref runnerCapacityRef, problems runnerProblemSet, at time.Time) (bool, error) {
	if ref.ProviderInstallationID == 0 || ref.ProviderRunID == 0 || strings.TrimSpace(ref.RepositoryFullName) == "" {
		return false, nil
	}
	commandKey := providerSurfaceCommandKey(ref)
	row, err := q.UpsertProviderSurfaceCommand(ctx, store.UpsertProviderSurfaceCommandParams{
		SurfaceID:              pgUUID(uuid.New()),
		CommandKey:             commandKey,
		CommandKind:            providerSurfaceCreateFailureCheckRun,
		OrgID:                  ref.OrgID,
		InstallationBindingID:  pgUUID(ref.InstallationBindingID),
		RepositoryBindingID:    pgUUID(ref.RepositoryBindingID),
		ProviderInstallationID: ref.ProviderInstallationID,
		ProviderRepositoryID:   ref.ProviderRepositoryID,
		RepositoryFullName:     ref.RepositoryFullName,
		ProviderRunID:          ref.ProviderRunID,
		ProviderRunAttempt:     ref.ProviderRunAttempt,
		ProviderJobID:          ref.ProviderJobID,
		RunnerID:               ref.RunnerID,
		RunnerName:             ref.RunnerName,
		RunnerClass:            ref.RunnerClass,
		NextAttemptAt:          pgTime(at),
		UpdatedAt:              pgTime(at),
	})
	if err != nil {
		return false, err
	}
	if row.State == "succeeded" {
		return false, nil
	}
	if err := appendProviderSurfaceProblems(ctx, q, uuidFromPG(row.SurfaceID), problems); err != nil {
		return false, err
	}
	return true, nil
}

func appendProviderSurfaceProblems(ctx context.Context, q *store.Queries, surfaceID uuid.UUID, problems runnerProblemSet) error {
	if surfaceID == uuid.Nil {
		return nil
	}
	for _, problem := range problems.problems {
		if problem.ObservedAt.IsZero() {
			problem.ObservedAt = time.Now().UTC()
		}
		if err := q.AppendProviderSurfaceProblem(ctx, store.AppendProviderSurfaceProblemParams{
			SurfaceID:   pgUUID(surfaceID),
			Phase:       problem.Phase,
			ProblemType: problem.Type,
			ProblemCode: problem.Code,
			Title:       problem.Title,
			Detail:      problem.Detail,
			DocsUrl:     problem.DocsURL,
			Status:      problem.Status,
			Retryable:   problem.Retryable,
			Pointer:     problem.Pointer,
			ObservedAt:  pgTime(problem.ObservedAt),
		}); err != nil {
			return err
		}
	}
	return nil
}

func providerSurfaceCommandKey(ref runnerCapacityRef) string {
	if ref.ProviderRunID != 0 {
		return strings.Join([]string{
			providerGitHub,
			providerSurfaceCreateFailureCheckRun,
			strconv.FormatInt(ref.ProviderInstallationID, 10),
			strconv.FormatInt(ref.ProviderRepositoryID, 10),
			strconv.FormatInt(ref.ProviderRunID, 10),
		}, ":")
	}
	return strings.Join([]string{
		providerGitHub,
		providerSurfaceCreateFailureCheckRun,
		strconv.FormatInt(ref.ProviderInstallationID, 10),
		strconv.FormatInt(ref.ProviderRepositoryID, 10),
		"job",
		strconv.FormatInt(ref.ProviderJobID, 10),
	}, ":")
}

func providerSurfaceCommandFromReady(row store.LockReadyProviderSurfaceCommandsRow) providerSurfaceCommand {
	return providerSurfaceCommand{
		SurfaceID:              uuidFromPG(row.SurfaceID),
		CommandKey:             row.CommandKey,
		CommandKind:            row.CommandKind,
		OrgID:                  row.OrgID,
		InstallationBindingID:  uuidFromPG(row.InstallationBindingID),
		RepositoryBindingID:    uuidFromPG(row.RepositoryBindingID),
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		RepositoryFullName:     row.RepositoryFullName,
		ProviderRunID:          row.ProviderRunID,
		ProviderRunAttempt:     row.ProviderRunAttempt,
		ProviderJobID:          row.ProviderJobID,
		RunnerID:               row.RunnerID,
		RunnerName:             row.RunnerName,
		RunnerClass:            row.RunnerClass,
		PrimaryProblemCode:     row.PrimaryProblemCode,
		PrimaryProblemDetail:   row.PrimaryProblemDetail,
		AttemptCount:           row.AttemptCount,
	}
}

func providerSurfaceCommandFromStale(row store.ListStaleProviderSurfaceCommandsRow) providerSurfaceCommand {
	return providerSurfaceCommand{
		SurfaceID:              uuidFromPG(row.SurfaceID),
		CommandKey:             row.CommandKey,
		CommandKind:            row.CommandKind,
		OrgID:                  row.OrgID,
		InstallationBindingID:  uuidFromPG(row.InstallationBindingID),
		RepositoryBindingID:    uuidFromPG(row.RepositoryBindingID),
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		RepositoryFullName:     row.RepositoryFullName,
		ProviderRunID:          row.ProviderRunID,
		ProviderRunAttempt:     row.ProviderRunAttempt,
		ProviderJobID:          row.ProviderJobID,
		RunnerID:               row.RunnerID,
		RunnerName:             row.RunnerName,
		RunnerClass:            row.RunnerClass,
		PrimaryProblemCode:     row.PrimaryProblemCode,
		PrimaryProblemDetail:   row.PrimaryProblemDetail,
		AttemptCount:           row.AttemptCount,
	}
}

func providerSurfaceMetadata(cmd providerSurfaceCommand) webhookMetadata {
	return runnerCapacityMetadata(runnerCapacityRef{
		OrgID:                  cmd.OrgID,
		InstallationBindingID:  cmd.InstallationBindingID,
		RepositoryBindingID:    cmd.RepositoryBindingID,
		ProviderInstallationID: cmd.ProviderInstallationID,
		ProviderRepositoryID:   cmd.ProviderRepositoryID,
		RepositoryFullName:     cmd.RepositoryFullName,
		ProviderRunID:          cmd.ProviderRunID,
		ProviderRunAttempt:     cmd.ProviderRunAttempt,
		ProviderJobID:          cmd.ProviderJobID,
		RunnerID:               cmd.RunnerID,
		RunnerName:             cmd.RunnerName,
		RunnerClass:            cmd.RunnerClass,
	})
}

func (cmd providerSurfaceCommand) PrimaryProblemReason() string {
	if strings.TrimSpace(cmd.PrimaryProblemDetail) != "" {
		return cmd.PrimaryProblemCode + ":" + truncate(cmd.PrimaryProblemDetail, 512)
	}
	return cmd.PrimaryProblemCode
}

func providerSurfaceFailureResult(terminal bool) string {
	if terminal {
		return "failed"
	}
	return "retryable"
}

func providerSurfaceCheckSummary(cmd providerSurfaceCommand) string {
	reason := strings.TrimSpace(cmd.PrimaryProblemReason())
	if reason == "" {
		return "Verself could not start the requested runner capacity."
	}
	return "Verself could not start the requested runner capacity: " + truncate(reason, 160)
}

func providerSurfaceCheckText(cmd providerSurfaceCommand) string {
	parts := []string{
		"Verself could not start the self-hosted runner capacity for this workflow run.",
		"repository=" + cmd.RepositoryFullName,
		"run_id=" + strconv.FormatInt(cmd.ProviderRunID, 10),
	}
	if cmd.ProviderJobID != 0 {
		parts = append(parts, "job_id="+strconv.FormatInt(cmd.ProviderJobID, 10))
	}
	if strings.TrimSpace(cmd.RunnerClass) != "" {
		parts = append(parts, "runner_class="+cmd.RunnerClass)
	}
	if reason := strings.TrimSpace(cmd.PrimaryProblemReason()); reason != "" {
		parts = append(parts, "problem="+truncate(reason, 512))
	}
	return strings.Join(parts, "\n")
}

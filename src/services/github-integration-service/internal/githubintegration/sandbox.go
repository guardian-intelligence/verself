package githubintegration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/github-integration-service/internal/store"
	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
)

func (s *Service) insertTerminalEvidence(ctx context.Context, job githubWorkflowJob, deliveryID string) error {
	evidenceID := uuid.New()
	assignment, err := s.queries.GetJobAssignmentContext(ctx, store.GetJobAssignmentContextParams{ProviderJobID: job.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		assignment = store.GetJobAssignmentContextRow{}
	} else if err != nil {
		return err
	}
	trustClass := assignment.TrustClass
	terminalEvidenceID, err := s.queries.InsertTerminalJobEvidence(ctx, store.InsertTerminalJobEvidenceParams{
		TerminalEvidenceID:     pgUUID(evidenceID),
		ProviderJobID:          job.ID,
		OrgID:                  assignment.OrgID,
		InstallationBindingID:  assignment.InstallationBindingID,
		RepositoryBindingID:    assignment.RepositoryBindingID,
		ProviderInstallationID: assignment.ProviderInstallationID,
		ProviderRepositoryID:   assignment.ProviderRepositoryID,
		ProviderRunID:          job.RunID,
		ProviderRunAttempt:     job.RunAttempt,
		SandboxAllocationID:    assignment.SandboxAllocationID,
		SandboxExecutionID:     assignment.SandboxExecutionID,
		SandboxAttemptID:       assignment.SandboxAttemptID,
		RunnerID:               assignment.RunnerID,
		RunnerName:             assignment.RunnerName,
		JobShapeID:             assignment.JobShapeID,
		TrustClass:             trustClass,
		Status:                 job.Status,
		Conclusion:             job.Conclusion,
		Source:                 "github-api",
		DeliveryID:             deliveryID,
		ObservedAt:             pgTime(time.Now().UTC()),
	})
	if err != nil {
		return err
	}
	if err := s.queries.MarkProviderDemandTerminal(ctx, store.MarkProviderDemandTerminalParams{
		ProviderJobID: job.ID,
		UpdatedAt:     pgTime(time.Now().UTC()),
	}); err != nil {
		return err
	}
	now := time.Now().UTC()
	evidenceMeta := webhookMetadata{
		EventName:             "workflow_job",
		DeliveryID:            deliveryID,
		OrgID:                 assignment.OrgID,
		InstallationBindingID: uuidFromPG(assignment.InstallationBindingID),
		RepositoryBindingID:   uuidFromPG(assignment.RepositoryBindingID),
		InstallationID:        assignment.ProviderInstallationID,
		RepositoryID:          assignment.ProviderRepositoryID,
		RepositoryFullName:    assignment.RepositoryFullName,
		RunID:                 job.RunID,
		RunAttempt:            job.RunAttempt,
		JobID:                 job.ID,
		RunnerID:              assignment.RunnerID,
		RunnerName:            assignment.RunnerName,
		RunnerClass:           assignment.RunnerClass,
		JobShapeID:            assignment.JobShapeID,
		TrustClass:            trustClass,
		ExecutionID:           uuidFromPG(assignment.SandboxExecutionID),
		AttemptID:             uuidFromPG(assignment.SandboxAttemptID),
	}
	s.writeEvent(ctx, githubEventFromMetadata(evidenceMeta, "github.terminal_evidence.emitted", job.Conclusion, "", now, now))
	if strings.TrimSpace(job.Conclusion) != "success" {
		return nil
	}
	var state, reason string
	if !assignment.SandboxExecutionID.Valid || !assignment.SandboxAttemptID.Valid || assignment.ProviderRepositoryID == 0 || assignment.ProviderRunAttempt == 0 {
		state = "blocked"
		reason = "sandbox_identity_missing"
	} else {
		sandboxState, sandboxReason := s.requestSandboxGoldenSnapshotBarrier(ctx, job, assignment)
		state = sandboxState
		reason = sandboxReason
	}
	if err := s.queries.UpsertGoldenSnapshotBarrier(ctx, store.UpsertGoldenSnapshotBarrierParams{
		BarrierID:             pgUUID(uuid.New()),
		TerminalEvidenceID:    terminalEvidenceID,
		ProviderJobID:         job.ID,
		OrgID:                 assignment.OrgID,
		InstallationBindingID: assignment.InstallationBindingID,
		RepositoryBindingID:   assignment.RepositoryBindingID,
		ProviderRunID:         job.RunID,
		ProviderRunAttempt:    job.RunAttempt,
		SandboxExecutionID:    assignment.SandboxExecutionID,
		SandboxAttemptID:      assignment.SandboxAttemptID,
		JobShapeID:            assignment.JobShapeID,
		TrustClass:            trustClass,
		State:                 state,
		FailureReason:         reason,
		RequestedAt:           pgTime(time.Now().UTC()),
	}); err != nil {
		return err
	}
	now = time.Now().UTC()
	s.writeEvent(ctx, githubEventFromMetadata(evidenceMeta, "github.golden_snapshot_barrier.requested", state, reason, now, now))
	return nil
}

func (s *Service) requestSandboxGoldenSnapshotBarrier(ctx context.Context, job githubWorkflowJob, assignment store.GetJobAssignmentContextRow) (string, string) {
	executionID := uuidFromPG(assignment.SandboxExecutionID)
	attemptID := uuidFromPG(assignment.SandboxAttemptID)
	resp, err := s.cfg.Sandbox.InternalRequestGoldenSnapshotBarrier(ctx, sandboxrentalclient.InternalRequestGoldenSnapshotBarrierRequest{
		Body: sandboxrentalclient.InternalRequestGoldenSnapshotBarrierInputBody{
			Evidence: sandboxrentalclient.GoldenSnapshotBarrierEvidence{
				Provider:               "github",
				ProviderInstallationID: decimalPtr(assignment.ProviderInstallationID),
				ProviderRepositoryID:   sandboxrentalclient.ProviderRepositoryId(fmt.Sprintf("%d", assignment.ProviderRepositoryID)),
				ProviderRunID:          sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.RunID)),
				ProviderRunAttempt:     sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.RunAttempt)),
				ProviderJobID:          sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.ID)),
				RepositoryFullName:     stringPtr[sandboxrentalclient.RepositoryFullName](assignment.RepositoryFullName),
				HeadSHA:                stringPtr[sandboxrentalclient.HeadSha](job.HeadSHA),
				Conclusion:             stringPtr[string](job.Conclusion),
				RunnerID:               decimalPtr(assignment.RunnerID),
				RunnerName:             stringPtr[sandboxrentalclient.RunnerName](assignment.RunnerName),
				ExecutionID:            sandboxrentalclient.ExecutionId(executionID.String()),
				AttemptID:              sandboxrentalclient.AttemptId(attemptID.String()),
				JobShapeID:             stringPtr[sandboxrentalclient.JobShapeId](assignment.JobShapeID),
				TrustClass:             stringPtr[sandboxrentalclient.TrustClass](assignment.TrustClass),
				PromotionPolicy:        stringPtr[sandboxrentalclient.PromotionPolicy]("protected_branch_success"),
			},
		},
	})
	if err != nil {
		return "blocked", "sandbox_barrier_request_failed"
	}
	if resp.Result == nil || resp.StatusCode != http.StatusAccepted {
		return "blocked", sandboxProblem(resp)
	}
	reason := ""
	if resp.Result.Reason != nil {
		reason = *resp.Result.Reason
	}
	return firstNonEmpty(resp.Result.State, "requested"), reason
}

func sandboxProblem(resp any) string {
	switch value := resp.(type) {
	case *sandboxrentalclient.InternalSubmitRunnerJobResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	case *sandboxrentalclient.InternalGetRunnerAllocationResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	case *sandboxrentalclient.InternalObserveRunnerJobResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	case *sandboxrentalclient.InternalObserveRunnerWorkflowRunResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	case *sandboxrentalclient.InternalRequestGoldenSnapshotBarrierResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	default:
		return "unknown sandbox response"
	}
}

func sandboxProblemDetail(status int, problem *sandboxrentalclient.ErrorModel, body []byte) string {
	if problem != nil {
		parts := make([]string, 0, 3)
		if problem.Code != nil {
			parts = append(parts, *problem.Code)
		}
		if problem.Title != nil {
			parts = append(parts, *problem.Title)
		}
		if problem.Detail != nil {
			parts = append(parts, *problem.Detail)
		}
		if len(parts) > 0 {
			return fmt.Sprintf("status %d: %s", status, strings.Join(parts, ": "))
		}
	}
	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		return fmt.Sprintf("status %d", status)
	}
	return fmt.Sprintf("status %d: %s", status, truncate(bodyText, 512))
}

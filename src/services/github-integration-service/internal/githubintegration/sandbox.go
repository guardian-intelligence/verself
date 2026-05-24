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
	demand, err := s.queries.GetProviderDemandForJob(ctx, store.GetProviderDemandForJobParams{ProviderJobID: job.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		demand = store.GetProviderDemandForJobRow{}
	} else if err != nil {
		return err
	}
	trustClass := demand.TrustClass
	terminalEvidenceID, err := s.queries.InsertTerminalJobEvidence(ctx, store.InsertTerminalJobEvidenceParams{
		TerminalEvidenceID:     pgUUID(evidenceID),
		ProviderJobID:          job.ID,
		OrgID:                  demand.OrgID,
		InstallationBindingID:  demand.InstallationBindingID,
		RepositoryBindingID:    demand.RepositoryBindingID,
		ProviderInstallationID: demand.ProviderInstallationID,
		ProviderRepositoryID:   demand.ProviderRepositoryID,
		ProviderRunID:          job.RunID,
		ProviderRunAttempt:     job.RunAttempt,
		SandboxAllocationID:    demand.SandboxAllocationID,
		SandboxExecutionID:     demand.SandboxExecutionID,
		SandboxAttemptID:       demand.SandboxAttemptID,
		RunnerID:               demand.RunnerID,
		RunnerName:             demand.RunnerName,
		JobShapeID:             demand.JobShapeID,
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
	now := time.Now().UTC()
	evidenceMeta := webhookMetadata{
		EventName:             "workflow_job",
		DeliveryID:            deliveryID,
		OrgID:                 demand.OrgID,
		InstallationBindingID: uuidFromPG(demand.InstallationBindingID),
		RepositoryBindingID:   uuidFromPG(demand.RepositoryBindingID),
		InstallationID:        demand.ProviderInstallationID,
		RepositoryID:          demand.ProviderRepositoryID,
		RepositoryFullName:    demand.RepositoryFullName,
		RunID:                 job.RunID,
		RunAttempt:            job.RunAttempt,
		JobID:                 job.ID,
		RunnerID:              demand.RunnerID,
		RunnerName:            demand.RunnerName,
		RunnerClass:           demand.RunnerClass,
		JobShapeID:            demand.JobShapeID,
		TrustClass:            trustClass,
		ExecutionID:           uuidFromPG(demand.SandboxExecutionID),
		AttemptID:             uuidFromPG(demand.SandboxAttemptID),
	}
	s.writeEvent(ctx, githubEventFromMetadata(evidenceMeta, "github.terminal_evidence.emitted", job.Conclusion, "", now, now))
	if strings.TrimSpace(job.Conclusion) != "success" {
		return nil
	}
	var state, reason string
	if !demand.SandboxExecutionID.Valid || !demand.SandboxAttemptID.Valid || demand.ProviderRepositoryID == 0 || demand.ProviderRunAttempt == 0 {
		state = "blocked"
		reason = "sandbox_identity_missing"
	} else {
		sandboxState, sandboxReason := s.requestSandboxGoldenSnapshotBarrier(ctx, job, demand)
		state = sandboxState
		reason = sandboxReason
	}
	if err := s.queries.UpsertGoldenSnapshotBarrier(ctx, store.UpsertGoldenSnapshotBarrierParams{
		BarrierID:             pgUUID(uuid.New()),
		TerminalEvidenceID:    terminalEvidenceID,
		ProviderJobID:         job.ID,
		OrgID:                 demand.OrgID,
		InstallationBindingID: demand.InstallationBindingID,
		RepositoryBindingID:   demand.RepositoryBindingID,
		ProviderRunID:         job.RunID,
		ProviderRunAttempt:    job.RunAttempt,
		SandboxExecutionID:    demand.SandboxExecutionID,
		SandboxAttemptID:      demand.SandboxAttemptID,
		JobShapeID:            demand.JobShapeID,
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

func (s *Service) requestSandboxGoldenSnapshotBarrier(ctx context.Context, job githubWorkflowJob, demand store.GetProviderDemandForJobRow) (string, string) {
	executionID := uuidFromPG(demand.SandboxExecutionID)
	attemptID := uuidFromPG(demand.SandboxAttemptID)
	resp, err := s.cfg.Sandbox.InternalRequestGoldenSnapshotBarrier(ctx, sandboxrentalclient.InternalRequestGoldenSnapshotBarrierRequest{
		Body: sandboxrentalclient.InternalRequestGoldenSnapshotBarrierInputBody{
			Evidence: sandboxrentalclient.GoldenSnapshotBarrierEvidence{
				Provider:               "github",
				ProviderInstallationID: decimalPtr(demand.ProviderInstallationID),
				ProviderRepositoryID:   sandboxrentalclient.ProviderRepositoryId(fmt.Sprintf("%d", demand.ProviderRepositoryID)),
				ProviderRunID:          sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.RunID)),
				ProviderRunAttempt:     sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.RunAttempt)),
				ProviderJobID:          sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.ID)),
				RepositoryFullName:     stringPtr[sandboxrentalclient.RepositoryFullName](demand.RepositoryFullName),
				HeadSHA:                stringPtr[sandboxrentalclient.HeadSha](job.HeadSHA),
				Conclusion:             stringPtr[string](job.Conclusion),
				RunnerID:               decimalPtr(demand.RunnerID),
				RunnerName:             stringPtr[sandboxrentalclient.RunnerName](demand.RunnerName),
				ExecutionID:            sandboxrentalclient.ExecutionId(executionID.String()),
				AttemptID:              sandboxrentalclient.AttemptId(attemptID.String()),
				JobShapeID:             stringPtr[sandboxrentalclient.JobShapeId](demand.JobShapeID),
				TrustClass:             stringPtr[sandboxrentalclient.TrustClass](demand.TrustClass),
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

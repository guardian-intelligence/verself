package githubintegration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/verself/github-integration-service/internal/store"
	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
)

func (s *Service) insertTerminalEvidence(ctx context.Context, job githubWorkflowJob, deliveryID string) error {
	return s.queries.InsertTerminalJobEvidence(ctx, store.InsertTerminalJobEvidenceParams{
		TerminalEvidenceID: pgUUID(uuid.New()),
		ProviderJobID:      job.ID,
		ProviderRunID:      job.RunID,
		ProviderRunAttempt: job.RunAttempt,
		Status:             job.Status,
		Conclusion:         job.Conclusion,
		Source:             "github-api",
		DeliveryID:         deliveryID,
		ObservedAt:         pgTime(time.Now().UTC()),
	})
}

func sandboxProblem(resp any) string {
	switch value := resp.(type) {
	case *sandboxrentalclient.InternalSubmitRunnerJobResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	case *sandboxrentalclient.InternalObserveRunnerJobResponse:
		return sandboxProblemDetail(value.StatusCode, value.Problem, value.Body)
	case *sandboxrentalclient.InternalObserveRunnerWorkflowRunResponse:
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

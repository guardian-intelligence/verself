package githubintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/verself/github-integration-service/internal/store"
	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
)

type webhookMetadata struct {
	EventName          string
	DeliveryID         string
	PayloadSHA256      string
	Action             string
	InstallationID     int64
	RepositoryID       int64
	RepositoryFullName string
	RunID              int64
	RunAttempt         int64
	JobID              int64
	RunnerID           int64
	RunnerName         string
	RunnerClass        string
	JobShapeID         string
	TrustClass         string
	AllocationID       uuidLike
	ExecutionID        uuidLike
	AttemptID          uuidLike
}

type uuidLike interface {
	String() string
}

type workflowJobWebhook struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	WorkflowJob workflowJobPayload `json:"workflow_job"`
}

type workflowJobPayload struct {
	ID           int64     `json:"id"`
	RunID        int64     `json:"run_id"`
	RunAttempt   int64     `json:"run_attempt"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	Labels       []string  `json:"labels"`
	RunnerID     int64     `json:"runner_id"`
	RunnerName   string    `json:"runner_name"`
	HeadSHA      string    `json:"head_sha"`
	HeadBranch   string    `json:"head_branch"`
	WorkflowName string    `json:"workflow_name"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

type workflowObservation struct {
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderRunID          int64
	ProviderRunAttempt     int64
	RepositoryFullName     string
	EventName              string
	HeadSHA                string
	HeadBranch             string
	HeadRepositoryFullName string
	BaseSHA                string
	BaseBranch             string
	WorkflowPath           string
	PullRequestNumber      int64
	CommitCount            int64
}

func parseWebhookMetadata(payload []byte) (webhookMetadata, error) {
	var envelope struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		WorkflowJob struct {
			ID         int64 `json:"id"`
			RunID      int64 `json:"run_id"`
			RunAttempt int64 `json:"run_attempt"`
		} `json:"workflow_job"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return webhookMetadata{}, err
	}
	return webhookMetadata{
		Action:             strings.TrimSpace(envelope.Action),
		InstallationID:     envelope.Installation.ID,
		RepositoryID:       envelope.Repository.ID,
		RepositoryFullName: strings.TrimSpace(envelope.Repository.FullName),
		RunID:              envelope.WorkflowJob.RunID,
		RunAttempt:         envelope.WorkflowJob.RunAttempt,
		JobID:              envelope.WorkflowJob.ID,
	}, nil
}

func metadataFromWorkflowJob(deliveryID string, event workflowJobWebhook) webhookMetadata {
	return webhookMetadata{
		EventName:          "workflow_job",
		DeliveryID:         deliveryID,
		Action:             event.Action,
		InstallationID:     event.Installation.ID,
		RepositoryID:       event.Repository.ID,
		RepositoryFullName: event.Repository.FullName,
		RunID:              event.WorkflowJob.RunID,
		RunAttempt:         event.WorkflowJob.RunAttempt,
		JobID:              event.WorkflowJob.ID,
		RunnerID:           event.WorkflowJob.RunnerID,
		RunnerName:         event.WorkflowJob.RunnerName,
	}
}

func metadataFromAPIJob(deliveryID string, installationID, repositoryID int64, repositoryFullName string, job githubWorkflowJob) webhookMetadata {
	return webhookMetadata{
		EventName:          "workflow_job",
		DeliveryID:         deliveryID,
		InstallationID:     installationID,
		RepositoryID:       repositoryID,
		RepositoryFullName: repositoryFullName,
		RunID:              job.RunID,
		RunAttempt:         job.RunAttempt,
		JobID:              job.ID,
		RunnerID:           job.RunnerID,
		RunnerName:         job.RunnerName,
	}
}

func (s *Service) persistWorkflowJob(ctx context.Context, event workflowJobWebhook, deliveryID string, observedFromAPI bool, now time.Time) error {
	if err := s.persistInstallationRepository(ctx, event, deliveryID, now); err != nil {
		return err
	}
	labels, err := json.Marshal(event.WorkflowJob.Labels)
	if err != nil {
		return err
	}
	status := firstNonEmpty(event.WorkflowJob.Status, event.Action)
	return s.queries.UpsertWorkflowJob(ctx, store.UpsertWorkflowJobParams{
		ProviderJobID:          event.WorkflowJob.ID,
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RepositoryFullName:     event.Repository.FullName,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		JobName:                event.WorkflowJob.Name,
		HeadSha:                event.WorkflowJob.HeadSHA,
		HeadBranch:             event.WorkflowJob.HeadBranch,
		WorkflowName:           event.WorkflowJob.WorkflowName,
		Status:                 status,
		Conclusion:             event.WorkflowJob.Conclusion,
		LabelsJson:             labels,
		RunnerID:               event.WorkflowJob.RunnerID,
		RunnerName:             event.WorkflowJob.RunnerName,
		StartedAt:              pgTime(event.WorkflowJob.StartedAt),
		CompletedAt:            pgTime(event.WorkflowJob.CompletedAt),
		LastDeliveryID:         deliveryID,
		ObservedFromApiAt:      optionalObservedTime(observedFromAPI, now),
		UpdatedAt:              pgTime(now),
	})
}

func (s *Service) persistWorkflowJobFromAPI(ctx context.Context, installationID, repositoryID int64, repositoryFullName string, job githubWorkflowJob, deliveryID string, now time.Time) error {
	labels, err := json.Marshal(job.Labels)
	if err != nil {
		return err
	}
	return s.queries.UpsertWorkflowJob(ctx, store.UpsertWorkflowJobParams{
		ProviderJobID:          job.ID,
		ProviderInstallationID: installationID,
		ProviderRepositoryID:   repositoryID,
		RepositoryFullName:     repositoryFullName,
		ProviderRunID:          job.RunID,
		ProviderRunAttempt:     job.RunAttempt,
		JobName:                job.Name,
		HeadSha:                job.HeadSHA,
		HeadBranch:             job.HeadBranch,
		WorkflowName:           job.WorkflowName,
		Status:                 job.Status,
		Conclusion:             job.Conclusion,
		LabelsJson:             labels,
		RunnerID:               job.RunnerID,
		RunnerName:             job.RunnerName,
		StartedAt:              pgTime(job.StartedAt),
		CompletedAt:            pgTime(job.CompletedAt),
		LastDeliveryID:         deliveryID,
		ObservedFromApiAt:      pgTime(now),
		UpdatedAt:              pgTime(now),
	})
}

func (s *Service) persistInstallationRepository(ctx context.Context, event workflowJobWebhook, deliveryID string, now time.Time) error {
	if event.Installation.ID > 0 {
		if err := s.queries.UpsertInstallation(ctx, store.UpsertInstallationParams{
			ProviderInstallationID: event.Installation.ID,
			State:                  "active",
			LastEventDeliveryID:    deliveryID,
			UpdatedAt:              pgTime(now),
		}); err != nil {
			return err
		}
	}
	if event.Repository.ID <= 0 || strings.TrimSpace(event.Repository.FullName) == "" {
		return nil
	}
	owner, repo, _ := strings.Cut(strings.TrimSpace(event.Repository.FullName), "/")
	return s.queries.UpsertRepository(ctx, store.UpsertRepositoryParams{
		ProviderRepositoryID:   event.Repository.ID,
		ProviderInstallationID: event.Installation.ID,
		OwnerLogin:             owner,
		RepositoryName:         repo,
		RepositoryFullName:     strings.TrimSpace(event.Repository.FullName),
		State:                  "active",
		LastEventDeliveryID:    deliveryID,
		UpdatedAt:              pgTime(now),
	})
}

func (s *Service) persistWorkflowRun(ctx context.Context, workflow workflowObservation, deliveryID string, observedFromAPI bool, now time.Time) error {
	return s.queries.UpsertWorkflowRun(ctx, store.UpsertWorkflowRunParams{
		ProviderInstallationID: workflow.ProviderInstallationID,
		ProviderRepositoryID:   workflow.ProviderRepositoryID,
		ProviderRunID:          workflow.ProviderRunID,
		ProviderRunAttempt:     workflow.ProviderRunAttempt,
		RepositoryFullName:     workflow.RepositoryFullName,
		EventName:              workflow.EventName,
		HeadSha:                workflow.HeadSHA,
		HeadBranch:             workflow.HeadBranch,
		HeadRepositoryFullName: workflow.HeadRepositoryFullName,
		BaseSha:                workflow.BaseSHA,
		BaseBranch:             workflow.BaseBranch,
		WorkflowPath:           workflow.WorkflowPath,
		PullRequestNumber:      workflow.PullRequestNumber,
		CommitCount:            workflow.CommitCount,
		LastDeliveryID:         deliveryID,
		ObservedFromApiAt:      optionalObservedTime(observedFromAPI, now),
		UpdatedAt:              pgTime(now),
	})
}

func sandboxObservationFromWebhook(event workflowJobWebhook, deliveryID string) sandboxrentalclient.RunnerJobObservation {
	runID := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", event.WorkflowJob.RunID))
	runAttempt := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", event.WorkflowJob.RunAttempt))
	installationID := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", event.Installation.ID))
	return sandboxrentalclient.RunnerJobObservation{
		Provider:               providerGitHub,
		ProviderJobID:          sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", event.WorkflowJob.ID)),
		ProviderRepositoryID:   sandboxrentalclient.ProviderRepositoryId(fmt.Sprintf("%d", event.Repository.ID)),
		ProviderInstallationID: &installationID,
		RepositoryFullName:     stringPtr[sandboxrentalclient.RepositoryFullName](event.Repository.FullName),
		ProviderRunID:          &runID,
		ProviderRunAttempt:     &runAttempt,
		JobName:                stringPtr[sandboxrentalclient.JobName](event.WorkflowJob.Name),
		HeadSHA:                stringPtr[sandboxrentalclient.HeadSha](event.WorkflowJob.HeadSHA),
		HeadBranch:             stringPtr[sandboxrentalclient.HeadBranch](event.WorkflowJob.HeadBranch),
		WorkflowName:           stringPtr[sandboxrentalclient.WorkflowName](event.WorkflowJob.WorkflowName),
		Status:                 firstNonEmpty(event.WorkflowJob.Status, event.Action),
		Conclusion:             stringPtr[string](event.WorkflowJob.Conclusion),
		Labels:                 sandboxrentalclient.RunnerLabels(event.WorkflowJob.Labels),
		RunnerID:               decimalPtr(event.WorkflowJob.RunnerID),
		RunnerName:             stringPtr[sandboxrentalclient.RunnerName](event.WorkflowJob.RunnerName),
		StartedAt:              timePtr(event.WorkflowJob.StartedAt),
		CompletedAt:            timePtr(event.WorkflowJob.CompletedAt),
		DeliveryID:             stringPtr[sandboxrentalclient.CorrelationId](deliveryID),
	}
}

func sandboxObservationFromAPI(installationID, repositoryID int64, repositoryFullName string, job githubWorkflowJob, deliveryID string) sandboxrentalclient.RunnerJobObservation {
	runID := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.RunID))
	runAttempt := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.RunAttempt))
	installation := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", installationID))
	return sandboxrentalclient.RunnerJobObservation{
		Provider:               providerGitHub,
		ProviderJobID:          sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", job.ID)),
		ProviderRepositoryID:   sandboxrentalclient.ProviderRepositoryId(fmt.Sprintf("%d", repositoryID)),
		ProviderInstallationID: &installation,
		RepositoryFullName:     stringPtr[sandboxrentalclient.RepositoryFullName](repositoryFullName),
		ProviderRunID:          &runID,
		ProviderRunAttempt:     &runAttempt,
		JobName:                stringPtr[sandboxrentalclient.JobName](job.Name),
		HeadSHA:                stringPtr[sandboxrentalclient.HeadSha](job.HeadSHA),
		HeadBranch:             stringPtr[sandboxrentalclient.HeadBranch](job.HeadBranch),
		WorkflowName:           stringPtr[sandboxrentalclient.WorkflowName](job.WorkflowName),
		Status:                 job.Status,
		Conclusion:             stringPtr[string](job.Conclusion),
		Labels:                 sandboxrentalclient.RunnerLabels(job.Labels),
		RunnerID:               decimalPtr(job.RunnerID),
		RunnerName:             stringPtr[sandboxrentalclient.RunnerName](job.RunnerName),
		StartedAt:              timePtr(job.StartedAt),
		CompletedAt:            timePtr(job.CompletedAt),
		DeliveryID:             stringPtr[sandboxrentalclient.CorrelationId](deliveryID),
	}
}

func sandboxWorkflowObservation(workflow workflowObservation) sandboxrentalclient.RunnerWorkflowRunObservation {
	runID := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", workflow.ProviderRunID))
	runAttempt := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", workflow.ProviderRunAttempt))
	installation := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", workflow.ProviderInstallationID))
	var prNumber *sandboxrentalclient.DecimalUint64
	if workflow.PullRequestNumber > 0 {
		value := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", workflow.PullRequestNumber))
		prNumber = &value
	}
	var commitCount *sandboxrentalclient.DecimalUint64
	if workflow.CommitCount > 0 {
		value := sandboxrentalclient.DecimalUint64(fmt.Sprintf("%d", workflow.CommitCount))
		commitCount = &value
	}
	return sandboxrentalclient.RunnerWorkflowRunObservation{
		Provider:               providerGitHub,
		ProviderRepositoryID:   sandboxrentalclient.ProviderRepositoryId(fmt.Sprintf("%d", workflow.ProviderRepositoryID)),
		ProviderInstallationID: &installation,
		ProviderRunID:          &runID,
		ProviderRunAttempt:     &runAttempt,
		RepositoryFullName:     stringPtr[sandboxrentalclient.RepositoryFullName](workflow.RepositoryFullName),
		EventName:              stringPtr[string](workflow.EventName),
		HeadSHA:                stringPtr[sandboxrentalclient.HeadSha](workflow.HeadSHA),
		HeadBranch:             stringPtr[sandboxrentalclient.HeadBranch](workflow.HeadBranch),
		HeadRepositoryFullName: stringPtr[sandboxrentalclient.RepositoryFullName](workflow.HeadRepositoryFullName),
		BaseSHA:                stringPtr[sandboxrentalclient.HeadSha](workflow.BaseSHA),
		BaseBranch:             stringPtr[sandboxrentalclient.HeadBranch](workflow.BaseBranch),
		WorkflowPath:           stringPtr[sandboxrentalclient.WorkflowPath](workflow.WorkflowPath),
		PullRequestNumber:      prNumber,
		CommitCount:            commitCount,
	}
}

func optionalObservedTime(ok bool, t time.Time) pgtype.Timestamptz {
	if !ok {
		return pgtype.Timestamptz{}
	}
	return pgTime(t)
}

func timePtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	out := t.UTC().Format(time.RFC3339Nano)
	return &out
}

package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

type RepoRecord struct {
	RepoID                   uuid.UUID       `json:"repo_id"`
	OrgID                    apiwire.OrgID   `json:"org_id"`
	Provider                 string          `json:"provider"`
	ProviderRepoID           string          `json:"provider_repo_id"`
	Owner                    string          `json:"owner"`
	Name                     string          `json:"name"`
	FullName                 string          `json:"full_name"`
	CloneURL                 string          `json:"clone_url"`
	DefaultBranch            string          `json:"default_branch"`
	RunnerProfileSlug        string          `json:"runner_profile_slug"`
	State                    string          `json:"state"`
	CompatibilityStatus      string          `json:"compatibility_status"`
	CompatibilitySummary     json.RawMessage `json:"compatibility_summary,omitempty"`
	LastScannedSHA           string          `json:"last_scanned_sha,omitempty"`
	ActiveGoldenGenerationID *uuid.UUID      `json:"active_golden_generation_id,omitempty"`
	LastReadySHA             string          `json:"last_ready_sha,omitempty"`
	LastError                string          `json:"last_error,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
	ArchivedAt               *time.Time      `json:"archived_at,omitempty"`
}

type RepoBootstrapRecord struct {
	Repo          *RepoRecord                  `json:"repo"`
	Generation    *jobs.GoldenGenerationRecord `json:"generation"`
	ExecutionID   uuid.UUID                    `json:"execution_id"`
	AttemptID     uuid.UUID                    `json:"attempt_id"`
	TriggerReason string                       `json:"trigger_reason"`
}

type ExecutionRecord struct {
	ExecutionID        uuid.UUID       `json:"execution_id"`
	OrgID              apiwire.OrgID   `json:"org_id"`
	ActorID            string          `json:"actor_id"`
	Kind               string          `json:"kind"`
	Provider           string          `json:"provider,omitempty"`
	ProductID          string          `json:"product_id"`
	Status             string          `json:"status"`
	CorrelationID      string          `json:"correlation_id,omitempty"`
	IdempotencyKey     string          `json:"idempotency_key,omitempty"`
	RepoID             string          `json:"repo_id,omitempty"`
	GoldenGenerationID string          `json:"golden_generation_id,omitempty"`
	Repo               string          `json:"repo,omitempty"`
	RepoURL            string          `json:"repo_url,omitempty"`
	Ref                string          `json:"ref,omitempty"`
	DefaultBranch      string          `json:"default_branch,omitempty"`
	RunCommand         string          `json:"run_command,omitempty"`
	CommitSHA          string          `json:"commit_sha,omitempty"`
	WorkflowPath       string          `json:"workflow_path,omitempty"`
	WorkflowJobName    string          `json:"workflow_job_name,omitempty"`
	ProviderRunID      string          `json:"provider_run_id,omitempty"`
	ProviderJobID      string          `json:"provider_job_id,omitempty"`
	LatestAttempt      AttemptRecord   `json:"latest_attempt"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	BillingWindows     []BillingWindow `json:"billing_windows,omitempty"`
}

type AttemptRecord struct {
	AttemptID         uuid.UUID  `json:"attempt_id"`
	AttemptSeq        int        `json:"attempt_seq" minimum:"0" maximum:"9007199254740991"`
	State             string     `json:"state"`
	OrchestratorJobID string     `json:"orchestrator_job_id,omitempty"`
	BillingJobID      int64      `json:"billing_job_id,omitempty" minimum:"0" maximum:"9007199254740991"`
	RunnerName        string     `json:"runner_name,omitempty"`
	GoldenSnapshot    string     `json:"golden_snapshot,omitempty"`
	FailureReason     string     `json:"failure_reason,omitempty"`
	ExitCode          int        `json:"exit_code,omitempty" minimum:"0" maximum:"255"`
	DurationMs        int64      `json:"duration_ms,omitempty" minimum:"0" maximum:"9007199254740991"`
	ZFSWritten        int64      `json:"zfs_written,omitempty" minimum:"0" maximum:"9007199254740991"`
	StdoutBytes       int64      `json:"stdout_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	StderrBytes       int64      `json:"stderr_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	TraceID           string     `json:"trace_id,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type BillingWindow struct {
	AttemptID        uuid.UUID  `json:"attempt_id"`
	BillingWindowID  string     `json:"billing_window_id"`
	WindowSeq        int        `json:"window_seq" minimum:"0" maximum:"9007199254740991"`
	ReservationShape string     `json:"reservation_shape"`
	ReservedQuantity int        `json:"reserved_quantity" minimum:"0" maximum:"9007199254740991"`
	ActualQuantity   int        `json:"actual_quantity,omitempty" minimum:"0" maximum:"9007199254740991"`
	PricingPhase     string     `json:"pricing_phase,omitempty"`
	State            string     `json:"state"`
	WindowStart      time.Time  `json:"window_start"`
	CreatedAt        time.Time  `json:"created_at"`
	SettledAt        *time.Time `json:"settled_at,omitempty"`
}

func repoRecord(record jobs.RepoRecord) RepoRecord {
	return RepoRecord{
		RepoID:                   record.RepoID,
		OrgID:                    apiwire.Uint64(record.OrgID),
		Provider:                 record.Provider,
		ProviderRepoID:           record.ProviderRepoID,
		Owner:                    record.Owner,
		Name:                     record.Name,
		FullName:                 record.FullName,
		CloneURL:                 record.CloneURL,
		DefaultBranch:            record.DefaultBranch,
		RunnerProfileSlug:        record.RunnerProfileSlug,
		State:                    record.State,
		CompatibilityStatus:      record.CompatibilityStatus,
		CompatibilitySummary:     append(json.RawMessage(nil), record.CompatibilitySummary...),
		LastScannedSHA:           record.LastScannedSHA,
		ActiveGoldenGenerationID: record.ActiveGoldenGenerationID,
		LastReadySHA:             record.LastReadySHA,
		LastError:                record.LastError,
		CreatedAt:                record.CreatedAt,
		UpdatedAt:                record.UpdatedAt,
		ArchivedAt:               record.ArchivedAt,
	}
}

func repoRecordPointer(record *jobs.RepoRecord) *RepoRecord {
	if record == nil {
		return nil
	}
	dto := repoRecord(*record)
	return &dto
}

func repoRecords(records []jobs.RepoRecord) []RepoRecord {
	out := make([]RepoRecord, 0, len(records))
	for _, record := range records {
		out = append(out, repoRecord(record))
	}
	return out
}

func repoBootstrapRecord(record jobs.RepoBootstrapRecord) RepoBootstrapRecord {
	return RepoBootstrapRecord{
		Repo:          repoRecordPointer(record.Repo),
		Generation:    record.Generation,
		ExecutionID:   record.ExecutionID,
		AttemptID:     record.AttemptID,
		TriggerReason: record.TriggerReason,
	}
}

func executionRecord(record jobs.ExecutionRecord) ExecutionRecord {
	return ExecutionRecord{
		ExecutionID:        record.ExecutionID,
		OrgID:              apiwire.Uint64(record.OrgID),
		ActorID:            record.ActorID,
		Kind:               record.Kind,
		Provider:           record.Provider,
		ProductID:          record.ProductID,
		Status:             record.Status,
		CorrelationID:      record.CorrelationID,
		IdempotencyKey:     record.IdempotencyKey,
		RepoID:             record.RepoID,
		GoldenGenerationID: record.GoldenGenerationID,
		Repo:               record.Repo,
		RepoURL:            record.RepoURL,
		Ref:                record.Ref,
		DefaultBranch:      record.DefaultBranch,
		RunCommand:         record.RunCommand,
		CommitSHA:          record.CommitSHA,
		WorkflowPath:       record.WorkflowPath,
		WorkflowJobName:    record.WorkflowJobName,
		ProviderRunID:      record.ProviderRunID,
		ProviderJobID:      record.ProviderJobID,
		LatestAttempt:      attemptRecord(record.LatestAttempt),
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
		BillingWindows:     billingWindows(record.BillingWindows),
	}
}

func attemptRecord(record jobs.AttemptRecord) AttemptRecord {
	return AttemptRecord{
		AttemptID:         record.AttemptID,
		AttemptSeq:        record.AttemptSeq,
		State:             record.State,
		OrchestratorJobID: record.OrchestratorJobID,
		BillingJobID:      record.BillingJobID,
		RunnerName:        record.RunnerName,
		GoldenSnapshot:    record.GoldenSnapshot,
		FailureReason:     record.FailureReason,
		ExitCode:          record.ExitCode,
		DurationMs:        record.DurationMs,
		ZFSWritten:        record.ZFSWritten,
		StdoutBytes:       record.StdoutBytes,
		StderrBytes:       record.StderrBytes,
		TraceID:           record.TraceID,
		StartedAt:         record.StartedAt,
		CompletedAt:       record.CompletedAt,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}
}

func billingWindow(record jobs.BillingWindow) BillingWindow {
	return BillingWindow{
		AttemptID:        record.AttemptID,
		BillingWindowID:  record.BillingWindowID,
		WindowSeq:        record.WindowSeq,
		ReservationShape: record.ReservationShape,
		ReservedQuantity: record.ReservedQuantity,
		ActualQuantity:   record.ActualQuantity,
		PricingPhase:     record.PricingPhase,
		State:            record.State,
		WindowStart:      record.WindowStart,
		CreatedAt:        record.CreatedAt,
		SettledAt:        record.SettledAt,
	}
}

func billingWindows(records []jobs.BillingWindow) []BillingWindow {
	if len(records) == 0 {
		return nil
	}
	out := make([]BillingWindow, 0, len(records))
	for _, record := range records {
		out = append(out, billingWindow(record))
	}
	return out
}

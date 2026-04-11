package api

import (
	"encoding/json"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func importRepoRequest(request apiwire.SandboxImportRepoRequest) jobs.ImportRepoRequest {
	return jobs.ImportRepoRequest{
		Provider:       request.Provider,
		ProviderHost:   request.ProviderHost,
		ProviderRepoID: request.ProviderRepoID,
		Owner:          request.Owner,
		Name:           request.Name,
		FullName:       request.FullName,
		CloneURL:       request.CloneURL,
		DefaultBranch:  request.DefaultBranch,
	}
}

func submitRequest(request apiwire.SandboxSubmitRequest) jobs.SubmitRequest {
	return jobs.SubmitRequest{
		Kind:            request.Kind,
		ProductID:       request.ProductID,
		Provider:        request.Provider,
		IdempotencyKey:  request.IdempotencyKey,
		RepoID:          request.RepoID,
		Repo:            request.Repo,
		RepoURL:         request.RepoURL,
		Ref:             request.Ref,
		DefaultBranch:   request.DefaultBranch,
		RunCommand:      request.RunCommand,
		WorkflowPath:    request.WorkflowPath,
		WorkflowJobName: request.WorkflowJobName,
		ProviderRunID:   request.ProviderRunID,
		ProviderJobID:   request.ProviderJobID,
	}
}

func repoRecord(record jobs.RepoRecord) apiwire.SandboxRepoRecord {
	return apiwire.SandboxRepoRecord{
		RepoID:               record.RepoID,
		OrgID:                apiwire.Uint64(record.OrgID),
		Provider:             record.Provider,
		ProviderHost:         record.ProviderHost,
		ProviderRepoID:       record.ProviderRepoID,
		Owner:                record.Owner,
		Name:                 record.Name,
		FullName:             record.FullName,
		CloneURL:             record.CloneURL,
		DefaultBranch:        record.DefaultBranch,
		State:                record.State,
		CompatibilityStatus:  record.CompatibilityStatus,
		CompatibilitySummary: append(json.RawMessage(nil), record.CompatibilitySummary...),
		LastScannedSHA:       record.LastScannedSHA,
		LastError:            record.LastError,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
		ArchivedAt:           record.ArchivedAt,
	}
}

func repoRecords(records []jobs.RepoRecord) []apiwire.SandboxRepoRecord {
	out := make([]apiwire.SandboxRepoRecord, 0, len(records))
	for _, record := range records {
		out = append(out, repoRecord(record))
	}
	return out
}

func executionRecord(record jobs.ExecutionRecord) apiwire.SandboxExecutionRecord {
	return apiwire.SandboxExecutionRecord{
		ExecutionID:     record.ExecutionID,
		OrgID:           apiwire.Uint64(record.OrgID),
		ActorID:         record.ActorID,
		Kind:            record.Kind,
		Provider:        record.Provider,
		ProductID:       record.ProductID,
		Status:          record.Status,
		CorrelationID:   record.CorrelationID,
		IdempotencyKey:  record.IdempotencyKey,
		RepoID:          record.RepoID,
		Repo:            record.Repo,
		RepoURL:         record.RepoURL,
		Ref:             record.Ref,
		DefaultBranch:   record.DefaultBranch,
		RunCommand:      record.RunCommand,
		CommitSHA:       record.CommitSHA,
		WorkflowPath:    record.WorkflowPath,
		WorkflowJobName: record.WorkflowJobName,
		ProviderRunID:   record.ProviderRunID,
		ProviderJobID:   record.ProviderJobID,
		LatestAttempt:   attemptRecord(record.LatestAttempt),
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
		BillingWindows:  billingWindows(record.BillingWindows),
	}
}

func attemptRecord(record jobs.AttemptRecord) apiwire.SandboxAttemptRecord {
	return apiwire.SandboxAttemptRecord{
		AttemptID:         record.AttemptID,
		AttemptSeq:        record.AttemptSeq,
		State:             record.State,
		OrchestratorJobID: record.OrchestratorJobID,
		BillingJobID:      record.BillingJobID,
		RunnerName:        record.RunnerName,
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

func billingWindow(record jobs.BillingWindow) apiwire.SandboxBillingWindow {
	return apiwire.SandboxBillingWindow{
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

func billingWindows(records []jobs.BillingWindow) []apiwire.SandboxBillingWindow {
	if len(records) == 0 {
		return nil
	}
	out := make([]apiwire.SandboxBillingWindow, 0, len(records))
	for _, record := range records {
		out = append(out, billingWindow(record))
	}
	return out
}

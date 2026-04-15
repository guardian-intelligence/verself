package api

import (
	"strconv"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func submitRequest(request apiwire.SandboxSubmitRequest) jobs.SubmitRequest {
	return jobs.SubmitRequest{
		Kind:           request.Kind,
		RunnerClass:    request.RunnerClass,
		ProductID:      request.ProductID,
		Provider:       request.Provider,
		IdempotencyKey: request.IdempotencyKey,
		RunCommand:     request.RunCommand,
		MaxWallSeconds: request.MaxWallSeconds,
	}
}

func githubInstallationRecord(record jobs.GitHubInstallationRecord) apiwire.SandboxGitHubInstallationRecord {
	return apiwire.SandboxGitHubInstallationRecord{
		InstallationID: strconv.FormatInt(record.InstallationID, 10),
		OrgID:          apiwire.Uint64(record.OrgID),
		AccountLogin:   record.AccountLogin,
		AccountType:    record.AccountType,
		Active:         record.Active,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}

func githubInstallationConnect(connect jobs.GitHubInstallationConnect) apiwire.SandboxGitHubInstallationConnectResponse {
	return apiwire.SandboxGitHubInstallationConnectResponse{
		State:     connect.State,
		SetupURL:  connect.SetupURL,
		ExpiresAt: connect.ExpiresAt,
	}
}

func githubInstallationRecords(records []jobs.GitHubInstallationRecord) []apiwire.SandboxGitHubInstallationRecord {
	out := make([]apiwire.SandboxGitHubInstallationRecord, 0, len(records))
	for _, record := range records {
		out = append(out, githubInstallationRecord(record))
	}
	return out
}

func executionRecord(record jobs.ExecutionRecord) apiwire.SandboxExecutionRecord {
	return apiwire.SandboxExecutionRecord{
		ExecutionID:      record.ExecutionID,
		OrgID:            apiwire.Uint64(record.OrgID),
		ActorID:          record.ActorID,
		Kind:             record.Kind,
		SourceKind:       record.SourceKind,
		WorkloadKind:     record.WorkloadKind,
		SourceRef:        record.SourceRef,
		RunnerClass:      record.RunnerClass,
		ExternalProvider: record.ExternalProvider,
		ExternalTaskID:   record.ExternalTaskID,
		Provider:         record.Provider,
		ProductID:        record.ProductID,
		Status:           record.Status,
		CorrelationID:    record.CorrelationID,
		IdempotencyKey:   record.IdempotencyKey,
		RunCommand:       record.RunCommand,
		LatestAttempt:    attemptRecord(record.LatestAttempt),
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
		BillingWindows:   billingWindows(record.BillingWindows),
	}
}

func attemptRecord(record jobs.AttemptRecord) apiwire.SandboxAttemptRecord {
	return apiwire.SandboxAttemptRecord{
		AttemptID:     record.AttemptID,
		AttemptSeq:    record.AttemptSeq,
		State:         record.State,
		LeaseID:       record.LeaseID,
		ExecID:        record.ExecID,
		BillingJobID:  record.BillingJobID,
		FailureReason: record.FailureReason,
		ExitCode:      record.ExitCode,
		DurationMs:    record.DurationMs,
		ZFSWritten:    record.ZFSWritten,
		StdoutBytes:   record.StdoutBytes,
		StderrBytes:   record.StderrBytes,
		TraceID:       record.TraceID,
		StartedAt:     record.StartedAt,
		CompletedAt:   record.CompletedAt,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
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

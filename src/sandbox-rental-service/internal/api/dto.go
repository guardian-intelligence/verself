package api

import (
	"strconv"
	"time"

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
		Resources:      request.Resources,
		SecretEnv:      secretEnvVars(request.SecretEnv),
	}
}

func secretEnvVars(vars []apiwire.SandboxSecretEnvVar) []jobs.SecretEnvVar {
	if len(vars) == 0 {
		return nil
	}
	out := make([]jobs.SecretEnvVar, 0, len(vars))
	for _, item := range vars {
		out = append(out, jobs.SecretEnvVar{
			EnvName:    item.EnvName,
			Kind:       item.Kind,
			SecretName: item.SecretName,
			ScopeLevel: item.ScopeLevel,
			SourceID:   item.SourceID,
			EnvID:      item.EnvID,
			Branch:     item.Branch,
		})
	}
	return out
}

func volumeCreateRequest(request apiwire.SandboxVolumeCreateRequest) jobs.VolumeCreateRequest {
	return jobs.VolumeCreateRequest{
		ProductID:      request.ProductID,
		IdempotencyKey: request.IdempotencyKey,
		DisplayName:    request.DisplayName,
	}
}

func volumeMeterTickRequest(request apiwire.SandboxVolumeMeterTickRequest) jobs.VolumeMeterTickRequest {
	observedAt := time.Time{}
	if request.ObservedAt != nil {
		observedAt = request.ObservedAt.UTC()
	}
	return jobs.VolumeMeterTickRequest{
		IdempotencyKey:       request.IdempotencyKey,
		WindowMillis:         request.WindowMillis,
		UsedBytes:            request.UsedBytes.Uint64(),
		UsedBySnapshotsBytes: request.UsedBySnapshotsBytes.Uint64(),
		WrittenBytes:         request.WrittenBytes.Uint64(),
		ProvisionedBytes:     request.ProvisionedBytes.Uint64(),
		ObservedAt:           observedAt,
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

func volumeRecord(record jobs.VolumeRecord) apiwire.SandboxVolumeRecord {
	return apiwire.SandboxVolumeRecord{
		VolumeID:              record.VolumeID,
		OrgID:                 apiwire.Uint64(record.OrgID),
		ActorID:               record.ActorID,
		ProductID:             record.ProductID,
		DisplayName:           record.DisplayName,
		State:                 record.State,
		StorageNodeID:         record.StorageNodeID,
		PoolID:                record.PoolID,
		DatasetRef:            record.DatasetRef,
		CurrentGenerationID:   record.CurrentGenerationID,
		UsedBytes:             apiwire.Uint64(record.UsedBytes),
		UsedBySnapshotsBytes:  apiwire.Uint64(record.UsedBySnapshotsBytes),
		BillableLiveBytes:     apiwire.Uint64(record.BillableLiveBytes),
		BillableRetainedBytes: apiwire.Uint64(record.BillableRetainedBytes),
		WrittenBytes:          apiwire.Uint64(record.WrittenBytes),
		ProvisionedBytes:      apiwire.Uint64(record.ProvisionedBytes),
		LastMeteredAt:         record.LastMeteredAt,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}
}

func volumeRecords(records []jobs.VolumeRecord) []apiwire.SandboxVolumeRecord {
	out := make([]apiwire.SandboxVolumeRecord, 0, len(records))
	for _, record := range records {
		out = append(out, volumeRecord(record))
	}
	return out
}

func volumeMeterTickRecord(record jobs.VolumeMeterTickRecord) apiwire.SandboxVolumeMeterTickRecord {
	return apiwire.SandboxVolumeMeterTickRecord{
		MeterTickID:           record.MeterTickID,
		VolumeID:              record.VolumeID,
		OrgID:                 apiwire.Uint64(record.OrgID),
		ActorID:               record.ActorID,
		ProductID:             record.ProductID,
		SourceType:            record.SourceType,
		SourceRef:             record.SourceRef,
		WindowSeq:             record.WindowSeq,
		WindowMillis:          record.WindowMillis,
		State:                 record.State,
		ObservedAt:            record.ObservedAt,
		WindowStart:           record.WindowStart,
		WindowEnd:             record.WindowEnd,
		UsedBytes:             apiwire.Uint64(record.UsedBytes),
		UsedBySnapshotsBytes:  apiwire.Uint64(record.UsedBySnapshotsBytes),
		BillableLiveBytes:     apiwire.Uint64(record.BillableLiveBytes),
		BillableRetainedBytes: apiwire.Uint64(record.BillableRetainedBytes),
		WrittenBytes:          apiwire.Uint64(record.WrittenBytes),
		ProvisionedBytes:      apiwire.Uint64(record.ProvisionedBytes),
		Allocation:            record.Allocation,
		BillingWindowID:       record.BillingWindowID,
		BilledChargeUnits:     apiwire.Uint64(record.BilledChargeUnits),
		BillingFailureReason:  record.BillingFailureReason,
		ClickHouseProjectedAt: record.ClickHouseProjectedAt,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
	}
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
	var exitCode *int
	if record.CompletedAt != nil {
		exitCodeValue := record.ExitCode
		exitCode = &exitCodeValue
	}

	return apiwire.SandboxAttemptRecord{
		AttemptID:     record.AttemptID,
		AttemptSeq:    record.AttemptSeq,
		State:         record.State,
		LeaseID:       record.LeaseID,
		ExecID:        record.ExecID,
		BillingJobID:  record.BillingJobID,
		FailureReason: record.FailureReason,
		ExitCode:      exitCode,
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

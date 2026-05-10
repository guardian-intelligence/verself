package api

import (
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/verself/domain-transfer-objects"
	"github.com/verself/sandbox-rental-service/internal/jobs"
	"github.com/verself/sandbox-rental-service/internal/recurring"
)

func githubInstallationRecord(record jobs.GitHubInstallationRecord) dto.SandboxGitHubInstallationRecord {
	return dto.SandboxGitHubInstallationRecord{
		InstallationID: strconv.FormatInt(record.InstallationID, 10),
		OrgID:          dto.Uint64(record.OrgID),
		AccountLogin:   record.AccountLogin,
		AccountType:    record.AccountType,
		Active:         record.Active,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}

func githubInstallationConnect(connect jobs.GitHubInstallationConnect) dto.SandboxGitHubInstallationConnectResponse {
	return dto.SandboxGitHubInstallationConnectResponse{
		State:     connect.State,
		SetupURL:  connect.SetupURL,
		ExpiresAt: connect.ExpiresAt,
	}
}

func githubInstallationRecords(records []jobs.GitHubInstallationRecord) []dto.SandboxGitHubInstallationRecord {
	out := make([]dto.SandboxGitHubInstallationRecord, 0, len(records))
	for _, record := range records {
		out = append(out, githubInstallationRecord(record))
	}
	return out
}

func githubInstallationRepositorySync(installationID string, records []jobs.GitHubRunnerRepositoryRecord) GitHubInstallationRepositorySync {
	syncedAt := time.Now().UTC()
	if len(records) > 0 {
		syncedAt = records[0].SyncedAt
	}
	repos := make([]GitHubInstallationRepository, 0, len(records))
	for _, record := range records {
		if record.SyncedAt.After(syncedAt) {
			syncedAt = record.SyncedAt
		}
		repos = append(repos, GitHubInstallationRepository{
			ProviderRepositoryID: strconv.FormatInt(record.ProviderRepositoryID, 10),
			ProviderOwner:        record.ProviderOwner,
			ProviderRepo:         record.ProviderRepo,
			RepositoryFullName:   record.RepositoryFullName,
			Private:              record.Private,
			Active:               record.Active,
			SyncedAt:             record.SyncedAt,
		})
	}
	return GitHubInstallationRepositorySync{
		InstallationID: installationID,
		SyncedAt:       syncedAt,
		Repositories:   repos,
	}
}

func executionRecord(record jobs.ExecutionRecord) dto.SandboxExecutionRecord {
	return dto.SandboxExecutionRecord{
		RunID:            record.RunID,
		ExecutionID:      record.ExecutionID,
		OrgID:            dto.Uint64(record.OrgID),
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
		BillingSummary:   runBillingSummary(record.BillingSummary),
		Runner:           runnerRunMetadata(record.Runner),
		Schedule:         scheduleRunMetadata(record.Schedule),
	}
}

func attemptRecord(record jobs.AttemptRecord) dto.SandboxAttemptRecord {
	var exitCode *int
	if record.CompletedAt != nil {
		exitCodeValue := record.ExitCode
		exitCode = &exitCodeValue
	}

	return dto.SandboxAttemptRecord{
		AttemptID:              record.AttemptID,
		AttemptSeq:             record.AttemptSeq,
		State:                  record.State,
		LeaseID:                record.LeaseID,
		ExecID:                 record.ExecID,
		BillingJobID:           record.BillingJobID,
		FailureReason:          record.FailureReason,
		ExitCode:               exitCode,
		DurationMs:             record.DurationMs,
		StdoutBytes:            record.StdoutBytes,
		StderrBytes:            record.StderrBytes,
		RootfsProvisionedBytes: record.RootfsProvisionedBytes,
		BootTimeUs:             record.BootTimeUs,
		BlockReadBytes:         record.BlockReadBytes,
		BlockWriteBytes:        record.BlockWriteBytes,
		NetRXBytes:             record.NetRXBytes,
		NetTXBytes:             record.NetTXBytes,
		VCPUExitCount:          record.VCPUExitCount,
		TraceID:                record.TraceID,
		StartedAt:              record.StartedAt,
		CompletedAt:            record.CompletedAt,
		CreatedAt:              record.CreatedAt,
		UpdatedAt:              record.UpdatedAt,
	}
}

func billingWindow(record jobs.BillingWindow) dto.SandboxBillingWindow {
	return dto.SandboxBillingWindow{
		AttemptID:           record.AttemptID,
		BillingWindowID:     record.BillingWindowID,
		WindowSeq:           record.WindowSeq,
		ReservationShape:    record.ReservationShape,
		ReservedQuantity:    record.ReservedQuantity,
		ActualQuantity:      record.ActualQuantity,
		ReservedChargeUnits: dto.Uint64(record.ReservedChargeUnits),
		BilledChargeUnits:   dto.Uint64(record.BilledChargeUnits),
		WriteoffChargeUnits: dto.Uint64(record.WriteoffChargeUnits),
		CostPerUnit:         dto.Uint64(record.CostPerUnit),
		PricingPhase:        record.PricingPhase,
		State:               record.State,
		WindowStart:         record.WindowStart,
		CreatedAt:           record.CreatedAt,
		SettledAt:           record.SettledAt,
	}
}

func billingWindows(records []jobs.BillingWindow) []dto.SandboxBillingWindow {
	if len(records) == 0 {
		return nil
	}
	out := make([]dto.SandboxBillingWindow, 0, len(records))
	for _, record := range records {
		out = append(out, billingWindow(record))
	}
	return out
}

func runBillingSummary(summary jobs.RunBillingSummary) *dto.SandboxRunBillingSummary {
	if summary.WindowCount == 0 && summary.ReservedChargeUnits == 0 && summary.BilledChargeUnits == 0 && summary.WriteoffChargeUnits == 0 && summary.CostPerUnit == 0 && summary.PricingPhase == "" {
		return nil
	}
	return &dto.SandboxRunBillingSummary{
		WindowCount:         int32FromInt(summary.WindowCount, "billing window count"),
		ReservedChargeUnits: dto.Uint64(summary.ReservedChargeUnits),
		BilledChargeUnits:   dto.Uint64(summary.BilledChargeUnits),
		WriteoffChargeUnits: dto.Uint64(summary.WriteoffChargeUnits),
		CostPerUnit:         dto.Uint64(summary.CostPerUnit),
		PricingPhase:        summary.PricingPhase,
	}
}

func runnerRunMetadata(metadata jobs.RunnerRunMetadata) *dto.SandboxRunnerRunMetadata {
	if metadata.ProviderInstallationID == 0 && metadata.ProviderRunID == 0 && metadata.ProviderJobID == 0 && metadata.RepositoryFullName == "" && metadata.WorkflowName == "" && metadata.JobName == "" && metadata.HeadBranch == "" && metadata.HeadSHA == "" {
		return nil
	}
	return &dto.SandboxRunnerRunMetadata{
		ProviderInstallationID: strconv.FormatInt(metadata.ProviderInstallationID, 10),
		ProviderRunID:          strconv.FormatInt(metadata.ProviderRunID, 10),
		ProviderJobID:          strconv.FormatInt(metadata.ProviderJobID, 10),
		RepositoryFullName:     metadata.RepositoryFullName,
		WorkflowName:           metadata.WorkflowName,
		JobName:                metadata.JobName,
		HeadBranch:             metadata.HeadBranch,
		HeadSHA:                metadata.HeadSHA,
	}
}

func scheduleRunMetadata(metadata jobs.ScheduleRunMetadata) *dto.SandboxScheduleRunMetadata {
	if metadata.ScheduleID == uuid.Nil && metadata.DisplayName == "" && metadata.TemporalWorkflowID == "" && metadata.TemporalRunID == "" {
		return nil
	}
	out := dto.SandboxScheduleRunMetadata{
		DisplayName:        metadata.DisplayName,
		TemporalWorkflowID: metadata.TemporalWorkflowID,
		TemporalRunID:      metadata.TemporalRunID,
	}
	if metadata.ScheduleID != uuid.Nil {
		scheduleID := metadata.ScheduleID
		out.ScheduleID = &scheduleID
	}
	return &out
}

func runPage(page jobs.RunPage, filters jobs.RunListFilters) dto.SandboxRunsPage {
	out := dto.SandboxRunsPage{
		Runs:       make([]dto.SandboxExecutionRecord, 0, len(page.Runs)),
		NextCursor: page.NextCursor,
		Limit:      int32FromInt(page.Limit, "runs page limit"),
		Filters: dto.SandboxRunsFilters{
			SourceKind:  filters.SourceKind,
			Status:      filters.Status,
			Repository:  filters.Repository,
			Workflow:    filters.Workflow,
			Branch:      filters.Branch,
			RunnerClass: filters.RunnerClass,
		},
	}
	for _, record := range page.Runs {
		out.Runs = append(out.Runs, executionRecord(record))
	}
	return out
}

func runLogSearchResult(record jobs.RunLogSearchResult) dto.SandboxRunLogSearchResult {
	return dto.SandboxRunLogSearchResult{
		ExecutionID:        record.ExecutionID,
		AttemptID:          record.AttemptID,
		SourceKind:         record.SourceKind,
		WorkloadKind:       record.WorkloadKind,
		RunnerClass:        record.RunnerClass,
		RepositoryFullName: record.RepositoryFullName,
		WorkflowName:       record.WorkflowName,
		JobName:            record.JobName,
		HeadBranch:         record.HeadBranch,
		ScheduleID:         record.ScheduleID,
		Seq:                record.Seq,
		Stream:             record.Stream,
		Chunk:              record.Chunk,
		CreatedAt:          record.CreatedAt,
	}
}

func runLogSearchPage(page jobs.RunLogSearchPage, filters jobs.RunLogSearchFilters) dto.SandboxRunLogSearchPage {
	out := dto.SandboxRunLogSearchPage{
		Results:    make([]dto.SandboxRunLogSearchResult, 0, len(page.Results)),
		NextCursor: page.NextCursor,
		Limit:      int32FromInt(page.Limit, "run log search page limit"),
		Filters: dto.SandboxRunLogSearchFilters{
			Query:       filters.Query,
			RunID:       filters.ExecutionID.String(),
			AttemptID:   filters.AttemptID.String(),
			SourceKind:  filters.SourceKind,
			Repository:  filters.Repository,
			Workflow:    filters.Workflow,
			Branch:      filters.Branch,
			RunnerClass: filters.RunnerClass,
		},
	}
	if filters.ExecutionID == uuid.Nil {
		out.Filters.RunID = ""
	}
	if filters.AttemptID == uuid.Nil {
		out.Filters.AttemptID = ""
	}
	for _, record := range page.Results {
		out.Results = append(out.Results, runLogSearchResult(record))
	}
	return out
}

func analyticsBucket(bucket jobs.AnalyticsBucket) dto.SandboxAnalyticsBucket {
	return dto.SandboxAnalyticsBucket{
		Key:                 bucket.Key,
		Count:               dto.Uint64(bucket.Count),
		ReservedChargeUnits: dto.Uint64(bucket.ReservedChargeUnits),
		BilledChargeUnits:   dto.Uint64(bucket.BilledChargeUnits),
		WriteoffChargeUnits: dto.Uint64(bucket.WriteoffChargeUnits),
	}
}

func analyticsBuckets(buckets []jobs.AnalyticsBucket) []dto.SandboxAnalyticsBucket {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]dto.SandboxAnalyticsBucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, analyticsBucket(bucket))
	}
	return out
}

func runDurationSample(sample jobs.RunDurationSample) dto.SandboxRunDurationSample {
	return dto.SandboxRunDurationSample{
		ExecutionID:        sample.ExecutionID,
		Status:             sample.Status,
		RunnerClass:        sample.RunnerClass,
		RepositoryFullName: sample.RepositoryFullName,
		WorkflowName:       sample.WorkflowName,
		JobName:            sample.JobName,
		DurationMs:         sample.DurationMs,
		CompletedAt:        sample.CompletedAt,
	}
}

func jobsAnalytics(analytics jobs.JobsAnalytics) dto.SandboxJobsAnalytics {
	out := dto.SandboxJobsAnalytics{
		WindowStart:   analytics.WindowStart,
		WindowEnd:     analytics.WindowEnd,
		TotalRuns:     dto.Uint64(analytics.TotalRuns),
		SucceededRuns: dto.Uint64(analytics.SucceededRuns),
		FailedRuns:    dto.Uint64(analytics.FailedRuns),
		P50DurationMs: dto.Uint64(analytics.P50DurationMs),
		P95DurationMs: dto.Uint64(analytics.P95DurationMs),
		P99DurationMs: dto.Uint64(analytics.P99DurationMs),
		BySource:      analyticsBuckets(analytics.BySource),
		ByRunnerClass: analyticsBuckets(analytics.ByRunnerClass),
		SlowestRuns:   make([]dto.SandboxRunDurationSample, 0, len(analytics.SlowestRuns)),
	}
	for _, sample := range analytics.SlowestRuns {
		out.SlowestRuns = append(out.SlowestRuns, runDurationSample(sample))
	}
	return out
}

func costsAnalytics(analytics jobs.CostsAnalytics) dto.SandboxCostsAnalytics {
	return dto.SandboxCostsAnalytics{
		WindowStart:         analytics.WindowStart,
		WindowEnd:           analytics.WindowEnd,
		ReservedChargeUnits: dto.Uint64(analytics.ReservedChargeUnits),
		BilledChargeUnits:   dto.Uint64(analytics.BilledChargeUnits),
		WriteoffChargeUnits: dto.Uint64(analytics.WriteoffChargeUnits),
		BySource:            analyticsBuckets(analytics.BySource),
		ByRunnerClass:       analyticsBuckets(analytics.ByRunnerClass),
		ByRepository:        analyticsBuckets(analytics.ByRepository),
	}
}

func runnerSizingAnalytics(analytics jobs.RunnerSizingAnalytics) dto.SandboxRunnerSizingAnalytics {
	out := dto.SandboxRunnerSizingAnalytics{
		WindowStart:   analytics.WindowStart,
		WindowEnd:     analytics.WindowEnd,
		ByRunnerClass: make([]dto.SandboxRunnerSizingSample, 0, len(analytics.ByRunnerClass)),
	}
	for _, sample := range analytics.ByRunnerClass {
		out.ByRunnerClass = append(out.ByRunnerClass, dto.SandboxRunnerSizingSample{
			RunnerClass:               sample.RunnerClass,
			RunCount:                  dto.Uint64(sample.RunCount),
			P95DurationMs:             dto.Uint64(sample.P95DurationMs),
			AvgRootfsProvisionedBytes: dto.Uint64(sample.AvgRootfsProvisionedBytes),
			AvgBootTimeUs:             dto.Uint64(sample.AvgBootTimeUs),
			AvgBlockWriteBytes:        dto.Uint64(sample.AvgBlockWriteBytes),
			AvgNetTxBytes:             dto.Uint64(sample.AvgNetTxBytes),
		})
	}
	return out
}

func executionScheduleCreateRequest(request dto.SandboxExecutionScheduleCreateRequest) recurring.CreateRequest {
	return recurring.CreateRequest{
		DisplayName:        request.DisplayName,
		IdempotencyKey:     request.IdempotencyKey,
		ProjectID:          request.ProjectID,
		SourceRepositoryID: request.SourceRepositoryID,
		WorkflowPath:       request.WorkflowPath,
		Ref:                request.Ref,
		Inputs:             request.Inputs,
		IntervalSeconds:    request.IntervalSeconds,
		Paused:             request.Paused,
	}
}

func executionScheduleRecord(record recurring.ScheduleRecord) dto.SandboxExecutionScheduleRecord {
	return dto.SandboxExecutionScheduleRecord{
		ScheduleID:         record.ScheduleID,
		OrgID:              dto.Uint64(record.OrgID),
		ActorID:            record.ActorID,
		DisplayName:        record.DisplayName,
		IdempotencyKey:     record.IdempotencyKey,
		TemporalScheduleID: record.TemporalScheduleID,
		TemporalNamespace:  record.TemporalNamespace,
		TaskQueue:          record.TaskQueue,
		State:              record.State,
		IntervalSeconds:    record.IntervalSeconds,
		ProjectID:          record.ProjectID,
		SourceRepositoryID: record.SourceRepositoryID,
		WorkflowPath:       record.WorkflowPath,
		Ref:                record.Ref,
		Inputs:             record.Inputs,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
		Dispatches:         executionScheduleDispatches(record.Dispatches),
	}
}

func executionScheduleDispatches(records []recurring.DispatchRecord) []dto.SandboxExecutionScheduleDispatchRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]dto.SandboxExecutionScheduleDispatchRecord, 0, len(records))
	for _, record := range records {
		out = append(out, executionScheduleDispatchRecord(record))
	}
	return out
}

func executionScheduleDispatchRecord(record recurring.DispatchRecord) dto.SandboxExecutionScheduleDispatchRecord {
	return dto.SandboxExecutionScheduleDispatchRecord{
		DispatchID:          record.DispatchID,
		ScheduleID:          record.ScheduleID,
		TemporalWorkflowID:  record.TemporalWorkflowID,
		TemporalRunID:       record.TemporalRunID,
		SourceWorkflowRunID: record.SourceWorkflowRunID,
		WorkflowState:       record.WorkflowState,
		State:               record.State,
		FailureReason:       record.FailureReason,
		ScheduledAt:         record.ScheduledAt,
		SubmittedAt:         record.SubmittedAt,
		CreatedAt:           record.CreatedAt,
		UpdatedAt:           record.UpdatedAt,
	}
}

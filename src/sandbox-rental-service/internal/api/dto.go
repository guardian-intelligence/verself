package api

import (
	"strconv"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
	"github.com/forge-metal/sandbox-rental-service/internal/recurring"
	"github.com/google/uuid"
)

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
		RunID:            record.RunID,
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
		BillingSummary:   runBillingSummary(record.BillingSummary),
		Runner:           runnerRunMetadata(record.Runner),
		Schedule:         scheduleRunMetadata(record.Schedule),
		StickyDiskMounts: stickyDiskMounts(record.StickyDiskMounts),
	}
}

func attemptRecord(record jobs.AttemptRecord) apiwire.SandboxAttemptRecord {
	var exitCode *int
	if record.CompletedAt != nil {
		exitCodeValue := record.ExitCode
		exitCode = &exitCodeValue
	}

	return apiwire.SandboxAttemptRecord{
		AttemptID:              record.AttemptID,
		AttemptSeq:             record.AttemptSeq,
		State:                  record.State,
		LeaseID:                record.LeaseID,
		ExecID:                 record.ExecID,
		BillingJobID:           record.BillingJobID,
		FailureReason:          record.FailureReason,
		ExitCode:               exitCode,
		DurationMs:             record.DurationMs,
		ZFSWritten:             record.ZFSWritten,
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

func billingWindow(record jobs.BillingWindow) apiwire.SandboxBillingWindow {
	return apiwire.SandboxBillingWindow{
		AttemptID:           record.AttemptID,
		BillingWindowID:     record.BillingWindowID,
		WindowSeq:           record.WindowSeq,
		ReservationShape:    record.ReservationShape,
		ReservedQuantity:    record.ReservedQuantity,
		ActualQuantity:      record.ActualQuantity,
		ReservedChargeUnits: apiwire.Uint64(record.ReservedChargeUnits),
		BilledChargeUnits:   apiwire.Uint64(record.BilledChargeUnits),
		WriteoffChargeUnits: apiwire.Uint64(record.WriteoffChargeUnits),
		CostPerUnit:         apiwire.Uint64(record.CostPerUnit),
		PricingPhase:        record.PricingPhase,
		State:               record.State,
		WindowStart:         record.WindowStart,
		CreatedAt:           record.CreatedAt,
		SettledAt:           record.SettledAt,
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

func runBillingSummary(summary jobs.RunBillingSummary) *apiwire.SandboxRunBillingSummary {
	if summary.WindowCount == 0 && summary.ReservedChargeUnits == 0 && summary.BilledChargeUnits == 0 && summary.WriteoffChargeUnits == 0 && summary.CostPerUnit == 0 && summary.PricingPhase == "" {
		return nil
	}
	return &apiwire.SandboxRunBillingSummary{
		WindowCount:         int32(summary.WindowCount),
		ReservedChargeUnits: apiwire.Uint64(summary.ReservedChargeUnits),
		BilledChargeUnits:   apiwire.Uint64(summary.BilledChargeUnits),
		WriteoffChargeUnits: apiwire.Uint64(summary.WriteoffChargeUnits),
		CostPerUnit:         apiwire.Uint64(summary.CostPerUnit),
		PricingPhase:        summary.PricingPhase,
	}
}

func runnerRunMetadata(metadata jobs.RunnerRunMetadata) *apiwire.SandboxRunnerRunMetadata {
	if metadata.ProviderInstallationID == 0 && metadata.ProviderRunID == 0 && metadata.ProviderJobID == 0 && metadata.RepositoryFullName == "" && metadata.WorkflowName == "" && metadata.JobName == "" && metadata.HeadBranch == "" && metadata.HeadSHA == "" {
		return nil
	}
	return &apiwire.SandboxRunnerRunMetadata{
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

func scheduleRunMetadata(metadata jobs.ScheduleRunMetadata) *apiwire.SandboxScheduleRunMetadata {
	if metadata.ScheduleID == uuid.Nil && metadata.DisplayName == "" && metadata.TemporalWorkflowID == "" && metadata.TemporalRunID == "" {
		return nil
	}
	out := apiwire.SandboxScheduleRunMetadata{
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

func stickyDiskMount(record jobs.StickyDiskMountRecord) apiwire.SandboxExecutionStickyDiskMount {
	return apiwire.SandboxExecutionStickyDiskMount{
		MountID:             record.MountID,
		MountName:           record.MountName,
		KeyHash:             record.KeyHash,
		MountPath:           record.MountPath,
		BaseGeneration:      apiwire.Uint64(uint64(record.BaseGeneration)),
		CommittedGeneration: apiwire.Uint64(uint64(record.CommittedGeneration)),
		SaveRequested:       record.SaveRequested,
		SaveState:           record.SaveState,
		FailureReason:       record.FailureReason,
		RequestedAt:         record.RequestedAt,
		CompletedAt:         record.CompletedAt,
	}
}

func stickyDiskMounts(records []jobs.StickyDiskMountRecord) []apiwire.SandboxExecutionStickyDiskMount {
	if len(records) == 0 {
		return nil
	}
	out := make([]apiwire.SandboxExecutionStickyDiskMount, 0, len(records))
	for _, record := range records {
		out = append(out, stickyDiskMount(record))
	}
	return out
}

func runPage(page jobs.RunPage, filters jobs.RunListFilters) apiwire.SandboxRunsPage {
	out := apiwire.SandboxRunsPage{
		Runs:       make([]apiwire.SandboxExecutionRecord, 0, len(page.Runs)),
		NextCursor: page.NextCursor,
		Limit:      int32(page.Limit),
		Filters: apiwire.SandboxRunsFilters{
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

func runLogSearchResult(record jobs.RunLogSearchResult) apiwire.SandboxRunLogSearchResult {
	return apiwire.SandboxRunLogSearchResult{
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

func runLogSearchPage(page jobs.RunLogSearchPage, filters jobs.RunLogSearchFilters) apiwire.SandboxRunLogSearchPage {
	out := apiwire.SandboxRunLogSearchPage{
		Results:    make([]apiwire.SandboxRunLogSearchResult, 0, len(page.Results)),
		NextCursor: page.NextCursor,
		Limit:      int32(page.Limit),
		Filters: apiwire.SandboxRunLogSearchFilters{
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

func analyticsBucket(bucket jobs.AnalyticsBucket) apiwire.SandboxAnalyticsBucket {
	return apiwire.SandboxAnalyticsBucket{
		Key:                 bucket.Key,
		Count:               apiwire.Uint64(bucket.Count),
		ReservedChargeUnits: apiwire.Uint64(bucket.ReservedChargeUnits),
		BilledChargeUnits:   apiwire.Uint64(bucket.BilledChargeUnits),
		WriteoffChargeUnits: apiwire.Uint64(bucket.WriteoffChargeUnits),
	}
}

func analyticsBuckets(buckets []jobs.AnalyticsBucket) []apiwire.SandboxAnalyticsBucket {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]apiwire.SandboxAnalyticsBucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, analyticsBucket(bucket))
	}
	return out
}

func runDurationSample(sample jobs.RunDurationSample) apiwire.SandboxRunDurationSample {
	return apiwire.SandboxRunDurationSample{
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

func jobsAnalytics(analytics jobs.JobsAnalytics) apiwire.SandboxJobsAnalytics {
	out := apiwire.SandboxJobsAnalytics{
		WindowStart:   analytics.WindowStart,
		WindowEnd:     analytics.WindowEnd,
		TotalRuns:     apiwire.Uint64(analytics.TotalRuns),
		SucceededRuns: apiwire.Uint64(analytics.SucceededRuns),
		FailedRuns:    apiwire.Uint64(analytics.FailedRuns),
		P50DurationMs: apiwire.Uint64(analytics.P50DurationMs),
		P95DurationMs: apiwire.Uint64(analytics.P95DurationMs),
		P99DurationMs: apiwire.Uint64(analytics.P99DurationMs),
		BySource:      analyticsBuckets(analytics.BySource),
		ByRunnerClass: analyticsBuckets(analytics.ByRunnerClass),
		SlowestRuns:   make([]apiwire.SandboxRunDurationSample, 0, len(analytics.SlowestRuns)),
	}
	for _, sample := range analytics.SlowestRuns {
		out.SlowestRuns = append(out.SlowestRuns, runDurationSample(sample))
	}
	return out
}

func costsAnalytics(analytics jobs.CostsAnalytics) apiwire.SandboxCostsAnalytics {
	return apiwire.SandboxCostsAnalytics{
		WindowStart:         analytics.WindowStart,
		WindowEnd:           analytics.WindowEnd,
		ReservedChargeUnits: apiwire.Uint64(analytics.ReservedChargeUnits),
		BilledChargeUnits:   apiwire.Uint64(analytics.BilledChargeUnits),
		WriteoffChargeUnits: apiwire.Uint64(analytics.WriteoffChargeUnits),
		BySource:            analyticsBuckets(analytics.BySource),
		ByRunnerClass:       analyticsBuckets(analytics.ByRunnerClass),
		ByRepository:        analyticsBuckets(analytics.ByRepository),
	}
}

func cachesAnalytics(analytics jobs.CachesAnalytics) apiwire.SandboxCachesAnalytics {
	return apiwire.SandboxCachesAnalytics{
		WindowStart:         analytics.WindowStart,
		WindowEnd:           analytics.WindowEnd,
		CheckoutRequests:    apiwire.Uint64(analytics.CheckoutRequests),
		CheckoutHits:        apiwire.Uint64(analytics.CheckoutHits),
		CheckoutMisses:      apiwire.Uint64(analytics.CheckoutMisses),
		StickyRestoreHits:   apiwire.Uint64(analytics.StickyRestoreHits),
		StickyRestoreMisses: apiwire.Uint64(analytics.StickyRestoreMisses),
		StickySaveRequests:  apiwire.Uint64(analytics.StickySaveRequests),
		StickyCommits:       apiwire.Uint64(analytics.StickyCommits),
		ByRepository:        analyticsBuckets(analytics.ByRepository),
	}
}

func runnerSizingAnalytics(analytics jobs.RunnerSizingAnalytics) apiwire.SandboxRunnerSizingAnalytics {
	out := apiwire.SandboxRunnerSizingAnalytics{
		WindowStart:   analytics.WindowStart,
		WindowEnd:     analytics.WindowEnd,
		ByRunnerClass: make([]apiwire.SandboxRunnerSizingSample, 0, len(analytics.ByRunnerClass)),
	}
	for _, sample := range analytics.ByRunnerClass {
		out.ByRunnerClass = append(out.ByRunnerClass, apiwire.SandboxRunnerSizingSample{
			RunnerClass:               sample.RunnerClass,
			RunCount:                  apiwire.Uint64(sample.RunCount),
			P95DurationMs:             apiwire.Uint64(sample.P95DurationMs),
			AvgRootfsProvisionedBytes: apiwire.Uint64(sample.AvgRootfsProvisionedBytes),
			AvgBootTimeUs:             apiwire.Uint64(sample.AvgBootTimeUs),
			AvgBlockWriteBytes:        apiwire.Uint64(sample.AvgBlockWriteBytes),
			AvgNetTxBytes:             apiwire.Uint64(sample.AvgNetTxBytes),
		})
	}
	return out
}

func stickyDiskRecord(record jobs.StickyDiskRecord) apiwire.SandboxStickyDiskRecord {
	return apiwire.SandboxStickyDiskRecord{
		InstallationID:     strconv.FormatInt(record.InstallationID, 10),
		RepositoryID:       strconv.FormatInt(record.RepositoryID, 10),
		RepositoryFullName: record.RepositoryFullName,
		KeyHash:            record.KeyHash,
		Key:                record.Key,
		CurrentGeneration:  apiwire.Uint64(uint64(record.CurrentGeneration)),
		CurrentSourceRef:   record.CurrentSourceRef,
		LastUsedAt:         record.LastUsedAt,
		LastCompletedAt:    record.LastCompletedAt,
		LastSaveState:      record.LastSaveState,
		LastExecutionID:    record.LastExecutionID,
		LastAttemptID:      record.LastAttemptID,
		LastRunnerClass:    record.LastRunnerClass,
		LastWorkflowName:   record.LastWorkflowName,
		LastJobName:        record.LastJobName,
		LastMountPath:      record.LastMountPath,
	}
}

func stickyDisksPage(page jobs.StickyDiskPage, filters jobs.StickyDiskListFilters) apiwire.SandboxStickyDisksPage {
	out := apiwire.SandboxStickyDisksPage{
		Disks:      make([]apiwire.SandboxStickyDiskRecord, 0, len(page.Disks)),
		NextCursor: page.NextCursor,
		Limit:      int32(page.Limit),
		Filters: apiwire.SandboxStickyDiskFilters{
			Repository: filters.Repository,
		},
	}
	for _, record := range page.Disks {
		out.Disks = append(out.Disks, stickyDiskRecord(record))
	}
	return out
}

func stickyDiskResetResult(result jobs.StickyDiskResetResult) apiwire.SandboxStickyDiskResetResult {
	return apiwire.SandboxStickyDiskResetResult{
		InstallationID:   strconv.FormatInt(result.InstallationID, 10),
		RepositoryID:     strconv.FormatInt(result.RepositoryID, 10),
		KeyHash:          result.KeyHash,
		DeletedSourceRef: result.DeletedSourceRef,
		ResetAt:          result.ResetAt,
	}
}

func executionScheduleCreateRequest(request apiwire.SandboxExecutionScheduleCreateRequest) recurring.CreateRequest {
	return recurring.CreateRequest{
		DisplayName:        request.DisplayName,
		IdempotencyKey:     request.IdempotencyKey,
		SourceRepositoryID: request.SourceRepositoryID,
		WorkflowPath:       request.WorkflowPath,
		Ref:                request.Ref,
		Inputs:             request.Inputs,
		IntervalSeconds:    request.IntervalSeconds,
		Paused:             request.Paused,
	}
}

func executionScheduleRecord(record recurring.ScheduleRecord) apiwire.SandboxExecutionScheduleRecord {
	return apiwire.SandboxExecutionScheduleRecord{
		ScheduleID:         record.ScheduleID,
		OrgID:              apiwire.Uint64(record.OrgID),
		ActorID:            record.ActorID,
		DisplayName:        record.DisplayName,
		IdempotencyKey:     record.IdempotencyKey,
		TemporalScheduleID: record.TemporalScheduleID,
		TemporalNamespace:  record.TemporalNamespace,
		TaskQueue:          record.TaskQueue,
		State:              record.State,
		IntervalSeconds:    record.IntervalSeconds,
		SourceRepositoryID: record.SourceRepositoryID,
		WorkflowPath:       record.WorkflowPath,
		Ref:                record.Ref,
		Inputs:             record.Inputs,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
		Dispatches:         executionScheduleDispatches(record.Dispatches),
	}
}

func executionScheduleDispatches(records []recurring.DispatchRecord) []apiwire.SandboxExecutionScheduleDispatchRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]apiwire.SandboxExecutionScheduleDispatchRecord, 0, len(records))
	for _, record := range records {
		out = append(out, executionScheduleDispatchRecord(record))
	}
	return out
}

func executionScheduleDispatchRecord(record recurring.DispatchRecord) apiwire.SandboxExecutionScheduleDispatchRecord {
	return apiwire.SandboxExecutionScheduleDispatchRecord{
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

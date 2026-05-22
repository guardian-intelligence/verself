package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/verself/sandbox-rental-service/internal/contractapi"
	"github.com/verself/sandbox-rental-service/internal/jobs"
	"github.com/verself/sandbox-rental-service/internal/recurring"
)

type resourcePathSegment struct {
	collection string
	id         string
}

func executionRecord(record jobs.ExecutionRecord, installationID string) contractapi.SandboxExecutionRecord {
	orgID := record.OrgID
	return contractapi.SandboxExecutionRecord{
		RunID:            contractapi.RunID(record.RunID.String()),
		ResourceName:     contractapi.ResourceName(resourceName(installationID, resourcePathSegment{"orgs", orgID}, resourcePathSegment{"runs", record.RunID.String()})),
		ExecutionID:      contractapi.ExecutionID(record.ExecutionID.String()),
		OrgID:            contractapi.OrgID(orgID),
		ActorID:          contractapi.ActorID(record.ActorID),
		Kind:             contractapi.ExecutionKind(record.Kind),
		SourceKind:       optionalContractString[contractapi.SourceKind](record.SourceKind),
		WorkloadKind:     optionalContractString[contractapi.WorkloadKind](record.WorkloadKind),
		SourceRef:        optionalContractString[contractapi.SourceRef](record.SourceRef),
		RunnerClass:      optionalContractString[contractapi.RunnerClass](record.RunnerClass),
		ExternalProvider: optionalContractString[contractapi.ExternalProvider](record.ExternalProvider),
		ExternalTaskID:   optionalContractString[contractapi.ExternalTaskID](record.ExternalTaskID),
		Provider:         optionalContractString[contractapi.Provider](record.Provider),
		ProductID:        contractapi.ProductID(record.ProductID),
		Status:           contractapi.ExecutionStatus(record.Status),
		CorrelationID:    optionalContractString[contractapi.CorrelationID](record.CorrelationID),
		IdempotencyKey:   optionalContractString[contractapi.IdempotencyKey](record.IdempotencyKey),
		RunCommand:       optionalContractString[contractapi.RunCommand](record.RunCommand),
		LatestAttempt:    attemptRecord(record.LatestAttempt, record.OrgID, record.RunID, installationID),
		CreatedAt:        timestamp(record.CreatedAt),
		UpdatedAt:        timestamp(record.UpdatedAt),
		BillingWindows:   billingWindows(record.BillingWindows),
		BillingSummary:   runBillingSummary(record.BillingSummary),
		Runner:           runnerRunMetadata(record.Runner),
		Schedule:         scheduleRunMetadata(record.Schedule, record.OrgID, installationID),
	}
}

func attemptRecord(record jobs.AttemptRecord, orgID string, runID uuid.UUID, installationID string) contractapi.SandboxAttemptRecord {
	var exitCode *contractapi.SafeExitCode
	if record.CompletedAt != nil {
		exitCode = optionalSafeExitCode(record.ExitCode)
	}
	return contractapi.SandboxAttemptRecord{
		AttemptID:              contractapi.AttemptID(record.AttemptID.String()),
		ResourceName:           optionalContractString[contractapi.ResourceName](resourceName(installationID, resourcePathSegment{"orgs", orgID}, resourcePathSegment{"runs", runID.String()}, resourcePathSegment{"attempts", record.AttemptID.String()})),
		AttemptSeq:             safeLongFromInt(record.AttemptSeq, "attempt seq"),
		State:                  contractapi.AttemptState(record.State),
		LeaseID:                optionalContractString[contractapi.LeaseID](record.LeaseID),
		ExecID:                 optionalContractString[contractapi.ExecID](record.ExecID),
		BillingJobID:           optionalSafeLong(record.BillingJobID, "billing job id"),
		FailureReason:          optionalContractString[contractapi.FailureReason](record.FailureReason),
		ExitCode:               exitCode,
		DurationMs:             optionalSafeLong(record.DurationMs, "duration ms"),
		StdoutBytes:            optionalSafeLong(record.StdoutBytes, "stdout bytes"),
		StderrBytes:            optionalSafeLong(record.StderrBytes, "stderr bytes"),
		RootfsProvisionedBytes: optionalSafeLong(record.RootfsProvisionedBytes, "rootfs provisioned bytes"),
		BootTimeUs:             optionalSafeLong(record.BootTimeUs, "boot time us"),
		BlockReadBytes:         optionalSafeLong(record.BlockReadBytes, "block read bytes"),
		BlockWriteBytes:        optionalSafeLong(record.BlockWriteBytes, "block write bytes"),
		NetRxBytes:             optionalSafeLong(record.NetRXBytes, "net rx bytes"),
		NetTxBytes:             optionalSafeLong(record.NetTXBytes, "net tx bytes"),
		VcpuExitCount:          optionalSafeLong(record.VCPUExitCount, "vcpu exit count"),
		TraceID:                optionalContractString[contractapi.TraceID](record.TraceID),
		StartedAt:              timestampPtr(record.StartedAt),
		CompletedAt:            timestampPtr(record.CompletedAt),
		CreatedAt:              timestamp(record.CreatedAt),
		UpdatedAt:              timestamp(record.UpdatedAt),
	}
}

func billingWindow(record jobs.BillingWindow) contractapi.SandboxBillingWindow {
	return contractapi.SandboxBillingWindow{
		AttemptID:           contractapi.AttemptID(record.AttemptID.String()),
		BillingWindowID:     contractapi.BillingWindowID(record.BillingWindowID),
		WindowSeq:           safeLongFromInt(record.WindowSeq, "billing window seq"),
		ReservationShape:    contractapi.ReservationShape(record.ReservationShape),
		ReservedQuantity:    safeLongFromInt(record.ReservedQuantity, "reserved quantity"),
		ActualQuantity:      optionalSafeLongFromInt(record.ActualQuantity, "actual quantity"),
		ReservedChargeUnits: decimalUint64(record.ReservedChargeUnits),
		BilledChargeUnits:   decimalUint64(record.BilledChargeUnits),
		WriteoffChargeUnits: decimalUint64(record.WriteoffChargeUnits),
		CostPerUnit:         decimalUint64(record.CostPerUnit),
		PricingPhase:        optionalContractString[contractapi.PricingPhase](record.PricingPhase),
		State:               record.State,
		WindowStart:         timestamp(record.WindowStart),
		CreatedAt:           timestamp(record.CreatedAt),
		SettledAt:           timestampPtr(record.SettledAt),
	}
}

func billingWindows(records []jobs.BillingWindow) *contractapi.SandboxBillingWindows {
	if len(records) == 0 {
		return nil
	}
	out := make(contractapi.SandboxBillingWindows, 0, len(records))
	for _, record := range records {
		out = append(out, billingWindow(record))
	}
	return &out
}

func runBillingSummary(summary jobs.RunBillingSummary) *contractapi.SandboxRunBillingSummary {
	if summary.WindowCount == 0 && summary.ReservedChargeUnits == 0 && summary.BilledChargeUnits == 0 && summary.WriteoffChargeUnits == 0 && summary.CostPerUnit == 0 && summary.PricingPhase == "" {
		return nil
	}
	return &contractapi.SandboxRunBillingSummary{
		WindowCount:         safeLongFromInt(summary.WindowCount, "billing window count"),
		ReservedChargeUnits: decimalUint64(summary.ReservedChargeUnits),
		BilledChargeUnits:   decimalUint64(summary.BilledChargeUnits),
		WriteoffChargeUnits: decimalUint64(summary.WriteoffChargeUnits),
		CostPerUnit:         decimalUint64(summary.CostPerUnit),
		PricingPhase:        optionalContractString[contractapi.PricingPhase](summary.PricingPhase),
	}
}

func runnerRunMetadata(metadata jobs.RunnerRunMetadata) *contractapi.SandboxRunnerRunMetadata {
	if metadata.ProviderInstallationID == 0 && metadata.ProviderRunID == 0 && metadata.ProviderJobID == 0 && metadata.RepositoryFullName == "" && metadata.WorkflowName == "" && metadata.JobName == "" && metadata.HeadBranch == "" && metadata.HeadSHA == "" {
		return nil
	}
	return &contractapi.SandboxRunnerRunMetadata{
		ProviderInstallationID: optionalDecimalInt64(metadata.ProviderInstallationID),
		ProviderRunID:          optionalDecimalInt64(metadata.ProviderRunID),
		ProviderJobID:          optionalDecimalInt64(metadata.ProviderJobID),
		RepositoryFullName:     optionalContractString[contractapi.RepositoryFullName](metadata.RepositoryFullName),
		WorkflowName:           optionalContractString[contractapi.WorkflowName](metadata.WorkflowName),
		JobName:                optionalContractString[contractapi.JobName](metadata.JobName),
		HeadBranch:             optionalContractString[contractapi.HeadBranch](metadata.HeadBranch),
		HeadSha:                optionalContractString[contractapi.HeadSHA](metadata.HeadSHA),
	}
}

func scheduleRunMetadata(metadata jobs.ScheduleRunMetadata, orgID string, installationID string) *contractapi.SandboxScheduleRunMetadata {
	if metadata.ScheduleID == uuid.Nil && metadata.DisplayName == "" && metadata.TemporalWorkflowID == "" && metadata.TemporalRunID == "" {
		return nil
	}
	out := contractapi.SandboxScheduleRunMetadata{
		DisplayName:        optionalContractString[contractapi.DisplayName](metadata.DisplayName),
		TemporalWorkflowID: optionalContractString[contractapi.TemporalID](metadata.TemporalWorkflowID),
		TemporalRunID:      optionalContractString[contractapi.TemporalID](metadata.TemporalRunID),
	}
	if metadata.ScheduleID != uuid.Nil {
		scheduleID := metadata.ScheduleID.String()
		out.ScheduleID = optionalContractString[contractapi.ScheduleID](scheduleID)
		out.ScheduleResourceName = optionalContractString[contractapi.ResourceName](resourceName(installationID, resourcePathSegment{"orgs", orgID}, resourcePathSegment{"schedules", scheduleID}))
	}
	return &out
}

func runPage(page jobs.RunPage, filters jobs.RunListFilters, installationID string) contractapi.SandboxRunsPage {
	out := contractapi.SandboxRunsPage{
		Body: contractapi.SandboxRunsPageBody{
			Runs:  make(contractapi.Runs, 0, len(page.Runs)),
			Limit: contractapi.RunListPageSize(page.Limit),
			Filters: contractapi.SandboxRunsFilters{
				SourceKind:  optionalContractString[contractapi.SourceKind](filters.SourceKind),
				Status:      optionalContractString[contractapi.ExecutionStatus](filters.Status),
				Repository:  optionalContractString[contractapi.RepositoryFullName](filters.Repository),
				Workflow:    optionalContractString[contractapi.WorkflowName](filters.Workflow),
				Branch:      optionalContractString[contractapi.GitRef](filters.Branch),
				RunnerClass: optionalContractString[contractapi.RunnerClass](filters.RunnerClass),
			},
			NextCursor: optionalContractString[contractapi.RunListCursor](page.NextCursor),
		},
	}
	for _, record := range page.Runs {
		out.Body.Runs = append(out.Body.Runs, executionRecord(record, installationID))
	}
	return out
}

func cachesView(records []jobs.DurableCacheRecord, installationID string) contractapi.SandboxCaches {
	out := make(contractapi.SandboxCaches, 0, len(records))
	for _, record := range records {
		out = append(out, cacheView(record, installationID))
	}
	return out
}

func cacheView(record jobs.DurableCacheRecord, installationID string) contractapi.SandboxCache {
	return contractapi.SandboxCache{
		CacheID:              contractapi.CacheID(record.CacheID.String()),
		CreatedAt:            timestamp(record.CreatedAt),
		CurrentGenerationID:  optionalUUIDPtr[contractapi.CacheGenerationID](record.CurrentGenerationID),
		GenerationCount:      safeLongFromUint64(record.GenerationCount, "cache generation count"),
		LastUsedAt:           timestamp(record.LastUsedAt),
		Name:                 contractapi.CacheName(record.Name),
		OrgID:                contractapi.OrgID(record.OrgID),
		Provider:             contractapi.Provider(record.Provider),
		ProviderRepositoryID: contractapi.ProviderRepositoryID(strconv.FormatInt(record.ProviderRepositoryID, 10)),
		RepositoryFullName:   optionalContractString[contractapi.RepositoryFullName](record.RepositoryFullName),
		ResourceName:         contractapi.ResourceName(resourceName(installationID, resourcePathSegment{"orgs", record.OrgID}, resourcePathSegment{"caches", record.CacheID.String()})),
		ScopeRef:             contractapi.GitRef(record.ScopeRef),
		UsedBytes:            safeLongFromUint64(record.UsedBytes, "cache used bytes"),
		WrittenBytes:         safeLongFromUint64(record.WrittenBytes, "cache written bytes"),
	}
}

func cacheGenerations(records []jobs.DurableCacheGenerationRecord, installationID string) contractapi.SandboxCacheGenerations {
	out := make(contractapi.SandboxCacheGenerations, 0, len(records))
	for _, record := range records {
		out = append(out, cacheGeneration(record, installationID))
	}
	return out
}

func cacheGeneration(record jobs.DurableCacheGenerationRecord, _ string) contractapi.SandboxCacheGeneration {
	return contractapi.SandboxCacheGeneration{
		BindPaths:            cachePaths(record.BindPaths),
		CacheGenerationID:    contractapi.CacheGenerationID(record.CacheGenerationID.String()),
		CacheID:              contractapi.CacheID(record.CacheID.String()),
		CommittedAt:          timestampPtr(&record.CommittedAt),
		Current:              record.IsCurrent,
		ExpiresAt:            timestampPtr(record.ExpiresAt),
		HeadSha:              optionalContractString[contractapi.HeadSHA](record.HeadSHA),
		LastUsedAt:           timestampPtr(&record.LastUsedAt),
		OrgID:                contractapi.OrgID(record.OrgID),
		Provider:             contractapi.Provider(record.Provider),
		ProviderJobID:        contractapi.DecimalUint64(strconv.FormatInt(record.ProviderJobID, 10)),
		ProviderRepositoryID: contractapi.ProviderRepositoryID(strconv.FormatInt(record.ProviderRepositoryID, 10)),
		ProviderRunAttempt:   contractapi.DecimalUint64(strconv.FormatInt(record.ProviderRunAttempt, 10)),
		ProviderRunID:        contractapi.DecimalUint64(strconv.FormatInt(record.ProviderRunID, 10)),
		RepositoryFullName:   optionalContractString[contractapi.RepositoryFullName](record.RepositoryFullName),
		Result:               record.Result,
		ScopeRef:             contractapi.GitRef(record.ScopeRef),
		SealedAt:             timestampPtr(&record.SealedAt),
		SourceGenerationID:   optionalUUIDPtr[contractapi.CacheGenerationID](record.SourceGenerationID),
		State:                record.State,
		TreeHash:             optionalContractString[string](record.TreeHash),
		UsedBytes:            safeLongFromUint64(record.UsedBytes, "cache generation used bytes"),
		CacheName:            contractapi.CacheName(record.CacheName),
		WrittenBytes:         safeLongFromUint64(record.WrittenBytes, "cache generation written bytes"),
		ZFSSnapshotRef:       record.ZFSSnapshotRef,
	}
}

func cachePaths(paths []string) contractapi.SandboxCachePaths {
	out := make(contractapi.SandboxCachePaths, 0, len(paths))
	for _, path := range paths {
		out = append(out, contractapi.CachePath(path))
	}
	return out
}

func runLogSearchResult(record jobs.RunLogSearchResult) contractapi.SandboxRunLogSearchResult {
	return contractapi.SandboxRunLogSearchResult{
		ExecutionID:        contractapi.ExecutionID(record.ExecutionID.String()),
		AttemptID:          contractapi.AttemptID(record.AttemptID.String()),
		SourceKind:         optionalContractString[contractapi.SourceKind](record.SourceKind),
		WorkloadKind:       optionalContractString[contractapi.WorkloadKind](record.WorkloadKind),
		RunnerClass:        optionalContractString[contractapi.RunnerClass](record.RunnerClass),
		RepositoryFullName: optionalContractString[contractapi.RepositoryFullName](record.RepositoryFullName),
		WorkflowName:       optionalContractString[contractapi.WorkflowName](record.WorkflowName),
		JobName:            optionalContractString[contractapi.JobName](record.JobName),
		HeadBranch:         optionalContractString[contractapi.HeadBranch](record.HeadBranch),
		ScheduleID:         optionalContractString[contractapi.ScheduleID](record.ScheduleID),
		Seq:                safeLongFromUint32(record.Seq, "run log seq"),
		Stream:             contractapi.StreamName(record.Stream),
		Chunk:              contractapi.LogChunk(record.Chunk),
		CreatedAt:          timestamp(record.CreatedAt),
	}
}

func runLogSearchPage(page jobs.RunLogSearchPage, filters jobs.RunLogSearchFilters) contractapi.SandboxRunLogSearchPage {
	out := contractapi.SandboxRunLogSearchPage{
		Body: contractapi.SandboxRunLogSearchPageBody{
			Results: make(contractapi.SandboxRunLogSearchResults, 0, len(page.Results)),
			Limit:   contractapi.RunLogSearchPageSize(page.Limit),
			Filters: contractapi.SandboxRunLogSearchFilters{
				Query:       optionalContractString[contractapi.LogQuery](filters.Query),
				RunID:       optionalUUID[contractapi.RunID](filters.ExecutionID),
				AttemptID:   optionalUUID[contractapi.AttemptID](filters.AttemptID),
				SourceKind:  optionalContractString[contractapi.SourceKind](filters.SourceKind),
				Repository:  optionalContractString[contractapi.RepositoryFullName](filters.Repository),
				Workflow:    optionalContractString[contractapi.WorkflowName](filters.Workflow),
				Branch:      optionalContractString[contractapi.GitRef](filters.Branch),
				RunnerClass: optionalContractString[contractapi.RunnerClass](filters.RunnerClass),
			},
			NextCursor: optionalContractString[contractapi.RunLogSearchCursor](page.NextCursor),
		},
	}
	for _, record := range page.Results {
		out.Body.Results = append(out.Body.Results, runLogSearchResult(record))
	}
	return out
}

func analyticsBucket(bucket jobs.AnalyticsBucket) contractapi.SandboxAnalyticsBucket {
	return contractapi.SandboxAnalyticsBucket{
		Key:                 bucket.Key,
		Count:               decimalUint64(bucket.Count),
		ReservedChargeUnits: optionalDecimalUint64(bucket.ReservedChargeUnits),
		BilledChargeUnits:   optionalDecimalUint64(bucket.BilledChargeUnits),
		WriteoffChargeUnits: optionalDecimalUint64(bucket.WriteoffChargeUnits),
	}
}

func analyticsBuckets(buckets []jobs.AnalyticsBucket) contractapi.SandboxAnalyticsBuckets {
	out := make(contractapi.SandboxAnalyticsBuckets, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, analyticsBucket(bucket))
	}
	return out
}

func runDurationSample(sample jobs.RunDurationSample) contractapi.SandboxRunDurationSample {
	return contractapi.SandboxRunDurationSample{
		ExecutionID:        contractapi.ExecutionID(sample.ExecutionID.String()),
		Status:             contractapi.ExecutionStatus(sample.Status),
		RunnerClass:        optionalContractString[contractapi.RunnerClass](sample.RunnerClass),
		RepositoryFullName: optionalContractString[contractapi.RepositoryFullName](sample.RepositoryFullName),
		WorkflowName:       optionalContractString[contractapi.WorkflowName](sample.WorkflowName),
		JobName:            optionalContractString[contractapi.JobName](sample.JobName),
		DurationMs:         safeLong(sample.DurationMs, "duration ms"),
		CompletedAt:        timestamp(sample.CompletedAt),
	}
}

func jobsAnalytics(analytics jobs.JobsAnalytics) contractapi.SandboxJobsAnalytics {
	out := contractapi.SandboxJobsAnalytics{
		WindowStart:   timestamp(analytics.WindowStart),
		WindowEnd:     timestamp(analytics.WindowEnd),
		TotalRuns:     decimalUint64(analytics.TotalRuns),
		SucceededRuns: decimalUint64(analytics.SucceededRuns),
		FailedRuns:    decimalUint64(analytics.FailedRuns),
		P50DurationMs: decimalUint64(analytics.P50DurationMs),
		P95DurationMs: decimalUint64(analytics.P95DurationMs),
		P99DurationMs: decimalUint64(analytics.P99DurationMs),
		BySource:      analyticsBuckets(analytics.BySource),
		ByRunnerClass: analyticsBuckets(analytics.ByRunnerClass),
		SlowestRuns:   make(contractapi.SandboxRunDurationSamples, 0, len(analytics.SlowestRuns)),
	}
	for _, sample := range analytics.SlowestRuns {
		out.SlowestRuns = append(out.SlowestRuns, runDurationSample(sample))
	}
	return out
}

func costsAnalytics(analytics jobs.CostsAnalytics) contractapi.SandboxCostsAnalytics {
	return contractapi.SandboxCostsAnalytics{
		WindowStart:         timestamp(analytics.WindowStart),
		WindowEnd:           timestamp(analytics.WindowEnd),
		ReservedChargeUnits: decimalUint64(analytics.ReservedChargeUnits),
		BilledChargeUnits:   decimalUint64(analytics.BilledChargeUnits),
		WriteoffChargeUnits: decimalUint64(analytics.WriteoffChargeUnits),
		BySource:            analyticsBuckets(analytics.BySource),
		ByRunnerClass:       analyticsBuckets(analytics.ByRunnerClass),
		ByRepository:        analyticsBuckets(analytics.ByRepository),
	}
}

func runnerSizingAnalytics(analytics jobs.RunnerSizingAnalytics) contractapi.SandboxRunnerSizingAnalytics {
	out := contractapi.SandboxRunnerSizingAnalytics{
		WindowStart:   timestamp(analytics.WindowStart),
		WindowEnd:     timestamp(analytics.WindowEnd),
		ByRunnerClass: make(contractapi.SandboxRunnerSizingSamples, 0, len(analytics.ByRunnerClass)),
	}
	for _, sample := range analytics.ByRunnerClass {
		out.ByRunnerClass = append(out.ByRunnerClass, contractapi.SandboxRunnerSizingSample{
			RunnerClass:               contractapi.RunnerClass(sample.RunnerClass),
			RunCount:                  decimalUint64(sample.RunCount),
			P95DurationMs:             decimalUint64(sample.P95DurationMs),
			AvgRootfsProvisionedBytes: decimalUint64(sample.AvgRootfsProvisionedBytes),
			AvgBootTimeUs:             decimalUint64(sample.AvgBootTimeUs),
			AvgBlockWriteBytes:        decimalUint64(sample.AvgBlockWriteBytes),
			AvgNetTxBytes:             decimalUint64(sample.AvgNetTxBytes),
		})
	}
	return out
}

func executionScheduleCreateRequest(ctx context.Context, request contractapi.CreateExecutionScheduleInputBody) (recurring.CreateRequest, error) {
	projectID, err := uuid.Parse(string(request.ProjectID))
	if err != nil {
		return recurring.CreateRequest{}, badRequest(ctx, "invalid-project-id", "project_id must be a UUID", err)
	}
	sourceRepositoryID, err := uuid.Parse(string(request.SourceRepositoryID))
	if err != nil {
		return recurring.CreateRequest{}, badRequest(ctx, "invalid-source-repository-id", "source_repository_id must be a UUID", err)
	}
	intervalSeconds, err := uint32FromInt(int(request.IntervalSeconds), "interval_seconds")
	if err != nil {
		return recurring.CreateRequest{}, badRequest(ctx, "invalid-interval-seconds", "interval_seconds must fit uint32", err)
	}
	return recurring.CreateRequest{
		DisplayName:        stringFromPtr(request.DisplayName),
		IdempotencyKey:     string(request.IdempotencyKey),
		ProjectID:          projectID,
		SourceRepositoryID: sourceRepositoryID,
		WorkflowPath:       string(request.WorkflowPath),
		Ref:                stringFromPtr(request.Ref),
		Inputs:             stringMapFromPtr(request.Inputs),
		IntervalSeconds:    intervalSeconds,
		Paused:             boolFromPtr(request.Paused),
	}, nil
}

func executionScheduleRecord(record recurring.ScheduleRecord, installationID string) contractapi.SandboxExecutionScheduleRecord {
	orgID := record.OrgID
	projectID := record.ProjectID.String()
	return contractapi.SandboxExecutionScheduleRecord{
		ScheduleID:                   contractapi.ScheduleID(record.ScheduleID.String()),
		ResourceName:                 contractapi.ResourceName(resourceName(installationID, resourcePathSegment{"orgs", orgID}, resourcePathSegment{"schedules", record.ScheduleID.String()})),
		OrgID:                        contractapi.OrgID(orgID),
		ActorID:                      contractapi.ActorID(record.ActorID),
		DisplayName:                  optionalContractString[contractapi.DisplayName](record.DisplayName),
		IdempotencyKey:               optionalContractString[contractapi.IdempotencyKey](record.IdempotencyKey),
		TemporalScheduleID:           contractapi.TemporalID(record.TemporalScheduleID),
		TemporalNamespace:            contractapi.TemporalNamespace(record.TemporalNamespace),
		TaskQueue:                    contractapi.TaskQueue(record.TaskQueue),
		State:                        contractapi.ScheduleState(record.State),
		IntervalSeconds:              contractapi.IntervalSeconds(record.IntervalSeconds),
		ProjectID:                    contractapi.ProjectID(projectID),
		ProjectResourceName:          contractapi.ResourceName(resourceName(installationID, resourcePathSegment{"orgs", orgID}, resourcePathSegment{"projects", projectID})),
		SourceRepositoryID:           contractapi.SourceRepositoryID(record.SourceRepositoryID.String()),
		SourceRepositoryResourceName: contractapi.ResourceName(resourceName(installationID, resourcePathSegment{"orgs", orgID}, resourcePathSegment{"projects", projectID}, resourcePathSegment{"repositories", record.SourceRepositoryID.String()})),
		WorkflowPath:                 contractapi.WorkflowPath(record.WorkflowPath),
		Ref:                          optionalContractString[contractapi.GitRef](record.Ref),
		Inputs:                       optionalStringMap(record.Inputs),
		CreatedAt:                    timestamp(record.CreatedAt),
		UpdatedAt:                    timestamp(record.UpdatedAt),
		Dispatches:                   executionScheduleDispatches(record.Dispatches),
	}
}

func executionSchedulePage(page recurring.SchedulePage, installationID string) contractapi.SandboxExecutionSchedulesPage {
	out := contractapi.SandboxExecutionSchedulesPage{
		Body: contractapi.SandboxExecutionSchedulesPageBody{
			Schedules:  make(contractapi.SandboxExecutionSchedules, 0, len(page.Schedules)),
			NextCursor: optionalContractString[contractapi.ScheduleListCursor](page.NextCursor),
			Limit:      contractapi.ScheduleListPageSize(page.Limit),
		},
	}
	for _, record := range page.Schedules {
		out.Body.Schedules = append(out.Body.Schedules, executionScheduleRecord(record, installationID))
	}
	return out
}

func executionScheduleDispatches(records []recurring.DispatchRecord) *contractapi.SandboxExecutionScheduleDispatches {
	if len(records) == 0 {
		return nil
	}
	out := make(contractapi.SandboxExecutionScheduleDispatches, 0, len(records))
	for _, record := range records {
		out = append(out, executionScheduleDispatchRecord(record))
	}
	return &out
}

func executionScheduleDispatchRecord(record recurring.DispatchRecord) contractapi.SandboxExecutionScheduleDispatchRecord {
	return contractapi.SandboxExecutionScheduleDispatchRecord{
		DispatchID:          contractapi.DispatchID(record.DispatchID.String()),
		ScheduleID:          contractapi.ScheduleID(record.ScheduleID.String()),
		TemporalWorkflowID:  contractapi.TemporalID(record.TemporalWorkflowID),
		TemporalRunID:       contractapi.TemporalID(record.TemporalRunID),
		SourceWorkflowRunID: optionalUUIDPtr[contractapi.SourceWorkflowRunID](record.SourceWorkflowRunID),
		WorkflowState:       optionalContractString[contractapi.WorkflowState](record.WorkflowState),
		State:               contractapi.ScheduleState(record.State),
		FailureReason:       optionalContractString[contractapi.FailureReason](record.FailureReason),
		ScheduledAt:         timestamp(record.ScheduledAt),
		SubmittedAt:         timestampPtr(record.SubmittedAt),
		CreatedAt:           timestamp(record.CreatedAt),
		UpdatedAt:           timestamp(record.UpdatedAt),
	}
}

func resourceName(installationID string, segments ...resourcePathSegment) string {
	parts := make([]string, 0, len(segments)*2)
	for _, segment := range segments {
		parts = append(parts, strings.TrimSpace(segment.collection), strings.TrimSpace(segment.id))
	}
	return "urn:verself:" + strings.TrimSpace(installationID) + ":" + strings.Join(parts, "/")
}

func timestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func timestampPtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := timestamp(*value)
	return &formatted
}

func decimalUint64(value uint64) contractapi.DecimalUint64 {
	return contractapi.DecimalUint64(strconv.FormatUint(value, 10))
}

func optionalDecimalUint64(value uint64) *contractapi.DecimalUint64 {
	if value == 0 {
		return nil
	}
	out := decimalUint64(value)
	return &out
}

func optionalDecimalInt64(value int64) *contractapi.DecimalUint64 {
	if value == 0 {
		return nil
	}
	out := contractapi.DecimalUint64(strconv.FormatInt(value, 10))
	return &out
}

func optionalContractString[T ~string](value string) *T {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	out := T(value)
	return &out
}

func optionalUUID[T ~string](value uuid.UUID) *T {
	if value == uuid.Nil {
		return nil
	}
	out := T(value.String())
	return &out
}

func optionalUUIDPtr[T ~string](value *uuid.UUID) *T {
	if value == nil {
		return nil
	}
	return optionalUUID[T](*value)
}

func safeLong(value int64, field string) contractapi.SafeNonNegativeLong {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	return contractapi.SafeNonNegativeLong(value)
}

func optionalSafeLong(value int64, field string) *contractapi.SafeNonNegativeLong {
	if value == 0 {
		return nil
	}
	out := safeLong(value, field)
	return &out
}

func safeLongFromInt(value int, field string) contractapi.SafeNonNegativeLong {
	return safeLong(int64(value), field)
}

func safeLongFromUint32(value uint32, _ string) contractapi.SafeNonNegativeLong {
	return contractapi.SafeNonNegativeLong(value)
}

func safeLongFromUint64(value uint64, field string) contractapi.SafeNonNegativeLong {
	if value > uint64(1<<63-1) {
		panic(fmt.Sprintf("%s exceeds int64: %d", field, value))
	}
	return contractapi.SafeNonNegativeLong(int64(value)) // #nosec G115 guarded above.
}

func optionalSafeLongFromInt(value int, field string) *contractapi.SafeNonNegativeLong {
	if value == 0 {
		return nil
	}
	out := safeLongFromInt(value, field)
	return &out
}

func optionalSafeExitCode(value int) *contractapi.SafeExitCode {
	if value < 0 || value > 255 {
		panic(fmt.Sprintf("exit code out of range: %d", value))
	}
	out := contractapi.SafeExitCode(value)
	return &out
}

func uint32FromInt(value int, field string) (uint32, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s is negative: %d", field, value)
	}
	if uint64(value) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("%s exceeds uint32 range: %d", field, value)
	}
	return uint32(value), nil
}

func stringFromPtr[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func boolFromPtr(value *bool) bool {
	return value != nil && *value
}

func stringMapFromPtr(value *contractapi.ScheduleInputs) map[string]string {
	if value == nil {
		return nil
	}
	out := make(map[string]string, len(*value))
	for key, item := range *value {
		out[string(key)] = string(item)
	}
	return out
}

func optionalStringMap(value map[string]string) *contractapi.ScheduleInputs {
	if len(value) == 0 {
		return nil
	}
	out := make(contractapi.ScheduleInputs, len(value))
	for key, item := range value {
		out[key] = contractapi.ScheduleInputValue(item)
	}
	return &out
}

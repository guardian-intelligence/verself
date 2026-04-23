package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

type AnalyticsWindow struct {
	Start time.Time
	End   time.Time
}

type AnalyticsBucket struct {
	Key                 string
	Count               uint64
	ReservedChargeUnits uint64
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
}

type RunDurationSample struct {
	ExecutionID        uuid.UUID
	Status             string
	RunnerClass        string
	RepositoryFullName string
	WorkflowName       string
	JobName            string
	DurationMs         int64
	CompletedAt        time.Time
}

type JobsAnalytics struct {
	WindowStart   time.Time
	WindowEnd     time.Time
	TotalRuns     uint64
	SucceededRuns uint64
	FailedRuns    uint64
	P50DurationMs uint64
	P95DurationMs uint64
	P99DurationMs uint64
	BySource      []AnalyticsBucket
	ByRunnerClass []AnalyticsBucket
	SlowestRuns   []RunDurationSample
}

type CostsAnalytics struct {
	WindowStart         time.Time
	WindowEnd           time.Time
	ReservedChargeUnits uint64
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
	BySource            []AnalyticsBucket
	ByRunnerClass       []AnalyticsBucket
	ByRepository        []AnalyticsBucket
}

type CachesAnalytics struct {
	WindowStart         time.Time
	WindowEnd           time.Time
	CheckoutRequests    uint64
	CheckoutHits        uint64
	CheckoutMisses      uint64
	StickyRestoreHits   uint64
	StickyRestoreMisses uint64
	StickySaveRequests  uint64
	StickyCommits       uint64
	ByRepository        []AnalyticsBucket
}

type RunnerSizingSample struct {
	RunnerClass               string
	RunCount                  uint64
	P95DurationMs             uint64
	AvgRootfsProvisionedBytes uint64
	AvgBootTimeUs             uint64
	AvgBlockWriteBytes        uint64
	AvgNetTxBytes             uint64
}

type RunnerSizingAnalytics struct {
	WindowStart   time.Time
	WindowEnd     time.Time
	ByRunnerClass []RunnerSizingSample
}

func normalizeAnalyticsWindow(window AnalyticsWindow) AnalyticsWindow {
	if window.End.IsZero() {
		window.End = time.Now().UTC()
	}
	if window.Start.IsZero() {
		window.Start = window.End.Add(-24 * time.Hour)
	}
	window.Start = window.Start.UTC()
	window.End = window.End.UTC()
	return window
}

func (s *Service) GetJobsAnalytics(ctx context.Context, orgID uint64, window AnalyticsWindow) (JobsAnalytics, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.analytics.jobs")
	defer span.End()
	if s.CH == nil {
		return JobsAnalytics{}, fmt.Errorf("clickhouse is not configured")
	}
	window = normalizeAnalyticsWindow(window)
	analytics := JobsAnalytics{WindowStart: window.Start, WindowEnd: window.End}
	var p50Duration float32
	var p95Duration float32
	var p99Duration float32

	if err := s.CH.QueryRow(ctx, `
		SELECT
			count(),
			countIf(status = 'succeeded'),
			countIf(status = 'failed'),
			quantileTDigest(0.50)(duration_ms),
			quantileTDigest(0.95)(duration_ms),
			quantileTDigest(0.99)(duration_ms)
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
	`, orgID, window.Start, window.End).Scan(&analytics.TotalRuns, &analytics.SucceededRuns, &analytics.FailedRuns, &p50Duration, &p95Duration, &p99Duration); err != nil {
		return JobsAnalytics{}, fmt.Errorf("query jobs analytics summary: %w", err)
	}
	analytics.P50DurationMs = float64ToUint64(float64(p50Duration))
	analytics.P95DurationMs = float64ToUint64(float64(p95Duration))
	analytics.P99DurationMs = float64ToUint64(float64(p99Duration))
	var err error
	if analytics.BySource, err = s.queryAnalyticsBuckets(ctx, `
		SELECT source_kind AS key, toUInt64(count()) AS count, toUInt64(0) AS reserved_charge_units, toUInt64(0) AS billed_charge_units, toUInt64(0) AS writeoff_charge_units
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY source_kind
		ORDER BY count DESC, key ASC
	`, orgID, window); err != nil {
		return JobsAnalytics{}, err
	}
	if analytics.ByRunnerClass, err = s.queryAnalyticsBuckets(ctx, `
		SELECT runner_class AS key, toUInt64(count()) AS count, toUInt64(0) AS reserved_charge_units, toUInt64(0) AS billed_charge_units, toUInt64(0) AS writeoff_charge_units
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY runner_class
		ORDER BY count DESC, key ASC
	`, orgID, window); err != nil {
		return JobsAnalytics{}, err
	}

	rows, err := s.CH.Query(ctx, `
		SELECT execution_id, status, runner_class, repository_full_name, workflow_name, job_name, duration_ms, completed_at
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		ORDER BY duration_ms DESC, completed_at DESC
		LIMIT 10
	`, orgID, window.Start, window.End)
	if err != nil {
		return JobsAnalytics{}, fmt.Errorf("query slowest runs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sample RunDurationSample
		if err := rows.Scan(&sample.ExecutionID, &sample.Status, &sample.RunnerClass, &sample.RepositoryFullName, &sample.WorkflowName, &sample.JobName, &sample.DurationMs, &sample.CompletedAt); err != nil {
			return JobsAnalytics{}, fmt.Errorf("scan slowest runs: %w", err)
		}
		analytics.SlowestRuns = append(analytics.SlowestRuns, sample)
	}
	if err := rows.Err(); err != nil {
		return JobsAnalytics{}, fmt.Errorf("iterate slowest runs: %w", err)
	}
	span.SetAttributes(traceOrgID(orgID), attribute.Int("sandbox.analytics_bucket_count", len(analytics.ByRunnerClass)))
	return analytics, nil
}

func (s *Service) GetCostsAnalytics(ctx context.Context, orgID uint64, window AnalyticsWindow) (CostsAnalytics, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.analytics.costs")
	defer span.End()
	if s.CH == nil {
		return CostsAnalytics{}, fmt.Errorf("clickhouse is not configured")
	}
	window = normalizeAnalyticsWindow(window)
	analytics := CostsAnalytics{WindowStart: window.Start, WindowEnd: window.End}
	if err := s.CH.QueryRow(ctx, `
		SELECT
			COALESCE(sum(reserved_charge_units), 0),
			COALESCE(sum(billed_charge_units), 0),
			COALESCE(sum(writeoff_charge_units), 0)
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
	`, orgID, window.Start, window.End).Scan(&analytics.ReservedChargeUnits, &analytics.BilledChargeUnits, &analytics.WriteoffChargeUnits); err != nil {
		return CostsAnalytics{}, fmt.Errorf("query costs analytics summary: %w", err)
	}
	var err error
	if analytics.BySource, err = s.queryAnalyticsBuckets(ctx, `
		SELECT source_kind AS key, count() AS count, sum(reserved_charge_units) AS reserved_charge_units, sum(billed_charge_units) AS billed_charge_units, sum(writeoff_charge_units) AS writeoff_charge_units
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY source_kind
		ORDER BY billed_charge_units DESC, key ASC
	`, orgID, window); err != nil {
		return CostsAnalytics{}, err
	}
	if analytics.ByRunnerClass, err = s.queryAnalyticsBuckets(ctx, `
		SELECT runner_class AS key, count() AS count, sum(reserved_charge_units) AS reserved_charge_units, sum(billed_charge_units) AS billed_charge_units, sum(writeoff_charge_units) AS writeoff_charge_units
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		GROUP BY runner_class
		ORDER BY billed_charge_units DESC, key ASC
	`, orgID, window); err != nil {
		return CostsAnalytics{}, err
	}
	if analytics.ByRepository, err = s.queryAnalyticsBuckets(ctx, `
		SELECT repository_full_name AS key, count() AS count, sum(reserved_charge_units) AS reserved_charge_units, sum(billed_charge_units) AS billed_charge_units, sum(writeoff_charge_units) AS writeoff_charge_units
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		  AND repository_full_name != ''
		GROUP BY repository_full_name
		ORDER BY billed_charge_units DESC, key ASC
	`, orgID, window); err != nil {
		return CostsAnalytics{}, err
	}
	span.SetAttributes(traceOrgID(orgID), attribute.Int("sandbox.analytics_bucket_count", len(analytics.ByRepository)))
	return analytics, nil
}

func (s *Service) GetCachesAnalytics(ctx context.Context, orgID uint64, window AnalyticsWindow) (CachesAnalytics, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.analytics.caches")
	defer span.End()
	if s.CH == nil {
		return CachesAnalytics{}, fmt.Errorf("clickhouse is not configured")
	}
	window = normalizeAnalyticsWindow(window)
	analytics := CachesAnalytics{WindowStart: window.Start, WindowEnd: window.End}
	if err := s.CH.QueryRow(ctx, `
		SELECT
			countIf(event_name = 'github.checkout.bundle'),
			countIf(event_name = 'github.checkout.bundle' AND checkout_cache_hit = 1),
			countIf(event_name = 'github.checkout.bundle' AND checkout_cache_hit = 0),
			COALESCE(sumIf(sticky_restore_hit_count, event_name = 'github.stickydisk.compile'), 0),
			COALESCE(sumIf(sticky_restore_miss_count, event_name = 'github.stickydisk.compile'), 0),
			countIf(event_name = 'github.stickydisk.save_request'),
			countIf(event_name = 'github.stickydisk.commit_zfs' AND sticky_state = 'committed')
		FROM forge_metal.job_cache_events
		WHERE org_id = $1
		  AND event_time BETWEEN $2 AND $3
	`, orgID, window.Start, window.End).Scan(
		&analytics.CheckoutRequests,
		&analytics.CheckoutHits,
		&analytics.CheckoutMisses,
		&analytics.StickyRestoreHits,
		&analytics.StickyRestoreMisses,
		&analytics.StickySaveRequests,
		&analytics.StickyCommits,
	); err != nil {
		return CachesAnalytics{}, fmt.Errorf("query caches analytics summary: %w", err)
	}

	rows, err := s.CH.Query(ctx, `
		SELECT
			repository_full_name AS key,
			count() AS count
		FROM forge_metal.job_cache_events
		WHERE org_id = $1
		  AND event_time BETWEEN $2 AND $3
		  AND repository_full_name != ''
		GROUP BY key
		ORDER BY count DESC, key ASC
	`, orgID, window.Start, window.End)
	if err != nil {
		return CachesAnalytics{}, fmt.Errorf("query caches analytics by repository: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket AnalyticsBucket
		if err := rows.Scan(&bucket.Key, &bucket.Count); err != nil {
			return CachesAnalytics{}, fmt.Errorf("scan caches analytics by repository: %w", err)
		}
		analytics.ByRepository = append(analytics.ByRepository, bucket)
	}
	if err := rows.Err(); err != nil {
		return CachesAnalytics{}, fmt.Errorf("iterate caches analytics by repository: %w", err)
	}
	span.SetAttributes(traceOrgID(orgID), attribute.Int("sandbox.analytics_bucket_count", len(analytics.ByRepository)))
	return analytics, nil
}

func (s *Service) GetRunnerSizingAnalytics(ctx context.Context, orgID uint64, window AnalyticsWindow) (RunnerSizingAnalytics, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.analytics.runner_sizing")
	defer span.End()
	if s.CH == nil {
		return RunnerSizingAnalytics{}, fmt.Errorf("clickhouse is not configured")
	}
	window = normalizeAnalyticsWindow(window)
	analytics := RunnerSizingAnalytics{WindowStart: window.Start, WindowEnd: window.End}
	rows, err := s.CH.Query(ctx, `
		SELECT
			runner_class,
			count(),
			quantileTDigest(0.95)(duration_ms),
			avg(rootfs_provisioned_bytes),
			avg(boot_time_us),
			avg(block_write_bytes),
			avg(net_tx_bytes)
		FROM forge_metal.job_events
		WHERE org_id = $1
		  AND created_at BETWEEN $2 AND $3
		  AND runner_class != ''
		GROUP BY runner_class
		ORDER BY count() DESC, runner_class ASC
	`, orgID, window.Start, window.End)
	if err != nil {
		return RunnerSizingAnalytics{}, fmt.Errorf("query runner sizing analytics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sample RunnerSizingSample
		var p95Duration float32
		var avgRootfsProvisionedBytes float64
		var avgBootTimeUs float64
		var avgBlockWriteBytes float64
		var avgNetTxBytes float64
		if err := rows.Scan(&sample.RunnerClass, &sample.RunCount, &p95Duration, &avgRootfsProvisionedBytes, &avgBootTimeUs, &avgBlockWriteBytes, &avgNetTxBytes); err != nil {
			return RunnerSizingAnalytics{}, fmt.Errorf("scan runner sizing analytics: %w", err)
		}
		sample.P95DurationMs = float64ToUint64(float64(p95Duration))
		sample.AvgRootfsProvisionedBytes = float64ToUint64(avgRootfsProvisionedBytes)
		sample.AvgBootTimeUs = float64ToUint64(avgBootTimeUs)
		sample.AvgBlockWriteBytes = float64ToUint64(avgBlockWriteBytes)
		sample.AvgNetTxBytes = float64ToUint64(avgNetTxBytes)
		analytics.ByRunnerClass = append(analytics.ByRunnerClass, sample)
	}
	if err := rows.Err(); err != nil {
		return RunnerSizingAnalytics{}, fmt.Errorf("iterate runner sizing analytics: %w", err)
	}
	span.SetAttributes(traceOrgID(orgID), attribute.Int("sandbox.analytics_bucket_count", len(analytics.ByRunnerClass)))
	return analytics, nil
}

func float64ToUint64(value float64) uint64 {
	if value <= 0 || value != value {
		return 0
	}
	return uint64(value + 0.5)
}

func (s *Service) queryAnalyticsBuckets(ctx context.Context, query string, orgID uint64, window AnalyticsWindow) ([]AnalyticsBucket, error) {
	rows, err := s.CH.Query(ctx, query, orgID, window.Start, window.End)
	if err != nil {
		return nil, fmt.Errorf("query analytics buckets: %w", err)
	}
	defer rows.Close()
	out := []AnalyticsBucket{}
	for rows.Next() {
		var bucket AnalyticsBucket
		if err := rows.Scan(&bucket.Key, &bucket.Count, &bucket.ReservedChargeUnits, &bucket.BilledChargeUnits, &bucket.WriteoffChargeUnits); err != nil {
			return nil, fmt.Errorf("scan analytics bucket: %w", err)
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate analytics buckets: %w", err)
	}
	return out, nil
}

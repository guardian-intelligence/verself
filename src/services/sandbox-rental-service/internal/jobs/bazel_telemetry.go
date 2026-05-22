package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/verself/observability/bazeltelemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const maxBazelTelemetryRows = 250000

var (
	ErrBazelTelemetryInvalid      = errors.New("bazel telemetry request is invalid")
	ErrBazelTelemetryUnauthorized = errors.New("bazel telemetry request is not authorized")
	ErrBazelTelemetryUnavailable  = errors.New("bazel telemetry sink is unavailable")
)

type bazelInvocationRow struct {
	ObservedAt           time.Time `ch:"observed_at"`
	OrgID                string    `ch:"org_id"`
	Provider             string    `ch:"provider"`
	ProviderRepositoryID uint64    `ch:"provider_repository_id"`
	ProviderRunID        uint64    `ch:"provider_run_id"`
	ProviderJobID        uint64    `ch:"provider_job_id"`
	ExecutionID          uuid.UUID `ch:"execution_id"`
	AttemptID            uuid.UUID `ch:"attempt_id"`
	InvocationID         string    `ch:"invocation_id"`
	Command              string    `ch:"command"`
	Args                 []string  `ch:"args"`
	TargetPatterns       []string  `ch:"target_patterns"`
	WorkingDirectory     string    `ch:"working_directory"`
	GitHubWorkflow       string    `ch:"github_workflow"`
	GitHubJob            string    `ch:"github_job"`
	GitHubRef            string    `ch:"github_ref"`
	GitHubSHA            string    `ch:"github_sha"`
	ExitCode             int32     `ch:"exit_code"`
	DurationMs           int64     `ch:"duration_ms"`
	ProfileBytes         uint64    `ch:"profile_bytes"`
	BEPBytes             uint64    `ch:"bep_bytes"`
	ExecutionLogBytes    uint64    `ch:"execution_log_bytes"`
	ProfileSpanCount     uint32    `ch:"profile_span_count"`
	PackageCount         uint32    `ch:"package_count"`
	SpawnCount           uint32    `ch:"spawn_count"`
	TargetCount          uint32    `ch:"target_count"`
	FailedTargetCount    uint32    `ch:"failed_target_count"`
	StartedAt            time.Time `ch:"started_at"`
	CompletedAt          time.Time `ch:"completed_at"`
	TraceID              string    `ch:"trace_id"`
	SpanID               string    `ch:"span_id"`
}

type bazelEventRow struct {
	ObservedAt           time.Time `ch:"observed_at"`
	OrgID                string    `ch:"org_id"`
	Provider             string    `ch:"provider"`
	ProviderRepositoryID uint64    `ch:"provider_repository_id"`
	ProviderRunID        uint64    `ch:"provider_run_id"`
	ProviderJobID        uint64    `ch:"provider_job_id"`
	ExecutionID          uuid.UUID `ch:"execution_id"`
	AttemptID            uuid.UUID `ch:"attempt_id"`
	InvocationID         string    `ch:"invocation_id"`
	Command              string    `ch:"command"`
	EventName            string    `ch:"event_name"`
	Result               string    `ch:"result"`
	Reason               string    `ch:"reason"`
	ExitCode             int32     `ch:"exit_code"`
	DurationMs           int64     `ch:"duration_ms"`
	ProfileSpanCount     uint32    `ch:"profile_span_count"`
	PackageCount         uint32    `ch:"package_count"`
	SpawnCount           uint32    `ch:"spawn_count"`
	TargetCount          uint32    `ch:"target_count"`
	TraceID              string    `ch:"trace_id"`
	SpanID               string    `ch:"span_id"`
}

type bazelProfileSpanRow struct {
	ObservedAt           time.Time `ch:"observed_at"`
	OrgID                string    `ch:"org_id"`
	Provider             string    `ch:"provider"`
	ProviderRepositoryID uint64    `ch:"provider_repository_id"`
	ProviderRunID        uint64    `ch:"provider_run_id"`
	ProviderJobID        uint64    `ch:"provider_job_id"`
	ExecutionID          uuid.UUID `ch:"execution_id"`
	AttemptID            uuid.UUID `ch:"attempt_id"`
	InvocationID         string    `ch:"invocation_id"`
	Command              string    `ch:"command"`
	SpanKind             string    `ch:"span_kind"`
	Category             string    `ch:"category"`
	EventName            string    `ch:"event_name"`
	Package              string    `ch:"package_name"`
	BuildFile            string    `ch:"build_file"`
	ExternalRepo         string    `ch:"external_repo"`
	StartedAt            time.Time `ch:"started_at"`
	DurationMs           int64     `ch:"duration_ms"`
	TraceID              string    `ch:"trace_id"`
	SpanID               string    `ch:"span_id"`
}

type bazelSpawnRow struct {
	ObservedAt           time.Time `ch:"observed_at"`
	OrgID                string    `ch:"org_id"`
	Provider             string    `ch:"provider"`
	ProviderRepositoryID uint64    `ch:"provider_repository_id"`
	ProviderRunID        uint64    `ch:"provider_run_id"`
	ProviderJobID        uint64    `ch:"provider_job_id"`
	ExecutionID          uuid.UUID `ch:"execution_id"`
	AttemptID            uuid.UUID `ch:"attempt_id"`
	InvocationID         string    `ch:"invocation_id"`
	Command              string    `ch:"command"`
	TargetLabel          string    `ch:"target_label"`
	Package              string    `ch:"package_name"`
	BuildFile            string    `ch:"build_file"`
	RuleName             string    `ch:"rule_name"`
	Mnemonic             string    `ch:"mnemonic"`
	Runner               string    `ch:"runner"`
	CacheHit             uint8     `ch:"cache_hit"`
	ExitCode             int32     `ch:"exit_code"`
	Status               string    `ch:"status"`
	OutputCount          uint32    `ch:"output_count"`
	OutputFirst          string    `ch:"output_first"`
	StartedAt            time.Time `ch:"started_at"`
	DurationMs           int64     `ch:"duration_ms"`
	TraceID              string    `ch:"trace_id"`
	SpanID               string    `ch:"span_id"`
}

type bazelTargetRow struct {
	ObservedAt           time.Time `ch:"observed_at"`
	OrgID                string    `ch:"org_id"`
	Provider             string    `ch:"provider"`
	ProviderRepositoryID uint64    `ch:"provider_repository_id"`
	ProviderRunID        uint64    `ch:"provider_run_id"`
	ProviderJobID        uint64    `ch:"provider_job_id"`
	ExecutionID          uuid.UUID `ch:"execution_id"`
	AttemptID            uuid.UUID `ch:"attempt_id"`
	InvocationID         string    `ch:"invocation_id"`
	Command              string    `ch:"command"`
	Label                string    `ch:"label"`
	Package              string    `ch:"package_name"`
	BuildFile            string    `ch:"build_file"`
	RuleName             string    `ch:"rule_name"`
	Success              uint8     `ch:"success"`
	OutputGroupCount     uint32    `ch:"output_group_count"`
	OutputFileCount      uint32    `ch:"output_file_count"`
	TraceID              string    `ch:"trace_id"`
	SpanID               string    `ch:"span_id"`
}

func (s *Service) AuthenticateBazelTelemetry(ctx context.Context, executionID, attemptID, bearer string) (GitHubExecutionIdentity, error) {
	executionID = strings.TrimSpace(executionID)
	attemptID = strings.TrimSpace(attemptID)
	bearer = strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	execUUID, err := uuid.Parse(executionID)
	if err != nil {
		return GitHubExecutionIdentity{}, fmt.Errorf("%w: invalid execution id", ErrBazelTelemetryUnauthorized)
	}
	attemptUUID, err := uuid.Parse(attemptID)
	if err != nil {
		return GitHubExecutionIdentity{}, fmt.Errorf("%w: invalid attempt id", ErrBazelTelemetryUnauthorized)
	}
	return s.authenticateGitHubExecution(ctx, execUUID, attemptUUID, bearer, s.deriveScopedExecutionToken("verself-bazel-telemetry", execUUID, attemptUUID), ErrBazelTelemetryUnauthorized)
}

func (s *Service) RecordBazelTelemetry(ctx context.Context, identity GitHubExecutionIdentity, upload bazeltelemetry.Upload) error {
	ctx, span := tracer.Start(ctx, "bazel.telemetry.record")
	defer span.End()
	span.SetAttributes(
		attribute.String("bazel.invocation_id", upload.Invocation.InvocationID),
		attribute.String("bazel.command", upload.Invocation.Command),
		attribute.Int("bazel.profile_span_count", len(upload.ProfileSpans)),
		attribute.Int("bazel.spawn_count", len(upload.Spawns)),
		attribute.Int("bazel.target_count", len(upload.Targets)),
	)
	if err := validateBazelTelemetryUpload(upload); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if s.CH == nil {
		err := ErrBazelTelemetryUnavailable
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	spanContext := span.SpanContext()
	traceID := spanContext.TraceID().String()
	spanID := spanContext.SpanID().String()
	common := bazelTelemetryCommon{
		ObservedAt:           time.Now().UTC(),
		OrgID:                identity.OrgID,
		Provider:             RunnerProviderGitHub,
		ProviderRepositoryID: uint64FromNonNegativeInt64(identity.RepositoryID),
		ProviderRunID:        uint64FromNonNegativeInt64(identity.GitHubRunID),
		ProviderJobID:        uint64FromNonNegativeInt64(identity.GitHubJobID),
		ExecutionID:          identity.ExecutionID,
		AttemptID:            identity.AttemptID,
		InvocationID:         strings.TrimSpace(upload.Invocation.InvocationID),
		Command:              strings.TrimSpace(upload.Invocation.Command),
		TraceID:              traceID,
		SpanID:               spanID,
	}
	for _, event := range upload.Events {
		span.AddEvent(event.EventName, trace.WithAttributes(
			attribute.String("bazel.event_name", event.EventName),
			attribute.String("bazel.result", event.Result),
			attribute.String("bazel.reason", event.Reason),
		))
	}

	if err := s.insertBazelInvocation(ctx, common, upload); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := s.insertBazelProfileSpans(ctx, common, upload.ProfileSpans); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := s.insertBazelSpawns(ctx, common, upload.Spawns); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := s.insertBazelTargets(ctx, common, upload.Targets); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := s.insertBazelEvents(ctx, common, upload); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

type bazelTelemetryCommon struct {
	ObservedAt           time.Time
	OrgID                string
	Provider             string
	ProviderRepositoryID uint64
	ProviderRunID        uint64
	ProviderJobID        uint64
	ExecutionID          uuid.UUID
	AttemptID            uuid.UUID
	InvocationID         string
	Command              string
	TraceID              string
	SpanID               string
}

func validateBazelTelemetryUpload(upload bazeltelemetry.Upload) error {
	if upload.SchemaVersion != bazeltelemetry.SchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version %d", ErrBazelTelemetryInvalid, upload.SchemaVersion)
	}
	if strings.TrimSpace(upload.Invocation.InvocationID) == "" {
		return fmt.Errorf("%w: invocation_id is required", ErrBazelTelemetryInvalid)
	}
	switch strings.TrimSpace(upload.Invocation.Command) {
	case "build", "test":
	default:
		return fmt.Errorf("%w: command must be build or test", ErrBazelTelemetryInvalid)
	}
	if upload.Invocation.StartedAt.IsZero() || upload.Invocation.CompletedAt.IsZero() {
		return fmt.Errorf("%w: invocation timestamps are required", ErrBazelTelemetryInvalid)
	}
	rows := len(upload.Events) + len(upload.ProfileSpans) + len(upload.Spawns) + len(upload.Targets) + 1
	if rows > maxBazelTelemetryRows {
		return fmt.Errorf("%w: row count %d exceeds %d", ErrBazelTelemetryInvalid, rows, maxBazelTelemetryRows)
	}
	return nil
}

func (s *Service) insertBazelInvocation(ctx context.Context, common bazelTelemetryCommon, upload bazeltelemetry.Upload) error {
	inv := upload.Invocation
	row := bazelInvocationRow{
		ObservedAt:           common.ObservedAt,
		OrgID:                common.OrgID,
		Provider:             common.Provider,
		ProviderRepositoryID: common.ProviderRepositoryID,
		ProviderRunID:        common.ProviderRunID,
		ProviderJobID:        common.ProviderJobID,
		ExecutionID:          common.ExecutionID,
		AttemptID:            common.AttemptID,
		InvocationID:         common.InvocationID,
		Command:              common.Command,
		Args:                 nonNilStrings(inv.Args),
		TargetPatterns:       nonNilStrings(inv.TargetPatterns),
		WorkingDirectory:     inv.WorkingDirectory,
		GitHubWorkflow:       inv.GitHubWorkflow,
		GitHubJob:            inv.GitHubJob,
		GitHubRef:            inv.GitHubRef,
		GitHubSHA:            inv.GitHubSHA,
		ExitCode:             int32FromInt(inv.ExitCode, "bazel exit code"),
		DurationMs:           inv.DurationMs,
		ProfileBytes:         uint64FromNonNegativeInt64(inv.ProfileBytes),
		BEPBytes:             uint64FromNonNegativeInt64(inv.BEPBytes),
		ExecutionLogBytes:    uint64FromNonNegativeInt64(inv.ExecutionLogBytes),
		ProfileSpanCount:     uint32FromInt(len(upload.ProfileSpans), "bazel profile span count"),
		PackageCount:         uint32FromInt(countBazelPackages(upload.ProfileSpans), "bazel package count"),
		SpawnCount:           uint32FromInt(len(upload.Spawns), "bazel spawn count"),
		TargetCount:          uint32FromInt(len(upload.Targets), "bazel target count"),
		FailedTargetCount:    uint32FromInt(countFailedTargets(upload.Targets), "bazel failed target count"),
		StartedAt:            inv.StartedAt.UTC(),
		CompletedAt:          inv.CompletedAt.UTC(),
		TraceID:              common.TraceID,
		SpanID:               common.SpanID,
	}
	return appendBazelRows(ctx, s.CH, s.CHDatabase, "bazel_invocations", []bazelInvocationRow{row})
}

func (s *Service) insertBazelEvents(ctx context.Context, common bazelTelemetryCommon, upload bazeltelemetry.Upload) error {
	if len(upload.Events) == 0 {
		return nil
	}
	rows := make([]bazelEventRow, 0, len(upload.Events))
	for _, event := range upload.Events {
		observedAt := event.ObservedAt.UTC()
		if observedAt.IsZero() {
			observedAt = common.ObservedAt
		}
		rows = append(rows, bazelEventRow{
			ObservedAt:           observedAt,
			OrgID:                common.OrgID,
			Provider:             common.Provider,
			ProviderRepositoryID: common.ProviderRepositoryID,
			ProviderRunID:        common.ProviderRunID,
			ProviderJobID:        common.ProviderJobID,
			ExecutionID:          common.ExecutionID,
			AttemptID:            common.AttemptID,
			InvocationID:         common.InvocationID,
			Command:              common.Command,
			EventName:            strings.TrimSpace(event.EventName),
			Result:               strings.TrimSpace(event.Result),
			Reason:               truncateReason(event.Reason),
			ExitCode:             int32FromInt(upload.Invocation.ExitCode, "bazel exit code"),
			DurationMs:           event.DurationMs,
			ProfileSpanCount:     uint32FromInt(len(upload.ProfileSpans), "bazel profile span count"),
			PackageCount:         uint32FromInt(countBazelPackages(upload.ProfileSpans), "bazel package count"),
			SpawnCount:           uint32FromInt(len(upload.Spawns), "bazel spawn count"),
			TargetCount:          uint32FromInt(len(upload.Targets), "bazel target count"),
			TraceID:              common.TraceID,
			SpanID:               common.SpanID,
		})
	}
	return appendBazelRows(ctx, s.CH, s.CHDatabase, "bazel_events", rows)
}

func (s *Service) insertBazelProfileSpans(ctx context.Context, common bazelTelemetryCommon, spans []bazeltelemetry.ProfileSpan) error {
	if len(spans) == 0 {
		return nil
	}
	rows := make([]bazelProfileSpanRow, 0, len(spans))
	for _, span := range spans {
		rows = append(rows, bazelProfileSpanRow{
			ObservedAt:           common.ObservedAt,
			OrgID:                common.OrgID,
			Provider:             common.Provider,
			ProviderRepositoryID: common.ProviderRepositoryID,
			ProviderRunID:        common.ProviderRunID,
			ProviderJobID:        common.ProviderJobID,
			ExecutionID:          common.ExecutionID,
			AttemptID:            common.AttemptID,
			InvocationID:         common.InvocationID,
			Command:              common.Command,
			SpanKind:             strings.TrimSpace(span.SpanKind),
			Category:             strings.TrimSpace(span.Category),
			EventName:            strings.TrimSpace(span.EventName),
			Package:              strings.TrimSpace(span.Package),
			BuildFile:            strings.TrimSpace(span.BuildFile),
			ExternalRepo:         strings.TrimSpace(span.ExternalRepo),
			StartedAt:            span.StartedAt.UTC(),
			DurationMs:           span.DurationMs,
			TraceID:              common.TraceID,
			SpanID:               common.SpanID,
		})
	}
	return appendBazelRows(ctx, s.CH, s.CHDatabase, "bazel_profile_spans", rows)
}

func (s *Service) insertBazelSpawns(ctx context.Context, common bazelTelemetryCommon, spawns []bazeltelemetry.Spawn) error {
	if len(spawns) == 0 {
		return nil
	}
	rows := make([]bazelSpawnRow, 0, len(spawns))
	for _, spawn := range spawns {
		rows = append(rows, bazelSpawnRow{
			ObservedAt:           common.ObservedAt,
			OrgID:                common.OrgID,
			Provider:             common.Provider,
			ProviderRepositoryID: common.ProviderRepositoryID,
			ProviderRunID:        common.ProviderRunID,
			ProviderJobID:        common.ProviderJobID,
			ExecutionID:          common.ExecutionID,
			AttemptID:            common.AttemptID,
			InvocationID:         common.InvocationID,
			Command:              common.Command,
			TargetLabel:          strings.TrimSpace(spawn.TargetLabel),
			Package:              strings.TrimSpace(spawn.Package),
			BuildFile:            strings.TrimSpace(spawn.BuildFile),
			RuleName:             strings.TrimSpace(spawn.RuleName),
			Mnemonic:             strings.TrimSpace(spawn.Mnemonic),
			Runner:               strings.TrimSpace(spawn.Runner),
			CacheHit:             boolUInt8(spawn.CacheHit),
			ExitCode:             spawn.ExitCode,
			Status:               strings.TrimSpace(spawn.Status),
			OutputCount:          uint32FromInt(spawn.OutputCount, "bazel spawn output count"),
			OutputFirst:          strings.TrimSpace(spawn.OutputFirst),
			StartedAt:            spawn.StartedAt.UTC(),
			DurationMs:           spawn.DurationMs,
			TraceID:              common.TraceID,
			SpanID:               common.SpanID,
		})
	}
	return appendBazelRows(ctx, s.CH, s.CHDatabase, "bazel_spawns", rows)
}

func (s *Service) insertBazelTargets(ctx context.Context, common bazelTelemetryCommon, targets []bazeltelemetry.Target) error {
	if len(targets) == 0 {
		return nil
	}
	rows := make([]bazelTargetRow, 0, len(targets))
	for _, target := range targets {
		rows = append(rows, bazelTargetRow{
			ObservedAt:           common.ObservedAt,
			OrgID:                common.OrgID,
			Provider:             common.Provider,
			ProviderRepositoryID: common.ProviderRepositoryID,
			ProviderRunID:        common.ProviderRunID,
			ProviderJobID:        common.ProviderJobID,
			ExecutionID:          common.ExecutionID,
			AttemptID:            common.AttemptID,
			InvocationID:         common.InvocationID,
			Command:              common.Command,
			Label:                strings.TrimSpace(target.Label),
			Package:              strings.TrimSpace(target.Package),
			BuildFile:            strings.TrimSpace(target.BuildFile),
			RuleName:             strings.TrimSpace(target.RuleName),
			Success:              boolUInt8(target.Success),
			OutputGroupCount:     uint32FromInt(target.OutputGroupCount, "bazel target output group count"),
			OutputFileCount:      uint32FromInt(target.OutputFileCount, "bazel target output file count"),
			TraceID:              common.TraceID,
			SpanID:               common.SpanID,
		})
	}
	return appendBazelRows(ctx, s.CH, s.CHDatabase, "bazel_targets", rows)
}

func appendBazelRows[T any](ctx context.Context, ch driver.Conn, database, table string, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := ch.PrepareBatch(ctx, "INSERT INTO "+database+"."+table)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := batch.AppendStruct(&rows[i]); err != nil {
			return err
		}
	}
	return batch.Send()
}

func countBazelPackages(spans []bazeltelemetry.ProfileSpan) int {
	count := 0
	for _, span := range spans {
		if span.SpanKind == "package" {
			count++
		}
	}
	return count
}

func countFailedTargets(targets []bazeltelemetry.Target) int {
	count := 0
	for _, target := range targets {
		if !target.Success {
			count++
		}
	}
	return count
}

func boolUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func uint32FromInt(value int, field string) uint32 {
	if value < 0 {
		panic(fmt.Sprintf("%s is negative: %d", field, value))
	}
	if uint64(value) > uint64(^uint32(0)) {
		panic(fmt.Sprintf("%s exceeds uint32 range: %d", field, value))
	}
	return uint32(value) // #nosec G115 -- value is checked against uint32 range above.
}

func truncateReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 4096 {
		return reason
	}
	return reason[:4096]
}

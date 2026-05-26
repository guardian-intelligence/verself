package distribution

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type event struct {
	RecordedAt     time.Time `ch:"recorded_at"`
	EventDate      time.Time `ch:"event_date"`
	OccurredAt     time.Time `ch:"occurred_at"`
	EventType      string    `ch:"event_type"`
	Decision       string    `ch:"decision"`
	PackageName    string    `ch:"package_name"`
	ChannelName    string    `ch:"channel_name"`
	PlatformOS     string    `ch:"platform_os"`
	PlatformArch   string    `ch:"platform_arch"`
	ArtifactDigest string    `ch:"artifact_digest"`
	Actor          string    `ch:"actor"`
	Reason         string    `ch:"reason"`
	RequestID      string    `ch:"request_id"`
	TraceID        string    `ch:"trace_id"`
	SpanID         string    `ch:"span_id"`
}

func (s *Service) emit(ctx context.Context, row event) {
	if s == nil || s.CH == nil {
		return
	}
	now := s.now()
	row.RecordedAt = now
	row.EventDate = now
	if row.OccurredAt.IsZero() {
		row.OccurredAt = now
	}
	if row.Actor == "" {
		row.Actor = "unknown"
	}
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		row.TraceID = spanContext.TraceID().String()
		row.SpanID = spanContext.SpanID().String()
	}
	insertCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	batch, err := s.CH.PrepareBatch(insertCtx, "INSERT INTO verself.distribution_events")
	if err != nil {
		s.log().WarnContext(ctx, "distribution clickhouse batch failed", "event_type", row.EventType, "error", err)
		return
	}
	if err := batch.AppendStruct(&row); err != nil {
		s.log().WarnContext(ctx, "distribution clickhouse append failed", "event_type", row.EventType, "error", err)
		return
	}
	if err := batch.Send(); err != nil {
		s.log().WarnContext(ctx, "distribution clickhouse insert failed", "event_type", row.EventType, "error", err)
	}
}

package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
)

type Service struct {
	CH              clickhouse.Conn
	AllowedPrefixes []string
}

func (s *Service) Ready(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("analytics service is nil")
	}
	if s.CH == nil {
		return fmt.Errorf("analytics clickhouse connection is nil")
	}
	if len(s.AllowedPrefixes) == 0 {
		return fmt.Errorf("analytics allowed event prefixes are empty")
	}
	if err := s.CH.Ping(ctx); err != nil {
		return fmt.Errorf("analytics clickhouse ping: %w", err)
	}
	return nil
}

func (s *Service) IngestLogs(ctx context.Context, source Source, req *logspb.ExportLogsServiceRequest) (int, error) {
	if s == nil || s.CH == nil {
		return 0, fmt.Errorf("analytics clickhouse connection unavailable")
	}
	rows, err := RowsFromLogs(source, req, s.AllowedPrefixes, time.Now())
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.analytics_events")
	if err != nil {
		return 0, fmt.Errorf("prepare analytics events batch: %w", err)
	}
	for i := range rows {
		if err := appendAnalyticsEvent(batch, &rows[i]); err != nil {
			return 0, err
		}
	}
	if err := batch.Send(); err != nil {
		return 0, fmt.Errorf("send analytics events batch: %w", err)
	}
	return len(rows), nil
}

func appendAnalyticsEvent(batch chdriver.Batch, row *EventRow) error {
	if err := batch.AppendStruct(row); err != nil {
		return fmt.Errorf("append analytics event: %w", err)
	}
	return nil
}

func ParseAllowedPrefixes(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

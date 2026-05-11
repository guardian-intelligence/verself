package analytics

import (
	"testing"
	"time"

	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logrecordpb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestRowsFromLogsPromotesBuildLabels(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	req := &logspb.ExportLogsServiceRequest{
		ResourceLogs: []*logrecordpb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					stringAttr("service.name", "verself-build-telemetry"),
					stringAttr("service.version", "0.1.0"),
				},
			},
			ScopeLogs: []*logrecordpb.ScopeLogs{{
				LogRecords: []*logrecordpb.LogRecord{{
					TimeUnixNano: uint64(now.UnixNano()),
					Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "build.typecheck"}},
					Attributes: []*commonpb.KeyValue{
						stringAttr("build.tool", "typescript"),
						stringAttr("build.package", "@verself/brand"),
						stringAttr("typescript.tsconfig_path", "packages/brand/tsconfig.json"),
						stringAttr("typescript.tsbuildinfo.result", "hit"),
						intAttr("duration_ms", 42),
					},
				}},
			}},
		}},
	}
	rows, err := RowsFromLogs(Source{
		Kind:            SourceKindGitHubActionsOIDC,
		Subject:         "repo:guardian-intelligence/verself:ref:refs/heads/main",
		TenantID:        "github:guardian-intelligence",
		Repository:      "guardian-intelligence/verself",
		RepositoryOwner: "guardian-intelligence",
		RunID:           "1",
		RunAttempt:      2,
	}, req, []string{"build."}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.EventName != "build.typecheck" {
		t.Fatalf("EventName = %q", row.EventName)
	}
	if row.BuildTool != "typescript" || row.BuildPackage != "@verself/brand" {
		t.Fatalf("promoted build labels = %q %q", row.BuildTool, row.BuildPackage)
	}
	if row.ConfigPath != "packages/brand/tsconfig.json" {
		t.Fatalf("ConfigPath = %q", row.ConfigPath)
	}
	if row.CacheResult != "hit" {
		t.Fatalf("CacheResult = %q", row.CacheResult)
	}
	if row.DurationMs != 42 {
		t.Fatalf("DurationMs = %d", row.DurationMs)
	}
}

func stringAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	}
}

func intAttr(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: value},
		},
	}
}

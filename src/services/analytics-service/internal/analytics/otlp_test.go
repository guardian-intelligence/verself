package analytics

import (
	"testing"
	"time"

	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logrecordpb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestRowsFromLogsStampsDatasetAndSourceAttributes(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	req := &logspb.ExportLogsServiceRequest{
		ResourceLogs: []*logrecordpb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					stringAttr("service.name", "verself-build-tools"),
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
						stringAttr("build.config_path", "packages/brand/tsconfig.json"),
						stringAttr("cache.result", "hit"),
						intAttr("duration_ms", 42),
					},
				}},
			}},
		}},
	}
	rows, err := RowsFromLogs(DatasetContext{
		OrgID:       "org_B7HWGKW0SH7G4EXW9XT8TCT60C",
		ProjectID:   "verself",
		DatasetID:   "build",
		Environment: "prod",
	}, Source{
		Kind:            SourceKindGitHubActionsOIDC,
		Subject:         "repo:guardian-intelligence/verself:ref:refs/heads/main",
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
	if row.OrgID != "org_B7HWGKW0SH7G4EXW9XT8TCT60C" || row.ProjectID != "verself" || row.DatasetID != "build" || row.Environment != "prod" {
		t.Fatalf("dataset identity = %q/%q/%q/%q", row.OrgID, row.ProjectID, row.DatasetID, row.Environment)
	}
	if row.EventName != "build.typecheck" {
		t.Fatalf("EventName = %q", row.EventName)
	}
	if row.SignalKind != SignalKindLog {
		t.Fatalf("SignalKind = %q", row.SignalKind)
	}
	if row.StringAttributes["build.tool"] != "typescript" || row.StringAttributes["build.package"] != "@verself/brand" {
		t.Fatalf("build attributes = %q %q", row.StringAttributes["build.tool"], row.StringAttributes["build.package"])
	}
	if row.StringAttributes["github.repository"] != "guardian-intelligence/verself" {
		t.Fatalf("github.repository = %q", row.StringAttributes["github.repository"])
	}
	if row.DurationMs != 42 {
		t.Fatalf("DurationMs = %d", row.DurationMs)
	}
}

func TestRowsFromLogsRedactsBearerMaterial(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	req := &logspb.ExportLogsServiceRequest{
		ResourceLogs: []*logrecordpb.ResourceLogs{{
			ScopeLogs: []*logrecordpb.ScopeLogs{{
				LogRecords: []*logrecordpb.LogRecord{{
					TimeUnixNano: uint64(now.UnixNano()),
					Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "Bearer secret-token build.secret"}},
					Attributes: []*commonpb.KeyValue{
						stringAttr("event.name", "build.secret"),
						stringAttr("authorization", "Bearer secret-token"),
					},
				}},
			}},
		}},
	}
	rows, err := RowsFromLogs(DatasetContext{OrgID: "org", ProjectID: "project", DatasetID: "build", Environment: "prod"}, Source{Kind: SourceKindGitHubActionsOIDC}, req, []string{"build."}, now)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.BodyRedactionStatus != "redacted" {
		t.Fatalf("BodyRedactionStatus = %q", row.BodyRedactionStatus)
	}
	if row.Body == "Bearer secret-token build.secret" || row.StringAttributes["authorization"] == "Bearer secret-token" {
		t.Fatalf("secret material was not redacted")
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

package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGovernanceAuditAndExportAPI(t *testing.T) {
	const exportID = "11111111-1111-1111-1111-111111111111"
	const traceparent = "00-11111111111111111111111111111111-2222222222222222-01"
	var createBody map[string]any
	var createKey string
	exportJSON := `{"export_id":"` + exportID + `","org_id":"370200542594579812","requested_by":"user_1","scopes":["audit"],"include_logs":true,"format":"tar.gz","state":"ready","artifact_sha256":"sha256","artifact_bytes":"10","download_url":"/api/v1/governance/exports/` + exportID + `/download","files":[{"path":"audit/audit_events.jsonl","content_type":"application/jsonl","rows":"1","bytes":"10","sha256":"file_sha256"}],"created_at":"2026-05-07T00:00:00Z","updated_at":"2026-05-07T00:00:01Z","completed_at":"2026-05-07T00:00:01Z","expires_at":"2026-05-08T00:00:00Z"}`
	auditEventJSON := `{"action":"governance.data_export.create","actor_id":"user_1","actor_type":"user","audit_event":"governance.data_export.create","content_sha256":"hash","decision":"allowed","event_category":"data_export","event_id":"22222222-2222-2222-2222-222222222222","operation_display":"Create data export","operation_id":"create-data-export","operation_type":"export","org_id":"370200542594579812","org_scope":"same_org","permission":"governance.exports.write","prev_hmac":"prev","recorded_at":"2026-05-07T00:00:01Z","result":"allowed","risk_level":"high","row_hmac":"row","sequence":"1","service_name":"governance-service","source_product_area":"Governance","target_id":"` + exportID + `","target_kind":"data_export","trace_id":"11111111111111111111111111111111"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok_governance" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("Traceparent") != traceparent {
			t.Fatalf("%s %s Traceparent = %q", r.Method, r.URL.Path, r.Header.Get("Traceparent"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/audit/events":
			if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("high_risk") != "true" || r.URL.Query().Get("operation_type") != "export" {
				t.Fatalf("audit query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"events":[` + auditEventJSON + `],"filters":{"high_risk":true,"operation_type":"export"},"limit":2,"next_cursor":"cursor_1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/exports":
			_, _ = w.Write([]byte(`{"exports":[` + exportJSON + `]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/governance/exports":
			createKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(exportJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/exports/"+exportID:
			_, _ = w.Write([]byte(exportJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/governance/exports/"+exportID+"/download":
			if !strings.Contains(r.Header.Get("Accept"), "application/gzip") {
				t.Fatalf("download Accept = %q", r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Content-Disposition", `attachment; filename="verself-export.tar.gz"`)
			_, _ = w.Write([]byte("gzip-bytes"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_governance", GovernanceURL: server.URL, Traceparent: traceparent})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Governance.ListAuditEvents(context.Background(), ListGovernanceAuditEventsOptions{
		Limit:         2,
		HighRisk:      true,
		OperationType: GovernanceOperationTypeExport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if events.NextCursor != "cursor_1" || len(events.Events) != 1 || events.Events[0].AuditEvent != "governance.data_export.create" {
		t.Fatalf("unexpected events: %#v", events)
	}
	export, err := client.Governance.CreateDataExport(context.Background(), CreateGovernanceExportInput{
		Scopes:         []GovernanceExportScope{GovernanceExportScopeAudit},
		IncludeLogs:    true,
		IdempotencyKey: "governance-export:test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if createKey != "governance-export:test" || createBody["include_logs"] != true || export.ArtifactBytes != 10 {
		t.Fatalf("unexpected create request/export: %q %#v %#v", createKey, createBody, export)
	}
	if scopes, ok := createBody["scopes"].([]any); !ok || len(scopes) != 1 || scopes[0] != "audit" {
		t.Fatalf("unexpected scopes: %#v", createBody)
	}
	exports, err := client.Governance.ListDataExports(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(exports) != 1 || exports[0].ExportID != exportID {
		t.Fatalf("unexpected exports: %#v", exports)
	}
	artifact, err := client.Governance.DownloadDataExport(context.Background(), exportID)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.FileName != "verself-export.tar.gz" || artifact.ContentType != "application/gzip" || string(artifact.Body) != "gzip-bytes" {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
}

func TestGovernanceAPIErrorsNormalizeProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:governance:forbidden","title":"Forbidden","status":403,"detail":"audit log denied"}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_governance", GovernanceURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Governance.ListAuditEvents(context.Background(), ListGovernanceAuditEventsOptions{})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.Service != "Governance API" || apiErr.StatusCode != http.StatusForbidden || apiErr.Detail != "audit log denied" {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

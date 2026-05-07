package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSandboxRunsAndSchedulesUsePublicAPI(t *testing.T) {
	const executionID = "11111111-1111-1111-1111-111111111111"
	const runID = "22222222-2222-2222-2222-222222222222"
	const projectID = "33333333-3333-3333-3333-333333333333"
	const repoID = "44444444-4444-4444-4444-444444444444"
	const scheduleID = "55555555-5555-5555-5555-555555555555"
	const traceparent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	runJSON := `{"execution_id":"` + executionID + `","run_id":"` + runID + `","org_id":"370200542594579812","actor_id":"user_1","product_id":"sandbox-ci","kind":"ci","status":"succeeded","source_kind":"github","workload_kind":"github_actions","runner_class":"linux-2vcpu","latest_attempt":{"attempt_id":"attempt_1","attempt_seq":1,"state":"succeeded","trace_id":"trace_1","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:01:00Z"},"created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:01:00Z"}`
	scheduleJSON := `{"schedule_id":"` + scheduleID + `","org_id":"370200542594579812","project_id":"` + projectID + `","source_repository_id":"` + repoID + `","actor_id":"user_1","display_name":"Nightly","workflow_path":".github/workflows/build.yml","ref":"main","inputs":{"target":"linux"},"interval_seconds":900,"state":"active","task_queue":"sandbox-recurring","temporal_namespace":"default","temporal_schedule_id":"verself-schedule","created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	var createBody map[string]any
	var pauseKey string
	var resumeKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_sandbox" {
			t.Fatalf("%s %s Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("Traceparent") != traceparent {
			t.Fatalf("%s %s Traceparent = %q", r.Method, r.URL.Path, r.Header.Get("Traceparent"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/runs":
			if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("status") != "succeeded" {
				t.Fatalf("runs query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"filters":{"status":"succeeded"},"limit":2,"next_cursor":"cursor_2","runs":[` + runJSON + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/executions/"+executionID:
			_, _ = w.Write([]byte(runJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/executions/"+executionID+"/logs":
			_, _ = w.Write([]byte(`{"execution_id":"` + executionID + `","attempt_id":"attempt_1","logs":"build log\n"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/execution-schedules":
			_, _ = w.Write([]byte(`[` + scheduleJSON + `]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execution-schedules":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/execution-schedules/"+scheduleID:
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execution-schedules/"+scheduleID+"/pause":
			pauseKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(scheduleJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execution-schedules/"+scheduleID+"/resume":
			resumeKey = r.Header.Get("Idempotency-Key")
			_, _ = w.Write([]byte(scheduleJSON))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_sandbox", SandboxURL: server.URL, Traceparent: traceparent})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Sandbox.ListRuns(context.Background(), ListSandboxRunsOptions{Limit: 2, Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Runs) != 1 || page.Runs[0].ExecutionID != executionID || page.NextCursor != "cursor_2" {
		t.Fatalf("unexpected runs page: %#v", page)
	}
	run, err := client.Sandbox.GetExecution(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	if run.LatestAttempt.TraceID == nil || *run.LatestAttempt.TraceID != "trace_1" {
		t.Fatalf("unexpected execution: %#v", run)
	}
	logs, err := client.Sandbox.GetExecutionLogs(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	if logs.Logs != "build log\n" {
		t.Fatalf("unexpected logs: %#v", logs)
	}
	schedules, err := client.Sandbox.ListSchedules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules.Schedules) != 1 || schedules.Schedules[0].ScheduleID != scheduleID {
		t.Fatalf("unexpected schedules: %#v", schedules)
	}
	created, err := client.Sandbox.CreateSchedule(context.Background(), CreateSandboxExecutionScheduleInput{
		ProjectID:          projectID,
		SourceRepositoryID: repoID,
		WorkflowPath:       ".github/workflows/build.yml",
		IntervalSeconds:    900,
		DisplayName:        "Nightly",
		Ref:                "main",
		Paused:             true,
		Inputs:             map[string]string{"target": "linux"},
		IdempotencyKey:     "sandbox:schedule",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ScheduleID != scheduleID {
		t.Fatalf("unexpected created schedule: %#v", created)
	}
	if createBody["idempotency_key"] != "sandbox:schedule" || createBody["project_id"] != projectID || createBody["source_repository_id"] != repoID || createBody["workflow_path"] != ".github/workflows/build.yml" || createBody["paused"] != true {
		t.Fatalf("unexpected create body: %#v", createBody)
	}
	inputs, ok := createBody["inputs"].(map[string]any)
	if !ok || inputs["target"] != "linux" {
		t.Fatalf("unexpected create inputs: %#v", createBody)
	}
	got, err := client.Sandbox.GetSchedule(context.Background(), scheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != projectID {
		t.Fatalf("unexpected schedule: %#v", got)
	}
	if _, err := client.Sandbox.PauseSchedule(context.Background(), scheduleID, SandboxMutationOptions{IdempotencyKey: "sandbox:pause"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Sandbox.ResumeSchedule(context.Background(), scheduleID, SandboxMutationOptions{IdempotencyKey: "sandbox:resume"}); err != nil {
		t.Fatal(err)
	}
	if pauseKey != "sandbox:pause" || resumeKey != "sandbox:resume" {
		t.Fatalf("unexpected schedule lifecycle keys: pause=%q resume=%q", pauseKey, resumeKey)
	}
}

func TestSandboxBillingReadsUsePublicAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/entitlements":
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","universal":{"scope_type":"account","product_id":"","product_display":"","bucket_id":"","bucket_display":"","sku_id":"","sku_display":"","coverage_label":"Account","available_units":"100","pending_units":"0","period_start_units":"100","spent_units":"0","sources":[]},"products":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/contracts":
			_, _ = w.Write([]byte(`{"contracts":[{"contract_id":"contract_1","product_id":"sandbox-ci","plan_id":"ci-pro","cadence_kind":"monthly","status":"active","payment_state":"current","entitlement_state":"active","phase_id":"phase_1","starts_at":"2026-05-06T00:00:00Z"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/plans":
			_, _ = w.Write([]byte(`{"plans":[{"plan_id":"ci-pro","product_id":"sandbox-ci","display_name":"CI Pro","tier":"pro","billing_mode":"subscription","currency":"USD","monthly_amount_cents":"9900","annual_amount_cents":"99000","active":true,"is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/billing/statement":
			if r.URL.Query().Get("product_id") != "sandbox-ci" {
				t.Fatalf("statement query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"org_id":"370200542594579812","product_id":"sandbox-ci","period_source":"current","period_start":"2026-05-01T00:00:00Z","period_end":"2026-06-01T00:00:00Z","generated_at":"2026-05-06T00:00:00Z","currency":"USD","unit_label":"credits","totals":{"reserved_units":"0","contract_units":"100","free_tier_units":"0","promo_units":"0","purchase_units":"0","receivable_units":"0","refund_units":"0","charge_units":"5","total_due_units":"0"},"grant_summaries":[],"line_items":[]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_sandbox", SandboxURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	entitlements, err := client.Sandbox.GetEntitlements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if entitlements.Universal.AvailableUnits != "100" {
		t.Fatalf("unexpected entitlements: %#v", entitlements)
	}
	contracts, err := client.Sandbox.ListContracts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts.Contracts) != 1 || contracts.Contracts[0].ContractID != "contract_1" {
		t.Fatalf("unexpected contracts: %#v", contracts)
	}
	plans, err := client.Sandbox.ListPlans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(plans.Plans) != 1 || plans.Plans[0].PlanID != "ci-pro" {
		t.Fatalf("unexpected plans: %#v", plans)
	}
	statement, err := client.Sandbox.GetStatement(context.Background(), SandboxStatementOptions{ProductID: "sandbox-ci"})
	if err != nil {
		t.Fatal(err)
	}
	if statement.Totals.TotalDueUnits != "0" {
		t.Fatalf("unexpected statement: %#v", statement)
	}
}

func TestSandboxErrorsNormalizeProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:sandbox:unauthorized","title":"Unauthorized","status":401,"detail":"Missing bearer token."}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_sandbox", SandboxURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Sandbox.ListRuns(context.Background(), ListSandboxRunsOptions{})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Title != "Unauthorized" || !strings.Contains(apiErr.Error(), "Missing bearer token") {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

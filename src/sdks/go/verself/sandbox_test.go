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
	analyticsWindow := `{"window_start":"2026-05-06T00:00:00Z","window_end":"2026-05-07T00:00:00Z"`
	jobsAnalyticsJSON := analyticsWindow + `,"total_runs":"1","succeeded_runs":"1","failed_runs":"0","p50_duration_ms":"1000","p95_duration_ms":"1000","p99_duration_ms":"1000","by_source":[{"key":"github","count":"1"}],"by_runner_class":[{"key":"linux-2vcpu","count":"1"}],"slowest_runs":[{"execution_id":"` + executionID + `","status":"succeeded","duration_ms":1000,"completed_at":"2026-05-06T00:01:00Z"}]}`
	costsAnalyticsJSON := analyticsWindow + `,"reserved_charge_units":"10","billed_charge_units":"9","writeoff_charge_units":"1","by_source":[{"key":"github","count":"1","reserved_charge_units":"10","billed_charge_units":"9","writeoff_charge_units":"1"}],"by_runner_class":[],"by_repository":[]}`
	runnerSizingAnalyticsJSON := analyticsWindow + `,"by_runner_class":[{"runner_class":"linux-2vcpu","run_count":"1","p95_duration_ms":"1000","avg_rootfs_provisioned_bytes":"1","avg_boot_time_us":"2","avg_block_write_bytes":"3","avg_net_tx_bytes":"4"}]}`
	logSearchJSON := `{"filters":{"query":"build","run_id":"` + runID + `"},"limit":1,"next_cursor":"logs_cursor","results":[{"execution_id":"` + executionID + `","attempt_id":"attempt_1","seq":1,"stream":"stdout","chunk":"build log\n","created_at":"2026-05-06T00:00:30Z","source_kind":"github"}]}`
	githubInstallationJSON := `{"installation_id":"123","org_id":"370200542594579812","account_login":"guardian","account_type":"Organization","active":true,"created_at":"2026-05-06T00:00:00Z","updated_at":"2026-05-06T00:00:00Z"}`
	var createBody map[string]any
	var pauseKey string
	var resumeKey string
	var githubInstallKey string
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/runs/"+runID:
			_, _ = w.Write([]byte(runJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-logs/search":
			if r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("query") != "build" || r.URL.Query().Get("run_id") != runID {
				t.Fatalf("run logs query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(logSearchJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-analytics/jobs":
			_, _ = w.Write([]byte(jobsAnalyticsJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-analytics/costs":
			_, _ = w.Write([]byte(costsAnalyticsJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/run-analytics/runner-sizing":
			_, _ = w.Write([]byte(runnerSizingAnalyticsJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/github/installations":
			_, _ = w.Write([]byte(`[` + githubInstallationJSON + `]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/github/installations/connect":
			githubInstallKey = r.Header.Get("Idempotency-Key")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"setup_url":"https://github.com/apps/verself/installations/new","state":"github_state","expires_at":"2026-05-06T00:10:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/execution-schedules":
			_, _ = w.Write([]byte(`{"limit":50,"schedules":[` + scheduleJSON + `]}`))
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
	runByID, err := client.Sandbox.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if runByID.RunID != runID {
		t.Fatalf("unexpected run: %#v", runByID)
	}
	logMatches, err := client.Sandbox.SearchRunLogs(context.Background(), SearchSandboxRunLogsOptions{Limit: 1, Query: "build", RunID: runID})
	if err != nil {
		t.Fatal(err)
	}
	if len(logMatches.Results) != 1 || logMatches.Results[0].Chunk != "build log\n" || logMatches.NextCursor != "logs_cursor" {
		t.Fatalf("unexpected log search page: %#v", logMatches)
	}
	jobsAnalytics, err := client.Sandbox.GetJobsAnalytics(context.Background(), SandboxAnalyticsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if jobsAnalytics.TotalRuns != "1" || len(jobsAnalytics.SlowestRuns) != 1 {
		t.Fatalf("unexpected jobs analytics: %#v", jobsAnalytics)
	}
	costsAnalytics, err := client.Sandbox.GetCostsAnalytics(context.Background(), SandboxAnalyticsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if costsAnalytics.BilledChargeUnits != "9" {
		t.Fatalf("unexpected costs analytics: %#v", costsAnalytics)
	}
	runnerSizingAnalytics, err := client.Sandbox.GetRunnerSizingAnalytics(context.Background(), SandboxAnalyticsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runnerSizingAnalytics.ByRunnerClass) != 1 || runnerSizingAnalytics.ByRunnerClass[0].RunnerClass != "linux-2vcpu" {
		t.Fatalf("unexpected runner sizing analytics: %#v", runnerSizingAnalytics)
	}
	installations, err := client.Sandbox.ListGitHubInstallations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(installations) != 1 || installations[0].InstallationID != "123" {
		t.Fatalf("unexpected github installations: %#v", installations)
	}
	connect, err := client.Sandbox.BeginGitHubInstallation(context.Background(), SandboxMutationOptions{IdempotencyKey: "sandbox:github-connect"})
	if err != nil {
		t.Fatal(err)
	}
	if connect.State != "github_state" || githubInstallKey != "sandbox:github-connect" {
		t.Fatalf("unexpected github connect: response=%#v key=%q", connect, githubInstallKey)
	}
	schedules, err := client.Sandbox.ListSchedules(context.Background(), ListSandboxExecutionSchedulesOptions{})
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

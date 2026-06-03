package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticateRejectsMissingBearerToken(t *testing.T) {
	_, err := (API{}).authenticate(httptest.NewRequest(http.MethodPost, "/api/v1/deployments", nil))
	if err == nil {
		t.Fatal("missing bearer token authenticated")
	}
	if ErrorCode(err) != "deployment.unauthorized" {
		t.Fatalf("error code = %q, want deployment.unauthorized", ErrorCode(err))
	}
}

func TestAuthenticateUsesConfiguredGitHubVerifier(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/bootstrap:check", nil)
	req.Header.Set("Authorization", "Bearer expected-token")
	source, err := (API{GitHub: testVerifier()}).authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != SourceGitHubActionsOIDC || source.Subject == "" {
		t.Fatalf("source = %#v, want github actions oidc", source)
	}
}

func TestAuthenticateRejectsBearerTokenWhenVerifierUnavailable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/bootstrap:check", nil)
	req.Header.Set("Authorization", "Bearer expected-token")
	_, err := (API{}).authenticate(req)
	if err == nil {
		t.Fatal("bearer token authenticated without a verifier")
	}
}

type staticVerifier struct {
	source Source
	err    error
}

func (v staticVerifier) SourceFromToken(_ context.Context, token string) (Source, error) {
	if v.err != nil {
		return Source{}, v.err
	}
	if token != "expected-token" {
		return Source{}, fmtUnauthorized("unexpected bearer token")
	}
	return v.source, nil
}

func testVerifier() staticVerifier {
	return staticVerifier{source: testGitHubSource()}
}

func testGitHubSource() Source {
	return Source{
		Kind:           SourceGitHubActionsOIDC,
		Subject:        "repo:guardian-intelligence/verself:ref:refs/heads/main",
		Repository:     "guardian-intelligence/verself",
		Ref:            "refs/heads/main",
		SHA:            "0123456789abcdef0123456789abcdef01234567",
		RunID:          "123456",
		RunAttempt:     1,
		Workflow:       "Gamma Deploy",
		JobWorkflowRef: "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main",
	}
}

func TestBazelBuildFlagsUsesDeclaredJobLimit(t *testing.T) {
	got := bazelBuildFlags(4)
	if len(got) != 1 || got[0] != "--jobs=4" {
		t.Fatalf("flags = %#v, want --jobs=4", got)
	}
}

func TestDeploymentEndpointsRequireAuthenticationBeforeServiceState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		handler func(API, http.ResponseWriter, *http.Request)
	}{
		{
			name:   "bootstrap-check",
			method: http.MethodGet,
			path:   "/api/v1/deployments/bootstrap:check",
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.bootstrapCheck(w, r)
			},
		},
		{
			name:   "submit",
			method: http.MethodPost,
			path:   "/api/v1/deployments",
			body:   `{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}`,
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.submit(w, r)
			},
		},
		{
			name:   "get",
			method: http.MethodGet,
			path:   "/api/v1/deployments/dep_123",
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.get(w, r)
			},
		},
		{
			name:   "events",
			method: http.MethodGet,
			path:   "/api/v1/deployments/dep_123/events",
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.events(w, r)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			tc.handler(API{}, rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got == "" {
				t.Fatal("missing WWW-Authenticate header")
			}
			var problem problemDetails
			if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
				t.Fatal(err)
			}
			if problem.Code != "deployment.unauthorized" || strings.Contains(problem.Detail, "deployment service is not configured") {
				t.Fatalf("problem = %+v, want unauthorized without service-state detail", problem)
			}
		})
	}
}

func TestReadyzReturnsGenericReadinessCode(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	API{Service: &Service{}}.readyz(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "deployment.not_ready" {
		t.Fatalf("code = %q, want deployment.not_ready", problem.Code)
	}
	if problem.Tag != "urn:verself:problem:deployment:not_ready" || problem.Message == "" {
		t.Fatalf("problem contract fields = %+v", problem)
	}
	if strings.Contains(problem.Detail, "S0") || strings.Contains(problem.Detail, "deployment.bootstrap.") {
		t.Fatalf("readyz leaked bootstrap details: %+v", problem)
	}
}

func TestWriteProblemIncludesCommonProblemContractFields(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	writeProblem(rec, req, http.StatusServiceUnavailable, "deployment.service_unavailable", "deployment service is not configured")
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Tag != problem.Type || problem.Tag != "urn:verself:problem:deployment:service_unavailable" {
		t.Fatalf("tag/type = %q/%q", problem.Tag, problem.Type)
	}
	if problem.Message != "deployment service is not configured" {
		t.Fatalf("message = %q", problem.Message)
	}
}

func TestBootstrapCheckReturnsTypedDependencyCodes(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/bootstrap:check", nil)
	req.Header.Set("Authorization", "Bearer expected-token")
	API{Service: &Service{}, GitHub: testVerifier()}.bootstrapCheck(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var out BootstrapCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Checks) == 0 {
		t.Fatal("bootstrap check returned no checks")
	}
	if out.Ready {
		t.Fatalf("ready = true, want false while bootstrap checks fail")
	}
	if out.FailedCount == 0 {
		t.Fatalf("failed_count = 0, want nonzero for failed bootstrap checks")
	}
	if out.Checks[0].Code != "deployment.bootstrap.s0.site" {
		t.Fatalf("first check code = %q, want deployment.bootstrap.s0.site", out.Checks[0].Code)
	}
}

func TestDependencyCheckFailureCountSaturates(t *testing.T) {
	checks := make([]DependencyCheck, int(^uint16(0))+1)
	for i := range checks {
		checks[i] = dependencyFailed("S7", "object_storage", "failed")
	}
	if got := dependencyCheckFailureCount(checks); got != ^uint16(0) {
		t.Fatalf("failure count = %d, want saturated uint16", got)
	}
}

func TestDependencyChecksReturnCanonicalS0ToS7Model(t *testing.T) {
	checks := (&Service{}).DependencyChecks(context.Background())
	got := make([]string, 0, len(checks))
	for _, check := range checks {
		got = append(got, check.Stage+"/"+check.Name)
	}
	want := []string{
		"S0/site",
		"S1/host_allocated",
		"S2/recovery_ssh_ready",
		"S3/bazelisk",
		"S3/git",
		"S6/nomad",
		"S7/postgres",
		"S7/repo_root",
		"S7/object_storage",
	}
	if len(got) != len(want) {
		t.Fatalf("checks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("checks = %v, want %v", got, want)
		}
	}
}

func TestSubmitAdmissionRejectsWhenFull(t *testing.T) {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", bytes.NewBufferString(`{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}`))
	req.Header.Set("Authorization", "Bearer expected-token")
	API{Service: &Service{}, GitHub: testVerifier(), SubmitAdmission: gate}.submit(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "deployment.admission_busy" {
		t.Fatalf("code = %q, want deployment.admission_busy", problem.Code)
	}
}

func TestSubmitAdmissionGateReleasesAfterFailure(t *testing.T) {
	ctx, store := newTestStore(t)
	gate := make(chan struct{}, 1)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/deployments", bytes.NewBufferString(`{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}`))
	req.Header.Set("Authorization", "Bearer expected-token")
	API{
		Service:         &Service{Store: store, Config: Config{Site: "gamma"}},
		GitHub:          testVerifier(),
		SubmitAdmission: gate,
	}.submit(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	select {
	case gate <- struct{}{}:
		<-gate
	default:
		t.Fatal("admission gate was not released after failed submission")
	}
}

func TestSubmitAuthenticatesBeforeAdmissionGate(t *testing.T) {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", bytes.NewBufferString(`{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}`))
	API{Service: &Service{}, SubmitAdmission: gate}.submit(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "deployment.unauthorized" {
		t.Fatalf("code = %q, want deployment.unauthorized", problem.Code)
	}
}

func TestSubmitReturnsBootstrapUnavailableWhenReadinessFails(t *testing.T) {
	ctx, store := newTestStore(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/deployments", bytes.NewBufferString(`{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}`))
	req.Header.Set("Authorization", "Bearer expected-token")
	API{
		Service: &Service{Store: store, Config: Config{Site: "gamma"}},
		GitHub:  testVerifier(),
	}.submit(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(problem.Code, "deployment.bootstrap.") {
		t.Fatalf("code = %q, want deployment.bootstrap.*", problem.Code)
	}
	if problem.Tag != "urn:verself:problem:"+strings.ReplaceAll(problem.Code, ".", ":") {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestDecodeJSONRejectsTrailingDocument(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", bytes.NewBufferString(`{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}{}`))
	var out SubmitRequest
	if decodeJSON(rec, req, &out) {
		t.Fatal("decodeJSON accepted a trailing JSON document")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "request.invalid_json" {
		t.Fatalf("code = %q, want request.invalid_json", problem.Code)
	}
}

func TestHandlersReturnTypedUnavailableWhenServiceIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		handler func(API, http.ResponseWriter, *http.Request)
	}{
		{
			name:   "submit",
			method: http.MethodPost,
			path:   "/api/v1/deployments",
			body:   `{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567"}`,
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.submit(w, r)
			},
		},
		{
			name:   "get",
			method: http.MethodGet,
			path:   "/api/v1/deployments/dep_123",
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.get(w, r)
			},
		},
		{
			name:   "events",
			method: http.MethodGet,
			path:   "/api/v1/deployments/dep_123/events",
			handler: func(api API, w http.ResponseWriter, r *http.Request) {
				api.events(w, r)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer expected-token")
			tc.handler(API{GitHub: testVerifier()}, rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
			}
			var problem problemDetails
			if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
				t.Fatal(err)
			}
			if problem.Code != "deployment.service_unavailable" {
				t.Fatalf("code = %q, want deployment.service_unavailable", problem.Code)
			}
		})
	}
}

func TestEventsReturnsNotFoundForMissingDeployment(t *testing.T) {
	ctx, store := newTestStore(t)
	mux := http.NewServeMux()
	RegisterRoutes(mux, API{
		Service: &Service{Store: store},
		GitHub:  testVerifier(),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/deployments/dep_missing/events", nil)
	req.Header.Set("Authorization", "Bearer expected-token")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "deployment.not_found" {
		t.Fatalf("code = %q, want deployment.not_found", problem.Code)
	}
}

func TestDependencyChecksRunSlowProbesConcurrently(t *testing.T) {
	slowOK := func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}
	nomad := httptest.NewServer(http.HandlerFunc(slowOK))
	t.Cleanup(nomad.Close)
	objectStorage := httptest.NewServer(http.HandlerFunc(slowOK))
	t.Cleanup(objectStorage.Close)

	started := time.Now()
	checks := (&Service{Config: Config{
		Site:              "gamma",
		NomadAddr:         nomad.URL,
		ObjectStorageAddr: objectStorage.URL,
		NomadAllocID:      "alloc-1",
		RecoverySSHReady:  "true",
	}}).DependencyChecks(context.Background())
	elapsed := time.Since(started)
	if elapsed > 350*time.Millisecond {
		t.Fatalf("dependency checks took %s, expected slow HTTP probes to run concurrently", elapsed)
	}
	if checks[0].Stage != "S0" || checks[0].Name != "site" {
		t.Fatalf("first check = %#v, want stable S0/site ordering", checks[0])
	}
}

func TestDependencyChecksDeadlineReturnsTypedFailureForNonCooperativeProbe(t *testing.T) {
	slowDone := make(chan struct{})
	t.Cleanup(func() { <-slowDone })
	started := time.Now()
	checks := runDependencyProbes(context.Background(), []dependencyProbe{
		{
			Stage: "S0",
			Name:  "site",
			Run: func(context.Context) DependencyCheck {
				return dependencyOK("S0", "site")
			},
		},
		{
			Stage: "S7",
			Name:  "object_storage",
			Run: func(context.Context) DependencyCheck {
				time.Sleep(80 * time.Millisecond)
				close(slowDone)
				return dependencyOK("S7", "object_storage")
			},
		},
	}, 10*time.Millisecond)
	elapsed := time.Since(started)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("dependency checks took %s, want deadline-bound return", elapsed)
	}
	if checks[0].Code != "ok" {
		t.Fatalf("first check = %+v, want ok", checks[0])
	}
	if checks[1].OK || checks[1].Code != "deployment.bootstrap.s7.object_storage" || checks[1].Detail != "deadline exceeded" {
		t.Fatalf("slow check = %+v, want typed deadline failure", checks[1])
	}
}

func TestDependencyChecksUseSubsecondAggregateDeadline(t *testing.T) {
	slowDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-slowDone:
		case <-time.After(2 * time.Second):
			t.Fatal("non-cooperative probe did not finish")
		}
	})
	started := time.Now()
	checks := runDependencyProbes(context.Background(), []dependencyProbe{
		{
			Stage: "S0",
			Name:  "site",
			Run: func(context.Context) DependencyCheck {
				return dependencyOK("S0", "site")
			},
		},
		{
			Stage: "S7",
			Name:  "postgres",
			Run: func(context.Context) DependencyCheck {
				time.Sleep(dependencyProbeDeadline + 200*time.Millisecond)
				close(slowDone)
				return dependencyOK("S7", "postgres")
			},
		},
	}, dependencyProbeDeadline)
	elapsed := time.Since(started)
	if elapsed >= time.Second {
		t.Fatalf("dependency checks took %s, want sub-second aggregate readiness failure", elapsed)
	}
	if checks[0].Code != "ok" {
		t.Fatalf("first check = %+v, want ok", checks[0])
	}
	if checks[1].OK || checks[1].Code != "deployment.bootstrap.s7.postgres" || checks[1].Detail != "deadline exceeded" {
		t.Fatalf("slow check = %+v, want typed deadline failure", checks[1])
	}
}

func TestDependencyChecksRequireNomadAllocationAndRecoveryHandoff(t *testing.T) {
	checks := (&Service{Config: Config{Site: "gamma"}}).DependencyChecks(context.Background())
	found := map[string]DependencyCheck{}
	for _, check := range checks {
		found[check.Code] = check
	}
	for _, code := range []string{"deployment.bootstrap.s1.host_allocated", "deployment.bootstrap.s2.recovery_ssh_ready"} {
		check, ok := found[code]
		if !ok {
			t.Fatalf("missing %s in checks: %+v", code, checks)
		}
		if check.OK || check.Detail == "" {
			t.Fatalf("check %s = %+v, want failed detail", code, check)
		}
	}
}

func TestValidateRequestRequiresGitHubSHAClaim(t *testing.T) {
	svc := Service{Config: Config{Site: "gamma"}}
	err := svc.validateRequest(SubmitRequest{
		Site: "gamma",
		SHA:  "0123456789abcdef0123456789abcdef01234567",
	}, Source{
		Kind:       SourceGitHubActionsOIDC,
		Subject:    "repo:guardian-intelligence/verself:ref:refs/heads/main",
		Repository: "guardian-intelligence/verself",
		Ref:        "refs/heads/main",
	})
	if err == nil {
		t.Fatal("github oidc request without sha claim was accepted")
	}
	if ErrorCode(err) != "deployment.source_sha_missing" {
		t.Fatalf("error code = %q, want deployment.source_sha_missing", ErrorCode(err))
	}
}

func TestValidateRequestRequiresGitHubRunID(t *testing.T) {
	svc := Service{Config: Config{Site: "gamma"}}
	err := svc.validateRequest(SubmitRequest{
		Site: "gamma",
		SHA:  "0123456789abcdef0123456789abcdef01234567",
	}, Source{
		Kind:           SourceGitHubActionsOIDC,
		Subject:        "repo:guardian-intelligence/verself:ref:refs/heads/main",
		Repository:     "guardian-intelligence/verself",
		Ref:            "refs/heads/main",
		SHA:            "0123456789abcdef0123456789abcdef01234567",
		RunAttempt:     1,
		JobWorkflowRef: "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main",
	})
	if err == nil {
		t.Fatal("github oidc request without run_id was accepted")
	}
	if ErrorCode(err) != "deployment.source_run_id_missing" {
		t.Fatalf("error code = %q, want deployment.source_run_id_missing", ErrorCode(err))
	}
}

func TestRepositoryForRecordUsesGitHubClaim(t *testing.T) {
	got := repositoryForRecord(Source{
		Kind:       SourceGitHubActionsOIDC,
		Repository: "guardian-intelligence/verself",
	})
	if got != "guardian-intelligence/verself" {
		t.Fatalf("repository = %q, want github claim", got)
	}
}

func TestSubmitRejectsClientSuppliedRepository(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", bytes.NewBufferString(`{"site":"gamma","sha":"0123456789abcdef0123456789abcdef01234567","repository":"other/repo"}`))
	req.Header.Set("Authorization", "Bearer expected-token")
	API{
		Service: &Service{Config: Config{Site: "gamma"}},
		GitHub:  testVerifier(),
	}.submit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "request.invalid_json" {
		t.Fatalf("code = %q, want request.invalid_json", problem.Code)
	}
}

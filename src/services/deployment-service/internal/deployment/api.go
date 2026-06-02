package deployment

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const maxRequestBody = 64 << 10

type API struct {
	Service         *Service
	GitHub          *GitHubOIDCVerifier
	OperatorToken   string
	SubmitAdmission chan struct{}
}

func RegisterRoutes(mux *http.ServeMux, api API) {
	mux.HandleFunc("GET /healthz", api.healthz)
	mux.HandleFunc("GET /readyz", api.readyz)
	mux.HandleFunc("GET /api/v1/deployments/bootstrap:check", api.bootstrapCheck)
	mux.HandleFunc("POST /api/v1/deployments", api.submit)
	mux.HandleFunc("GET /api/v1/deployments/{deployment_id}", api.get)
	mux.HandleFunc("GET /api/v1/deployments/{deployment_id}/events", api.events)
}

func (a API) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (a API) readyz(w http.ResponseWriter, r *http.Request) {
	if a.Service == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "deployment.service_unavailable", "deployment service is not configured")
		return
	}
	if err := a.Service.Ready(r.Context()); err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "deployment.not_ready", "deployment service is not ready")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (a API) bootstrapCheck(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authenticate(r); err != nil {
		writeProblem(w, r, http.StatusUnauthorized, ErrorCode(err), "unauthorized")
		return
	}
	if a.Service == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "deployment.service_unavailable", "deployment service is not configured")
		return
	}
	checks := a.Service.DependencyChecks(r.Context())
	writeJSON(w, http.StatusOK, BootstrapCheckResponse{
		Ready:       dependencyChecksReady(checks),
		FailedCount: dependencyCheckFailureCount(checks),
		Checks:      checks,
		Traceparent: traceparent(r),
	})
}

func (a API) submit(w http.ResponseWriter, r *http.Request) {
	source, err := a.authenticate(r)
	if err != nil {
		writeProblem(w, r, http.StatusUnauthorized, ErrorCode(err), "unauthorized")
		return
	}
	var req SubmitRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if a.Service == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "deployment.service_unavailable", "deployment service is not configured")
		return
	}
	release, ok := acquireAdmission(a.SubmitAdmission)
	if !ok {
		writeProblem(w, r, http.StatusTooManyRequests, "deployment.admission_busy", "too many deployment submissions are being admitted")
		return
	}
	defer release()
	record, err := a.Service.Submit(r.Context(), req, source)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"deployment":  record,
		"traceparent": traceparent(r),
	})
}

func acquireAdmission(ch chan struct{}) (func(), bool) {
	if ch == nil {
		return func() {}, true
	}
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, true
	default:
		return nil, false
	}
}

func (a API) get(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authenticate(r); err != nil {
		writeProblem(w, r, http.StatusUnauthorized, ErrorCode(err), "unauthorized")
		return
	}
	if a.Service == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "deployment.service_unavailable", "deployment service is not configured")
		return
	}
	record, err := a.Service.Store.Get(r.Context(), r.PathValue("deployment_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deployment":  record,
		"traceparent": traceparent(r),
	})
}

func (a API) events(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authenticate(r); err != nil {
		writeProblem(w, r, http.StatusUnauthorized, ErrorCode(err), "unauthorized")
		return
	}
	if a.Service == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "deployment.service_unavailable", "deployment service is not configured")
		return
	}
	deploymentID := r.PathValue("deployment_id")
	if _, err := a.Service.Store.Get(r.Context(), deploymentID); err != nil {
		writeError(w, r, err)
		return
	}
	events, err := a.Service.Store.ListEvents(r.Context(), deploymentID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events":      events,
		"traceparent": traceparent(r),
	})
}

func (a API) authenticate(r *http.Request) (Source, error) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return Source{}, fmtUnauthorized("missing bearer token")
	}
	if a.operatorTokenMatches(token) {
		return Source{
			Kind:    SourceOperatorBearer,
			Subject: "operator:deployment-token",
		}, nil
	}
	if a.GitHub == nil {
		return Source{}, fmtUnauthorized("github oidc verifier unavailable")
	}
	return a.GitHub.SourceFromToken(r.Context(), token)
}

func (a API) operatorTokenMatches(token string) bool {
	configured := strings.TrimSpace(a.OperatorToken)
	got := strings.TrimSpace(token)
	if configured == "" || got == "" || len(configured) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(configured)) == 1
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method == http.MethodPost && r.ContentLength > maxRequestBody {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "request.too_large", "request body is too large")
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "request.read_failed", "request body could not be read")
		return false
	}
	if len(body) > maxRequestBody {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "request.too_large", "request body is too large")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "request.invalid_json", "request JSON is invalid")
		return false
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		writeProblem(w, r, http.StatusBadRequest, "request.invalid_json", "request JSON is invalid")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		writeProblem(w, r, http.StatusUnauthorized, ErrorCode(err), "unauthorized")
	case errors.Is(err, ErrInvalid):
		writeProblem(w, r, http.StatusBadRequest, ErrorCode(err), err.Error())
	case errors.Is(err, ErrBusy):
		writeProblem(w, r, http.StatusConflict, ErrorCode(err), "another deployment is already running")
	case errors.Is(err, ErrDuplicate):
		writeProblem(w, r, http.StatusConflict, ErrorCode(err), "deployment source attempt already exists with different request data")
	case errors.Is(err, ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, ErrorCode(err), "deployment not found")
	case errors.Is(err, ErrUnavailable):
		writeProblem(w, r, http.StatusServiceUnavailable, ErrorCode(err), err.Error())
	default:
		writeProblem(w, r, http.StatusInternalServerError, ErrorCode(err), err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func dependencyChecksReady(checks []DependencyCheck) bool {
	return dependencyCheckFailureCount(checks) == 0
}

func dependencyCheckFailureCount(checks []DependencyCheck) uint16 {
	failures := 0
	for _, check := range checks {
		if !check.OK {
			failures++
		}
	}
	if failures > int(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(failures)
}

type problemDetails struct {
	Tag         string `json:"_tag"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Code        string `json:"code"`
	Detail      string `json:"detail"`
	Message     string `json:"message"`
	Instance    string `json:"instance,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code string, detail string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	problemType := "urn:verself:problem:" + strings.ReplaceAll(code, ".", ":")
	message := strings.TrimSpace(detail)
	if message == "" {
		message = http.StatusText(status)
	}
	problem := problemDetails{
		Tag:         problemType,
		Type:        problemType,
		Title:       http.StatusText(status),
		Status:      status,
		Code:        code,
		Detail:      detail,
		Message:     message,
		Traceparent: traceparent(r),
	}
	if spanContext := trace.SpanContextFromContext(r.Context()); spanContext.HasTraceID() {
		problem.Instance = "urn:verself:trace:" + spanContext.TraceID().String()
	}
	_ = json.NewEncoder(w).Encode(problem)
}

func traceparent(r *http.Request) string {
	spanContext := trace.SpanContextFromContext(r.Context())
	if !spanContext.HasTraceID() || !spanContext.HasSpanID() {
		return ""
	}
	return "00-" + spanContext.TraceID().String() + "-" + spanContext.SpanID().String() + "-" + spanContext.TraceFlags().String()
}

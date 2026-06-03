package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/verself/deployment-service/siteconfig"
)

type runOptions struct {
	Site     string
	SHA      string
	RepoRoot string
}

type submitRequest struct {
	Site string `json:"site"`
	SHA  string `json:"sha"`
}

type submitResponse struct {
	Deployment struct {
		DeploymentID string `json:"deployment_id"`
		State        string `json:"state"`
		SHA          string `json:"sha"`
	} `json:"deployment"`
	Traceparent string `json:"traceparent"`
}

type bootstrapCheckResponse struct {
	Ready       bool              `json:"ready"`
	FailedCount uint16            `json:"failed_count"`
	Checks      []dependencyCheck `json:"checks"`
	Traceparent string            `json:"traceparent"`
}

type dependencyCheck struct {
	Stage  string `json:"stage"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type githubOIDCResponse struct {
	Value string `json:"value"`
}

const (
	deploymentFollowTimeout      = 20 * time.Minute
	deploymentFollowPollInterval = time.Second
)

type problemDetails struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Code        string `json:"code"`
	Detail      string `json:"detail"`
	Instance    string `json:"instance,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

func runRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	sha := fs.String("sha", "", "git SHA being deployed")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy run: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	if err := run(context.Background(), runOptions{Site: *site, SHA: *sha, RepoRoot: rr}); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy run: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, opts runOptions) error {
	if strings.TrimSpace(opts.Site) == "" {
		return fmt.Errorf("site is required")
	}
	if strings.TrimSpace(opts.SHA) == "" {
		return fmt.Errorf("sha is required")
	}
	model, err := siteconfig.Load(opts.RepoRoot, opts.Site)
	if err != nil {
		return err
	}
	baseURL := deploymentServiceBaseURL(model)
	if err := deploymentServiceReachable(ctx, baseURL); err != nil {
		return err
	}
	token, err := deploymentBearerToken(ctx, baseURL)
	if err != nil {
		return err
	}
	if _, err := bootstrapCheck(ctx, baseURL, token); err != nil {
		return err
	}
	out, err := submitDeployment(ctx, baseURL, token, submitRequest{
		Site: opts.Site,
		SHA:  opts.SHA,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "deployment_id=%s state=%s sha=%s traceparent=%s\n", out.Deployment.DeploymentID, out.Deployment.State, out.Deployment.SHA, out.Traceparent); err != nil {
		return fmt.Errorf("write deployment response: %w", err)
	}
	record, traceparent, err := waitForDeploymentTerminal(ctx, baseURL, token, out.Deployment.DeploymentID, deploymentFollowTimeout, deploymentFollowPollInterval)
	if err != nil {
		return err
	}
	if err := printDeploymentRecord(record, traceparent); err != nil {
		return err
	}
	if record.State != "succeeded" {
		return fmt.Errorf("deployment_finished_unsuccessfully: deployment_id=%s state=%s error_code=%s error_detail=%s", record.DeploymentID, record.State, record.ErrorCode, record.ErrorDetail)
	}
	return nil
}

func deploymentServiceBaseURL(model siteconfig.Model) string {
	domain := strings.TrimSpace(model.Domains["deployment_service"])
	if domain == "" {
		domain = "deployments.api." + model.ProductDomain
	}
	return "https://" + domain
}

func deploymentServiceReachable(ctx context.Context, baseURL string) error {
	checkCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/healthz", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deploymentServiceReachabilityError(baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return httpProblem("deployment_service_unavailable: "+baseURL+" healthz failed during S7 bootstrap check", resp.StatusCode, body)
	}
	return nil
}

func bootstrapCheck(ctx context.Context, baseURL string, token string) (bootstrapCheckResponse, error) {
	checkCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/deployments/bootstrap:check", http.NoBody)
	if err != nil {
		return bootstrapCheckResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return bootstrapCheckResponse{}, deploymentServiceReachabilityError(baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return bootstrapCheckResponse{}, httpProblem("deployment_service_unavailable: "+baseURL+" bootstrap check failed", resp.StatusCode, body)
	}
	var out bootstrapCheckResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return bootstrapCheckResponse{}, fmt.Errorf("deployment_service_unavailable: decode bootstrap check response: %w", err)
	}
	failures := []string{}
	for _, check := range out.Checks {
		if !check.OK {
			failures = append(failures, check.Stage+"/"+check.Name+" "+check.Code+": "+check.Detail)
		}
	}
	if len(failures) > 0 {
		return out, fmt.Errorf("deployment_service_unavailable: site is not past S7 deployment-service-ready: %s", strings.Join(failures, "; "))
	}
	return out, nil
}

func deploymentBearerToken(ctx context.Context, audience string) (string, error) {
	reqURL := strings.TrimSpace(os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"))
	reqToken := strings.TrimSpace(os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	if reqURL == "" || reqToken == "" {
		return "", fmt.Errorf("deployment_auth_unavailable: GitHub Actions OIDC request environment is missing; run from an authorized GitHub workflow")
	}
	u, err := url.Parse(reqURL)
	if err != nil {
		return "", fmt.Errorf("deployment_auth_unavailable: parse GitHub OIDC request URL: %w", err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()
	authCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(authCtx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+reqToken)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("deployment_auth_unavailable: request GitHub OIDC token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("deployment_auth_unavailable: read GitHub OIDC response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("deployment_auth_unavailable: GitHub OIDC token endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out githubOIDCResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("deployment_auth_unavailable: decode GitHub OIDC response: %w", err)
	}
	if strings.TrimSpace(out.Value) == "" {
		return "", fmt.Errorf("deployment_auth_unavailable: GitHub OIDC response did not contain a token")
	}
	return out.Value, nil
}

func deploymentServiceReachabilityError(baseURL string, err error) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Errorf("deployment.bootstrap.s7.deployment_service_dns: %s is not resolvable within the S7 deployment-service-ready check: %w. Bootstrap or recover the site first; normal aspect deploy does not SSH or run Ansible", baseURL, err)
	}
	return fmt.Errorf("deployment.bootstrap.s7.deployment_service: %s is not reachable within the S7 deployment-service-ready check: %w. Bootstrap or recover the site first; normal aspect deploy does not SSH or run Ansible", baseURL, err)
}

func submitDeployment(ctx context.Context, baseURL string, token string, reqBody submitRequest) (submitResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return submitResponse{}, err
	}
	submitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(submitCtx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/deployments", bytes.NewReader(body))
	if err != nil {
		return submitResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return submitResponse{}, fmt.Errorf("deployment_request_failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return submitResponse{}, fmt.Errorf("deployment_request_failed: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return submitResponse{}, httpProblem("deployment_request_failed", resp.StatusCode, raw)
	}
	var out submitResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return submitResponse{}, fmt.Errorf("deployment_request_failed: decode response: %w", err)
	}
	if out.Deployment.DeploymentID == "" {
		return submitResponse{}, fmt.Errorf("deployment_request_failed: response omitted deployment_id")
	}
	return out, nil
}

func waitForDeploymentTerminal(ctx context.Context, baseURL string, token string, deploymentID string, timeout, pollInterval time.Duration) (deploymentRecord, string, error) {
	if timeout <= 0 {
		return deploymentRecord{}, "", fmt.Errorf("deployment_follow_failed: timeout must be positive")
	}
	if pollInterval <= 0 {
		return deploymentRecord{}, "", fmt.Errorf("deployment_follow_failed: poll interval must be positive")
	}
	followCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last deploymentRecord
	var lastTraceparent string
	var lastStatusErr error
	for {
		record, traceparent, err := getDeployment(followCtx, baseURL, token, deploymentID)
		if err != nil {
			if transientDeploymentStatusError(err) {
				lastStatusErr = err
				select {
				case <-followCtx.Done():
					return last, lastTraceparent, deploymentFollowTimeoutError(followCtx.Err(), deploymentID, last.State, lastStatusErr)
				case <-time.After(pollInterval):
					continue
				}
			}
			return deploymentRecord{}, "", fmt.Errorf("deployment_follow_failed: %w", err)
		}
		last = record
		lastTraceparent = traceparent
		lastStatusErr = nil
		switch record.State {
		case "succeeded", "failed":
			return record, traceparent, nil
		}
		select {
		case <-followCtx.Done():
			return last, lastTraceparent, deploymentFollowTimeoutError(followCtx.Err(), deploymentID, last.State, lastStatusErr)
		case <-time.After(pollInterval):
		}
	}
}

func deploymentFollowTimeoutError(err error, deploymentID string, lastState string, lastStatusErr error) error {
	if lastStatusErr != nil {
		return fmt.Errorf("deployment_follow_failed: %w: deployment_id=%s last_state=%s last_status_error=%v", err, deploymentID, lastState, lastStatusErr)
	}
	return fmt.Errorf("deployment_follow_failed: %w: deployment_id=%s last_state=%s", err, deploymentID, lastState)
}

func httpProblem(prefix string, status int, body []byte) error {
	var problem problemDetails
	if err := json.Unmarshal(body, &problem); err == nil && strings.TrimSpace(problem.Code) != "" {
		detail := strings.TrimSpace(problem.Detail)
		if detail == "" {
			detail = strings.TrimSpace(problem.Title)
		}
		if strings.TrimSpace(problem.Traceparent) != "" {
			return fmt.Errorf("%s: status %d %s: %s traceparent=%s", prefix, status, problem.Code, detail, problem.Traceparent)
		}
		return fmt.Errorf("%s: status %d %s: %s", prefix, status, problem.Code, detail)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = http.StatusText(status)
	}
	return fmt.Errorf("%s: status %d: %s", prefix, status, text)
}

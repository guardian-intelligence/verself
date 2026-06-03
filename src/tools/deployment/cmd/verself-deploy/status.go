package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/verself/deployment-service/siteconfig"
)

type statusOptions struct {
	Site         string
	DeploymentID string
	RepoRoot     string
	Events       bool
}

type deploymentRecord struct {
	DeploymentID       string    `json:"deployment_id"`
	Site               string    `json:"site"`
	SHA                string    `json:"sha"`
	DeployRunKey       string    `json:"deploy_run_key"`
	State              string    `json:"state"`
	ErrorCode          string    `json:"error_code"`
	ErrorDetail        string    `json:"error_detail"`
	NomadSubmittedJobs uint32    `json:"nomad_submitted_jobs"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	StartedAt          time.Time `json:"started_at"`
	FinishedAt         time.Time `json:"finished_at"`
}

type deploymentEvent struct {
	EventID      int64     `json:"event_id"`
	DeploymentID string    `json:"deployment_id"`
	At           time.Time `json:"at"`
	State        string    `json:"state"`
	Code         string    `json:"code"`
	Detail       string    `json:"detail"`
}

type getDeploymentResponse struct {
	Deployment  deploymentRecord `json:"deployment"`
	Traceparent string           `json:"traceparent"`
}

type listEventsResponse struct {
	Events      []deploymentEvent `json:"events"`
	Traceparent string            `json:"traceparent"`
}

type deploymentStatusError struct {
	err       error
	transient bool
	status    int
}

func (e deploymentStatusError) Error() string {
	if e.err == nil {
		return "deployment_status_failed"
	}
	return e.err.Error()
}

func (e deploymentStatusError) Unwrap() error {
	return e.err
}

func transientDeploymentStatusError(err error) bool {
	var statusErr deploymentStatusError
	return errors.As(err, &statusErr) && statusErr.transient
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	site := fs.String("site", "prod", "deployment site")
	deploymentID := fs.String("deployment-id", "", "deployment id returned by verself-deploy run")
	repoRoot := fs.String("repo-root", "", "verself-sh checkout root")
	events := fs.Bool("events", false, "print the deployment event ledger")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rr := *repoRoot
	if rr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy status: cwd: %v\n", err)
			return 1
		}
		rr = cwd
	}
	if err := status(context.Background(), statusOptions{Site: *site, DeploymentID: *deploymentID, RepoRoot: rr, Events: *events}); err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy status: %v\n", err)
		return 1
	}
	return 0
}

func status(ctx context.Context, opts statusOptions) error {
	if strings.TrimSpace(opts.Site) == "" {
		return fmt.Errorf("site is required")
	}
	if strings.TrimSpace(opts.DeploymentID) == "" {
		return fmt.Errorf("deployment id is required")
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
	record, traceparent, err := getDeployment(ctx, baseURL, token, opts.DeploymentID)
	if err != nil {
		return err
	}
	if err := printDeploymentRecord(record, traceparent); err != nil {
		return err
	}
	if opts.Events {
		events, eventsTraceparent, err := listDeploymentEvents(ctx, baseURL, token, opts.DeploymentID)
		if err != nil {
			return err
		}
		if err := printDeploymentEvents(events, eventsTraceparent); err != nil {
			return err
		}
	}
	return nil
}

func getDeployment(ctx context.Context, baseURL string, token string, deploymentID string) (deploymentRecord, string, error) {
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(statusCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/deployments/"+url.PathEscape(deploymentID), http.NoBody)
	if err != nil {
		return deploymentRecord{}, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deploymentRecord{}, "", deploymentStatusError{
			err:       fmt.Errorf("deployment_status_failed: %w", err),
			transient: true,
		}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return deploymentRecord{}, "", fmt.Errorf("deployment_status_failed: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return deploymentRecord{}, "", deploymentStatusError{
			err:       httpProblem("deployment_status_failed", resp.StatusCode, body),
			transient: resp.StatusCode >= 500,
			status:    resp.StatusCode,
		}
	}
	var out getDeploymentResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return deploymentRecord{}, "", fmt.Errorf("deployment_status_failed: decode response: %w", err)
	}
	if out.Deployment.DeploymentID == "" {
		return deploymentRecord{}, "", fmt.Errorf("deployment_status_failed: response omitted deployment_id")
	}
	return out.Deployment, out.Traceparent, nil
}

func listDeploymentEvents(ctx context.Context, baseURL string, token string, deploymentID string) ([]deploymentEvent, string, error) {
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(statusCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/deployments/"+url.PathEscape(deploymentID)+"/events", http.NoBody)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("deployment_events_failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", fmt.Errorf("deployment_events_failed: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", httpProblem("deployment_events_failed", resp.StatusCode, body)
	}
	var out listEventsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, "", fmt.Errorf("deployment_events_failed: decode response: %w", err)
	}
	return out.Events, out.Traceparent, nil
}

func printDeploymentRecord(record deploymentRecord, traceparent string) error {
	if _, err := fmt.Fprintf(os.Stdout, "deployment_id=%s state=%s site=%s sha=%s deploy_run_key=%s submitted_jobs=%d error_code=%s traceparent=%s\n",
		record.DeploymentID, record.State, record.Site, record.SHA, record.DeployRunKey,
		record.NomadSubmittedJobs,
		record.ErrorCode, traceparent); err != nil {
		return fmt.Errorf("write deployment record: %w", err)
	}
	if strings.TrimSpace(record.ErrorDetail) != "" {
		if _, err := fmt.Fprintf(os.Stdout, "error_detail=%s\n", record.ErrorDetail); err != nil {
			return fmt.Errorf("write deployment error detail: %w", err)
		}
	}
	return nil
}

func printDeploymentEvents(events []deploymentEvent, traceparent string) error {
	for _, event := range events {
		if _, err := fmt.Fprintf(os.Stdout, "event_id=%d at=%s state=%s code=%s detail=%s\n",
			event.EventID, event.At.Format(time.RFC3339Nano), event.State, event.Code, event.Detail); err != nil {
			return fmt.Errorf("write deployment event: %w", err)
		}
	}
	if strings.TrimSpace(traceparent) != "" {
		if _, err := fmt.Fprintf(os.Stdout, "events_traceparent=%s\n", traceparent); err != nil {
			return fmt.Errorf("write deployment events traceparent: %w", err)
		}
	}
	return nil
}

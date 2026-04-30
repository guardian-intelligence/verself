// Command nomad-deploy submits a rendered Nomad job spec to Nomad's HTTP API
// and watches the deployment to completion.
//
// The resolved job spec is the deploy unit. Bazel builds task artifacts,
// materializes artifact URLs/checksums into the JSON spec, and writes digest
// metadata into Job.Meta before this binary runs. nomad-deploy submits the spec
// and polls until Nomad reports the deployment healthy or failed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultNomadAddr = "http://127.0.0.1:4646"
	defaultTimeout   = 2 * time.Minute
	pollInterval     = 2 * time.Second
	// failFastDeadAllocs aborts the await-healthy loop once at least
	// this many distinct allocs for the deployment have entered a
	// terminal failed state. The default rolling-restart contract
	// (max_parallel=1, auto_revert=true) gives Nomad three chances by
	// design; if all three die the same way, no amount of further
	// waiting fixes it and the operator wants the error now.
	failFastDeadAllocs = 3
)

func main() {
	runSubmit(os.Args[1:])
}

func runSubmit(args []string) {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	specPath := fs.String("spec", "", "rendered job spec path")
	nomadAddr := fs.String("nomad-addr", defaultNomadAddr, "Nomad agent HTTP address")
	timeout := fs.Duration("timeout", defaultTimeout, "deployment poll timeout")
	fs.Parse(args)

	if *specPath == "" {
		fail("--spec is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := run(ctx, runArgs{
		specPath:  *specPath,
		nomadAddr: *nomadAddr,
	}); err != nil {
		fail(err.Error())
	}
}

type runArgs struct {
	specPath  string
	nomadAddr string
}

func run(ctx context.Context, args runArgs) error {
	specBytes, err := os.ReadFile(args.specPath)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	jobID, artifactDigest, specDigest, err := specMeta(spec)
	if err != nil {
		return err
	}

	current, err := currentJobState(ctx, args.nomadAddr, jobID)
	if err != nil {
		return fmt.Errorf("inspect current job: %w", err)
	}
	if current.ArtifactDigest == artifactDigest && current.SpecDigest == specDigest && !current.Stopped {
		fmt.Fprintf(os.Stdout, "nomad-deploy: %s already at artifact_sha256=%s spec_sha256=%s (job version %d); no submit\n",
			jobID, shortDigest(artifactDigest), shortDigest(specDigest), current.Version)
		return nil
	}

	deployment, err := submit(ctx, args.nomadAddr, spec)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	fmt.Fprintf(os.Stdout, "nomad-deploy: %s submitted job_modify_index=%d eval_id=%s\n",
		jobID, deployment.JobModifyIndex, deployment.EvalID)

	if err := awaitHealthy(ctx, args.nomadAddr, jobID, deployment.JobModifyIndex); err != nil {
		return fmt.Errorf("await healthy: %w", err)
	}
	fmt.Fprintf(os.Stdout, "nomad-deploy: %s healthy at artifact_sha256=%s\n", jobID, shortDigest(artifactDigest))
	return nil
}

func specMeta(spec map[string]any) (jobID, artifactDigest, specDigest string, err error) {
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		return "", "", "", fmt.Errorf("spec.Job: missing or wrong type")
	}
	id, _ := job["ID"].(string)
	if id == "" {
		return "", "", "", fmt.Errorf("spec.Job.ID: missing")
	}
	meta, _ := job["Meta"].(map[string]any)
	artifactDigest, _ = meta["artifact_sha256"].(string)
	specDigest, _ = meta["spec_sha256"].(string)
	if artifactDigest == "" || specDigest == "" {
		return "", "", "", fmt.Errorf("spec.Job.Meta must include artifact_sha256 and spec_sha256")
	}
	return id, artifactDigest, specDigest, nil
}

func shortDigest(digest string) string {
	if len(digest) < 12 {
		return digest
	}
	return digest[:12]
}

type currentJob struct {
	ArtifactDigest string
	SpecDigest     string
	Version        int64
	Stopped        bool
}

// currentJobState returns the currently-registered job metadata. A stopped
// Nomad job can still carry the target digests, so Stop must participate in
// the no-op decision or reset playbooks can leave a service dead.
func currentJobState(ctx context.Context, nomadAddr, jobID string) (currentJob, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nomadAddr+"/v1/job/"+jobID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return currentJob{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return currentJob{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return currentJob{}, fmt.Errorf("GET /v1/job/%s: %d %s", jobID, resp.StatusCode, body)
	}
	var payload struct {
		Meta    map[string]string `json:"Meta"`
		Version int64             `json:"Version"`
		Stop    bool              `json:"Stop"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return currentJob{}, err
	}
	return currentJob{
		ArtifactDigest: payload.Meta["artifact_sha256"],
		SpecDigest:     payload.Meta["spec_sha256"],
		Version:        payload.Version,
		Stopped:        payload.Stop,
	}, nil
}

type submitResult struct {
	EvalID         string `json:"EvalID"`
	JobModifyIndex int64  `json:"JobModifyIndex"`
}

func submit(ctx context.Context, nomadAddr string, spec map[string]any) (submitResult, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return submitResult{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, nomadAddr+"/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return submitResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		return submitResult{}, fmt.Errorf("POST /v1/jobs: %d %s", resp.StatusCode, out)
	}
	var out submitResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return submitResult{}, err
	}
	return out, nil
}

// awaitHealthy polls the latest deployment for the job. For service-type
// jobs Nomad creates a deployment per submission; we wait for that
// deployment to either reach Status="successful" or fail.
//
// The poller also fail-fasts on a wedged deployment: if
// failFastDeadAllocs distinct allocs in the latest deployment have
// already terminated, no further waiting helps. We extract the failure
// reason from the most recent alloc's TaskStates and surface it.
func awaitHealthy(ctx context.Context, nomadAddr, jobID string, jobModifyIndex int64) error {
	return awaitHealthyWithPoll(ctx, nomadAddr, jobID, jobModifyIndex, pollInterval)
}

func awaitHealthyWithPoll(ctx context.Context, nomadAddr, jobID string, jobModifyIndex int64, pollEvery time.Duration) error {
	deadline := time.NewTimer(0)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			if reason := latestAllocFailureReason(context.Background(), nomadAddr, jobID, jobModifyIndex); reason != "" {
				return fmt.Errorf("await timeout (deployment never became healthy); last alloc failure: %s", reason)
			}
			return ctx.Err()
		case <-deadline.C:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nomadAddr+"/v1/job/"+jobID+"/deployment", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		// Nomad deployment shape: timeouts are nanosecond int64;
		// RequireProgressBy is a nullable RFC3339 string. We only act on
		// Status here, but keep the per-group counts decoded so future
		// progress logging needs no shape change.
		var payload struct {
			ID                string `json:"ID"`
			Status            string `json:"Status"`
			StatusDescription string `json:"StatusDescription"`
			JobVersion        int64  `json:"JobVersion"`
			JobModifyIndex    int64  `json:"JobModifyIndex"`
			TaskGroups        map[string]struct {
				DesiredTotal    int `json:"DesiredTotal"`
				HealthyAllocs   int `json:"HealthyAllocs"`
				UnhealthyAllocs int `json:"UnhealthyAllocs"`
				PlacedAllocs    int `json:"PlacedAllocs"`
			} `json:"TaskGroups"`
		}
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				resp.Body.Close()
				return err
			}
			resp.Body.Close()
			if payload.JobModifyIndex < jobModifyIndex {
				// /job/<id>/deployment can briefly return the prior
				// successful deployment after submit; do not let a stale
				// green deployment satisfy the fresh rollout.
				deadline.Reset(pollEvery)
				continue
			}
			if payload.JobModifyIndex > jobModifyIndex {
				return fmt.Errorf("latest deployment %s belongs to newer job_modify_index=%d; submitted job_modify_index=%d",
					payload.ID, payload.JobModifyIndex, jobModifyIndex)
			}
			switch payload.Status {
			case "successful":
				return nil
			case "failed", "cancelled":
				if reason := latestAllocFailureReason(ctx, nomadAddr, jobID, jobModifyIndex); reason != "" {
					return fmt.Errorf("deployment %s ended with status=%s: %s; last alloc failure: %s",
						payload.ID, payload.Status, payload.StatusDescription, reason)
				}
				return fmt.Errorf("deployment %s ended with status=%s: %s", payload.ID, payload.Status, payload.StatusDescription)
			}
			if dead := countDeadAllocs(ctx, nomadAddr, jobID, jobModifyIndex); dead >= failFastDeadAllocs {
				reason := latestAllocFailureReason(ctx, nomadAddr, jobID, jobModifyIndex)
				return fmt.Errorf("fail-fast: %d allocs already dead; last alloc failure: %s", dead, reason)
			}
		} else if resp.StatusCode == http.StatusNotFound {
			// service-type jobs always create a deployment, but on the
			// very first submit there can be a brief window before the
			// deployment object exists.
			resp.Body.Close()
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("GET /v1/job/%s/deployment: %d %s", jobID, resp.StatusCode, body)
		}
		deadline.Reset(pollEvery)
	}
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
	os.Exit(1)
}

// allocSummary is the slice of /v1/job/<id>/allocations we care about
// for fail-fast detection and error reporting.
type allocSummary struct {
	ID             string `json:"ID"`
	ClientStatus   string `json:"ClientStatus"`
	ModifyIndex    int64  `json:"ModifyIndex"`
	JobVersion     int64  `json:"JobVersion"`
	CreateIndex    int64  `json:"CreateIndex"`
	JobModifyIndex int64  `json:"JobModifyIndex"`
	TaskStates     map[string]struct {
		State  string `json:"State"`
		Failed bool   `json:"Failed"`
		Events []struct {
			Type           string `json:"Type"`
			DisplayMessage string `json:"DisplayMessage"`
			DriverError    string `json:"DriverError"`
			Time           int64  `json:"Time"`
		} `json:"Events"`
	} `json:"TaskStates"`
}

func listAllocs(ctx context.Context, nomadAddr, jobID string) []allocSummary {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nomadAddr+"/v1/job/"+jobID+"/allocations", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var allocs []allocSummary
	if err := json.NewDecoder(resp.Body).Decode(&allocs); err != nil {
		return nil
	}
	return allocs
}

// countDeadAllocs counts allocs from the current submission (matched by
// JobModifyIndex) that have entered a terminal failed state. Allocs from
// prior submissions stick around in /v1/job/<id>/allocations until
// garbage-collected; counting them would fail-fast on the very first
// poll of any re-submit, even when the new spec is healthy.
func countDeadAllocs(ctx context.Context, nomadAddr, jobID string, jobModifyIndex int64) int {
	dead := 0
	for _, a := range listAllocs(ctx, nomadAddr, jobID) {
		if a.JobModifyIndex != jobModifyIndex {
			continue
		}
		if a.ClientStatus == "failed" || a.ClientStatus == "lost" {
			dead++
		}
	}
	return dead
}

// latestAllocFailureReason walks the most recently-modified failed
// alloc's TaskStates and returns the most-informative event message —
// the DriverError when present, the DisplayMessage otherwise. Filters
// to the current submission (matching JobModifyIndex) so a re-submit
// after a prior failure doesn't echo the prior failure's reason.
func latestAllocFailureReason(ctx context.Context, nomadAddr, jobID string, jobModifyIndex int64) string {
	allocs := listAllocs(ctx, nomadAddr, jobID)
	var newest *allocSummary
	for i := range allocs {
		a := &allocs[i]
		if a.JobModifyIndex != jobModifyIndex {
			continue
		}
		if a.ClientStatus != "failed" && a.ClientStatus != "lost" {
			continue
		}
		if newest == nil || a.ModifyIndex > newest.ModifyIndex {
			newest = a
		}
	}
	if newest == nil {
		return ""
	}
	var lastEvent string
	var lastEventTime int64
	for _, ts := range newest.TaskStates {
		for _, ev := range ts.Events {
			if ev.Time < lastEventTime {
				continue
			}
			msg := ev.DriverError
			if msg == "" {
				msg = ev.DisplayMessage
			}
			if msg == "" {
				continue
			}
			lastEventTime = ev.Time
			lastEvent = ev.Type + ": " + msg
		}
	}
	return lastEvent
}

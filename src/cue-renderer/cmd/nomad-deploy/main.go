// Command nomad-deploy submits a rendered Nomad job spec to the local
// Nomad agent's HTTP API and watches the deployment to completion.
//
// It is the only software-deploy primitive on the bare-metal node: the
// substrate Ansible playbook stops at /etc/verself/jobs/<id>.nomad.json
// and /etc/verself/<id>/auth_audience; this binary takes the spec, fills
// in deploy-time-dynamic values (binary sha256, auth audience), submits,
// and polls the deployment until it's healthy or the deadline expires.
//
// Aspect-deploy invokes one nomad-deploy per opted-in component over SSH
// after the substrate playbook returns. Ansible no longer touches Nomad's
// API or the alloc lifecycle.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
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

	authAudienceSentinel = "{{ component_auth_audience }}"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "enumerate" {
		runEnumerate(os.Args[2:])
		return
	}
	runSubmit(os.Args[1:])
}

// runEnumerate prints one TSV row per nomad-supervised component:
//   <component>\t<job_id>\t<bazel_label>\t<output>
//
// Reads .cache/render/<site>/jobs/index.json (written by the Nomad
// renderer) directly. The shell wrapper iterates the output; no Python
// heredocs and no YAML parser needed.
func runEnumerate(args []string) {
	fs := flag.NewFlagSet("enumerate", flag.ExitOnError)
	indexPath := fs.String("index", "", "rendered jobs index (e.g. .cache/render/prod/jobs/index.json)")
	fs.Parse(args)
	if *indexPath == "" {
		fail("enumerate: --index is required")
	}
	rows, err := enumerate(*indexPath)
	if err != nil {
		fail(err.Error())
	}
	w := bufio.NewWriter(os.Stdout)
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Component, r.JobID, r.BazelLabel, r.Output)
	}
	w.Flush()
}

func runSubmit(args []string) {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	component := fs.String("component", "", "component name (snake_case)")
	specPath := fs.String("spec", "", "rendered job spec path; default /etc/verself/jobs/<id>.nomad.json")
	binaryPath := fs.String("binary", "", "service binary path; default /opt/verself/profile/bin/<id>")
	audPath := fs.String("auth-audience-file", "", "auth audience file; default /etc/verself/<id>/auth_audience")
	nomadAddr := fs.String("nomad-addr", defaultNomadAddr, "Nomad agent HTTP address")
	timeout := fs.Duration("timeout", defaultTimeout, "deployment poll timeout")
	fs.Parse(args)

	if *component == "" {
		fail("--component is required")
	}
	jobID := strings.ReplaceAll(*component, "_", "-")
	if *specPath == "" {
		*specPath = "/etc/verself/jobs/" + jobID + ".nomad.json"
	}
	if *binaryPath == "" {
		*binaryPath = "/opt/verself/profile/bin/" + jobID
	}
	if *audPath == "" {
		*audPath = "/etc/verself/" + jobID + "/auth_audience"
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := run(ctx, runArgs{
		jobID:      jobID,
		specPath:   *specPath,
		binaryPath: *binaryPath,
		audPath:    *audPath,
		nomadAddr:  *nomadAddr,
	}); err != nil {
		fail(err.Error())
	}
}

type runArgs struct {
	jobID      string
	specPath   string
	binaryPath string
	audPath    string
	nomadAddr  string
}

func run(ctx context.Context, args runArgs) error {
	specBytes, err := os.ReadFile(args.specPath)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}

	digest, err := sha256File(args.binaryPath)
	if err != nil {
		return fmt.Errorf("digest binary: %w", err)
	}

	audience, audErr := os.ReadFile(args.audPath)
	if audErr != nil && !errors.Is(audErr, os.ErrNotExist) {
		return fmt.Errorf("read auth audience: %w", audErr)
	}
	audienceStr := strings.TrimSpace(string(audience))

	var spec map[string]any
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	if err := substituteAuthAudience(spec, audienceStr); err != nil {
		return err
	}
	if err := injectMeta(spec, "binary_sha256", digest); err != nil {
		return err
	}

	specDigest := sha256Bytes(specBytes)

	currentBinaryDigest, currentSpecDigest, currentVersion, err := currentJobMeta(ctx, args.nomadAddr, args.jobID)
	if err != nil {
		return fmt.Errorf("inspect current job: %w", err)
	}
	// Short-circuit only when BOTH the binary digest AND the rendered
	// spec digest match what Nomad already has. Either drift is a real
	// reason to re-submit; the binary digest catches code changes, and
	// the spec digest catches schema/topology changes that didn't move
	// the binary (env vars, ports, resources, supervisor knobs).
	if currentBinaryDigest == digest && currentSpecDigest == specDigest {
		fmt.Fprintf(os.Stdout, "nomad-deploy: %s already at binary_sha256=%s spec_sha256=%s (job version %d); no submit\n",
			args.jobID, digest[:12], specDigest[:12], currentVersion)
		return nil
	}
	if err := injectMeta(spec, "spec_sha256", specDigest); err != nil {
		return err
	}

	deployment, err := submit(ctx, args.nomadAddr, spec)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	fmt.Fprintf(os.Stdout, "nomad-deploy: %s submitted job_modify_index=%d eval_id=%s\n",
		args.jobID, deployment.JobModifyIndex, deployment.EvalID)

	if err := awaitHealthy(ctx, args.nomadAddr, args.jobID, deployment.JobModifyIndex); err != nil {
		return fmt.Errorf("await healthy: %w", err)
	}
	fmt.Fprintf(os.Stdout, "nomad-deploy: %s healthy at binary_sha256=%s\n", args.jobID, digest[:12])
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// substituteAuthAudience walks Tasks > Env and replaces the
// `{{ component_auth_audience }}` sentinel with the concrete audience
// read off disk. The renderer leaves only this sentinel; everything
// else is already a literal value.
func substituteAuthAudience(spec map[string]any, audience string) error {
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		return errors.New("spec.Job: missing or wrong type")
	}
	groups, _ := job["TaskGroups"].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		tasks, _ := group["Tasks"].([]any)
		for _, t := range tasks {
			task, _ := t.(map[string]any)
			env, _ := task["Env"].(map[string]any)
			needsAudience := false
			for _, v := range env {
				if s, ok := v.(string); ok && strings.Contains(s, authAudienceSentinel) {
					needsAudience = true
					break
				}
			}
			if needsAudience && audience == "" {
				return errors.New("spec references {{ component_auth_audience }} but auth-audience-file is missing or empty")
			}
			for k, v := range env {
				if s, ok := v.(string); ok {
					env[k] = strings.ReplaceAll(s, authAudienceSentinel, audience)
				}
			}
		}
	}
	return nil
}

// injectMeta sets a single key on Job.Meta, overwriting any prior value
// at that key. Other Meta keys are preserved so future per-component
// metadata (e.g. provenance, change-id) coexists.
func injectMeta(spec map[string]any, key, value string) error {
	job, ok := spec["Job"].(map[string]any)
	if !ok {
		return errors.New("spec.Job: missing or wrong type")
	}
	meta, _ := job["Meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta[key] = value
	job["Meta"] = meta
	return nil
}

// currentJobMeta returns the (binary_sha256, spec_sha256, Version)
// tuple of the currently-registered job, or ("", "", 0) when the job
// is unknown to the Nomad agent. Both digests live in Job.Meta and are
// the only knobs the deploy short-circuit consults.
func currentJobMeta(ctx context.Context, nomadAddr, jobID string) (string, string, int64, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nomadAddr+"/v1/job/"+jobID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", 0, fmt.Errorf("GET /v1/job/%s: %d %s", jobID, resp.StatusCode, body)
	}
	var payload struct {
		Meta    map[string]string `json:"Meta"`
		Version int64             `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", 0, err
	}
	return payload.Meta["binary_sha256"], payload.Meta["spec_sha256"], payload.Version, nil
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
	deadline := time.NewTimer(0)
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
		deadline.Reset(pollInterval)
	}
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
	os.Exit(1)
}

// enumerateRow is the per-component shape both the Nomad renderer
// (writer) and nomad-deploy enumerate (reader) agree on. JSON tags
// must stay stable — bumping them breaks the shell wrapper.
type enumerateRow struct {
	Component  string `json:"component"`
	JobID      string `json:"job_id"`
	BazelLabel string `json:"bazel_label"`
	Output     string `json:"output"`
}

type jobsIndex struct {
	Components []enumerateRow `json:"components"`
}

func enumerate(indexPath string) ([]enumerateRow, error) {
	body, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read jobs index: %w", err)
	}
	var idx jobsIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parse jobs index: %w", err)
	}
	out := append([]enumerateRow(nil), idx.Components...)
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out, nil
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
